package tui

import (
	"strings"
	"testing"

	"github.com/user/mirror-sync/internal/config"
	"github.com/user/mirror-sync/types"
)

func TestViewConfirmShowsDirs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SelectedTask = types.PypiTaskArtifactDownload
	cfg.PyPI.ArtifactDownload.MetadataDate = "2025-07-25"
	cfg.PyPI.ArtifactDownload.OutputDir = "pypi-2025-07-25"

	m := model{cfg: cfg, taskIdx: 1} // taskIdx 1 = 按单日快照下载包
	out := m.viewConfirm(200)        // 加宽避免 lipgloss 折行影响断言

	for _, want := range []string{
		"Metadata Root",       // 通用目录信息
		"Mirror Root",         // 通用目录信息
		cfg.Base.MetadataRoot, // 具体路径
		cfg.Base.MirrorRoot,   // 具体路径
		"Snapshot Root",       // 输入快照目录
		"Output Dir",          // 输出目录名
		"Output Root",         // 输出根目录
		cfg.PyPI.ArtifactDownload.OutputDir,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("viewConfirm 输出缺少 %q\n输出:\n%s", want, out)
		}
	}
}

func TestViewDoneShowsIncrementalSummary(t *testing.T) {
	removedPkg, removedArt, skipped := 1, 3, 2
	diff := &types.SnapshotDiff{
		AddedPackages:   []string{"fresh"},
		RemovedPackages: []string{"gone"},
		Added:           []types.ArtifactRecord{{Package: "fresh"}},
		Changed:         []types.ArtifactChange{{}},
		Removed:         []types.ArtifactRecord{{Package: "gone"}},
	}
	cleanupPath := "/data/mirror/pypi/pypi-diff-2025-07-25-2025-07-24/cleanup-2025-07-24-to-2025-07-25.sh"

	m := model{lastResult: &types.SyncRunResult{
		TaskType:                    types.PypiTaskIncrementalDownload,
		Diff:                        diff,
		CleanupScriptPath:           &cleanupPath,
		RemovedPackageCount:         &removedPkg,
		RemovedArtifactCount:        &removedArt,
		RemovedArtifactSkippedCount: &skipped,
	}}
	out := m.viewDone(120)

	for _, want := range []string{
		"Task Complete",
		"Diff:     +1/-1 packages, +1/~1/-1 artifacts",
		"Cleanup:",
		cleanupPath,
		"(未执行)",
		"Cleanup Skipped: 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("viewDone 输出缺少 %q\n输出:\n%s", want, out)
		}
	}
}

func TestViewConfirmShowsIncrementalOutputRoot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SelectedTask = types.PypiTaskIncrementalDownload
	cfg.PyPI.IncrementalDownload.OldMetadataDate = "2025-07-24"
	cfg.PyPI.IncrementalDownload.NewMetadataDate = "2025-07-25"
	cfg.PyPI.IncrementalDownload.OutputDir = "pypi-diff-2025-07-25-2025-07-24"
	cfg.PyPI.IncrementalDownload.CleanupRoot = "/data/mirror/pypi/pypi-2025-07-24"

	m := model{cfg: cfg, taskIdx: 2} // taskIdx 2 = 增量下载
	out := m.viewConfirm(200)

	for _, want := range []string{
		"Metadata Root",
		"Mirror Root",
		"Output Root",
		"pypi-diff-2025-07-25-2025-07-24",
		"Cleanup Root",
		cfg.PyPI.IncrementalDownload.CleanupRoot,
		"请确认该目录是 2025-07-24 的 pypi mirror",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("viewConfirm 输出缺少 %q\n输出:\n%s", want, out)
		}
	}
}
