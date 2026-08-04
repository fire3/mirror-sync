package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

// errNotFound marks a 404 response. It is returned without retries and is
// recorded separately from network failures so the caller can classify it.
var errNotFound = errors.New("not found")

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
				// No total request timeout: the body read is guarded by an idle
				// (no-progress) timeout inside downloadEntry, so large files can
				// download for as long as data keeps flowing. Only the time to
				// reach the response headers is bounded.
				Transport: &http.Transport{
					// ForceAttemptHTTP2 is needed so resumable Range requests
					// work against servers that reject Range over HTTP/1.1.
					ForceAttemptHTTP2:     true,
					ResponseHeaderTimeout: time.Duration(opts.TimeoutMs) * time.Millisecond,
				},
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
						Entry:    entry,
						Error:    err.Error(),
						NotFound: errors.Is(err, errNotFound),
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
		if attempt > 0 {
			// Brief backoff between retries so the upstream can recover and we
			// do not hammer it under concurrency.
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		if err := downloadEntry(client, entry, time.Duration(opts.TimeoutMs)*time.Millisecond); err != nil {
			if errors.Is(err, errNotFound) {
				// 404 is deterministic — retrying will not help.
				return errNotFound
			}
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("after %d retries: %w", opts.Retry, lastErr)
}

// downloadEntry streams a file to a temp location (with resume support) and
// atomically renames it into place on success.
//
// Reliability notes:
//   - The body is streamed via io.Copy instead of being buffered in memory,
//     so large files do not exhaust RAM.
//   - A pre-existing partial .tmp file is resumed with an HTTP Range request,
//     so a killed/interrupted download continues where it left off instead of
//     restarting from zero.
//   - An idle (no-progress) timeout guards the body read: if no data arrives
//     within the timeout the download fails (and can be resumed later), but a
//     slow-but-flowing download is never cut off by a fixed total deadline.
func downloadEntry(client *http.Client, entry types.DownloadPlanEntry, idleTimeout time.Duration) error {
	destDir := filepath.Dir(entry.DestinationPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tempPath := entry.DestinationPath + ".tmp"

	// Resume from a partially downloaded temp file if present.
	var resumeOffset int64
	if fi, err := os.Stat(tempPath); err == nil && fi.Size() > 0 {
		resumeOffset = fi.Size()
	}

	// Loop so that a server which refuses Range (403/416) or ignores it (200)
	// falls back to a full download from scratch at most once.
	var resp *http.Response
	for {
		req, err := http.NewRequest("GET", entry.URL, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		ua := DefaultUserAgent
		req.Header.Set("User-Agent", ua)
		if resumeOffset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
		}

		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			if resumeOffset > 0 {
				// Server ignored our Range request — restart from scratch.
				resp.Body.Close()
				if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove stale temp: %w", err)
				}
				resumeOffset = 0
				continue
			}
		case resp.StatusCode == http.StatusPartialContent:
			// Append to the partial file — but first verify the server resumed
			// at the offset we asked for; a mismatched start would corrupt it.
			if resumeOffset > 0 {
				start, ok := parseContentRangeStart(resp.Header.Get("Content-Range"))
				if !ok || start != resumeOffset {
					resp.Body.Close()
					if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("remove stale temp: %w", err)
					}
					resumeOffset = 0
					continue // fall back to a full download
				}
			}
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			return errNotFound
		case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable) && resumeOffset > 0:
			// Server does not support Range; fall back to a full download.
			resp.Body.Close()
			if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale temp: %w", err)
			}
			resumeOffset = 0
			continue
		default:
			code := resp.StatusCode
			resp.Body.Close()
			return fmt.Errorf("unexpected response %d", code)
		}
		break
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}
	if _, err := f.Seek(resumeOffset, io.SeekStart); err != nil {
		f.Close()
		return fmt.Errorf("seek temp: %w", err)
	}

	body := &idleTimeoutReader{body: resp.Body, timeout: idleTimeout}
	_, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("write temp: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close temp: %w", closeErr)
	}

	if entry.Hash != nil {
		if err := verifyHash(tempPath, *entry.Hash); err != nil {
			// Do not leave a corrupt file in the mirror: remove the temp so a
			// later run re-downloads it (a size>0 check elsewhere would
			// otherwise treat a bad file as already downloaded).
			os.Remove(tempPath)
			return err
		}
	}

	if err := os.Rename(tempPath, entry.DestinationPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// parseContentRangeStart extracts the first byte position from a
// "bytes START-END/TOTAL" Content-Range header.
func parseContentRangeStart(v string) (int64, bool) {
	const prefix = "bytes "
	if !strings.HasPrefix(v, prefix) {
		return 0, false
	}
	rest := v[len(prefix):]
	dash := strings.IndexByte(rest, '-')
	if dash < 0 {
		return 0, false
	}
	start, err := strconv.ParseInt(rest[:dash], 10, 64)
	if err != nil {
		return 0, false
	}
	return start, true
}

// idleTimeoutReader wraps an http response body so a read that makes no
// progress for `timeout` fails (closing the body to unblock the reader)
// instead of letting the whole request hit a fixed total deadline.
//
// The one-goroutine-per-read design is a deliberate simplicity tradeoff: each
// read is a network round-trip (milliseconds), so the goroutine spawn cost is
// negligible compared to the timeout granularity it provides.
type idleTimeoutReader struct {
	body    io.ReadCloser
	timeout time.Duration
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := r.body.Read(p)
		ch <- readResult{n, err}
	}()
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-timer.C:
		// Close the underlying body to unblock the pending Read.
		r.body.Close()
		<-ch
		return 0, fmt.Errorf("no progress for %s", r.timeout)
	}
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
