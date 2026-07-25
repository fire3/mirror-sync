import {readFileSync} from 'node:fs';
import {mkdir, writeFile} from 'node:fs/promises';
import {dirname, join} from 'node:path';

import type {TaskController} from '../../app/task-controller.js';

export interface FetchPackageMetadataOptions {
  simpleUrl: string;
  snapshotRoot: string;
  packageNames: string[];
  concurrency: number;
  userAgent?: string | undefined;
  taskController?: TaskController | undefined;
  onProgress?: ((current: number, total: number, active: string[]) => void) | undefined;
}

export interface FetchPackageMetadataSummary {
  packagesTotal: number;
  htmlSuccess: number;
  jsonSuccess: number;
  failures: Array<{package: string; stage: 'html' | 'json'; error: string}>;
}

function packageUrl(simpleUrl: string, packageName: string): string {
  const normalized = simpleUrl.endsWith('/') ? simpleUrl : `${simpleUrl}/`;
  return new URL(`${encodeURIComponent(packageName)}/`, normalized).toString();
}

async function writeResponse(
  url: string,
  outputPath: string,
  accept: string,
  userAgent?: string
): Promise<boolean> {
  const response = await fetch(url, {
    headers: {
      accept,
      ...(userAgent ? {'user-agent': userAgent} : {})
    }
  });

  if (response.status === 404) {
    return false;
  }

  if (!response.ok) {
    throw new Error(`Unexpected response ${response.status}`);
  }

  await mkdir(dirname(outputPath), {recursive: true});
  await writeFile(outputPath, await response.text(), 'utf8');
  return true;
}

export async function fetchPackageMetadata(
  options: FetchPackageMetadataOptions
): Promise<FetchPackageMetadataSummary> {
  const failures: FetchPackageMetadataSummary['failures'] = [];
  let htmlSuccess = 0;
  let jsonSuccess = 0;
  let processed = 0;
  const total = options.packageNames.length;
  const activeDownloads = new Set<string>();
  let lastReported = -1;

  const updateProgress = () => {
    options.onProgress?.(processed, total, Array.from(activeDownloads));
    if ((processed === 0 || processed === total || processed % 1000 === 0) && processed !== lastReported) {
      lastReported = processed;
      // #region debug-point C:metadata-progress
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
            hypothesisId: 'C',
            location: 'fetch-package-metadata.ts:updateProgress',
            msg: '[DEBUG] metadata progress',
            data: {
              processed,
              total,
              activeCount: activeDownloads.size,
              failureCount: failures.length,
              heapUsed: process.memoryUsage().heapUsed
            },
            ts: Date.now()
          })
        }).catch(() => {});
      })();
      // #endregion
    }
  };

  let index = 0;
  const workers = Array.from({ length: options.concurrency }, async () => {
    while (index < total) {
      if (options.taskController) {
        await options.taskController.check();
      }

      const packageName = options.packageNames[index];
      index += 1;
      
      if (!packageName) continue;

      activeDownloads.add(packageName);
      updateProgress();

      const url = packageUrl(options.simpleUrl, packageName);
      const packageRoot = join(options.snapshotRoot, 'simple', packageName);
      const htmlPath = join(packageRoot, 'index.html');
      const jsonPath = join(packageRoot, 'index_v1.json');

      try {
        if (await writeResponse(url, htmlPath, 'text/html', options.userAgent)) {
          htmlSuccess += 1;
        }
      } catch (error) {
        failures.push({
          package: packageName,
          stage: 'html',
          error: error instanceof Error ? error.message : String(error)
        });
      }

      try {
        if (
          await writeResponse(
            url,
            jsonPath,
            'application/vnd.pypi.simple.v1+json',
            options.userAgent
          )
        ) {
          jsonSuccess += 1;
        }
      } catch (error) {
        failures.push({
          package: packageName,
          stage: 'json',
          error: error instanceof Error ? error.message : String(error)
        });
      }

      processed += 1;
      activeDownloads.delete(packageName);
      updateProgress();
    }
  });

  await Promise.all(workers);

  return {
    packagesTotal: total,
    htmlSuccess,
    jsonSuccess,
    failures
  };
}
