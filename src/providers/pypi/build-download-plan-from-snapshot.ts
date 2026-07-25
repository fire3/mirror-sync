import {existsSync} from 'node:fs';
import {readdir, readFile} from 'node:fs/promises';
import {join} from 'node:path';

import type {DownloadPlan, DownloadPlanEntry, PypiFilterOptions} from '../../shared/types.js';
import {defaultPypiFilterOptions, shouldIncludeArtifact} from './platform-filter.js';
import {resolveArtifactPath} from './path-resolution.js';
import {parseSimpleIndexHtml} from './simple-index.js';

interface JsonFileRecord {
  filename: string;
  url: string;
  hashes?: Record<string, string>;
  'requires-python'?: string;
  yanked?: boolean | string;
}

interface JsonIndexPayload {
  files?: JsonFileRecord[];
}

interface BuildDownloadPlanFromSnapshotOptions {
  snapshotRoot: string;
  simpleBaseUrl: string;
  mirrorRoot: string;
  filter?: PypiFilterOptions | undefined;
  completedPaths?: Set<string> | undefined;
  notFoundPaths?: Set<string> | undefined;
}

function firstHash(hashes?: Record<string, string>): string | undefined {
  if (!hashes) {
    return undefined;
  }

  const [algorithm, value] = Object.entries(hashes)[0] ?? [];
  return algorithm && value ? `${algorithm}:${value}` : undefined;
}

function buildPackageUrl(simpleBaseUrl: string, packageName: string): string {
  const normalized = simpleBaseUrl.endsWith('/') ? simpleBaseUrl : `${simpleBaseUrl}/`;
  return new URL(`${encodeURIComponent(packageName)}/`, normalized).toString();
}

function addEntry(
  entry: DownloadPlanEntry,
  filter: PypiFilterOptions,
  completedPaths: Set<string>,
  notFoundPaths: Set<string>,
  skippedExisting: string[],
  skippedCheckpoint: string[],
  skippedNotFound: string[],
  entries: DownloadPlanEntry[]
): void {
  if (!shouldIncludeArtifact(entry, filter)) {
    return;
  }

  if (completedPaths.has(entry.relativePath)) {
    skippedCheckpoint.push(entry.relativePath);
    return;
  }

  if (notFoundPaths.has(entry.relativePath)) {
    skippedNotFound.push(entry.relativePath);
    return;
  }

  if (existsSync(entry.destinationPath)) {
    skippedExisting.push(entry.relativePath);
    return;
  }

  entries.push(entry);
}

export async function buildDownloadPlanFromSnapshot(
  options: BuildDownloadPlanFromSnapshotOptions
): Promise<DownloadPlan> {
  const filter = options.filter ?? defaultPypiFilterOptions;
  const completedPaths = options.completedPaths ?? new Set<string>();
  const notFoundPaths = options.notFoundPaths ?? new Set<string>();
  const skippedExisting: string[] = [];
  const skippedCheckpoint: string[] = [];
  const skippedNotFound: string[] = [];
  const entries: DownloadPlanEntry[] = [];
  const simpleRoot = join(options.snapshotRoot, 'simple');
  const packageEntries = await readdir(simpleRoot, {withFileTypes: true});
  const packageDirs = packageEntries.filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort();

  for (const packageName of packageDirs) {
    const packageRoot = join(simpleRoot, packageName);
    const htmlPath = join(packageRoot, 'index.html');
    const jsonPath = join(packageRoot, 'index_v1.json');
    const simplePageUrl = buildPackageUrl(options.simpleBaseUrl, packageName);
    const seenRelativePaths = new Set<string>();

    try {
      const jsonContent = await readFile(jsonPath, 'utf8');
      const payload = JSON.parse(jsonContent) as JsonIndexPayload;

      for (const file of payload.files ?? []) {
        const resolvedPath = resolveArtifactPath(packageName, simplePageUrl, file.url);
        seenRelativePaths.add(resolvedPath.relativePath);
        addEntry(
          {
            package: packageName,
            filename: file.filename,
            relativePath: resolvedPath.relativePath,
            destinationPath: join(options.mirrorRoot, resolvedPath.relativePath),
            url: resolvedPath.remoteUrl,
            ...(firstHash(file.hashes) ? {hash: firstHash(file.hashes)} : {}),
            reason: 'full-sync'
          },
          filter,
          completedPaths,
          notFoundPaths,
          skippedExisting,
          skippedCheckpoint,
          skippedNotFound,
          entries
        );
      }
    } catch {
      // Ignore missing or invalid JSON metadata and continue with HTML.
    }

    try {
      const htmlContent = await readFile(htmlPath, 'utf8');
      for (const item of parseSimpleIndexHtml(htmlContent)) {
        const resolvedPath = resolveArtifactPath(packageName, simplePageUrl, item.href);
        if (seenRelativePaths.has(resolvedPath.relativePath)) {
          continue;
        }

        const hashFragment = item.href.split('#', 2)[1];
        addEntry(
          {
            package: packageName,
            filename: item.filename,
            relativePath: resolvedPath.relativePath,
            destinationPath: join(options.mirrorRoot, resolvedPath.relativePath),
            url: resolvedPath.remoteUrl,
            ...(hashFragment ? {hash: hashFragment.replace('=', ':')} : {}),
            reason: 'full-sync'
          },
          filter,
          completedPaths,
          notFoundPaths,
          skippedExisting,
          skippedCheckpoint,
          skippedNotFound,
          entries
        );
      }
    } catch {
      // Ignore missing HTML metadata.
    }
  }

  return {
    entries,
    skippedExisting,
    skippedCheckpoint,
    skippedNotFound
  };
}
