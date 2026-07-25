import {readdir, readFile} from 'node:fs/promises';
import {join, resolve} from 'node:path';

import {writeJsonFile, writeJsonLines} from '../../core/storage/jsonl.js';
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
  const packageEntries = await readdir(simpleRoot, {withFileTypes: true});
  const packageDirs = packageEntries.filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort();

  const packages: PackageRecord[] = [];
  const artifacts = new Map<string, ArtifactRecord>();

  for (const packageName of packageDirs) {
    const packageRoot = join(simpleRoot, packageName);
    const htmlPath = join(packageRoot, 'index.html');
    const jsonPath = join(packageRoot, 'index_v1.json');
    const simplePageUrl = buildPackageUrl(options.simpleBaseUrl, packageName);

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
        artifacts.set(resolvedPath.relativePath, {
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
        });
      }
    }

    if (htmlContent) {
      for (const entry of parseSimpleIndexHtml(htmlContent)) {
        const resolvedPath = resolveArtifactPath(packageName, simplePageUrl, entry.href);
        if (artifacts.has(resolvedPath.relativePath)) {
          continue;
        }

        const hashFragment = entry.href.split('#', 2)[1];
        artifacts.set(resolvedPath.relativePath, {
          package: packageName,
          filename: entry.filename,
          relativePath: resolvedPath.relativePath,
          url: resolvedPath.remoteUrl,
          ...(hashFragment ? {hash: hashFragment.replace('=', ':')} : {}),
          ...(entry.requiresPython ? {requiresPython: entry.requiresPython} : {}),
          ...(entry.yanked ? {yanked: entry.yanked} : {}),
          source: 'html',
          snapshotId
        });
      }
    }

    packages.push({
      name: packageName,
      snapshotId,
      htmlPresent: Boolean(htmlContent),
      jsonPresent: Boolean(jsonContent),
      artifactCount: Array.from(artifacts.values()).filter((artifact) => artifact.package === packageName).length
    });
  }

  const sortedArtifacts = Array.from(artifacts.values()).sort((left, right) =>
    left.relativePath.localeCompare(right.relativePath)
  );

  const stats: SnapshotStats = {
    packagesTotal: packages.length,
    packagesWithHtml: packages.filter((item) => item.htmlPresent).length,
    packagesWithJson: packages.filter((item) => item.jsonPresent).length,
    artifactsTotal: sortedArtifacts.length
  };

  const manifest: SnapshotManifest = {
    snapshotId,
    packages,
    artifacts: sortedArtifacts,
    stats
  };

  if (options.writeOutputs ?? true) {
    await writeJsonLines(join(options.snapshotRoot, 'manifests', 'packages.jsonl'), packages);
    await writeJsonLines(join(options.snapshotRoot, 'manifests', 'artifacts.jsonl'), sortedArtifacts);
    await writeJsonFile(join(options.snapshotRoot, 'stats.json'), stats);
    await writeJsonFile(join(options.snapshotRoot, 'manifest.json'), manifest);
  }

  return manifest;
}
