package planner

import "github.com/user/mirror-sync/types"

// artifactChanged returns true if two artifact records differ.
func artifactChanged(previous, current types.ArtifactRecord) bool {
	if previous.Hash != nil && current.Hash != nil {
		return *previous.Hash != *current.Hash
	}
	return previous.URL != current.URL || previous.Filename != current.Filename
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

	return types.SnapshotDiff{
		Added:     added,
		Changed:   changed,
		Removed:   removed,
		Unchanged: unchanged,
	}
}
