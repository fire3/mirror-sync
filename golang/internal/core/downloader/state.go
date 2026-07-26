package downloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FailedRecord records a single failed download.
type FailedRecord struct {
	Package      string `json:"package"`
	Filename     string `json:"filename"`
	RelativePath string `json:"relativePath"`
	Error        string `json:"error"`
	AttemptedAt  string `json:"attemptedAt"`
}

// NotFoundRecord records a single 404 (not found) artifact.
type NotFoundRecord struct {
	Package      string `json:"package"`
	Filename     string `json:"filename"`
	RelativePath string `json:"relativePath"`
	URL          string `json:"url"`
	AttemptedAt  string `json:"attemptedAt"`
}

// StateStore manages persistence of failed and not-found download records.
// Uses JSONL append for thread-safe concurrent writes.
type StateStore struct {
	mu      sync.Mutex
	dir     string
	closed  bool

	failedFile    *os.File
	failedBuf     *jsonLineWriter
	notFoundFile  *os.File
	notFoundBuf   *jsonLineWriter
}

type jsonLineWriter struct {
	enc *json.Encoder
	f   *os.File
}

func newJSONLineWriter(path string) (*jsonLineWriter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &jsonLineWriter{enc: json.NewEncoder(f), f: f}, nil
}

func (w *jsonLineWriter) write(v interface{}) error {
	return w.enc.Encode(v)
}

func (w *jsonLineWriter) close() error {
	return w.f.Close()
}

// NewStateStore creates or opens the state store at the given directory.
// It opens failed.jsonl and not-found.jsonl for append.
func NewStateStore(dir string) (*StateStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	failedW, err := newJSONLineWriter(filepath.Join(dir, "failed.jsonl"))
	if err != nil {
		return nil, err
	}
	notFoundW, err := newJSONLineWriter(filepath.Join(dir, "not-found.jsonl"))
	if err != nil {
		failedW.close()
		return nil, err
	}

	return &StateStore{
		dir:          dir,
		failedFile:   failedW.f,
		failedBuf:    failedW,
		notFoundFile: notFoundW.f,
		notFoundBuf:  notFoundW,
	}, nil
}

// RecordFailed appends a failed download record.
func (ss *StateStore) RecordFailed(pkg, filename, relativePath, errMsg string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return nil
	}
	return ss.failedBuf.write(FailedRecord{
		Package:      pkg,
		Filename:     filename,
		RelativePath: relativePath,
		Error:        errMsg,
		AttemptedAt:  time.Now().UTC().Format(time.RFC3339),
	})
}

// RecordNotFound appends a 404 download record.
func (ss *StateStore) RecordNotFound(pkg, filename, relativePath, url string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return nil
	}
	return ss.notFoundBuf.write(NotFoundRecord{
		Package:      pkg,
		Filename:     filename,
		RelativePath: relativePath,
		URL:          url,
		AttemptedAt:  time.Now().UTC().Format(time.RFC3339),
	})
}

// Close flushes and closes all state files.
func (ss *StateStore) Close() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return nil
	}
	ss.closed = true
	err1 := ss.failedBuf.close()
	err2 := ss.notFoundBuf.close()
	if err1 != nil {
		return err1
	}
	return err2
}
