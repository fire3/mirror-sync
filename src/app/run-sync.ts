import {readFileSync} from 'node:fs';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

import {executeDownloadPlan} from '../core/downloader/downloader.js';
import {buildDownloadPlan} from '../core/planner/download-plan.js';
import {diffSnapshotManifests} from '../core/planner/snapshot-diff.js';
import {readJsonLines, writeJsonFile} from '../core/storage/jsonl.js';
import {pathExists} from './config.js';
import {fetchAndWritePackageList} from '../providers/pypi/fetch-package-list.js';
import {buildDownloadPlanFromSnapshot} from '../providers/pypi/build-download-plan-from-snapshot.js';
import {fetchPackageMetadata} from '../providers/pypi/fetch-package-metadata.js';
import {buildManifestFromSnapshot} from '../providers/pypi/manifest-builder.js';
import type {AppConfig, DownloadPlan, PypiTaskType, SnapshotManifest, SyncRunResult} from '../shared/types.js';
import {
  DEFAULT_BROWSER_USER_AGENT,
  buildManifestPath,
  buildMirrorOutputRoot,
  buildPlanPathFromMetadataDate,
  buildSnapshotRoot,
  normalizeConfig,
  taskLabel
} from './config.js';
import type {TaskController} from './task-controller.js';

export interface SyncEvent {
  stage: string;
  message: string;
  progress?: {
    current: number;
    total: number;
    failed: number;
    active: string[];
  };
}

interface RunSyncOptions {
  config: AppConfig;
  onEvent?: ((event: SyncEvent) => void) | undefined;
  taskController?: TaskController;
}

function emit(onEvent: ((event: SyncEvent) => void) | undefined, stage: string, message: string): void {
  onEvent?.({stage, message});
}

async function readJsonFile<T>(path: string): Promise<T> {
  return JSON.parse(await readFile(path, 'utf8')) as T;
}

async function loadManifestByDate(
  metadataRoot: string,
  metadataDate: string,
  simpleBaseUrl: string
): Promise<SnapshotManifest> {
  const manifestPath = buildManifestPath(metadataRoot, metadataDate);
  if (!(await pathExists(manifestPath))) {
    return buildManifestFromSnapshot({
      snapshotRoot: buildSnapshotRoot(metadataRoot, metadataDate),
      simpleBaseUrl,
      writeOutputs: false
    });
  }

  const manifest = await readJsonFile<SnapshotManifest>(manifestPath);
  if (manifest.packages.length > 0 || manifest.artifacts.length > 0) {
    return manifest;
  }

  const snapshotRoot = buildSnapshotRoot(metadataRoot, metadataDate);
  const [packages, artifacts] = await Promise.all([
    readJsonLines<SnapshotManifest['packages'][number]>(join(snapshotRoot, 'manifests', 'packages.jsonl')),
    readJsonLines<SnapshotManifest['artifacts'][number]>(join(snapshotRoot, 'manifests', 'artifacts.jsonl'))
  ]);

  return {
    ...manifest,
    packages,
    artifacts
  };
}

async function writePlan(metadataRoot: string, metadataDate: string, fileName: string, plan: DownloadPlan): Promise<string> {
  const outputPath = buildPlanPathFromMetadataDate(metadataRoot, metadataDate, fileName);
  await writeJsonFile(outputPath, plan);
  return outputPath;
}

