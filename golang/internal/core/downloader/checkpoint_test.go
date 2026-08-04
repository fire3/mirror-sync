package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCheckpointSurvivesCrash verifies that a completed package is durable on
// disk immediately after CompletePackage, without relying on Close() — i.e. a
// killed process / TUI quit loses at most the in-flight package.
func TestCheckpointSurvivesCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "checkpoint.jsonl")

	cs, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	// Simulate several completed packages, then crash WITHOUT calling Close()
	// (defer Close() never runs when the process is killed).
	for _, pkg := range []string{"numpy", "pandas", "requests"} {
		if err := cs.CompletePackage(PackageCheckpoint{
			Package:     pkg,
			CompletedAt: time.Now().UTC(),
			Files:       3,
			Bytes:       1024,
		}); err != nil {
			t.Fatalf("CompletePackage(%s): %v", pkg, err)
		}
	}

	// "Restart": load the checkpoint file fresh in a new store.
	cs2, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("reload LoadCheckpoint: %v", err)
	}
	defer cs2.Close()

	for _, pkg := range []string{"numpy", "pandas", "requests"} {
		if !cs2.IsCompleted(pkg) {
			t.Errorf("restart: package %q not found in checkpoint — record was lost", pkg)
		}
	}
	if cs2.Count() != 3 {
		t.Errorf("restart: Count() = %d, want 3", cs2.Count())
	}
}

// TestCheckpointIgnoreCorruptLines ensures a partially-written / corrupt line
// does not break resume (the rest of the checkpoint still loads).
func TestCheckpointIgnoreCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.jsonl")
	if err := writeTestLines(path, []string{
		`{"package":"numpy","completedAt":"2026-07-25T10:00:00Z","files":1,"bytes":1}`,
		`{"package":"pandas`, // truncated/corrupt line (simulates kill mid-write)
		`{"package":"requests","completedAt":"2026-07-25T10:01:00Z","files":1,"bytes":1}`,
	}); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cs, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	defer cs.Close()

	if !cs.IsCompleted("numpy") || !cs.IsCompleted("requests") {
		t.Error("valid lines before/after corrupt line should still load")
	}
	if cs.IsCompleted("pandas") {
		t.Error("corrupt line must not be loaded as completed")
	}
	if cs.Count() != 2 {
		t.Errorf("Count() = %d, want 2", cs.Count())
	}
}

// TestCheckpointConcurrentWrites verifies that concurrent CompletePackage
// calls (as done by the download worker pool) lose no records and stay race-free.
func TestCheckpointConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.jsonl")
	cs, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	const workers = 8
	const perWorker = 100
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				pkg := fmt.Sprintf("pkg-%d-%d", w, i)
				if err := cs.CompletePackage(PackageCheckpoint{
					Package:     pkg,
					CompletedAt: time.Now().UTC(),
					Files:       1,
				}); err != nil {
					t.Errorf("CompletePackage(%s): %v", pkg, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	cs.Close()

	want := workers * perWorker
	cs2, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("reload LoadCheckpoint: %v", err)
	}
	defer cs2.Close()
	if cs2.Count() != want {
		t.Errorf("after reload Count() = %d, want %d (lost records)", cs2.Count(), want)
	}
}

func writeTestLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			return err
		}
	}
	return nil
}
