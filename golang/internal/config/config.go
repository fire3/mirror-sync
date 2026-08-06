package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/user/mirror-sync/types"
)

const (
	DefaultConfigPath = "~/.mirror-sync/config.json"

	DefaultBrowserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
)

// DefaultBaseConfig returns the default base configuration.
func DefaultBaseConfig() types.BaseAppConfig {
	cwd, _ := os.Getwd()
	return types.BaseAppConfig{
		ProfileName:  "pypi-tsinghua",
		Provider:     types.ProviderTypePyPI,
		SimpleURL:    "https://pypi.tuna.tsinghua.edu.cn/simple/",
		MetadataRoot: filepath.Join(cwd, "data", "meta", "pypi"),
		MirrorRoot:   filepath.Join(cwd, "data", "mirror", "pypi"),
		Concurrency:  16,
		Retry:        2,
		TimeoutMs:    60_000,
	}
}

// DefaultConfig returns the default application configuration.
func DefaultConfig() types.AppConfig {
	snapshotID := BuildSnapshotID(time.Now())
	return types.AppConfig{
		Base:         DefaultBaseConfig(),
		SelectedTask: types.PypiTaskMetadataSync,
		PyPI: types.PypiTaskConfigs{
			MetadataSync: types.MetadataSyncTaskConfig{
				SnapshotDate: snapshotID,
			},
			ArtifactDownload: types.ArtifactDownloadTaskConfig{
				MetadataDate: "",
				// OutputDir defaults to the source snapshot directory name
				// (pypi-<metadataDate>); derived in NormalizeConfig, not preset
				// to today so it always matches the chosen snapshot.
				OutputDir: "",
			},
			IncrementalDownload: types.IncrementalDownloadTaskConfig{
				OldMetadataDate: "",
				NewMetadataDate: "",
				// OutputDir / CleanupRoot derive from the dates in
				// NormalizeConfig; not preset here.
				OutputDir:   "",
				CleanupRoot: "",
			},
		},
	}
}

func normalizeURL(url string) string {
	if strings.HasSuffix(url, "/") {
		return url
	}
	return url + "/"
}

func fallbackDate(value string) string {
	if value == "" {
		return BuildSnapshotID(time.Now())
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return BuildSnapshotID(time.Now())
	}
	// If it's an old ISO format (contains T), just take the date part
	if strings.Contains(trimmed, "T") {
		return strings.SplitN(trimmed, "T", 2)[0]
	}
	return trimmed
}

