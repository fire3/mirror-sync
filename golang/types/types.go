package types

// ArtifactSource represents the source of artifact metadata.
type ArtifactSource string

const (
	ArtifactSourceHTML ArtifactSource = "html"
	ArtifactSourceJSON ArtifactSource = "json"
)

// PackageRecord describes a package entry in a snapshot.
type PackageRecord struct {
	Name         string `json:"name"`
	SnapshotID   string `json:"snapshotId"`
	HTMLPresent  bool   `json:"htmlPresent"`
	JSONPresent  bool   `json:"jsonPresent"`
	ArtifactCount int   `json:"artifactCount"`
}

// ArtifactRecord describes a single artifact file in a snapshot.
type ArtifactRecord struct {
	Package        string         `json:"package"`
	Filename       string         `json:"filename"`
	RelativePath   string         `json:"relativePath"`
	URL            string         `json:"url"`
	Hash           *string        `json:"hash,omitempty"`
	RequiresPython *string        `json:"requiresPython,omitempty"`
	Yanked         interface{}    `json:"yanked,omitempty"`
	UploadTime     *string        `json:"uploadTime,omitempty"`
	Source         ArtifactSource `json:"source"`
	SnapshotID     string         `json:"snapshotId"`
}

// SnapshotStats contains aggregate stats for a snapshot.
type SnapshotStats struct {
	PackagesTotal    int `json:"packagesTotal"`
	PackagesWithHTML int `json:"packagesWithHtml"`
	PackagesWithJSON int `json:"packagesWithJson"`
	ArtifactsTotal   int `json:"artifactsTotal"`
}

// SnapshotManifest is the full manifest of a snapshot.
type SnapshotManifest struct {
	SnapshotID string          `json:"snapshotId"`
	Packages   []PackageRecord `json:"packages"`
	Artifacts  []ArtifactRecord `json:"artifacts"`
	Stats      SnapshotStats   `json:"stats"`
}

// SnapshotDiff represents the diff between two snapshot manifests.
type SnapshotDiff struct {
	Added     []ArtifactRecord             `json:"added"`
	Changed   []ArtifactChange             `json:"changed"`
	Removed   []ArtifactRecord             `json:"removed"`
	Unchanged []ArtifactRecord             `json:"unchanged"`
}

// ArtifactChange represents an artifact that changed between snapshots.
type ArtifactChange struct {
	Previous ArtifactRecord `json:"previous"`
	Current  ArtifactRecord `json:"current"`
}

// PypiFilterOptions controls which artifacts to include.
type PypiFilterOptions struct {
	IncludeSource       bool     `json:"includeSource"`
	IncludePlatformAny  bool     `json:"includePlatformAny"`
	IncludeLinuxAmd64   bool     `json:"includeLinuxAmd64"`
	IncludeWindowsAmd64 bool     `json:"includeWindowsAmd64"`
	ExcludeMusllinux    bool     `json:"excludeMusllinux"`
	ExcludeMacos        bool     `json:"excludeMacos"`
	ExcludeArm          bool     `json:"excludeArm"`
	IncludePackages     []string `json:"includePackages,omitempty"`
	ExcludePackages     []string `json:"excludePackages,omitempty"`
}

// PackageArtifactGroup groups artifact records by package name.
// Used for per-package checkpoint and download processing.
type PackageArtifactGroup struct {
	Package   string           `json:"package"`
	Artifacts []ArtifactRecord `json:"artifacts"`
}

// DownloadPlanEntry is a single entry in a download plan.
type DownloadPlanEntry struct {
	Package         string `json:"package"`
	Filename        string `json:"filename"`
	RelativePath    string `json:"relativePath"`
	DestinationPath string `json:"destinationPath"`
	URL             string `json:"url"`
	Hash            *string `json:"hash,omitempty"`
	Reason          string `json:"reason"`
}

// DownloadPlan is the complete download plan.
type DownloadPlan struct {
	Entries            []DownloadPlanEntry `json:"entries"`
	SkippedExisting    []string            `json:"skippedExisting"`
	SkippedCheckpoint  []string            `json:"skippedCheckpoint"`
	SkippedNotFound    []string            `json:"skippedNotFound"`
}

