package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/mirror-sync/internal/config"
	"github.com/user/mirror-sync/types"
)

// writeSimplePage writes a PyPI simple index HTML page for a package.
func writeSimplePage(t *testing.T, root, pkg string, artifacts map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "simple", pkg)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var links strings.Builder
	for relPath, content := range artifacts {
		sum := sha256.Sum256([]byte(content))
		fmt.Fprintf(&links, "<a href=\"/%s#sha256=%s\">%s</a>\n", relPath, hex.EncodeToString(sum[:]), filepath.Base(relPath))
	}
	html := fmt.Sprintf(`<!DOCTYPE html><html><head><title>Links for %s</title></head><body><h1>Links for %s</h1>%s</body></html>`,
		pkg, pkg, links.String())
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestRunIncrementalDownloadEndToEnd builds two tiny snapshots, serves the
// packages over an httptest server, and asserts the full flow: diff report,
// incremental download into pypi-diff-*, cleanup script, removal list.
func TestRunIncrementalDownloadEndToEnd(t *testing.T) {
	metaRoot := t.TempDir()
	mirrorRoot := t.TempDir()

	// Artifact contents served by the httptest server.
	files := map[string]string{
		"packages/aa/bb/cc/gone-1.0.tar.gz":               "gone-src",
		"packages/ab/cd/ef/both-1.0.tar.gz":               "both-v1",
		"packages/ab/cd/ef/both-2.0.tar.gz":               "both-v2",
		"packages/ab/cd/ef/both-3.0.tar.gz":               "both-v3",
		"packages/de/f0/12/fresh-1.0-py3-none-any.whl":    "fresh-wheel",
		"packages/de/f0/12/stable-1.0-py3-none-any.whl":   "stable-wheel",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/")
		content, ok := files[rel]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, content)
	}))
	defer server.Close()

	// Old snapshot (2025-07-24): gone, both (v1+v2), stable.
	oldSnap := filepath.Join(metaRoot, "snapshots", "pypi-2025-07-24")
	writeSimplePage(t, oldSnap, "gone", map[string]string{
		"packages/aa/bb/cc/gone-1.0.tar.gz": files["packages/aa/bb/cc/gone-1.0.tar.gz"],
	})
	writeSimplePage(t, oldSnap, "both", map[string]string{
		"packages/ab/cd/ef/both-1.0.tar.gz": files["packages/ab/cd/ef/both-1.0.tar.gz"],
		"packages/ab/cd/ef/both-2.0.tar.gz": files["packages/ab/cd/ef/both-2.0.tar.gz"],
	})
	writeSimplePage(t, oldSnap, "stable", map[string]string{
		"packages/de/f0/12/stable-1.0-py3-none-any.whl": files["packages/de/f0/12/stable-1.0-py3-none-any.whl"],
	})

	// New snapshot (2025-07-25): both (v2+v3), fresh, stable.
	newSnap := filepath.Join(metaRoot, "snapshots", "pypi-2025-07-25")
	writeSimplePage(t, newSnap, "both", map[string]string{
		"packages/ab/cd/ef/both-2.0.tar.gz": files["packages/ab/cd/ef/both-2.0.tar.gz"],
		"packages/ab/cd/ef/both-3.0.tar.gz": files["packages/ab/cd/ef/both-3.0.tar.gz"],
	})
	writeSimplePage(t, newSnap, "fresh", map[string]string{
		"packages/de/f0/12/fresh-1.0-py3-none-any.whl": files["packages/de/f0/12/fresh-1.0-py3-none-any.whl"],
	})
	writeSimplePage(t, newSnap, "stable", map[string]string{
		"packages/de/f0/12/stable-1.0-py3-none-any.whl": files["packages/de/f0/12/stable-1.0-py3-none-any.whl"],
	})

	cfg := types.AppConfig{
		Base: types.BaseAppConfig{
			Provider:     types.ProviderTypePyPI,
			SimpleURL:    server.URL + "/simple/",
			MetadataRoot: metaRoot,
			MirrorRoot:   mirrorRoot,
			Concurrency:  4,
			Retry:        1,
			TimeoutMs:    5000,
		},
		SelectedTask: types.PypiTaskIncrementalDownload,
		PyPI: types.PypiTaskConfigs{
			IncrementalDownload: types.IncrementalDownloadTaskConfig{
				OldMetadataDate: "2025-07-24",
				NewMetadataDate: "2025-07-25",
				CleanupRoot:     filepath.Join(mirrorRoot, "pypi-2025-07-24"),
			},
		},
	}

	var events []string
	result, err := RunSync(RunSyncOptions{
		Config: cfg,
		OnEvent: func(ev SyncEvent) {
			events = append(events, ev.Stage+": "+ev.Message)
		},
	})
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	// ── 1. Diff report ────────────────────────────────────────────────
	if result.DiffReportPath == nil {
		t.Fatal("DiffReportPath 未设置")
	}
	reportData, err := os.ReadFile(*result.DiffReportPath)
	if err != nil {
		t.Fatalf("read diff-report: %v", err)
	}
	report := string(reportData)
	for _, want := range []string{
		`"removedPackages": [` + "\n    \"gone\"",
		`"addedPackages": [` + "\n    \"fresh\"",
		"both-3.0.tar.gz",
		"fresh-1.0-py3-none-any.whl",
		"gone-1.0.tar.gz",
		`"reason": "package-removed"`,
		`"reason": "artifact-removed"`,
	} {
		if !strings.Contains(report, want) {
			t.Errorf("diff-report.json 缺少 %q\nreport:\n%s", want, report)
		}
	}

	// Package-level diff counts.
	if result.RemovedPackageCount == nil || *result.RemovedPackageCount != 1 {
		t.Errorf("RemovedPackageCount = %v, want 1", result.RemovedPackageCount)
	}
	if result.Diff == nil || len(result.Diff.RemovedPackages) != 1 || result.Diff.RemovedPackages[0] != "gone" {
		t.Errorf("Diff.RemovedPackages = %v, want [gone]", result.Diff.RemovedPackages)
	}
	if result.Diff == nil || len(result.Diff.AddedPackages) != 1 || result.Diff.AddedPackages[0] != "fresh" {
		t.Errorf("Diff.AddedPackages = %v, want [fresh]", result.Diff.AddedPackages)
	}

	// ── 2. Downloads into pypi-diff-* with packages/ layout ───────────
	outputRoot := filepath.Join(mirrorRoot, "pypi-diff-2025-07-25-2025-07-24")
	for _, rel := range []string{
		"packages/ab/cd/ef/both-3.0.tar.gz",
		"packages/de/f0/12/fresh-1.0-py3-none-any.whl",
	} {
		got, err := os.ReadFile(filepath.Join(outputRoot, rel))
		if err != nil {
			t.Errorf("新增文件未下载: %s: %v", rel, err)
			continue
		}
		if string(got) != files[rel] {
			t.Errorf("%s 内容 = %q, want %q", rel, got, files[rel])
		}
	}
	// Unchanged (stable, both-2.0) and removed (gone/both-1.0) must NOT be downloaded.
	for _, rel := range []string{
		"packages/de/f0/12/stable-1.0-py3-none-any.whl",
		"packages/ab/cd/ef/both-2.0.tar.gz",
		"packages/aa/bb/cc/gone-1.0.tar.gz",
		"packages/ab/cd/ef/both-1.0.tar.gz",
	} {
		if _, err := os.Stat(filepath.Join(outputRoot, rel)); err == nil {
			t.Errorf("不应下载 %s", rel)
		}
	}

	// ── 3. Cleanup script (generated, not executed) ───────────────────
	if result.CleanupScriptPath == nil {
		t.Fatal("CleanupScriptPath 未设置")
	}
	script, err := os.ReadFile(*result.CleanupScriptPath)
	if err != nil {
		t.Fatalf("read cleanup script: %v", err)
	}
	scriptStr := string(script)
	for _, want := range []string{
		`CLEANUP_ROOT="` + filepath.Join(mirrorRoot, "pypi-2025-07-24") + `"`,
		`rm -f -- "${CLEANUP_ROOT}/packages/aa/bb/cc/gone-1.0.tar.gz"`,
		`rm -f -- "${CLEANUP_ROOT}/packages/ab/cd/ef/both-1.0.tar.gz"`,
		"removed packages",
		"removed artifacts of existing packages",
		"人工审查",
	} {
		if !strings.Contains(scriptStr, want) {
			t.Errorf("cleanup 脚本缺少 %q\nscript:\n%s", want, scriptStr)
		}
	}
	for _, notWant := range []string{
		"fresh-1.0-py3-none-any.whl",
		"both-3.0.tar.gz",
		"both-2.0.tar.gz",
		"stable-1.0-py3-none-any.whl",
		"rm -rf",
	} {
		if strings.Contains(scriptStr, notWant) {
			t.Errorf("cleanup 脚本不应包含 %q", notWant)
		}
	}

	// ── 4. Removed artifacts list (JSONL) ─────────────────────────────
	removedList, err := os.ReadFile(filepath.Join(outputRoot, "removed-artifacts.jsonl"))
	if err != nil {
		t.Fatalf("read removed-artifacts.jsonl: %v", err)
	}
	removedStr := string(removedList)
	if !strings.Contains(removedStr, `"package":"gone"`) || !strings.Contains(removedStr, `"reason":"package-removed"`) {
		t.Errorf("removed-artifacts.jsonl 缺少 gone 记录:\n%s", removedStr)
	}
	if !strings.Contains(removedStr, `"package":"both"`) || !strings.Contains(removedStr, `"reason":"artifact-removed"`) {
		t.Errorf("removed-artifacts.jsonl 缺少 both 记录:\n%s", removedStr)
	}

	// ── 5. run-summary.json + events ──────────────────────────────────
	summary, err := os.ReadFile(filepath.Join(outputRoot, "run-summary.json"))
	if err != nil {
		t.Fatalf("read run-summary.json: %v", err)
	}
	for _, want := range []string{
		"cleanupScriptPath",
		"diffReportPath",
		"removedArtifacts",
	} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("run-summary.json 缺少 %q", want)
		}
	}
	joined := strings.Join(events, "\n")
	for _, want := range []string{
		"Packages +1/-1",
		"Cleanup script written",
		"Downloaded 2/2",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("事件流缺少 %q\n事件:\n%s", want, joined)
		}
	}
}

