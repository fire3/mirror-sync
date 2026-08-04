package downloader

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PackageCheckpoint records a fully-downloaded package.
type PackageCheckpoint struct {
	Package     string    `json:"package"`
	CompletedAt time.Time `json:"completedAt"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
}

// CheckpointStore manages per-package download checkpoint persistence.
// It tracks which packages have been fully downloaded so that on restart
// they can be skipped entirely.
//
// Atomic unit = one package: either all its filtered artifacts are
// downloaded, or none are marked completed.
type CheckpointStore struct {
	mu        sync.Mutex
	path      string
	completed map[string]struct{} // set of completed package names
	file      *os.File
	buf       *bufio.Writer
	closed    bool
}

// LoadCheckpoint opens or creates a checkpoint file and loads
// all previously completed packages into memory.
func LoadCheckpoint(path string) (*CheckpointStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	completed := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var cp PackageCheckpoint
		if err := json.Unmarshal(line, &cp); err != nil {
			continue // skip corrupt lines
		}
		completed[cp.Package] = struct{}{}
	}

	// Seek to end for appending
	if _, err := f.Seek(0, 2); err != nil {
		f.Close()
		return nil, err
	}

	return &CheckpointStore{
		path:      path,
		completed: completed,
		file:      f,
		buf:       bufio.NewWriterSize(f, 16*1024), // 16KB buffer
	}, nil
}

// IsCompleted returns true if the package has been fully downloaded.
func (cs *CheckpointStore) IsCompleted(pkgName string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	_, ok := cs.completed[pkgName]
	return ok
}

// CompletePackage marks a package as fully downloaded by appending
// one line to the checkpoint file.
func (cs *CheckpointStore) CompletePackage(pkg PackageCheckpoint) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.closed {
		return nil
	}

	data, err := json.Marshal(pkg)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if _, err := cs.buf.Write(data); err != nil {
		return err
	}

	// Flush immediately: the process may be killed or the TUI may quit at any
	// time (defer Close() may never run), so a completed package must survive
	// a process kill right away for resume-after-restart to work. Note this is
	// a buf flush, not fsync — it does not guard against OS crash / power loss.
	if err := cs.buf.Flush(); err != nil {
		return err
	}

	cs.completed[pkg.Package] = struct{}{}
	return nil
}

// Count returns the number of completed packages.
func (cs *CheckpointStore) Count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.completed)
}

// Flush writes any buffered checkpoint data to disk.
func (cs *CheckpointStore) Flush() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.closed {
		return nil
	}
	return cs.buf.Flush()
}

// Close flushes and closes the checkpoint file.
func (cs *CheckpointStore) Close() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.closed {
		return nil
	}
	cs.closed = true
	if err := cs.buf.Flush(); err != nil {
		cs.file.Close()
		return err
	}
	return cs.file.Close()
}
