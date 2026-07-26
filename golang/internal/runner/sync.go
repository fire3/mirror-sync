package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/mirror-sync/internal/config"
	"github.com/user/mirror-sync/internal/core/downloader"
	"github.com/user/mirror-sync/internal/core/planner"
	"github.com/user/mirror-sync/internal/core/storage"
	"github.com/user/mirror-sync/internal/pypi"
	"github.com/user/mirror-sync/types"
)

// SyncEvent is emitted during a sync run.
type SyncEvent struct {
	Stage    string
	Message  string
	Progress *SyncProgress
}

// SyncProgress contains progress information.
type SyncProgress struct {
	Current int
	Total   int
	Failed  int
	Active  []string
}

// RunSyncOptions configures a sync run.
type RunSyncOptions struct {
	Config         types.AppConfig
	OnEvent        func(SyncEvent)
	TaskController types.Canceller
}

func emit(onEvent func(SyncEvent), stage, message string, progress *SyncProgress) {
	if onEvent != nil {
		onEvent(SyncEvent{Stage: stage, Message: message, Progress: progress})
	}
}

func readJSONFile[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read file: %w", err)
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("parse json: %w", err)
	}
	return result, nil
}

func loadManifestByDate(metadataRoot, metadataDate, simpleBaseURL string) (types.SnapshotManifest, error) {
	manifestPath := config.BuildManifestPath(metadataRoot, metadataDate)
	if !config.PathExists(manifestPath) {
		return pypi.BuildManifestFromSnapshot(pypi.BuildManifestOptions{
			SnapshotRoot:  config.BuildSnapshotRoot(metadataRoot, metadataDate),
			SimpleBaseURL: simpleBaseURL,
			WriteOutputs:  false,
		})
	}

	manifest, err := readJSONFile[types.SnapshotManifest](manifestPath)
	if err != nil {
		return types.SnapshotManifest{}, err
	}

	if len(manifest.Packages) > 0 || len(manifest.Artifacts) > 0 {
		return manifest, nil
	}

	snapshotRoot := config.BuildSnapshotRoot(metadataRoot, metadataDate)
	packages, err := storage.ReadJSONLines[types.PackageRecord](filepath.Join(snapshotRoot, "manifests", "packages.jsonl"))
	if err != nil {
		return manifest, nil // Return partial manifest on error
	}
	artifacts, err := storage.ReadJSONLines[types.ArtifactRecord](filepath.Join(snapshotRoot, "manifests", "artifacts.jsonl"))
	if err != nil {
		return manifest, nil
	}

	manifest.Packages = packages
	manifest.Artifacts = artifacts
	return manifest, nil
}

func writePlan(metadataRoot, metadataDate, fileName string, plan types.DownloadPlan) (string, error) {
	outputPath := config.BuildPlanPathFromMetadataDate(metadataRoot, metadataDate, fileName)
	if err := storage.WriteJSONFile(outputPath, plan); err != nil {
		return "", err
	}
	return outputPath, nil
}

func runMetadataSync(cfg types.AppConfig, onEvent func(SyncEvent), tc types.Canceller) (types.SyncRunResult, error) {
	snapshotID := cfg.PyPI.MetadataSync.SnapshotDate
	snapshotRoot := config.BuildSnapshotRoot(cfg.Base.MetadataRoot, snapshotID)
	pkgListPath := filepath.Join(snapshotRoot, "package-list.txt")

	emit(onEvent, "Prepare", fmt.Sprintf("PyPI / %s", config.TaskLabel(types.PypiTaskMetadataSync)), nil)
	emit(onEvent, "Fetch /simple/", fmt.Sprintf("Fetching package list from %s", cfg.Base.SimpleURL), nil)

	pkgNames, err := pypi.FetchAndWritePackageList(cfg.Base.SimpleURL, pkgListPath, config.DefaultBrowserUserAgent)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("fetch package list: %w", err)
	}
	emit(onEvent, "Fetch /simple/", fmt.Sprintf("Fetched %d packages", len(pkgNames)), nil)

	emit(onEvent, "Download Metadata", fmt.Sprintf("Writing snapshot into %s", snapshotRoot), nil)
	summary := pypi.FetchPackageMetadata(pypi.FetchPackageMetadataOptions{
		SimpleURL:      cfg.Base.SimpleURL,
		PackageNames:   pkgNames,
		SnapshotRoot:   snapshotRoot,
		Concurrency:    cfg.Base.Concurrency,
		UserAgent:      config.DefaultBrowserUserAgent,
		TaskController: tc,
		OnProgress: func(current, total int, active []string) {
			emit(onEvent, "Download Metadata", fmt.Sprintf("Downloading package metadata %d/%d", current, total),
				&SyncProgress{Current: current, Total: total, Active: active})
		},
	})
	emit(onEvent, "Download Metadata",
		fmt.Sprintf("HTML %d/%d, JSON %d/%d", summary.HTMLSuccess, summary.PackagesTotal, summary.JSONSuccess, summary.PackagesTotal),
		nil)

	// Write current.json
	currentData := map[string]interface{}{
		"provider":     "pypi",
		"taskType":     "metadata-sync",
		"snapshotId":   snapshotID,
		"snapshotRoot": snapshotRoot,
	}
	if err := storage.WriteJSONFile(filepath.Join(cfg.Base.MetadataRoot, "current.json"), currentData); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write current.json: %w", err)
	}

	emit(onEvent, "Finalize", fmt.Sprintf("Snapshot %s completed", snapshotID), nil)

	pc := len(pkgNames)
	return types.SyncRunResult{
		Provider:     types.ProviderTypePyPI,
		TaskType:     types.PypiTaskMetadataSync,
		SnapshotID:   &snapshotID,
		SnapshotRoot: &snapshotRoot,
		PackageCount: &pc,
	}, nil
}