async function runMetadataSyncTask(
  config: AppConfig,
  onEvent: RunSyncOptions['onEvent'],
  taskController?: TaskController
): Promise<SyncRunResult> {
  const snapshotId = config.pypi.metadataSync.snapshotDate;
  const snapshotRoot = buildSnapshotRoot(config.base.metadataRoot, snapshotId);
  const packageListPath = join(snapshotRoot, 'package-list.txt');

  emit(onEvent, 'Prepare', `PyPI / ${taskLabel('metadata-sync')}`);
  emit(onEvent, 'Fetch /simple/', `Fetching package list from ${config.base.simpleUrl}`);
  const packageNames = await fetchAndWritePackageList(config.base.simpleUrl, packageListPath, DEFAULT_BROWSER_USER_AGENT);
  // #region debug-point D:package-list-retained
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
        hypothesisId: 'D',
        location: 'run-sync.ts:runMetadataSyncTask:afterPackageList',
        msg: '[DEBUG] package list retained for metadata stage',
        data: {
          packageCount: packageNames.length,
          heapUsed: process.memoryUsage().heapUsed
        },
        ts: Date.now()
      })
    }).catch(() => {});
  })();
  // #endregion
  emit(onEvent, 'Fetch /simple/', `Fetched ${packageNames.length} packages`);

  emit(onEvent, 'Download Metadata', `Writing snapshot into ${snapshotRoot}`);
  const metadataSummary = await fetchPackageMetadata({
    simpleUrl: config.base.simpleUrl,
    packageNames,
    snapshotRoot,
    concurrency: config.base.concurrency,
    userAgent: DEFAULT_BROWSER_USER_AGENT,
    taskController,
    onProgress: (current, total, active) => {
      onEvent?.({
        stage: 'Download Metadata',
        message: `Downloading package metadata ${current}/${total}`,
        progress: { current, total, failed: 0, active }
      });
    }
  });
  emit(
    onEvent,
    'Download Metadata',
    `HTML ${metadataSummary.htmlSuccess}/${metadataSummary.packagesTotal}, JSON ${metadataSummary.jsonSuccess}/${metadataSummary.packagesTotal}`
  );
  // #region debug-point A:metadata-stage-summary
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
        location: 'run-sync.ts:runMetadataSyncTask:afterMetadata',
        msg: '[DEBUG] metadata stage finished',
        data: {
          htmlSuccess: metadataSummary.htmlSuccess,
          jsonSuccess: metadataSummary.jsonSuccess,
          failureCount: metadataSummary.failures.length,
          heapUsed: process.memoryUsage().heapUsed
        },
        ts: Date.now()
      })
    }).catch(() => {});
  })();
  // #endregion

  await writeJsonFile(join(config.base.metadataRoot, 'current.json'), {
    provider: 'pypi',
    taskType: 'metadata-sync',
    snapshotId,
    snapshotRoot
  });

  emit(onEvent, 'Finalize', `Snapshot ${snapshotId} completed`);
  return {
    provider: 'pypi',
    taskType: 'metadata-sync',
    snapshotId,
    snapshotRoot,
    packageCount: packageNames.length
  };
}

async function runArtifactDownloadTask(
  config: AppConfig,
  onEvent: RunSyncOptions['onEvent'],
  taskController?: TaskController
): Promise<SyncRunResult> {
  const metadataDate = config.pypi.artifactDownload.metadataDate;
  const outputDate = config.pypi.artifactDownload.outputDate;
  const snapshotRoot = buildSnapshotRoot(config.base.metadataRoot, metadataDate);
  const outputRoot = buildMirrorOutputRoot(config.base.mirrorRoot, outputDate);

  emit(onEvent, 'Prepare', `PyPI / ${taskLabel('artifact-download')}`);
  emit(onEvent, 'Load Snapshot', `Loading package metadata from ${snapshotRoot}`);

  const plan = await buildDownloadPlanFromSnapshot({
    snapshotRoot,
    simpleBaseUrl: config.base.simpleUrl,
    mirrorRoot: outputRoot
  });
  const planPath = await writePlan(config.base.metadataRoot, metadataDate, `download-plan-${outputDate}.json`, plan);
  emit(onEvent, 'Build Plan', `Planned ${plan.entries.length} downloads -> ${outputRoot}`);

  const downloadSummary = await executeDownloadPlan(plan, {
    concurrency: config.base.concurrency,
    retry: config.base.retry,
    timeoutMs: config.base.timeoutMs,
    userAgent: DEFAULT_BROWSER_USER_AGENT,
    taskController,
    onProgress: (current, failed, total, active) => {
      onEvent?.({
        stage: 'Download',
        message: `Downloading ${current}/${total} (Failed: ${failed})`,
        progress: { current, total, failed, active }
      });
    }
  });
  emit(
    onEvent,
    'Download',
    `Downloaded ${downloadSummary.downloaded}/${downloadSummary.attempted}, failed ${downloadSummary.failed.length}`
  );

  await writeJsonFile(join(outputRoot, 'run-summary.json'), {
    provider: 'pypi',
    taskType: 'artifact-download',
    metadataDate,
    outputDate,
    outputRoot,
    planPath
  });

  emit(onEvent, 'Finalize', `Artifact download completed into ${outputRoot}`);
  return {
    provider: 'pypi',
    taskType: 'artifact-download',
    snapshotId: metadataDate,
    plan,
    downloadSummary,
    outputRoot
  };
}