// NormalizeConfig normalizes and validates the configuration.
func NormalizeConfig(cfg types.AppConfig) types.AppConfig {
	defBase := DefaultBaseConfig()

	base := cfg.Base

	trimmedURL := strings.TrimSpace(base.SimpleURL)
	if trimmedURL == "" {
		trimmedURL = defBase.SimpleURL
	}
	trimmedProfile := strings.TrimSpace(base.ProfileName)
	if trimmedProfile == "" {
		trimmedProfile = defBase.ProfileName
	}
	metadataRoot := strings.TrimSpace(base.MetadataRoot)
	if metadataRoot == "" {
		metadataRoot = defBase.MetadataRoot
	}
	metadataRoot, _ = filepath.Abs(metadataRoot)
	mirrorRoot := strings.TrimSpace(base.MirrorRoot)
	if mirrorRoot == "" {
		mirrorRoot = defBase.MirrorRoot
	}
	mirrorRoot, _ = filepath.Abs(mirrorRoot)

	concurrency := base.Concurrency
	if concurrency <= 0 || !isFinite(float64(concurrency)) {
		concurrency = defBase.Concurrency
	}
	retry := base.Retry
	if retry < 0 || !isFinite(float64(retry)) {
		retry = defBase.Retry
	}
	timeoutMs := base.TimeoutMs
	if timeoutMs <= 0 || !isFinite(float64(timeoutMs)) {
		timeoutMs = defBase.TimeoutMs
	}

	return types.AppConfig{
		Base: types.BaseAppConfig{
			ProfileName:  trimmedProfile,
			Provider:     types.ProviderTypePyPI,
			SimpleURL:    normalizeURL(trimmedURL),
			MetadataRoot: metadataRoot,
			MirrorRoot:   mirrorRoot,
			Concurrency:  concurrency,
			Retry:        retry,
			TimeoutMs:    timeoutMs,
		},
		SelectedTask: cfg.SelectedTask,
		PyPI: types.PypiTaskConfigs{
			MetadataSync: types.MetadataSyncTaskConfig{
				SnapshotDate: fallbackDate(cfg.PyPI.MetadataSync.SnapshotDate),
			},
			ArtifactDownload: types.ArtifactDownloadTaskConfig{
				MetadataDate: fallbackDate(cfg.PyPI.ArtifactDownload.MetadataDate),
				// Default output dir matches the snapshot directory name.
				OutputDir: FallbackOutputDir(cfg.PyPI.ArtifactDownload.MetadataDate, cfg.PyPI.ArtifactDownload.OutputDir),
			},
			IncrementalDownload: types.IncrementalDownloadTaskConfig{
				OldMetadataDate: fallbackDate(cfg.PyPI.IncrementalDownload.OldMetadataDate),
				NewMetadataDate: fallbackDate(cfg.PyPI.IncrementalDownload.NewMetadataDate),
				OutputDir: FallbackIncrementalOutputDir(
					cfg.PyPI.IncrementalDownload.NewMetadataDate,
					cfg.PyPI.IncrementalDownload.OldMetadataDate,
					cfg.PyPI.IncrementalDownload.OutputDir),
				CleanupRoot: FallbackCleanupRoot(
					mirrorRoot,
					cfg.PyPI.IncrementalDownload.OldMetadataDate,
					cfg.PyPI.IncrementalDownload.CleanupRoot),
			},
		},
	}
}

// FallbackOutputDir returns the output directory name for artifact download.
// If empty, it defaults to the same name as the source snapshot directory
// (e.g. "pypi-2025-07-25" when the metadata date is 2025-07-25).
func FallbackOutputDir(metadataDate, outputDir string) string {
	if trimmed := strings.TrimSpace(outputDir); trimmed != "" {
		return trimmed
	}
	return "pypi-" + fallbackDate(metadataDate)
}

// FallbackIncrementalOutputDir returns the output directory name for
// incremental download. If empty, it defaults to "pypi-diff-{new}-{old}".
func FallbackIncrementalOutputDir(newDate, oldDate, outputDir string) string {
	if trimmed := strings.TrimSpace(outputDir); trimmed != "" {
		return trimmed
	}
	return "pypi-diff-" + fallbackDate(newDate) + "-" + fallbackDate(oldDate)
}

// FallbackCleanupRoot returns the cleanup script's target root directory.
// If empty, it defaults to the old-date mirror directory. The old date is
// normalized via fallbackDate so an empty value cannot produce a dangling
// "<mirrorRoot>/pypi-" prefix.
func FallbackCleanupRoot(mirrorRoot, oldDate, cleanupRoot string) string {
	if trimmed := strings.TrimSpace(cleanupRoot); trimmed != "" {
		return trimmed
	}
	return BuildMirrorOutputRoot(mirrorRoot, fallbackDate(oldDate))
}

func isFinite(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}

