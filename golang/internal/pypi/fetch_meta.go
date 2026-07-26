package pypi

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/mirror-sync/types"
)

// FetchPackageMetadataOptions configures metadata fetching.
type FetchPackageMetadataOptions struct {
	SimpleURL    string
	SnapshotRoot string
	PackageNames []string
	Concurrency  int
	UserAgent    string
	TaskController types.Canceller
	OnProgress   func(current, total int, active []string)
}

// FetchPackageMetadataSummary is the result of metadata fetching.
type FetchPackageMetadataSummary struct {
	PackagesTotal int
	HTMLSuccess   int
	JSONSuccess   int
	Failures      []MetadataFailure
}

// MetadataFailure records a failure for a single package.
type MetadataFailure struct {
	Package string
	Stage   string // "html" or "json"
	Error   string
}

func packageURL(simpleURL, packageName string) string {
	normalized := simpleURL
	if !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}
	u, _ := url.JoinPath(normalized, url.PathEscape(packageName)+"/")
	return u
}

// FetchPackageMetadata downloads HTML and JSON metadata for each package.
func FetchPackageMetadata(opts FetchPackageMetadataOptions) FetchPackageMetadataSummary {
	total := len(opts.PackageNames)
	var (
		mu          sync.Mutex
		htmlSuccess int
		jsonSuccess int
		failures    []MetadataFailure
		processed   int
	)

	pt := newMetaProgressTracker(total, opts.OnProgress)

	var wg sync.WaitGroup
	workCh := make(chan int, total)

	for range opts.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{
				Timeout: 120 * time.Second,
			}

			for idx := range workCh {
				pkgName := opts.PackageNames[idx]

				if opts.TaskController != nil {
					if err := opts.TaskController.Check(); err != nil {
						return
					}
				}

				pt.addActive(pkgName)

				url := packageURL(opts.SimpleURL, pkgName)
				pkgRoot := filepath.Join(opts.SnapshotRoot, "simple", pkgName)
				htmlPath := filepath.Join(pkgRoot, "index.html")
				jsonPath := filepath.Join(pkgRoot, "index_v1.json")

				var localHTML, localJSON bool

				// Fetch HTML
				if ok, err := writeMetadataResponse(client, url, htmlPath, "text/html", opts.UserAgent); err != nil {
					mu.Lock()
					failures = append(failures, MetadataFailure{Package: pkgName, Stage: "html", Error: err.Error()})
					mu.Unlock()
				} else if ok {
					localHTML = true
				}

				// Fetch JSON
				if ok, err := writeMetadataResponse(client, url, jsonPath, "application/vnd.pypi.simple.v1+json", opts.UserAgent); err != nil {
					mu.Lock()
					failures = append(failures, MetadataFailure{Package: pkgName, Stage: "json", Error: err.Error()})
					mu.Unlock()
				} else if ok {
					localJSON = true
				}

				mu.Lock()
				if localHTML {
					htmlSuccess++
				}
				if localJSON {
					jsonSuccess++
				}
				processed++
				mu.Unlock()

				pt.removeActive(pkgName)
			}
		}()
	}

	for i := range total {
		workCh <- i
	}
	close(workCh)
	wg.Wait()

	return FetchPackageMetadataSummary{
		PackagesTotal: total,
		HTMLSuccess:   htmlSuccess,
		JSONSuccess:   jsonSuccess,
		Failures:      failures,
	}
}

func writeMetadataResponse(client *http.Client, urlStr, outputPath, accept, userAgent string) (bool, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", accept)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected response %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read body: %w", err)
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(outputPath, body, 0644); err != nil {
		return false, fmt.Errorf("write: %w", err)
	}

	return true, nil
}

type metaProgressTracker struct {
	mu           sync.Mutex
	active       map[string]struct{}
	processed    int
	lastEmit     int
	lastEmitAt   int64
	onProgress   func(processed, total int, active []string)
	total        int
}

func newMetaProgressTracker(total int, onProgress func(int, int, []string)) *metaProgressTracker {
	return &metaProgressTracker{
		active:     make(map[string]struct{}),
		lastEmitAt: time.Now().UnixMilli(),
		lastEmit:   -1,
		onProgress: onProgress,
		total:      total,
	}
}

func (pt *metaProgressTracker) addActive(name string) {
	pt.mu.Lock()
	pt.active[name] = struct{}{}
	pt.mu.Unlock()
}

func (pt *metaProgressTracker) removeActive(name string) {
	pt.mu.Lock()
	delete(pt.active, name)
	pt.processed++
	pt.mu.Unlock()
	pt.emit()
}

func (pt *metaProgressTracker) emit() {
	if pt.onProgress == nil {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()

	now := time.Now().UnixMilli()
	shouldEmit := pt.processed == pt.total ||
		pt.processed-pt.lastEmit >= 100 ||
		now-pt.lastEmitAt >= 250

	if shouldEmit {
		pt.lastEmit = pt.processed
		pt.lastEmitAt = now
		activeList := make([]string, 0, len(pt.active))
		for k := range pt.active {
			activeList = append(activeList, k)
		}
		go pt.onProgress(pt.processed, pt.total, activeList)
	}
}
