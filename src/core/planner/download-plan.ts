import {existsSync} from 'node:fs';
import {join} from 'node:path';

import type {DownloadPlan, DownloadPlanEntry, PypiFilterOptions, SnapshotDiff, SnapshotManifest} from '../../shared/types.js';
import {defaultPypiFilterOptions, shouldIncludeArtifact} from '../../providers/pypi/platform-filter.js';

interface BuildDownloadPlanOptions {
  mirrorRoot: string;
  newManifest: SnapshotManifest;
  diff?: SnapshotDiff;
  filter?: PypiFilterOptions;
  completedPaths?: Set<string>;
  notFoundPaths?: Set<string>;
}

export function buildDownloadPlan(options: BuildDownloadPlanOptions): DownloadPlan {
  const filter = options.filter ?? defaultPypiFilterOptions;
  const completedPaths = options.completedPaths ?? new Set<string>();
  const notFoundPaths = options.notFoundPaths ?? new Set<string>();
  const skippedExisting: string[] = [];
  const skippedCheckpoint: string[] = [];
  const skippedNotFound: string[] = [];

  const entriesSource: DownloadPlanEntry[] = options.diff
    ? [
        ...options.diff.added.map((artifact) => toPlanEntry(artifact, 'added', options.mirrorRoot)),
        ...options.diff.changed.map(({current}) => toPlanEntry(current, 'changed', options.mirrorRoot))
      ]
    : options.newManifest.artifacts.map((artifact) => toPlanEntry(artifact, 'full-sync', options.mirrorRoot));

  const entries = entriesSource.filter((entry) => {
    if (!shouldIncludeArtifact(entry, filter)) {
      return false;
    }

    if (completedPaths.has(entry.relativePath)) {
      skippedCheckpoint.push(entry.relativePath);
      return false;
    }

    if (notFoundPaths.has(entry.relativePath)) {
      skippedNotFound.push(entry.relativePath);
      return false;
    }

    if (existsSync(entry.destinationPath)) {
      skippedExisting.push(entry.relativePath);
      return false;
    }

    return true;
  });

  return {
    entries,
    skippedExisting,
    skippedCheckpoint,
    skippedNotFound
  };
}

function toPlanEntry(
  artifact: SnapshotManifest['artifacts'][number],
  reason: DownloadPlanEntry['reason'],
  mirrorRoot: string
): DownloadPlanEntry {
  return {
    package: artifact.package,
    filename: artifact.filename,
    relativePath: artifact.relativePath,
    destinationPath: join(mirrorRoot, artifact.relativePath),
    url: artifact.url,
    ...(artifact.hash ? {hash: artifact.hash} : {}),
    reason
  };
}
