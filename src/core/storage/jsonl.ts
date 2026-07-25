import {mkdir, writeFile} from 'node:fs/promises';
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
