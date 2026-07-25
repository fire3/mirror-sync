import {join} from 'node:path';
import {readFile} from 'node:fs/promises';

import {executeDownloadPlan} from '../core/downloader/downloader.js';
import {buildDownloadPlan} from '../core/planner/download-plan.js';
import {diffSnapshotManifests} from '../core/planner/snapshot-diff.js';
import {writeJsonFile} from '../core/storage/jsonl.js';
import {fetchAndWritePackageList} from '../providers/pypi/fetch-package-list.js';
import {fetchPackageMetadata} from '../providers/pypi/fetch-package-metadata.js';
import {buildManifestFromSnapshot} from '../providers/pypi/manifest-builder.js';
import type {AppConfig, SnapshotManifest, SyncRunResult} from '../shared/types.js';
import {buildSnapshotId, buildSnapshotRoot, getLatestManifestPath, normalizeConfig} from './config.js';

export interface SyncEvent {
  stage: string;
  message: string;
}

interface RunSyncOptions {
  config: AppConfig;
  onEvent?: ((event: SyncEvent) => void) | undefined;
}

function emit(
  onEvent: ((event: SyncEvent) => void) | undefined,
  stage: string,
  message: string
): void {
  onEvent?.({stage, message});
}

async function readJsonFile<T>(path: string): Promise<T> {
  return JSON.parse(await readFile(path, 'utf8')) as T;
}

export async function runSync(options: RunSyncOptions): Promise<SyncRunResult> {
  const config = normalizeConfig(options.config);
  const snapshotId = buildSnapshotId();
  const snapshotRoot = buildSnapshotRoot(config.metadataRoot, snapshotId);
  const previousManifestPath = await getLatestManifestPath(config.metadataRoot);
  const packageListPath = join(snapshotRoot, 'package-list.txt');

  emit(options.onEvent, 'Prepare', `Using profile ${config.profileName}`);
  emit(options.onEvent, 'Fetch /simple/', `Fetching package list from ${config.simpleUrl}`);
  const packageNames = await fetchAndWritePackageList(config.simpleUrl, packageListPath, config.userAgent);
  emit(options.onEvent, 'Fetch /simple/', `Fetched ${packageNames.length} packages`);

  emit(options.onEvent, 'Download Metadata', 'Fetching package HTML and JSON metadata');
  const metadataSummary = await fetchPackageMetadata({
    simpleUrl: config.simpleUrl,
    packageNames,
    snapshotRoot,
    concurrency: config.concurrency,
    userAgent: config.userAgent
  });
  emit(
    options.onEvent,
    'Download Metadata',
    `HTML ${metadataSummary.htmlSuccess}/${metadataSummary.packagesTotal}, JSON ${metadataSummary.jsonSuccess}/${metadataSummary.packagesTotal}`
  );

  emit(options.onEvent, 'Build Manifest', 'Building normalized snapshot manifest');
  const manifest = await buildManifestFromSnapshot({
    snapshotRoot,
    simpleBaseUrl: config.simpleUrl
  });

  let previousManifest: SnapshotManifest | undefined;
  if (previousManifestPath) {
    emit(options.onEvent, 'Compare / Plan', `Comparing with previous snapshot ${previousManifestPath}`);
    previousManifest = await readJsonFile<SnapshotManifest>(previousManifestPath);
  } else {
    emit(options.onEvent, 'Compare / Plan', 'No previous snapshot found, using full sync plan');
  }

  const diff = previousManifest ? diffSnapshotManifests(previousManifest, manifest) : undefined;
  const plan =
    previousManifest && diff
      ? buildDownloadPlan({
          mirrorRoot: config.mirrorRoot,
          newManifest: manifest,
          diff
        })
      : buildDownloadPlan({
          mirrorRoot: config.mirrorRoot,
          newManifest: manifest
        });

  const planPath = join(snapshotRoot, 'download-plan.json');
  await writeJsonFile(planPath, plan);
  emit(options.onEvent, 'Compare / Plan', `Planned ${plan.entries.length} downloads`);

  let downloadSummary: SyncRunResult['downloadSummary'];
  if (config.downloadArtifacts) {
    emit(options.onEvent, 'Download', 'Downloading artifacts');
    downloadSummary = await executeDownloadPlan(plan, {
      concurrency: config.concurrency,
      retry: config.retry,
      timeoutMs: config.timeoutMs,
      userAgent: config.userAgent
    });
    emit(
      options.onEvent,
      'Download',
      `Downloaded ${downloadSummary.downloaded}/${downloadSummary.attempted}, failed ${downloadSummary.failed.length}`
    );
  } else {
    emit(options.onEvent, 'Download', 'Artifact download disabled, plan only');
  }

  await writeJsonFile(join(config.metadataRoot, 'current.json'), {
    snapshotId,
    snapshotRoot,
    manifestPath: join(snapshotRoot, 'manifest.json'),
    planPath
  });

  emit(options.onEvent, 'Finalize', `Snapshot ${snapshotId} completed`);

  return {
    snapshotId,
    snapshotRoot,
    packageCount: packageNames.length,
    manifest,
    plan,
    ...(diff ? {diff} : {}),
    ...(downloadSummary ? {downloadSummary} : {})
  };
}
