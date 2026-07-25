import {access, mkdir, readFile, readdir} from 'node:fs/promises';
import {constants as fsConstants} from 'node:fs';
import {homedir} from 'node:os';
import {dirname, join, resolve} from 'node:path';

import {writeJsonFile} from '../core/storage/jsonl.js';
import type {AppConfig, BaseAppConfig, PypiTaskType} from '../shared/types.js';

export const DEFAULT_CONFIG_PATH = join(homedir(), '.mirror-sync', 'config.json');
export const DEFAULT_BROWSER_USER_AGENT =
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36';

export function defaultBaseConfig(): BaseAppConfig {
  return {
    profileName: 'pypi-tsinghua',
    provider: 'pypi',
    simpleUrl: 'https://pypi.tuna.tsinghua.edu.cn/simple/',
    metadataRoot: resolve(process.cwd(), 'data', 'meta', 'pypi'),
    mirrorRoot: resolve(process.cwd(), 'data', 'mirror', 'pypi'),
    concurrency: 16,
    retry: 2,
    timeoutMs: 60_000
  };
}

export function defaultConfig(): AppConfig {
  return {
    base: defaultBaseConfig(),
    selectedTask: 'metadata-sync',
    pypi: {
      metadataSync: {
        snapshotDate: buildSnapshotId()
      },
      artifactDownload: {
        metadataDate: '',
        outputDate: buildSnapshotId()
      },
      incrementalDownload: {
        oldMetadataDate: '',
        newMetadataDate: '',
        outputDate: buildSnapshotId()
      }
    }
  };
}

function normalizeUrl(url: string): string {
  return url.endsWith('/') ? url : `${url}/`;
}

function fallbackDate(value: string): string {
  return value.trim() || buildSnapshotId();
}

export function normalizeConfig(config: AppConfig): AppConfig {
  const defaults = defaultConfig();
  const baseDefaults = defaultBaseConfig();
  const base = config.base;
  const trimmedUrl = base.simpleUrl.trim();

  return {
    base: {
      ...base,
      profileName: base.profileName.trim() || baseDefaults.profileName,
      provider: 'pypi',
      simpleUrl: normalizeUrl(trimmedUrl || baseDefaults.simpleUrl),
      metadataRoot: resolve(base.metadataRoot.trim() || baseDefaults.metadataRoot),
      mirrorRoot: resolve(base.mirrorRoot.trim() || baseDefaults.mirrorRoot),
      concurrency: Number.isFinite(base.concurrency) && base.concurrency > 0 ? Math.floor(base.concurrency) : baseDefaults.concurrency,
      retry: Number.isFinite(base.retry) && base.retry >= 0 ? Math.floor(base.retry) : baseDefaults.retry,
      timeoutMs: Number.isFinite(base.timeoutMs) && base.timeoutMs > 0 ? Math.floor(base.timeoutMs) : baseDefaults.timeoutMs
    },
    selectedTask: config.selectedTask,
    pypi: {
      metadataSync: {
        snapshotDate: fallbackDate(config.pypi.metadataSync.snapshotDate || defaults.pypi.metadataSync.snapshotDate)
      },
      artifactDownload: {
        metadataDate: config.pypi.artifactDownload.metadataDate.trim(),
        outputDate: fallbackDate(config.pypi.artifactDownload.outputDate || defaults.pypi.artifactDownload.outputDate)
      },
      incrementalDownload: {
        oldMetadataDate: config.pypi.incrementalDownload.oldMetadataDate.trim(),
        newMetadataDate: config.pypi.incrementalDownload.newMetadataDate.trim(),
        outputDate: fallbackDate(config.pypi.incrementalDownload.outputDate || defaults.pypi.incrementalDownload.outputDate)
      }
    }
  };
}

export async function loadConfig(configPath = DEFAULT_CONFIG_PATH): Promise<AppConfig> {
  try {
    const content = await readFile(configPath, 'utf8');
    const parsed = JSON.parse(content) as Partial<AppConfig>;
    return normalizeConfig({
      ...defaultConfig(),
      ...parsed,
      base: {
        ...defaultConfig().base,
        ...(parsed.base ?? {})
      },
      pypi: {
        ...defaultConfig().pypi,
        ...(parsed.pypi ?? {}),
        metadataSync: {
          ...defaultConfig().pypi.metadataSync,
          ...(parsed.pypi?.metadataSync ?? {})
        },
        artifactDownload: {
          ...defaultConfig().pypi.artifactDownload,
          ...(parsed.pypi?.artifactDownload ?? {})
        },
        incrementalDownload: {
          ...defaultConfig().pypi.incrementalDownload,
          ...(parsed.pypi?.incrementalDownload ?? {})
        }
      }
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

export function buildSnapshotRoot(metadataRoot: string, snapshotId: string): string {
  return join(metadataRoot, 'snapshots', `pypi-${snapshotId}`);
}

export function buildMirrorOutputRoot(mirrorRoot: string, outputDate: string): string {
  return join(mirrorRoot, `pypi-${outputDate}`);
}

export function buildManifestPath(metadataRoot: string, metadataDate: string): string {
  return join(buildSnapshotRoot(metadataRoot, metadataDate), 'manifest.json');
}

export function buildPlanPathFromMetadataDate(metadataRoot: string, metadataDate: string, fileName = 'download-plan.json'): string {
  return join(buildSnapshotRoot(metadataRoot, metadataDate), fileName);
}

export function buildSnapshotId(now = new Date()): string {
  return now.toISOString().split('T')[0]!;
}

export function taskLabel(taskType: PypiTaskType): string {
  switch (taskType) {
    case 'metadata-sync':
      return '下载元数据';
    case 'artifact-download':
      return '按单日元数据下载包';
    case 'incremental-download':
      return '按两日元数据增量下载';
  }
}
