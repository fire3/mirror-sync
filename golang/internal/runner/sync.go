package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/mirror-sync/internal/config"
	"github.com/user/mirror-sync/internal/core/cleanup"
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
	Current   int
	Total     int
	Failed    int
	Active    []string
	Completed string // name of the most recently completed package (empty if none)
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

// loadPackageListCount reads package-list.txt from the snapshot root and
// returns the number of lines (≈ total packages) for progress estimation.
// Returns 0 if the file doesn't exist or can't be read.
func loadPackageListCount(snapshotRoot string) int {
	path := filepath.Join(snapshotRoot, "package-list.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func runArtifactDownload(cfg types.AppConfig, onEvent func(SyncEvent), tc types.Canceller) (types.SyncRunResult, error) {
	metadataDate := cfg.PyPI.ArtifactDownload.MetadataDate
	outputDir := cfg.PyPI.ArtifactDownload.OutputDir
	if outputDir == "" {
		// Defensive: RunSync normally normalizes this, but keep a safe default
		// that matches the snapshot directory name.
		outputDir = config.FallbackOutputDir(metadataDate, "")
	}
	snapshotRoot := config.BuildSnapshotRoot(cfg.Base.MetadataRoot, metadataDate)
	outputRoot := filepath.Join(cfg.Base.MirrorRoot, outputDir)
	stateDir := filepath.Join(outputRoot, "state")

	emit(onEvent, "Prepare", fmt.Sprintf("PyPI / %s", config.TaskLabel(types.PypiTaskArtifactDownload)), nil)
	emit(onEvent, "Scan Metadata", fmt.Sprintf("Scanning %s/simple/ ...", snapshotRoot), nil)

	// ── Stage 1: Initialize checkpoint & state store ────────────────
	checkpointPath := filepath.Join(stateDir, "checkpoint.jsonl")
	checkpointStore, err := downloader.LoadCheckpoint(checkpointPath)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("load checkpoint: %w", err)
	}
	defer checkpointStore.Close()

	stateStore, err := downloader.NewStateStore(stateDir)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("init state store: %w", err)
	}
	defer stateStore.Close()

	// Report resume state so the user sees downloads continue from checkpoint.
	if n := checkpointStore.Count(); n > 0 {
		emit(onEvent, "Resume", fmt.Sprintf("Checkpoint found at %s: %d package(s) already completed — skipping them", checkpointPath, n), nil)
	} else {
		emit(onEvent, "Resume", fmt.Sprintf("No checkpoint at %s — starting fresh", checkpointPath), nil)
	}

	// Get total package estimation from package-list.txt
	totalEstimate := loadPackageListCount(snapshotRoot)
	estimatedStr := fmt.Sprintf("%d", totalEstimate)
	if totalEstimate == 0 {
		estimatedStr = "?"
	}
	emit(onEvent, "Scan Metadata", fmt.Sprintf("Estimated packages: %s (pipeline started)", estimatedStr),
		&SyncProgress{Current: 0, Total: totalEstimate, Failed: 0})

	// ── Stage 2: Pipeline — scan simple/ & download concurrently ────
	// Producer: traverses simple/ directory, parses HTML, sends packages to channel
	// Consumers: receive packages and download their files concurrently.
	//
	// Two-level concurrency:
	//   - Inter-package: up to 4 packages processed simultaneously
	//   - Intra-package: each package's files use cfg.Base.Concurrency workers

	const pipelineDepth = 4 // max concurrent packages in pipeline
	pkgCh := make(chan types.PackageArtifactGroup, 50)
	errCh := make(chan error, 1)
	done := make(chan struct{})

	// Producer goroutine: traverse simple/ directory directly (no manifest dependency)
	go func() {
		defer close(pkgCh)
		filter := pypi.DefaultFilterOptions
		err := pypi.ForEachPackageInSnapshot(pypi.ForEachPackageOptions{
			SnapshotRoot:  snapshotRoot,
			SimpleBaseURL: cfg.Base.SimpleURL,
			MirrorRoot:    outputRoot,
			Filter:        &filter,
		}, func(group types.PackageArtifactGroup) error {
			if tc != nil {
				if err := tc.Check(); err != nil {
					return err
				}
			}
			select {
			case pkgCh <- group:
				return nil
			case <-done:
				return fmt.Errorf("cancelled")
			}
		})
		if err != nil {
			errCh <- err
		}
	}()

	// Progress tracking (thread-safe via atomic)
	var completed atomic.Int64
	var failed atomic.Int64
	var skipped atomic.Int64

	// Active packages for TUI display
	var activeMu sync.Mutex
	activePkgs := make(map[string]struct{})

	// Shared failed list (append-only, guarded by mutex)
	var failedMu sync.Mutex
	var allFailed []types.DownloadFailedEntry

	// Consumer worker pool
	var wg sync.WaitGroup
	for range pipelineDepth {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range pkgCh {
				// Check cancellation
				if tc != nil {
					if err := tc.Check(); err != nil {
						return
					}
				}

				// Track active package
				activeMu.Lock()
				activePkgs[group.Package] = struct{}{}
				// Build active list
				activeList := make([]string, 0, len(activePkgs))
				for p := range activePkgs {
					if len(activeList) < 4 {
						activeList = append(activeList, p)
					}
				}
				activeMu.Unlock()

				if checkpointStore.IsCompleted(group.Package) {
					activeMu.Lock()
					delete(activePkgs, group.Package)
					activeMu.Unlock()
					completed.Add(1)
					skipped.Add(1)
					emit(onEvent, "Download",
						fmt.Sprintf("Processing %s (skipped, already done)", group.Package),
						&SyncProgress{
							Current:   int(completed.Load()),
							Total:     totalEstimate,
							Failed:    int(failed.Load()),
							Completed: group.Package,
						})
					continue
				}

				// Build per-package download entries
				var pkgEntries []types.DownloadPlanEntry
				for _, artifact := range group.Artifacts {
					destPath := filepath.Join(outputRoot, artifact.RelativePath)
					entry := types.DownloadPlanEntry{
						Package:         artifact.Package,
						Filename:        artifact.Filename,
						RelativePath:    artifact.RelativePath,
						DestinationPath: destPath,
						URL:             artifact.URL,
						Reason:          "full-sync",
					}
					if artifact.Hash != nil {
						entry.Hash = artifact.Hash
					}
					if fi, err := os.Stat(destPath); err == nil && fi.Size() > 0 {
						continue
					}
					pkgEntries = append(pkgEntries, entry)
				}

				if len(pkgEntries) == 0 {
					// All files already exist
					if err := checkpointStore.CompletePackage(downloader.PackageCheckpoint{
						Package: group.Package, CompletedAt: time.Now().UTC(),
						Files: len(group.Artifacts), Bytes: 0,
					}); err != nil {
						emit(onEvent, "Warning", fmt.Sprintf("checkpoint write failed for %s: %v", group.Package, err), nil)
					}
					activeMu.Lock()
					delete(activePkgs, group.Package)
					activeMu.Unlock()
					completed.Add(1)
					skipped.Add(1)
					emit(onEvent, "Download",
						fmt.Sprintf("%s: all files exist (skipped)", group.Package),
						&SyncProgress{
							Current:   int(completed.Load()),
							Total:     totalEstimate,
							Failed:    int(failed.Load()),
							Active:    activeList,
							Completed: group.Package,
						})
					continue
				}

				// Execute download for this package's files
				pkgPlan := types.DownloadPlan{Entries: pkgEntries}
				summary := downloader.ExecuteDownloadPlan(pkgPlan, types.DownloaderOptions{
					Concurrency:    cfg.Base.Concurrency,
					Retry:          cfg.Base.Retry,
					TimeoutMs:      cfg.Base.TimeoutMs,
					UserAgent:      config.DefaultBrowserUserAgent,
					TaskController: tc,
					OnProgress: func(current, failedFiles, total int, active []string) {
						emit(onEvent, "Download",
							fmt.Sprintf("%s: %d/%d files", group.Package, current, total),
							&SyncProgress{
								Current: int(completed.Load()),
								Total:   totalEstimate,
								Failed:  int(failed.Load()),
								Active:  active,
							})
					},
				})

				activeMu.Lock()
				delete(activePkgs, group.Package)
				activeMu.Unlock()

				if len(summary.Failed) > 0 {
					failedMu.Lock()
					for _, f := range summary.Failed {
						if f.NotFound {
							_ = stateStore.RecordNotFound(f.Entry.Package, f.Entry.Filename, f.Entry.RelativePath, f.Entry.URL)
						} else {
							_ = stateStore.RecordFailed(f.Entry.Package, f.Entry.Filename, f.Entry.RelativePath, f.Error)
						}
					}
					allFailed = append(allFailed, summary.Failed...)
					failedMu.Unlock()
					failed.Add(1)
				} else {
					if err := checkpointStore.CompletePackage(downloader.PackageCheckpoint{
						Package: group.Package, CompletedAt: time.Now().UTC(),
						Files: len(group.Artifacts), Bytes: 0,
					}); err != nil {
						emit(onEvent, "Warning", fmt.Sprintf("checkpoint write failed for %s: %v", group.Package, err), nil)
					}
				}
				completed.Add(1)
				emit(onEvent, "Download",
					fmt.Sprintf("%s: done (%d files)", group.Package, len(group.Artifacts)),
					&SyncProgress{
						Current:   int(completed.Load()),
						Total:     totalEstimate,
						Failed:    int(failed.Load()),
						Completed: group.Package,
					})
			}
		}()
	}

	// Wait for producer and consumers to finish
	wg.Wait()
	close(done)

	// Check for producer error
	select {
	case err = <-errCh:
		if err != nil {
			return types.SyncRunResult{}, fmt.Errorf("package iteration: %w", err)
		}
	default:
	}

	completedVal := int(completed.Load())
	failedVal := int(failed.Load())
	skippedVal := int(skipped.Load())

	emit(onEvent, "Download",
		fmt.Sprintf("Packages: %d completed, %d failed", completedVal-failedVal, failedVal), nil)

	// ── Stage 4: Write summary ────────────────────────────────────────
	summaryData := map[string]interface{}{
		"provider":          "pypi",
		"taskType":          "artifact-download",
		"metadataDate":      metadataDate,
		"outputDir":         outputDir,
		"outputRoot":        outputRoot,
		"packagesTotal":     completedVal,
		"packagesCompleted": completedVal - failedVal,
		"packagesFailed":    failedVal,
	}
	if err := storage.WriteJSONFile(filepath.Join(outputRoot, "run-summary.json"), summaryData); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write run-summary: %w", err)
	}

	emit(onEvent, "Finalize", fmt.Sprintf("Artifact download completed into %s", outputRoot), nil)

	return types.SyncRunResult{
		Provider:   types.ProviderTypePyPI,
		TaskType:   types.PypiTaskArtifactDownload,
		SnapshotID: &metadataDate,
		OutputRoot: &outputRoot,
		DownloadSummary: &types.DownloadSummary{
			Attempted:  completedVal,
			Downloaded: completedVal - failedVal - skippedVal,
			Skipped:    skippedVal,
			Failed:     allFailed,
		},
	}, nil
}

