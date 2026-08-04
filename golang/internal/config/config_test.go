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