// TestSanitizeFileName locks in path-separator neutralization for values
// embedded into generated file names.
func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean date", "2025-07-25", "2025-07-25"},
		{"forward slash", "a/b", "a-b"},
		{"backslash", `a\b`, "a-b"},
		{"traversal", "../x", "..-x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFileName(tt.in); got != tt.want {
				t.Errorf("sanitizeFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestRunIncrementalDownloadDefaults verifies config normalization fills the
// derived output dir and cleanup root when the task runs.
func TestRunIncrementalDownloadDefaults(t *testing.T) {
	metaRoot := t.TempDir()
	mirrorRoot := t.TempDir()

	cfg := types.AppConfig{
		Base: types.BaseAppConfig{
			Provider:     types.ProviderTypePyPI,
			SimpleURL:    "https://example.invalid/simple/",
			MetadataRoot: metaRoot,
			MirrorRoot:   mirrorRoot,
			Concurrency:  2,
		},
		SelectedTask: types.PypiTaskIncrementalDownload,
		PyPI: types.PypiTaskConfigs{
			IncrementalDownload: types.IncrementalDownloadTaskConfig{
				OldMetadataDate: "2025-07-24",
				NewMetadataDate: "2025-07-25",
			},
		},
	}
	normalized := config.NormalizeConfig(cfg)
	if got := normalized.PyPI.IncrementalDownload.OutputDir; got != "pypi-diff-2025-07-25-2025-07-24" {
		t.Errorf("OutputDir = %q, want pypi-diff-2025-07-25-2025-07-24", got)
	}
	if got := normalized.PyPI.IncrementalDownload.CleanupRoot; got != filepath.Join(mirrorRoot, "pypi-2025-07-24") {
		t.Errorf("CleanupRoot = %q, want %q", got, filepath.Join(mirrorRoot, "pypi-2025-07-24"))
	}
}
