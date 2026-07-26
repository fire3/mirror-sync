package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/mirror-sync/types"
)

// DefaultUserAgent is the default user-agent for HTTP requests.
const DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

type progressTracker struct {
	mu            sync.Mutex
	downloaded    int
	failed        int
	total         int
	active        map[string]struct{}
	lastEmitDl    int
	lastEmitFail  int
	lastEmitAt    int64
	onProgress    func(downloaded, failed, total int, active []string)
}

func newProgressTracker(total int, onProgress func(int, int, int, []string)) *progressTracker {
	return &progressTracker{
		total:      total,
		active:     make(map[string]struct{}),
		lastEmitDl: -1,
		lastEmitAt: time.Now().UnixMilli(),
		onProgress: onProgress,
	}
}

func (pt *progressTracker) addActive(key string) {
	pt.mu.Lock()
	pt.active[key] = struct{}{}
	pt.mu.Unlock()
	pt.emit()
}

func (pt *progressTracker) removeActive(key string) {
	pt.mu.Lock()
	delete(pt.active, key)
	pt.mu.Unlock()
	pt.emit()
}

func (pt *progressTracker) incDownloaded() {
	pt.mu.Lock()
	pt.downloaded++
	pt.mu.Unlock()
	pt.emit()
}

func (pt *progressTracker) incFailed() {
	pt.mu.Lock()
	pt.failed++
	pt.mu.Unlock()
	pt.emit()
}

func (pt *progressTracker) emit() {
	if pt.onProgress == nil {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()

	now := time.Now().UnixMilli()
	shouldEmit := pt.downloaded == pt.total ||
		(pt.downloaded == 0 && pt.failed == 0 && pt.lastEmitDl < 0) ||
		pt.downloaded-pt.lastEmitDl >= 50 ||
		pt.failed != pt.lastEmitFail ||
		now-pt.lastEmitAt >= 250

	if shouldEmit {
		pt.lastEmitDl = pt.downloaded
		pt.lastEmitFail = pt.failed
		pt.lastEmitAt = now
		activeList := make([]string, 0, len(pt.active))
		for k := range pt.active {
			activeList = append(activeList, k)
		}
		go pt.onProgress(pt.downloaded, pt.failed, pt.total, activeList)
	}
}

// ExecuteDownloadPlan executes all entries in a download plan with concurrency control.
func ExecuteDownloadPlan(plan types.DownloadPlan, opts types.DownloaderOptions) types.DownloadSummary {
	total := len(plan.Entries)
	var failedMu sync.Mutex
	var failed []types.DownloadFailedEntry

	pt := newProgressTracker(total, opts.OnProgress)

	var wg sync.WaitGroup
	workCh := make(chan int, total)

	// Worker goroutines
	for range opts.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{
				Timeout: time.Duration(opts.TimeoutMs) * time.Millisecond,
			}
			for idx := range workCh {
				entry := plan.Entries[idx]

				if opts.TaskController != nil {
					if err := opts.TaskController.Check(); err != nil {
						return
					}
				}

				downloadKey := entry.Package + "/" + entry.Filename
				pt.addActive(downloadKey)

				// Check if file already exists (size > 0)
				if fi, err := os.Stat(entry.DestinationPath); err == nil && fi.Size() > 0 {
					pt.removeActive(downloadKey)
					pt.incDownloaded()
					continue
				}

				err := fetchWithRetry(client, entry, opts)
				pt.removeActive(downloadKey)

				if err != nil {
					failedMu.Lock()
					failed = append(failed, types.DownloadFailedEntry{
						Entry: entry,
						Error: err.Error(),
					})
					failedMu.Unlock()
					pt.incFailed()
				} else {
					pt.incDownloaded()
				}
			}
		}()
	}

	// Send work
	for i := range total {
		workCh <- i
	}
	close(workCh)
	wg.Wait()

	return types.DownloadSummary{
		Attempted:  total,
		Downloaded: pt.downloaded,
		Skipped:    len(plan.SkippedCheckpoint) + len(plan.SkippedExisting) + len(plan.SkippedNotFound),
		Failed:     failed,
	}
}

func fetchWithRetry(client *http.Client, entry types.DownloadPlanEntry, opts types.DownloaderOptions) error {
	var lastErr error
	for attempt := 0; attempt <= opts.Retry; attempt++ {
		if err := downloadEntry(client, entry); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("after %d retries: %w", opts.Retry, lastErr)
}

func downloadEntry(client *http.Client, entry types.DownloadPlanEntry) error {
	destDir := filepath.Dir(entry.DestinationPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tempPath := entry.DestinationPath + ".tmp"

	req, err := http.NewRequest("GET", entry.URL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	ua := DefaultUserAgent
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}

	if err := os.Rename(tempPath, entry.DestinationPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	if entry.Hash != nil {
		if err := verifyHash(entry.DestinationPath, *entry.Hash); err != nil {
			return err
		}
	}

	return nil
}

func verifyHash(path, hashStr string) error {
	parts := strings.SplitN(hashStr, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("unsupported hash format: %s", hashStr)
	}
	expected := parts[1]

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read for hash: %w", err)
	}
	h := sha256.Sum256(data)
	actual := hex.EncodeToString(h[:])
	if actual != expected {
		return fmt.Errorf("hash mismatch for %s", path)
	}
	return nil
}
