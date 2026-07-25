import {createHash} from 'node:crypto';
import {mkdir, readFile, rename, stat, unlink, writeFile} from 'node:fs/promises';
import {dirname} from 'node:path';

import pLimit from 'p-limit';

import type {DownloadPlan, DownloadPlanEntry, DownloaderOptions, DownloadSummary} from '../../shared/types.js';

const DEFAULT_USER_AGENT =
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36';

async function verifyHash(entry: DownloadPlanEntry): Promise<void> {
  if (!entry.hash) {
    return;
  }

  const [algorithm, expected] = entry.hash.split(':', 2);
  if (!algorithm || !expected) {
    throw new Error(`Unsupported hash format: ${entry.hash}`);
  }

  const contents = await readFile(entry.destinationPath);
  const actual = createHash(algorithm).update(contents).digest('hex');
  if (actual !== expected) {
    throw new Error(`Hash mismatch for ${entry.relativePath}`);
  }
}

async function fetchWithRetry(
  entry: DownloadPlanEntry,
  options: DownloaderOptions
): Promise<void> {
  let lastError: unknown;

  for (let attempt = 0; attempt <= options.retry; attempt += 1) {
    try {
      await downloadEntry(entry, options);
      return;
    } catch (error) {
      lastError = error;
      if (attempt >= options.retry) {
        break;
      }
    }
  }

  throw lastError;
}

async function downloadEntry(entry: DownloadPlanEntry, options: DownloaderOptions): Promise<void> {
  const destinationDir = dirname(entry.destinationPath);
  const tempPath = `${entry.destinationPath}.tmp`;

  await mkdir(destinationDir, {recursive: true});

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), options.timeoutMs);

  try {
    const response = await fetch(entry.url, {
      headers: {
        'user-agent': options.userAgent ?? DEFAULT_USER_AGENT
      },
      signal: controller.signal
    });

    if (!response.ok) {
      throw new Error(`Unexpected response ${response.status} for ${entry.url}`);
    }

    const arrayBuffer = await response.arrayBuffer();
    await writeFile(tempPath, Buffer.from(arrayBuffer));
    await rename(tempPath, entry.destinationPath);
    await verifyHash(entry);
  } finally {
    clearTimeout(timer);
    try {
      await unlink(tempPath);
    } catch {
      // Ignore missing temp files after successful rename.
    }
  }
}

export async function executeDownloadPlan(
  plan: DownloadPlan,
  options: DownloaderOptions
): Promise<DownloadSummary> {
  const limit = pLimit(options.concurrency);
  const failed: Array<{entry: DownloadPlanEntry; error: string}> = [];
  let downloaded = 0;
  const total = plan.entries.length;
  const activeDownloads = new Set<string>();

  const updateProgress = () => {
    options.onProgress?.(downloaded, failed.length, total, Array.from(activeDownloads));
  };

  await Promise.all(
    plan.entries.map((entry) =>
      limit(async () => {
        if (options.taskController) {
          await options.taskController.check();
        }

        const downloadKey = `${entry.package}/${entry.filename}`;
        activeDownloads.add(downloadKey);
        updateProgress();

        try {
          const destinationStats = await stat(entry.destinationPath).catch(() => undefined);
          if (destinationStats && destinationStats.size > 0) {
            downloaded += 1;
            activeDownloads.delete(downloadKey);
            updateProgress();
            return;
          }

          await fetchWithRetry(entry, options);
          downloaded += 1;
          activeDownloads.delete(downloadKey);
          updateProgress();
        } catch (error) {
          failed.push({
            entry,
            error: error instanceof Error ? error.message : String(error)
          });
          activeDownloads.delete(downloadKey);
          updateProgress();
        }
      })
    )
  );

  return {
    attempted: plan.entries.length,
    downloaded,
    skipped: plan.skippedCheckpoint.length + plan.skippedExisting.length + plan.skippedNotFound.length,
    failed
  };
}