// Canceller is an interface for pause/resume/cancel support.
// Implemented by taskctrl.Controller.
type Canceller interface {
	Check() error
	IsPaused() bool
	IsCancelled() bool
}

// DownloaderOptions configures the downloader.
type DownloaderOptions struct {
	Concurrency    int
	Retry          int
	TimeoutMs      int
	UserAgent      string
	TaskController Canceller
	OnProgress     func(downloaded, failed, total int, active []string)
}

// DownloadFailedEntry records a failed download.
type DownloadFailedEntry struct {
	Entry DownloadPlanEntry `json:"entry"`
	Error string            `json:"error"`
	// NotFound is true when the failure was a deterministic 404 (the file
	// does not exist upstream); such failures are not retried.
	NotFound bool `json:"notFound,omitempty"`
}

// DownloadSummary is the result of executing a download plan.
type DownloadSummary struct {
	Attempted  int                  `json:"attempted"`
	Downloaded int                  `json:"downloaded"`
	Skipped    int                  `json:"skipped"`
	Failed     []DownloadFailedEntry `json:"failed"`
}

// ProviderType is the type of provider.
type ProviderType string

const ProviderTypePyPI ProviderType = "pypi"

// PypiTaskType is the type of PyPI task.
type PypiTaskType string

const (
	PypiTaskMetadataSync       PypiTaskType = "metadata-sync"
	PypiTaskArtifactDownload   PypiTaskType = "artifact-download"
	PypiTaskIncrementalDownload PypiTaskType = "incremental-download"
)

// BaseAppConfig contains shared configuration fields.
type BaseAppConfig struct {
	ProfileName  string `json:"profileName"`
	Provider     ProviderType `json:"provider"`
	SimpleURL    string `json:"simpleUrl"`
	MetadataRoot string `json:"metadataRoot"`
	MirrorRoot   string `json:"mirrorRoot"`
	Concurrency  int    `json:"concurrency"`
	Retry        int    `json:"retry"`
	TimeoutMs    int    `json:"timeoutMs"`
}

// MetadataSyncTaskConfig configures a metadata sync task.
type MetadataSyncTaskConfig struct {
	// SnapshotDate is only used in-memory; the config file always omits it.
	SnapshotDate string `json:"-"`
}

// ArtifactDownloadTaskConfig configures an artifact download task.
type ArtifactDownloadTaskConfig struct {
	MetadataDate string `json:"-"`
	// OutputDir is the mirror output directory name (e.g. "pypi-2025-07-25").
	// Defaults to the same name as the source snapshot directory.
	OutputDir string `json:"-"`
}

// IncrementalDownloadTaskConfig configures an incremental download task.
type IncrementalDownloadTaskConfig struct {
	OldMetadataDate string `json:"-"`
	NewMetadataDate string `json:"-"`
	OutputDate      string `json:"-"`
}

// PypiTaskConfigs holds all PyPI task configs.
type PypiTaskConfigs struct {
	MetadataSync       MetadataSyncTaskConfig       `json:"metadataSync"`
	ArtifactDownload   ArtifactDownloadTaskConfig   `json:"artifactDownload"`
	IncrementalDownload IncrementalDownloadTaskConfig `json:"incrementalDownload"`
}

// AppConfig is the full application configuration.
type AppConfig struct {
	Base         BaseAppConfig    `json:"base"`
	SelectedTask PypiTaskType     `json:"selectedTask"`
	PyPI         PypiTaskConfigs  `json:"pypi"`
}

// SyncRunResult holds the result of a sync run.
type SyncRunResult struct {
	Provider       ProviderType       `json:"provider"`
	TaskType       PypiTaskType       `json:"taskType"`
	SnapshotID     *string            `json:"snapshotId,omitempty"`
	SnapshotRoot   *string            `json:"snapshotRoot,omitempty"`
	PackageCount   *int               `json:"packageCount,omitempty"`
	Manifest       *SnapshotManifest  `json:"manifest,omitempty"`
	Plan           *DownloadPlan      `json:"plan,omitempty"`
	Diff           *SnapshotDiff      `json:"diff,omitempty"`
	DownloadSummary *DownloadSummary  `json:"downloadSummary,omitempty"`
	OutputRoot     *string            `json:"outputRoot,omitempty"`
}