func runArtifactDownload(cfg types.AppConfig, onEvent func(SyncEvent), tc types.Canceller) (types.SyncRunResult, error) {
	metadataDate := cfg.PyPI.ArtifactDownload.MetadataDate
	outputDate := cfg.PyPI.ArtifactDownload.OutputDate
	snapshotRoot := config.BuildSnapshotRoot(cfg.Base.MetadataRoot, metadataDate)
	outputRoot := config.BuildMirrorOutputRoot(cfg.Base.MirrorRoot, outputDate)

	emit(onEvent, "Prepare", fmt.Sprintf("PyPI / %s", config.TaskLabel(types.PypiTaskArtifactDownload)), nil)
	emit(onEvent, "Load Snapshot", fmt.Sprintf("Loading package metadata from %s", snapshotRoot), nil)

	plan, err := pypi.BuildDownloadPlanFromSnapshot(pypi.BuildDownloadPlanFromSnapshotOptions{
		SnapshotRoot:  snapshotRoot,
		SimpleBaseURL: cfg.Base.SimpleURL,
		MirrorRoot:    outputRoot,
	})
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("build plan: %w", err)
	}

	planPath, err := writePlan(cfg.Base.MetadataRoot, metadataDate, fmt.Sprintf("download-plan-%s.json", outputDate), plan)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write plan: %w", err)
	}
	emit(onEvent, "Build Plan", fmt.Sprintf("Planned %d downloads -> %s", len(plan.Entries), outputRoot), nil)

	downloadSummary := downloader.ExecuteDownloadPlan(plan, types.DownloaderOptions{
		Concurrency:    cfg.Base.Concurrency,
		Retry:          cfg.Base.Retry,
		TimeoutMs:      cfg.Base.TimeoutMs,
		UserAgent:      config.DefaultBrowserUserAgent,
		TaskController: tc,
		OnProgress: func(current, failed, total int, active []string) {
			emit(onEvent, "Download", fmt.Sprintf("Downloading %d/%d (Failed: %d)", current, total, failed),
				&SyncProgress{Current: current, Total: total, Failed: failed, Active: active})
		},
	})
	emit(onEvent, "Download",
		fmt.Sprintf("Downloaded %d/%d, failed %d", downloadSummary.Downloaded, downloadSummary.Attempted, len(downloadSummary.Failed)),
		nil)

	// Write run-summary.json
	summaryData := map[string]interface{}{
		"provider":     "pypi",
		"taskType":     "artifact-download",
		"metadataDate": metadataDate,
		"outputDate":   outputDate,
		"outputRoot":   outputRoot,
		"planPath":     planPath,
	}
	if err := storage.WriteJSONFile(filepath.Join(outputRoot, "run-summary.json"), summaryData); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write run-summary: %w", err)
	}

	emit(onEvent, "Finalize", fmt.Sprintf("Artifact download completed into %s", outputRoot), nil)

	return types.SyncRunResult{
		Provider:        types.ProviderTypePyPI,
		TaskType:        types.PypiTaskArtifactDownload,
		SnapshotID:      &metadataDate,
		Plan:            &plan,
		DownloadSummary: &downloadSummary,
		OutputRoot:      &outputRoot,
	}, nil
}

