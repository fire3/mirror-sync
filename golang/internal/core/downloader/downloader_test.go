package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/mirror-sync/types"
)

func testClient() *http.Client {
	return &http.Client{}
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestDownloadEntryLargeFile verifies streaming download of a large file and
// its hash verification.
func TestDownloadEntryLargeFile(t *testing.T) {
	data := make([]byte, 5<<20) // 5 MiB — larger than a typical read buffer
	for i := range data {
		data[i] = byte(i)
	}
	hash := hashOf(data)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "big-file.bin")
	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL, Hash: &hash}
	if err := downloadEntry(testClient(), entry, time.Second); err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("size = %d, want %d", len(got), len(data))
	}
	if hashOf(got) != hash {
		t.Fatal("hash mismatch after download")
	}
}

// TestDownloadEntryNotFound verifies a 404 returns errNotFound without
// leaving any partial file.
func TestDownloadEntryNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "missing.bin")
	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL}
	err := downloadEntry(testClient(), entry, time.Second)
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want errNotFound", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("destination should not exist after 404")
	}
}

// TestDownloadEntryResume verifies that a pre-existing partial .tmp file is
// resumed via a Range request and the final file is complete.
func TestDownloadEntryResume(t *testing.T) {
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			var start int64
			if _, err := fmt.Sscanf(rng, "bytes=%d-", &start); err != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[start:])
			return
		}
		w.Write(data)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "resume.bin")
	tmp := dest + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, data[:10], 0644); err != nil { // first 10 bytes already downloaded
		t.Fatal(err)
	}

	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL}
	if err := downloadEntry(testClient(), entry, time.Second); err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("resumed file = %q, want %q", got, data)
	}
}

// TestDownloadEntryResumeIgnored verifies that when the server ignores Range
// (returns 200), the partial file is discarded and a full download happens.
func TestDownloadEntryResumeIgnored(t *testing.T) {
	data := []byte("full-download-content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data) // always 200, ignores Range
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "ignored.bin")
	tmp := dest + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("stale-partial"), 0644); err != nil {
		t.Fatal(err)
	}

	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL}
	if err := downloadEntry(testClient(), entry, time.Second); err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("file = %q, want %q", got, data)
	}
}

// TestDownloadEntryResumeForbidden verifies that when the server rejects a
// Range request (403), the partial file is discarded and a full download is
// performed instead of failing.
func TestDownloadEntryResumeForbidden(t *testing.T) {
	data := []byte("full-download-content-with-403-fallback")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Write(data)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "fallback.bin")
	tmp := dest + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("stale-partial"), 0644); err != nil {
		t.Fatal(err)
	}

	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL}
	if err := downloadEntry(testClient(), entry, time.Second); err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("file = %q, want %q", got, data)
	}
}

// TestDownloadEntryResumeContentRangeMismatch verifies that a 206 whose
// Content-Range start does not match the requested offset is treated as an
// invalid resume: the partial file is discarded and a full download happens.
func TestDownloadEntryResumeContentRangeMismatch(t *testing.T) {
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// Respond 206 but always starting at offset 5, regardless of the
			// requested offset (misbehaving server).
			w.Header().Set("Content-Range", "bytes 5-15/36")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[5:16])
			return
		}
		w.Write(data)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "mismatch.bin")
	tmp := dest + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, data[:10], 0644); err != nil { // resume would ask bytes=10-
		t.Fatal(err)
	}

	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL}
	if err := downloadEntry(testClient(), entry, time.Second); err != nil {
		t.Fatalf("downloadEntry: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("file = %q, want %q", got, data)
	}
}

// TestDownloadEntryIdleTimeout verifies that a body read that stalls (no
// progress) fails with an idle timeout error instead of hanging forever.
func TestDownloadEntryIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(2 * time.Second) // stall without closing
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg", "stalled.bin")
	entry := types.DownloadPlanEntry{DestinationPath: dest, URL: srv.URL}
	start := time.Now()
	err := downloadEntry(testClient(), entry, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected idle timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "no progress") {
		t.Fatalf("err = %v, want idle timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

// TestFetchWithRetryNotFoundNoRetry verifies 404 fails fast without retries.
func TestFetchWithRetryNotFoundNoRetry(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()

	entry := types.DownloadPlanEntry{DestinationPath: filepath.Join(t.TempDir(), "x.bin"), URL: srv.URL}
	opts := types.DownloaderOptions{Retry: 3, TimeoutMs: 1000}
	err := fetchWithRetry(testClient(), entry, opts)
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want errNotFound", err)
	}
	if hits != 1 {
		t.Fatalf("server hit %d times, want 1 (no retry on 404)", hits)
	}
}
