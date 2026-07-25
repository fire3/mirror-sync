export type ArtifactSource = 'html' | 'json';

export interface PackageRecord {
  name: string;
  snapshotId: string;
  htmlPresent: boolean;
  jsonPresent: boolean;
  artifactCount: number;
}

export interface ArtifactRecord {
  package: string;
  filename: string;
  relativePath: string;
  url: string;
  hash?: string | undefined;
  requiresPython?: string | undefined;
  yanked?: boolean | string | undefined;
  uploadTime?: string | undefined;
  source: ArtifactSource;
  snapshotId: string;
}

export interface SnapshotStats {
  packagesTotal: number;
  packagesWithHtml: number;
  packagesWithJson: number;
  artifactsTotal: number;
}

export interface SnapshotManifest {
  snapshotId: string;
  packages: PackageRecord[];
  artifacts: ArtifactRecord[];
  stats: SnapshotStats;
}

export interface SnapshotDiff {
  added: ArtifactRecord[];
  changed: Array<{previous: ArtifactRecord; current: ArtifactRecord}>;
  removed: ArtifactRecord[];
  unchanged: ArtifactRecord[];
}

export interface PypiFilterOptions {
  includeSource: boolean;
  includePlatformAny: boolean;
  includeLinuxAmd64: boolean;
  includeWindowsAmd64: boolean;
  excludeMusllinux: boolean;
  excludeMacos: boolean;
  excludeArm: boolean;
  includePackages?: string[] | undefined;
  excludePackages?: string[] | undefined;
}

export interface DownloadPlanEntry {
  package: string;
  filename: string;
  relativePath: string;
  destinationPath: string;
  url: string;
  hash?: string | undefined;
  reason: 'added' | 'changed' | 'full-sync';
}

export interface DownloadPlan {
  entries: DownloadPlanEntry[];
  skippedExisting: string[];
  skippedCheckpoint: string[];
  skippedNotFound: string[];
}

export interface DownloaderOptions {
  concurrency: number;
  retry: number;
  timeoutMs: number;
  userAgent?: string | undefined;
}

export interface DownloadSummary {
  attempted: number;
  downloaded: number;
  skipped: number;
  failed: Array<{entry: DownloadPlanEntry; error: string}>;
}

export type ProviderType = 'pypi';
export type PypiTaskType = 'metadata-sync' | 'artifact-download' | 'incremental-download';

export interface BaseAppConfig {
  profileName: string;
  provider: ProviderType;
  simpleUrl: string;
  metadataRoot: string;
  mirrorRoot: string;
  concurrency: number;
  retry: number;
  timeoutMs: number;
}

export interface MetadataSyncTaskConfig {
  snapshotDate: string;
}

export interface ArtifactDownloadTaskConfig {
  metadataDate: string;
  outputDate: string;
}

export interface IncrementalDownloadTaskConfig {
  oldMetadataDate: string;
  newMetadataDate: string;
  outputDate: string;
}

export interface PypiTaskConfigs {
  metadataSync: MetadataSyncTaskConfig;
  artifactDownload: ArtifactDownloadTaskConfig;
  incrementalDownload: IncrementalDownloadTaskConfig;
}

export interface AppConfig {
  base: BaseAppConfig;
  selectedTask: PypiTaskType;
  pypi: PypiTaskConfigs;
}

export interface TaskExecutionContext {
  provider: ProviderType;
  taskType: PypiTaskType;
}

export interface SyncRunResult {
  provider: ProviderType;
  taskType: PypiTaskType;
  snapshotId?: string | undefined;
  snapshotRoot?: string | undefined;
  packageCount?: number | undefined;
  manifest?: SnapshotManifest | undefined;
  plan?: DownloadPlan | undefined;
  diff?: SnapshotDiff | undefined;
  downloadSummary?: DownloadSummary | undefined;
  outputRoot?: string | undefined;
}
