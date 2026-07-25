import {appendFile, mkdir, readFile, writeFile} from 'node:fs/promises';
import {dirname} from 'node:path';

export async function writeJsonFile(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), {recursive: true});
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, 'utf8');
}

export async function writeJsonLines(path: string, records: unknown[]): Promise<void> {
  await mkdir(dirname(path), {recursive: true});
  const content = records.map((record) => JSON.stringify(record)).join('\n');
  await writeFile(path, content.length > 0 ? `${content}\n` : '', 'utf8');
}

export async function appendJsonLine(path: string, record: unknown): Promise<void> {
  await mkdir(dirname(path), {recursive: true});
  await appendFile(path, `${JSON.stringify(record)}\n`, 'utf8');
}

export async function readJsonLines<T>(path: string): Promise<T[]> {
  const content = await readFile(path, 'utf8');
  return content
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line) => JSON.parse(line) as T);
}
