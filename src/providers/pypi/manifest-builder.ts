import {readFileSync} from 'node:fs';
import {readdir, readFile, writeFile} from 'node:fs/promises';
import {join, resolve} from 'node:path';

import {appendJsonLine, writeJsonFile} from '../../core/storage/jsonl.js';
import type {ArtifactRecord, PackageRecord, SnapshotManifest, SnapshotStats} from '../../shared/types.js';
import {resolveArtifactPath} from './path-resolution.js';
import {parseSimpleIndexHtml} from './simple-index.js';

interface BuildManifestOptions {
  snapshotRoot: string;
  simpleBaseUrl: string;
  writeOutputs?: boolean;
}

interface JsonFileRecord {
  filename: string;
  url: string;
  hashes?: Record<string, string>;
  'requires-python'?: string;
  yanked?: boolean | string;
  uploadTime?: string;
}

interface JsonIndexPayload {
  files?: JsonFileRecord[];
}

function snapshotIdFromPath(snapshotRoot: string): string {
  const normalized = resolve(snapshotRoot);
  return normalized.split('/').at(-1) ?? 'snapshot';
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

export async function buildManifestFromSnapshot(
  options: BuildManifestOptions
): Promise<SnapshotManifest> {
  const snapshotId = snapshotIdFromPath(options.snapshotRoot);
  const simpleRoot = join(options.snapshotRoot, 'simple');
  const shouldWriteOutputs = options.writeOutputs ?? true;
  const packagesPath = join(options.snapshotRoot, 'manifests', 'packages.jsonl');
  const artifactsPath = join(options.snapshotRoot, 'manifests', 'artifacts.jsonl');
  const packageEntries = await readdir(simpleRoot, {withFileTypes: true});
  const packageDirs = packageEntries.filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort();

  const packages: PackageRecord[] = [];
  const artifacts = shouldWriteOutputs ? undefined : new Map<string, ArtifactRecord>();
  let packagesWithHtml = 0;
  let packagesWithJson = 0;
  let artifactsTotal = 0;

  if (shouldWriteOutputs) {
    await writeFile(packagesPath, '', 'utf8');
    await writeFile(artifactsPath, '', 'utf8');
  }

  for (const [index, packageName] of packageDirs.entries()) {
    const packageRoot = join(simpleRoot, packageName);
    const htmlPath = join(packageRoot, 'index.html');
    const jsonPath = join(packageRoot, 'index_v1.json');
    const simplePageUrl = buildPackageUrl(options.simpleBaseUrl, packageName);
    const packageArtifacts = new Map<string, ArtifactRecord>();

    let htmlContent: string | undefined;
    let jsonContent: string | undefined;

    try {
      htmlContent = await readFile(htmlPath, 'utf8');
    } catch {
      htmlContent = undefined;
    }

    try {
      jsonContent = await readFile(jsonPath, 'utf8');
    } catch {
      jsonContent = undefined;
    }

    if (jsonContent) {
      const payload = JSON.parse(jsonContent) as JsonIndexPayload;
      for (const file of payload.files ?? []) {
        const resolvedPath = resolveArtifactPath(packageName, simplePageUrl, file.url);
        const hash = firstHash(file.hashes);
        const artifactRecord: ArtifactRecord = {
          package: packageName,
          filename: file.filename,
          relativePath: resolvedPath.relativePath,
          url: resolvedPath.remoteUrl,
          ...(hash ? {hash} : {}),
          ...(file['requires-python'] ? {requiresPython: file['requires-python']} : {}),
          ...(file.yanked !== undefined ? {yanked: file.yanked} : {}),
          ...(file.uploadTime ? {uploadTime: file.uploadTime} : {}),
          source: 'json',
          snapshotId
        };
        packageArtifacts.set(resolvedPath.relativePath, artifactRecord);
        artifacts?.set(resolvedPath.relativePath, artifactRecord);
      }
    }

    if (htmlContent) {
      for (const entry of parseSimpleIndexHtml(htmlContent)) {
        const resolvedPath = resolveArtifactPath(packageName, simplePageUrl, entry.href);
        if (packageArtifacts.has(resolvedPath.relativePath)) {
          continue;
        }

        const hashFragment = entry.href.split('#', 2)[1];
        const artifactRecord: ArtifactRecord = {
          package: packageName,
          filename: entry.filename,
          relativePath: resolvedPath.relativePath,
          url: resolvedPath.remoteUrl,
          ...(hashFragment ? {hash: hashFragment.replace('=', ':')} : {}),
          ...(entry.requiresPython ? {requiresPython: entry.requiresPython} : {}),
          ...(entry.yanked ? {yanked: entry.yanked} : {}),
          source: 'html',
          snapshotId
        };
        packageArtifacts.set(resolvedPath.relativePath, artifactRecord);
        artifacts?.set(resolvedPath.relativePath, artifactRecord);
      }
    }

    const packageRecord: PackageRecord = {
      name: packageName,
      snapshotId,
      htmlPresent: Boolean(htmlContent),
      jsonPresent: Boolean(jsonContent),
      artifactCount: packageArtifacts.size
    };

    if (htmlContent) {
      packagesWithHtml += 1;
    }
    if (jsonContent) {
      packagesWithJson += 1;
    }
    artifactsTotal += packageArtifacts.size;

    if (shouldWriteOutputs) {
      await appendJsonLine(packagesPath, packageRecord);
      for (const artifact of packageArtifacts.values()) {
        await appendJsonLine(artifactsPath, artifact);
      }
    } else {
      packages.push(packageRecord);
    }

    if (index === 0 || (index + 1) % 1000 === 0 || index + 1 === packageDirs.length) {
      // #region debug-point E:manifest-build-progress
      (() => {
        let u = 'http://127.0.0.1:7777/event';
        let s = 'metadata-oom';
        try {
          const e = readFileSync('.dbg/metadata-oom.env', 'utf8');
          u = e.match(/DEBUG_SERVER_URL=(.+)/)?.[1] ?? u;
          s = e.match(/DEBUG_SESSION_ID=(.+)/)?.[1] ?? s;
        } catch {}
        fetch(u, {
          method: 'POST',
          body: JSON.stringify({
            sessionId: s,
            runId: 'pre-fix',
            hypothesisId: 'A',
            location: 'manifest-builder.ts:buildManifestFromSnapshot',
            msg: '[DEBUG] manifest build progress',
            data: {
              processedPackages: index + 1,
              totalPackages: packageDirs.length,
              artifactCount: shouldWriteOutputs ? artifactsTotal : (artifacts?.size ?? 0),
              heapUsed: process.memoryUsage().heapUsed
            },
            ts: Date.now()
          })
        }).catch(() => {});
      })();
      // #endregion
    }
  }

  const sortedArtifacts = shouldWriteOutputs
    ? []
    : Array.from((artifacts ?? new Map<string, ArtifactRecord>()).values()).sort((left, right) =>
        left.relativePath.localeCompare(right.relativePath)
      );

  const stats: SnapshotStats = {
    packagesTotal: packageDirs.length,
    packagesWithHtml,
    packagesWithJson,
    artifactsTotal
  };

  const manifest: SnapshotManifest = {
    snapshotId,
    packages,
    artifacts: sortedArtifacts,
    stats
  };

  if (shouldWriteOutputs) {
    await writeJsonFile(join(options.snapshotRoot, 'stats.json'), stats);
    await writeJsonFile(join(options.snapshotRoot, 'manifest.json'), manifest);
  }

  return manifest;
}
