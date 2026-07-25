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

  return extractPackageNames(await response.text());
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
