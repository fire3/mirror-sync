package config

import (
	"testing"
	"time"

	"github.com/user/mirror-sync/types"
)

func TestDefaultOutputDirFollowsSnapshot(t *testing.T) {
	// Default config: output dir matches the default snapshot directory name.
	def := NormalizeConfig(DefaultConfig())
	today := BuildSnapshotID(time.Now())
	if def.PyPI.ArtifactDownload.OutputDir != "pypi-"+today {
		t.Errorf("DefaultConfig normalized OutputDir = %q, want %q (same as snapshot dir)", def.PyPI.ArtifactDownload.OutputDir, "pypi-"+today)
	}

	// Explicit metadata date: output dir follows the chosen snapshot, not today.
	cfg := types.AppConfig{}
	cfg.PyPI.ArtifactDownload.MetadataDate = "2025-07-25"
	got := NormalizeConfig(cfg)
	if got.PyPI.ArtifactDownload.OutputDir != "pypi-2025-07-25" {
		t.Errorf("NormalizeConfig OutputDir = %q, want %q (same as snapshot dir)", got.PyPI.ArtifactDownload.OutputDir, "pypi-2025-07-25")
	}

	// Custom output dir is kept.
	cfg.PyPI.ArtifactDownload.OutputDir = "pypi-custom"
	got = NormalizeConfig(cfg)
	if got.PyPI.ArtifactDownload.OutputDir != "pypi-custom" {
		t.Errorf("NormalizeConfig OutputDir = %q, want %q", got.PyPI.ArtifactDownload.OutputDir, "pypi-custom")
	}
}

func TestFallbackOutputDir(t *testing.T) {
	tests := []struct {
		name         string
		metadataDate string
		outputDir    string
		want         string
	}{
		{
			name:         "empty output dir defaults to snapshot directory name",
			metadataDate: "2025-07-25",
			outputDir:    "",
			want:         "pypi-2025-07-25",
		},
		{
			name:         "explicit output dir is kept",
			metadataDate: "2025-07-25",
			outputDir:    "pypi-custom",
			want:         "pypi-custom",
		},
		{
			name:         "whitespace output dir treated as empty",
			metadataDate: "2025-07-25",
			outputDir:    "   ",
			want:         "pypi-2025-07-25",
		},
		{
			name:         "ISO timestamp metadata date is normalized to date part",
			metadataDate: "2025-07-25T10:00:00Z",
			outputDir:    "",
			want:         "pypi-2025-07-25",
		},
		{
			name:         "empty metadata date falls back to today",
			metadataDate: "",
			outputDir:    "",
			want:         "pypi-" + BuildSnapshotID(time.Now()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FallbackOutputDir(tt.metadataDate, tt.outputDir)
			if got != tt.want {
				t.Errorf("FallbackOutputDir(%q, %q) = %q, want %q", tt.metadataDate, tt.outputDir, got, tt.want)
			}
		})
	}
}

func TestFallbackIncrementalOutputDir(t *testing.T) {
	tests := []struct {
		name      string
		newDate   string
		oldDate   string
		outputDir string
		want      string
	}{
		{
			name:    "defaults to pypi-diff-new-old",
			newDate: "2025-07-25",
			oldDate: "2025-07-24",
			want:    "pypi-diff-2025-07-25-2025-07-24",
		},
		{
			name:      "explicit output dir is kept",
			newDate:   "2025-07-25",
			oldDate:   "2025-07-24",
			outputDir: "pypi-custom",
			want:      "pypi-custom",
		},
		{
			name:    "empty dates fall back to today",
			want:    "pypi-diff-" + BuildSnapshotID(time.Now()) + "-" + BuildSnapshotID(time.Now()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FallbackIncrementalOutputDir(tt.newDate, tt.oldDate, tt.outputDir)
			if got != tt.want {
				t.Errorf("FallbackIncrementalOutputDir(%q, %q, %q) = %q, want %q", tt.newDate, tt.oldDate, tt.outputDir, got, tt.want)
			}
		})
	}
}

func TestFallbackCleanupRoot(t *testing.T) {
	tests := []struct {
		name        string
		mirrorRoot  string
		oldDate     string
		cleanupRoot string
		want        string
	}{
		{
			name:       "defaults to old-date mirror directory",
			mirrorRoot: "/data/mirror/pypi",
			oldDate:    "2025-07-24",
			want:       "/data/mirror/pypi/pypi-2025-07-24",
		},
		{
			name:        "explicit cleanup root is kept",
			mirrorRoot:  "/data/mirror/pypi",
			oldDate:     "2025-07-24",
			cleanupRoot: "/data/mirror/old",
			want:        "/data/mirror/old",
		},
		{
			name:       "empty old date falls back to today",
			mirrorRoot: "/data/mirror/pypi",
			oldDate:    "",
			want:       "/data/mirror/pypi/pypi-" + BuildSnapshotID(time.Now()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FallbackCleanupRoot(tt.mirrorRoot, tt.oldDate, tt.cleanupRoot)
			if got != tt.want {
				t.Errorf("FallbackCleanupRoot(%q, %q, %q) = %q, want %q", tt.mirrorRoot, tt.oldDate, tt.cleanupRoot, got, tt.want)
			}
		})
	}
}

func TestNormalizeConfigIncrementalDefaults(t *testing.T) {
	cfg := types.AppConfig{}
	cfg.PyPI.IncrementalDownload.OldMetadataDate = "2025-07-24"
	cfg.PyPI.IncrementalDownload.NewMetadataDate = "2025-07-25"
	cfg.Base.MirrorRoot = "/data/mirror/pypi"

	got := NormalizeConfig(cfg)
	if got.PyPI.IncrementalDownload.OutputDir != "pypi-diff-2025-07-25-2025-07-24" {
		t.Errorf("NormalizeConfig OutputDir = %q, want %q", got.PyPI.IncrementalDownload.OutputDir, "pypi-diff-2025-07-25-2025-07-24")
	}
	if got.PyPI.IncrementalDownload.CleanupRoot != "/data/mirror/pypi/pypi-2025-07-24" {
		t.Errorf("NormalizeConfig CleanupRoot = %q, want %q", got.PyPI.IncrementalDownload.CleanupRoot, "/data/mirror/pypi/pypi-2025-07-24")
	}
}
