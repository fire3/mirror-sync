import {mkdir, writeFile} from 'node:fs/promises';
import {dirname, join} from 'node:path';

import pLimit from 'p-limit';
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
  const limit = pLimit(options.concurrency);
  const failures: FetchPackageMetadataSummary['failures'] = [];
  let htmlSuccess = 0;
  let jsonSuccess = 0;
  let processed = 0;
  const total = options.packageNames.length;
  const activeDownloads = new Set<string>();

  const updateProgress = () => {
    options.onProgress?.(processed, total, Array.from(activeDownloads));
  };

  await Promise.all(
    options.packageNames.map((packageName) =>
      limit(async () => {
        if (options.taskController) {
          await options.taskController.check();
        }

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
      })
    )
  );

  return {
    packagesTotal: options.packageNames.length,
    htmlSuccess,
    jsonSuccess,
    failures
  };
}