// diffReportStats aggregates the diff counts for the report.
type diffReportStats struct {
	OldPackages      int `json:"oldPackages"`
	NewPackages      int `json:"newPackages"`
	AddedPackages    int `json:"addedPackages"`
	RemovedPackages  int `json:"removedPackages"`
	AddedArtifacts   int `json:"addedArtifacts"`
	ChangedArtifacts int `json:"changedArtifacts"`
	RemovedArtifacts int `json:"removedArtifacts"`
	// RemovedArtifactsSkipped counts removals excluded from the cleanup
	// script (filtered out or non-packages/ paths).
	RemovedArtifactsSkipped int `json:"removedArtifactsSkipped"`
}

// diffReport is the structured snapshot-comparison report (diff-report.json).
type diffReport struct {
	OldSnapshotID    string                    `json:"oldSnapshotId"`
	NewSnapshotID    string                    `json:"newSnapshotId"`
	GeneratedAt      string                    `json:"generatedAt"`
	Stats            diffReportStats           `json:"stats"`
	RemovedPackages  []string                  `json:"removedPackages"`
	AddedPackages    []string                  `json:"addedPackages"`
	AddedArtifacts   []types.ArtifactRecord    `json:"addedArtifacts"`
	ChangedArtifacts []types.ArtifactChange    `json:"changedArtifacts"`
	RemovedArtifacts []cleanup.RemovedArtifact `json:"removedArtifacts"`
}

