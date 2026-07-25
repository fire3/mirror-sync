import {readFileSync} from 'node:fs';
import {mkdir, writeFile} from 'node:fs/promises';
import {dirname} from 'node:path';

function extractPackageNames(html: string): string[] {
  return Array.from(html.matchAll(/<a\b[^>]*>([^<]+)<\/a>/gi))
    .map((match) => match[1]?.trim())
    .filter((value): value is string => Boolean(value))
    .sort();
}

export async function fetchPackageList(simpleUrl: string, userAgent?: string): Promise<string[]> {
  const response = await fetch(simpleUrl, userAgent ? {headers: {'user-agent': userAgent}} : undefined);
  if (!response.ok) {
    throw new Error(`Failed to fetch package list from ${simpleUrl}: ${response.status}`);
  }

  const html = await response.text();
  const packages = extractPackageNames(html);
  // #region debug-point A:package-list-size
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
        location: 'fetch-package-list.ts:fetchPackageList',
        msg: '[DEBUG] package list extracted',
        data: {
          htmlLength: html.length,
          packageCount: packages.length,
          heapUsed: process.memoryUsage().heapUsed
        },
        ts: Date.now()
      })
    }).catch(() => {});
  })();
  // #endregion
  return packages;
}

export async function fetchAndWritePackageList(
  simpleUrl: string,
  outputPath: string,
  userAgent?: string
): Promise<string[]> {
  const packages = await fetchPackageList(simpleUrl, userAgent);
  await mkdir(dirname(outputPath), {recursive: true});
  await writeFile(outputPath, `${packages.join('\n')}\n`, 'utf8');
  return packages;
}
