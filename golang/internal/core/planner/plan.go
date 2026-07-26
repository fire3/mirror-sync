package planner

import (
	"os"
	"path/filepath"

	"github.com/user/mirror-sync/internal/pypi"
	"github.com/user/mirror-sync/types"
)

// BuildDownloadPlanOptions configures plan building.
type BuildDownloadPlanOptions struct {
	MirrorRoot     string
	NewManifest    types.SnapshotManifest
	Diff           *types.SnapshotDiff
	Filter         *types.PypiFilterOptions
	CompletedPaths map[string]struct{}
	NotFoundPaths  map[string]struct{}
}

// BuildDownloadPlan builds a download plan from a manifest and optional diff.
func BuildDownloadPlan(opts BuildDownloadPlanOptions) types.DownloadPlan {
	filter := opts.Filter
	if filter == nil {
		f := pypi.DefaultFilterOptions
		filter = &f
	}
	completedPaths := opts.CompletedPaths
	if completedPaths == nil {
		completedPaths = make(map[string]struct{})
	}
	notFoundPaths := opts.NotFoundPaths
	if notFoundPaths == nil {
		notFoundPaths = make(map[string]struct{})
	}

	var skippedExisting, skippedCheckpoint, skippedNotFound []string
	var entries []types.DownloadPlanEntry

	var source []types.DownloadPlanEntry
	if opts.Diff != nil {
		for _, a := range opts.Diff.Added {
			source = append(source, toPlanEntry(a, "added", opts.MirrorRoot))
		}
		for _, c := range opts.Diff.Changed {
			source = append(source, toPlanEntry(c.Current, "changed", opts.MirrorRoot))
		}
	} else {
		for _, a := range opts.NewManifest.Artifacts {
			source = append(source, toPlanEntry(a, "full-sync", opts.MirrorRoot))
		}
	}

	for _, entry := range source {
		if !pypi.ShouldIncludeArtifact(entry.Package, entry.Filename, *filter) {
			continue
		}
		if _, ok := completedPaths[entry.RelativePath]; ok {
			skippedCheckpoint = append(skippedCheckpoint, entry.RelativePath)
			continue
		}
		if _, ok := notFoundPaths[entry.RelativePath]; ok {
			skippedNotFound = append(skippedNotFound, entry.RelativePath)
			continue
		}
		if _, err := os.Stat(entry.DestinationPath); err == nil {
			skippedExisting = append(skippedExisting, entry.RelativePath)
			continue
		}
		entries = append(entries, entry)
	}

	return types.DownloadPlan{
		Entries:           entries,
		SkippedExisting:   skippedExisting,
		SkippedCheckpoint: skippedCheckpoint,
		SkippedNotFound:   skippedNotFound,
	}
}

func toPlanEntry(artifact types.ArtifactRecord, reason, mirrorRoot string) types.DownloadPlanEntry {
	entry := types.DownloadPlanEntry{
		Package:         artifact.Package,
		Filename:        artifact.Filename,
		RelativePath:    artifact.RelativePath,
		DestinationPath: filepath.Join(mirrorRoot, artifact.RelativePath),
		URL:             artifact.URL,
		Reason:          reason,
	}
	if artifact.Hash != nil {
		entry.Hash = artifact.Hash
	}
	return entry
}