func runIncrementalDownload(cfg types.AppConfig, onEvent func(SyncEvent), tc types.Canceller) (types.SyncRunResult, error) {
	oldDate := cfg.PyPI.IncrementalDownload.OldMetadataDate
	newDate := cfg.PyPI.IncrementalDownload.NewMetadataDate
	outputDir := cfg.PyPI.IncrementalDownload.OutputDir
	cleanupRoot := cfg.PyPI.IncrementalDownload.CleanupRoot

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
	emit(onEvent, "Load Manifest",
		fmt.Sprintf("Old: %d packages / %d artifacts; New: %d packages / %d artifacts",
			len(oldManifest.Packages), len(oldManifest.Artifacts),
			len(newManifest.Packages), len(newManifest.Artifacts)), nil)

	// ── Stage 1: Diff (artifact level + package level) ────────────────
	diff := planner.DiffSnapshotManifests(oldManifest, newManifest)
	emit(onEvent, "Diff",
		fmt.Sprintf("Packages +%d/-%d; artifacts +%d/~%d/-%d",
			len(diff.AddedPackages), len(diff.RemovedPackages),
			len(diff.Added), len(diff.Changed), len(diff.Removed)), nil)

	// ── Stage 2: Output roots ─────────────────────────────────────────
	outputRoot := filepath.Join(cfg.Base.MirrorRoot, outputDir)
	stateDir := filepath.Join(outputRoot, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("mkdir state: %w", err)
	}

	// Same filter for downloads and for the cleanup script.
	filter := pypi.DefaultFilterOptions

	// ── Stage 3: Cleanup script (generated, never executed) ───────────
	cleanupResult, err := cleanup.Generate(cleanup.Options{
		Diff:        diff,
		Filter:      filter,
		CleanupRoot: cleanupRoot,
		OldDate:     oldDate,
		NewDate:     newDate,
	})
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("generate cleanup script: %w", err)
	}
	scriptPath := filepath.Join(outputRoot,
		fmt.Sprintf("cleanup-%s-to-%s.sh", sanitizeFileName(oldDate), sanitizeFileName(newDate)))
	if err := os.WriteFile(scriptPath, []byte(cleanupResult.Script), 0644); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write cleanup script: %w", err)
	}
	removedArtifactsPath := filepath.Join(outputRoot, "removed-artifacts.jsonl")
	if err := writeRemovedArtifacts(removedArtifactsPath, cleanupResult.Removed); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write removed artifacts: %w", err)
	}
	if len(cleanupResult.Removed) > 0 {
		emit(onEvent, "Cleanup",
			fmt.Sprintf("Cleanup script written: %s (%d files, 未执行)", scriptPath, len(cleanupResult.Removed)), nil)
	}
	if cleanupResult.Skipped > 0 {
		emit(onEvent, "Cleanup",
			fmt.Sprintf("⚠ %d removed artifacts skipped (过滤/非 packages/ 路径), 不在脚本内", cleanupResult.Skipped), nil)
	}

	// ── Stage 4: Build plan (added + changed) & download ──────────────
	plan := planner.BuildDownloadPlan(planner.BuildDownloadPlanOptions{
		MirrorRoot:  outputRoot,
		NewManifest: newManifest,
		Diff:        &diff,
		Filter:      &filter,
	})
	planPath, err := writePlan(cfg.Base.MetadataRoot, newDate,
		fmt.Sprintf("incremental-plan-%s-to-%s.json", sanitizeFileName(oldDate), sanitizeFileName(outputDir)), plan)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write plan: %w", err)
	}
	emit(onEvent, "Build Plan", fmt.Sprintf("Planned %d incremental downloads -> %s", len(plan.Entries), outputRoot), nil)

	stateStore, err := downloader.NewStateStore(stateDir)
	if err != nil {
		return types.SyncRunResult{}, fmt.Errorf("init state store: %w", err)
	}
	defer stateStore.Close()

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
	for _, f := range downloadSummary.Failed {
		if f.NotFound {
			_ = stateStore.RecordNotFound(f.Entry.Package, f.Entry.Filename, f.Entry.RelativePath, f.Entry.URL)
		} else {
			_ = stateStore.RecordFailed(f.Entry.Package, f.Entry.Filename, f.Entry.RelativePath, f.Error)
		}
	}
	emit(onEvent, "Download",
		fmt.Sprintf("Downloaded %d/%d, failed %d", downloadSummary.Downloaded, downloadSummary.Attempted, len(downloadSummary.Failed)),
		nil)

	// ── Stage 5: Write diff report & run summary ──────────────────────
	report := diffReport{
		OldSnapshotID: "pypi-" + oldDate,
		NewSnapshotID: "pypi-" + newDate,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Stats: diffReportStats{
			OldPackages:             len(oldManifest.Packages),
			NewPackages:             len(newManifest.Packages),
			AddedPackages:           len(diff.AddedPackages),
			RemovedPackages:         len(diff.RemovedPackages),
			AddedArtifacts:          len(diff.Added),
			ChangedArtifacts:        len(diff.Changed),
			RemovedArtifacts:        len(cleanupResult.Removed),
			RemovedArtifactsSkipped: cleanupResult.Skipped,
		},
		RemovedPackages:  diff.RemovedPackages,
		AddedPackages:    diff.AddedPackages,
		AddedArtifacts:   diff.Added,
		ChangedArtifacts: diff.Changed,
		RemovedArtifacts: cleanupResult.Removed,
	}
	reportPath := filepath.Join(outputRoot, "diff-report.json")
	if err := storage.WriteJSONFile(reportPath, report); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write diff report: %w", err)
	}

	summaryData := map[string]interface{}{
		"provider":                "pypi",
		"taskType":                "incremental-download",
		"oldMetadataDate":         oldDate,
		"newMetadataDate":         newDate,
		"outputDir":               outputDir,
		"outputRoot":              outputRoot,
		"cleanupRoot":             cleanupRoot,
		"planPath":                planPath,
		"diffReportPath":          reportPath,
		"cleanupScriptPath":       scriptPath,
		"removedArtifactsPath":    removedArtifactsPath,
		"addedPackages":           len(diff.AddedPackages),
		"removedPackages":         len(diff.RemovedPackages),
		"addedArtifacts":          len(diff.Added),
		"changedArtifacts":        len(diff.Changed),
		"removedArtifacts":        len(cleanupResult.Removed),
		"removedArtifactsSkipped": cleanupResult.Skipped,
		"attempted":               downloadSummary.Attempted,
		"downloaded":              downloadSummary.Downloaded,
		"failed":                  len(downloadSummary.Failed),
	}
	if err := storage.WriteJSONFile(filepath.Join(outputRoot, "run-summary.json"), summaryData); err != nil {
		return types.SyncRunResult{}, fmt.Errorf("write run-summary: %w", err)
	}

	emit(onEvent, "Finalize",
		fmt.Sprintf("Incremental download completed into %s; cleanup script: %s",
			outputRoot, scriptPath), nil)

	removedPkgCount := len(diff.RemovedPackages)
	removedArtifactCount := len(cleanupResult.Removed)
	removedArtifactSkipped := cleanupResult.Skipped
	return types.SyncRunResult{
		Provider:                    types.ProviderTypePyPI,
		TaskType:                    types.PypiTaskIncrementalDownload,
		SnapshotID:                  &newDate,
		Manifest:                    &newManifest,
		Plan:                        &plan,
		Diff:                        &diff,
		DownloadSummary:             &downloadSummary,
		OutputRoot:                  &outputRoot,
		DiffReportPath:              &reportPath,
		CleanupScriptPath:           &scriptPath,
		RemovedPackageCount:         &removedPkgCount,
		RemovedArtifactCount:        &removedArtifactCount,
		RemovedArtifactSkippedCount: &removedArtifactSkipped,
	}, nil
}

// sanitizeFileName neutralizes path separators so a user-supplied output dir
// cannot inject directories into generated file names.
func sanitizeFileName(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-")
	return r.Replace(s)
}

// writeRemovedArtifacts writes the removal list as newline-delimited JSON.
func writeRemovedArtifacts(path string, removed []cleanup.RemovedArtifact) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range removed {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
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
