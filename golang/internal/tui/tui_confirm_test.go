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

func TestViewConfirmShowsIncrementalOutputRoot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SelectedTask = types.PypiTaskIncrementalDownload
	cfg.PyPI.IncrementalDownload.OldMetadataDate = "2025-07-24"
	cfg.PyPI.IncrementalDownload.NewMetadataDate = "2025-07-25"
	cfg.PyPI.IncrementalDownload.OutputDate = "2025-07-25"

	m := model{cfg: cfg, taskIdx: 2} // taskIdx 2 = 增量下载
	out := m.viewConfirm(80)

	for _, want := range []string{
		"Metadata Root",
		"Mirror Root",
		"Output Root",
		"pypi-2025-07-25",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("viewConfirm 输出缺少 %q\n输出:\n%s", want, out)
		}
	}
}