// LoadConfig loads configuration from a JSON file.
func LoadConfig(configPath string) (types.AppConfig, error) {
	if configPath == "" {
		configPath = expandPath(DefaultConfigPath)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return types.AppConfig{}, fmt.Errorf("read config: %w", err)
	}

	var parsed types.AppConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return types.AppConfig{}, fmt.Errorf("parse config: %w", err)
	}

	// Merge with defaults for missing fields
	def := DefaultConfig()
	if parsed.Base.ProfileName == "" {
		parsed.Base = def.Base
	}
	if parsed.PyPI.MetadataSync.SnapshotDate == "" {
		parsed.PyPI.MetadataSync = def.PyPI.MetadataSync
	}
	if parsed.PyPI.ArtifactDownload.OutputDir == "" {
		parsed.PyPI.ArtifactDownload.OutputDir = FallbackOutputDir(parsed.PyPI.ArtifactDownload.MetadataDate, "")
	}
	if parsed.PyPI.IncrementalDownload.OutputDir == "" {
		parsed.PyPI.IncrementalDownload.OutputDir = FallbackIncrementalOutputDir(
			parsed.PyPI.IncrementalDownload.NewMetadataDate,
			parsed.PyPI.IncrementalDownload.OldMetadataDate, "")
	}
	if parsed.PyPI.IncrementalDownload.CleanupRoot == "" {
		parsed.PyPI.IncrementalDownload.CleanupRoot = FallbackCleanupRoot(
			parsed.Base.MirrorRoot,
			parsed.PyPI.IncrementalDownload.OldMetadataDate, "")
	}

	return NormalizeConfig(parsed), nil
}

// SaveConfig saves the configuration to a JSON file.
func SaveConfig(cfg types.AppConfig, configPath string) error {
	if configPath == "" {
		configPath = expandPath(DefaultConfigPath)
	}
	normalized := NormalizeConfig(cfg)
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// PathExists checks if a path exists.
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetLatestManifestPath finds the most recent manifest.json in the snapshots directory.
func GetLatestManifestPath(metadataRoot string) *string {
	snapshotsRoot := filepath.Join(metadataRoot, "snapshots")
	if !PathExists(snapshotsRoot) {
		return nil
	}

	entries, err := os.ReadDir(snapshotsRoot)
	if err != nil {
		return nil
	}

	var snapshotNames []string
	for _, entry := range entries {
		if entry.IsDir() {
			snapshotNames = append(snapshotNames, entry.Name())
		}
	}
	slices.Sort(snapshotNames)
	slices.Reverse(snapshotNames)

	for _, name := range snapshotNames {
		manifestPath := filepath.Join(snapshotsRoot, name, "manifest.json")
		if PathExists(manifestPath) {
			return &manifestPath
		}
	}
	return nil
}

// BuildSnapshotRoot builds the path to a snapshot directory.
func BuildSnapshotRoot(metadataRoot, snapshotID string) string {
	return filepath.Join(metadataRoot, "snapshots", "pypi-"+snapshotID)
}

// BuildMirrorOutputRoot builds the path to a mirror output directory.
func BuildMirrorOutputRoot(mirrorRoot, outputDate string) string {
	return filepath.Join(mirrorRoot, "pypi-"+outputDate)
}

// BuildManifestPath builds the path to a manifest.json file.
func BuildManifestPath(metadataRoot, metadataDate string) string {
	return filepath.Join(BuildSnapshotRoot(metadataRoot, metadataDate), "manifest.json")
}

// BuildPlanPathFromMetadataDate builds the path to a download plan JSON file.
func BuildPlanPathFromMetadataDate(metadataRoot, metadataDate, fileName string) string {
	return filepath.Join(BuildSnapshotRoot(metadataRoot, metadataDate), fileName)
}

// BuildSnapshotID builds a date-based snapshot ID.
func BuildSnapshotID(now time.Time) string {
	return now.Format("2006-01-02")
}

// TaskLabel returns a human-readable label for a task type.
func TaskLabel(taskType types.PypiTaskType) string {
	switch taskType {
	case types.PypiTaskMetadataSync:
		return "下载元数据"
	case types.PypiTaskArtifactDownload:
		return "按单日元数据下载包"
	case types.PypiTaskIncrementalDownload:
		return "按两日元数据增量下载"
	default:
		return string(taskType)
	}
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		return abs
	}
	return path
}