async function runIncrementalDownloadTask(
  config: AppConfig,
  onEvent: RunSyncOptions['onEvent'],
  taskController?: TaskController
): Promise<SyncRunResult> {
  const oldMetadataDate = config.pypi.incrementalDownload.oldMetadataDate;
  const newMetadataDate = config.pypi.incrementalDownload.newMetadataDate;
  const outputDate = config.pypi.incrementalDownload.outputDate;
  const [oldManifest, newManifest] = await Promise.all([
    loadManifestByDate(config.base.metadataRoot, oldMetadataDate, config.base.simpleUrl),
    loadManifestByDate(config.base.metadataRoot, newMetadataDate, config.base.simpleUrl)
  ]);
  const outputRoot = buildMirrorOutputRoot(config.base.mirrorRoot, outputDate);

  emit(onEvent, 'Prepare', `PyPI / ${taskLabel('incremental-download')}`);
  emit(onEvent, 'Load Manifest', `Comparing ${oldMetadataDate} -> ${newMetadataDate}`);
  const diff = diffSnapshotManifests(oldManifest, newManifest);
  const plan = buildDownloadPlan({
    mirrorRoot: outputRoot,
    newManifest,
    diff
  });
  const planPath = await writePlan(
    config.base.metadataRoot,
    newMetadataDate,
    `incremental-plan-${oldMetadataDate}-to-${outputDate}.json`,
    plan
  );
  emit(onEvent, 'Build Plan', `Planned ${plan.entries.length} incremental downloads -> ${outputRoot}`);

  const downloadSummary = await executeDownloadPlan(plan, {
    concurrency: config.base.concurrency,
    retry: config.base.retry,
    timeoutMs: config.base.timeoutMs,
    userAgent: DEFAULT_BROWSER_USER_AGENT,
    taskController,
    onProgress: (current, failed, total, active) => {
      onEvent?.({
        stage: 'Download',
        message: `Downloading ${current}/${total} (Failed: ${failed})`,
        progress: { current, total, failed, active }
      });
    }
  });
  emit(
    onEvent,
    'Download',
    `Downloaded ${downloadSummary.downloaded}/${downloadSummary.attempted}, failed ${downloadSummary.failed.length}`
  );

  await writeJsonFile(join(outputRoot, 'run-summary.json'), {
    provider: 'pypi',
    taskType: 'incremental-download',
    oldMetadataDate,
    newMetadataDate,
    outputDate,
    outputRoot,
    planPath
  });

  emit(onEvent, 'Finalize', `Incremental download completed into ${outputRoot}`);
  return {
    provider: 'pypi',
    taskType: 'incremental-download',
    snapshotId: newMetadataDate,
    manifest: newManifest,
    plan,
    diff,
    downloadSummary,
    outputRoot
  };
}

async function runPypiTask(
  taskType: PypiTaskType,
  config: AppConfig,
  onEvent: RunSyncOptions['onEvent'],
  taskController?: TaskController
): Promise<SyncRunResult> {
  switch (taskType) {
    case 'metadata-sync':
      return runMetadataSyncTask(config, onEvent, taskController);
    case 'artifact-download':
      return runArtifactDownloadTask(config, onEvent, taskController);
    case 'incremental-download':
      return runIncrementalDownloadTask(config, onEvent, taskController);
  }
}

export async function runSync(options: RunSyncOptions): Promise<SyncRunResult> {
  const config = normalizeConfig(options.config);
  return runPypiTask(config.selectedTask, config, options.onEvent, options.taskController);
}
