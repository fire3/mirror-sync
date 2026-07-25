import {access, mkdir, readFile, readdir} from 'node:fs/promises';
import {constants as fsConstants} from 'node:fs';
import {dirname, join, resolve} from 'node:path';

import {writeJsonFile} from '../core/storage/jsonl.js';
import type {AppConfig} from '../shared/types.js';

export const DEFAULT_CONFIG_PATH = resolve(process.cwd(), '.mirror-sync', 'config.json');

export function defaultConfig(): AppConfig {
  return {
    profileName: 'pypi-tsinghua',
    simpleUrl: 'https://pypi.tuna.tsinghua.edu.cn/simple/',
    metadataRoot: resolve(process.cwd(), 'data', 'meta', 'pypi'),
    mirrorRoot: resolve(process.cwd(), 'data', 'mirror', 'pypi'),
    concurrency: 16,
    retry: 2,
    timeoutMs: 60_000,
    userAgent: 'mirror-sync/0.1',
    downloadArtifacts: true
  };
}

function normalizeUrl(url: string): string {
  return url.endsWith('/') ? url : `${url}/`;
}

export function normalizeConfig(config: AppConfig): AppConfig {
  const defaults = defaultConfig();
  const trimmedUrl = config.simpleUrl.trim();
  return {
    ...config,
    profileName: config.profileName.trim() || defaults.profileName,
    simpleUrl: normalizeUrl(trimmedUrl || defaults.simpleUrl),
    metadataRoot: resolve(config.metadataRoot.trim() || defaults.metadataRoot),
    mirrorRoot: resolve(config.mirrorRoot.trim() || defaults.mirrorRoot),
    concurrency: Number.isFinite(config.concurrency) && config.concurrency > 0 ? Math.floor(config.concurrency) : 16,
    retry: Number.isFinite(config.retry) && config.retry >= 0 ? Math.floor(config.retry) : 2,
    timeoutMs: Number.isFinite(config.timeoutMs) && config.timeoutMs > 0 ? Math.floor(config.timeoutMs) : 60_000,
    userAgent: config.userAgent.trim() || 'mirror-sync/0.1'
  };
}

export async function loadConfig(configPath = DEFAULT_CONFIG_PATH): Promise<AppConfig> {
  try {
    const content = await readFile(configPath, 'utf8');
    return normalizeConfig({
      ...defaultConfig(),
      ...(JSON.parse(content) as Partial<AppConfig>)
    });
  } catch {
    return defaultConfig();
  }
}

export async function saveConfig(config: AppConfig, configPath = DEFAULT_CONFIG_PATH): Promise<void> {
  await mkdir(dirname(configPath), {recursive: true});
  await writeJsonFile(configPath, normalizeConfig(config));
}

export async function pathExists(path: string): Promise<boolean> {
  try {
    await access(path, fsConstants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export async function getLatestManifestPath(metadataRoot: string): Promise<string | undefined> {
  const snapshotsRoot = join(metadataRoot, 'snapshots');
  if (!(await pathExists(snapshotsRoot))) {
    return undefined;
  }

  const entries = await readdir(snapshotsRoot, {withFileTypes: true});
  const snapshotNames = entries
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort()
    .reverse();

  for (const snapshotName of snapshotNames) {
    const manifestPath = join(snapshotsRoot, snapshotName, 'manifest.json');
    if (await pathExists(manifestPath)) {
      return manifestPath;
    }
  }

  return undefined;
}

export function buildSnapshotId(now = new Date()): string {
  return now.toISOString().replaceAll(':', '-').replaceAll('.', '-');
}

export function buildSnapshotRoot(metadataRoot: string, snapshotId: string): string {
  return join(metadataRoot, 'snapshots', snapshotId);
}