func runIncrementalDownload(cfg types.AppConfig, onEvent func(SyncEvent), tc types.Canceller) (types.SyncRunResult, error) {
	oldDate := cfg.PyPI.IncrementalDownload.OldMetadataDate
	newDate := cfg.PyPI.IncrementalDownload.NewMetadataDate
	outputDate := cfg.PyPI.IncrementalDownload.OutputDate

	emit(onEvent, "Prepare", fmt.Sprintf("PyPI / %s", config.TaskLabel(types.PypiTaskIncrementalDownload)), nil)
	emit(onEvent, "Load Manifest", fmt.Sprintf("Comparing %s -> %s", oldDate, newDate), nil)

	oldManifest, err := loadManifestByDate(cfg.Base.MetadataRoot, oldDate, cfg.Base.SimpleURL)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("load old manifest: %w", err)
	}
	newManifest, err := loadManifestByDate(cfg.Base.MetadataRoot, newDate, cfg.Base.SimpleURL)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("load new manifest: %w", err)
	}

	outputRoot := config.BuildMirrorOutputRoot(cfg.Base.MirrorRoot, outputDate)
	diff := planner.DiffSnapshotManifests(oldManifest, newManifest)
	plan := planner.BuildDownloadPlan(planner.BuildDownloadPlanOptions{
		MirrorRoot:  outputRoot,
		NewManifest: newManifest,
		Diff:        &diff,
	})

	planPath, err := writePlan(cfg.Base.MetadataRoot, newDate,
		fmt.Sprintf("incremental-plan-%s-to-%s.json", oldDate, outputDate), plan)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write plan: %w", err)
	}
	emit(onEvent, "Build Plan", fmt.Sprintf("Planned %d incremental downloads -> %s", len(plan.Entries), outputRoot), nil)

	downloadSummary := downloader.ExecuteDownloadPlan(plan, types.DownloaderOptions{
		Concurrency:    cfg.Base.Concurrency,
		Retry:          cfg.Base.Retry,
		TimeoutMs:      cfg.Base.TimeoutMs,
		UserAgent:      config.DefaultBrowserUserAgent,
		TaskController: tc,
		OnProgress: func(current, failed, total int, active []string) {
			emit(onEvent, "Download", fmt.Sprintf("Downloading %d/%d (Failed: %d)", current, total, failed),
				&SyncProgress{Current: current, Total: total, Failed: failed, Active: active})
		},
	})
	emit(onEvent, "Download",
		fmt.Sprintf("Downloaded %d/%d, failed %d", downloadSummary.Downloaded, downloadSummary.Attempted, len(downloadSummary.Failed)),
		nil)

	// Write run-summary.json
	summaryData := map[string]interface{}{
		"provider":       "pypi",
		"taskType":       "incremental-download",
		"oldMetadataDate": oldDate,
		"newMetadataDate": newDate,
		"outputDate":     outputDate,
		"outputRoot":     outputRoot,
		"planPath":       planPath,
	}
	if err := storage.WriteJSONFile(filepath.Join(outputRoot, "run-summary.json"), summaryData); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write run-summary: %w", err)
	}

	emit(onEvent, "Finalize", fmt.Sprintf("Incremental download completed into %s", outputRoot), nil)

	return types.SyncRunResult{
		Provider:        types.ProviderTypePyPI,
		TaskType:        types.PypiTaskIncrementalDownload,
		SnapshotID:      &newDate,
		Manifest:        &newManifest,
		Plan:            &plan,
		Diff:            &diff,
		DownloadSummary: &downloadSummary,
		OutputRoot:      &outputRoot,
	}, nil
}

// RunSync runs a sync task based on the config.
func RunSync(opts RunSyncOptions) (types.SyncRunResult, error) {
	cfg := config.NormalizeConfig(opts.Config)

	switch cfg.SelectedTask {
	case types.PypiTaskMetadataSync:
		return runMetadataSync(cfg, opts.OnEvent, opts.TaskController)
	case types.PypiTaskArtifactDownload:
		return runArtifactDownload(cfg, opts.OnEvent, opts.TaskController)
	case types.PypiTaskIncrementalDownload:
		return runIncrementalDownload(cfg, opts.OnEvent, opts.TaskController)
	default:
		return types.SyncRunResult{}, fmt.Errorf("unknown task type: %s", cfg.SelectedTask)
	}
}
