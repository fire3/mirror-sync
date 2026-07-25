import type {ArtifactRecord, SnapshotDiff, SnapshotManifest} from '../../shared/types.js';

function artifactChanged(previous: ArtifactRecord, current: ArtifactRecord): boolean {
  if (previous.hash && current.hash) {
    return previous.hash !== current.hash;
  }

  return previous.url !== current.url || previous.filename !== current.filename;
}

export function diffSnapshotManifests(oldManifest: SnapshotManifest, newManifest: SnapshotManifest): SnapshotDiff {
  const oldArtifacts = new Map(oldManifest.artifacts.map((artifact) => [artifact.relativePath, artifact]));
  const unchanged: ArtifactRecord[] = [];
  const added: ArtifactRecord[] = [];
  const changed: Array<{previous: ArtifactRecord; current: ArtifactRecord}> = [];

  for (const artifact of newManifest.artifacts) {
    const previous = oldArtifacts.get(artifact.relativePath);
    if (!previous) {
      added.push(artifact);
      continue;
    }

    if (artifactChanged(previous, artifact)) {
      changed.push({previous, current: artifact});
    } else {
      unchanged.push(artifact);
    }
  }

  const newArtifactPaths = new Set(newManifest.artifacts.map((artifact) => artifact.relativePath));
  const removed = oldManifest.artifacts.filter((artifact) => !newArtifactPaths.has(artifact.relativePath));

  return {
    added,
    changed,
    removed,
    unchanged
  };
}
