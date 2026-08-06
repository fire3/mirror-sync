package planner

import (
	"sort"

	"github.com/user/mirror-sync/types"
)

// artifactChanged returns true if two artifact records differ.
func artifactChanged(previous, current types.ArtifactRecord) bool {
	if previous.Hash != nil && current.Hash != nil {
		return *previous.Hash != *current.Hash
	}
	return previous.URL != current.URL || previous.Filename != current.Filename
}

// packageNameSet returns the set of package names in a manifest. It prefers
// the Packages records and falls back to deriving names from artifacts (for
// manifests that only carry artifact records).
func packageNameSet(m types.SnapshotManifest) map[string]struct{} {
	set := make(map[string]struct{}, len(m.Packages))
	for _, p := range m.Packages {
		set[p.Name] = struct{}{}
	}
	if len(set) == 0 {
		for _, a := range m.Artifacts {
			set[a.Package] = struct{}{}
		}
	}
	return set
}

// diffPackages computes the added/removed package name sets between two
// manifests and returns them sorted.
func diffPackages(oldManifest, newManifest types.SnapshotManifest) (added, removed []string) {
	oldSet := packageNameSet(oldManifest)
	newSet := packageNameSet(newManifest)
	for name := range newSet {
		if _, ok := oldSet[name]; !ok {
			added = append(added, name)
		}
	}
	for name := range oldSet {
		if _, ok := newSet[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// DiffSnapshotManifests computes the diff between two snapshot manifests.
func DiffSnapshotManifests(oldManifest, newManifest types.SnapshotManifest) types.SnapshotDiff {
	oldArtifacts := make(map[string]types.ArtifactRecord, len(oldManifest.Artifacts))
	for _, a := range oldManifest.Artifacts {
		oldArtifacts[a.RelativePath] = a
	}

	var added, unchanged, removed []types.ArtifactRecord
	var changed []types.ArtifactChange

	for _, artifact := range newManifest.Artifacts {
		previous, exists := oldArtifacts[artifact.RelativePath]
		if !exists {
			added = append(added, artifact)
			continue
		}
		if artifactChanged(previous, artifact) {
			changed = append(changed, types.ArtifactChange{Previous: previous, Current: artifact})
		} else {
			unchanged = append(unchanged, artifact)
		}
	}

	newPaths := make(map[string]struct{}, len(newManifest.Artifacts))
	for _, a := range newManifest.Artifacts {
		newPaths[a.RelativePath] = struct{}{}
	}
	for _, a := range oldManifest.Artifacts {
		if _, ok := newPaths[a.RelativePath]; !ok {
			removed = append(removed, a)
		}
	}

	addedPackages, removedPackages := diffPackages(oldManifest, newManifest)

	return types.SnapshotDiff{
		AddedPackages:   addedPackages,
		RemovedPackages: removedPackages,
		Added:           added,
		Changed:         changed,
		Removed:         removed,
		Unchanged:       unchanged,
	}
}
