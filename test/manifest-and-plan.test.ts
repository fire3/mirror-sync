import {mkdtemp, mkdir, rm, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

import {afterEach, describe, expect, it} from 'vitest';

import {buildDownloadPlan} from '../src/core/planner/download-plan.js';
import {diffSnapshotManifests} from '../src/core/planner/snapshot-diff.js';
import {buildManifestFromSnapshot} from '../src/providers/pypi/manifest-builder.js';

const tempRoots: string[] = [];

afterEach(async () => {
  await Promise.all(tempRoots.splice(0).map((root) => rm(root, {recursive: true, force: true})));
});

async function createSnapshot(name: string): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), `mirror-sync-${name}-`));
  tempRoots.push(root);
  await mkdir(join(root, 'simple', 'demo'), {recursive: true});
  return root;
}

describe('buildManifestFromSnapshot', () => {
  it('prefers JSON metadata and preserves packages paths', async () => {
    const snapshotRoot = await createSnapshot('json');
    await writeFile(
      join(snapshotRoot, 'simple', 'demo', 'index_v1.json'),
      JSON.stringify({
        files: [
          {
            filename: 'demo-1.0.0-py3-none-any.whl',
            url: '../../packages/ab/cd/demo-1.0.0-py3-none-any.whl',
            hashes: {sha256: 'abc123'}
          }
        ]
      }),
      'utf8'
    );

    const manifest = await buildManifestFromSnapshot({
      snapshotRoot,
      simpleBaseUrl: 'https://pypi.tuna.tsinghua.edu.cn/simple/',
      writeOutputs: false
    });

    expect(manifest.stats.packagesTotal).toBe(1);
    expect(manifest.stats.artifactsTotal).toBe(1);
    expect(manifest.artifacts[0]?.relativePath).toBe('packages/ab/cd/demo-1.0.0-py3-none-any.whl');
    expect(manifest.artifacts[0]?.hash).toBe('sha256:abc123');
    expect(manifest.artifacts[0]?.source).toBe('json');
  });

  it('builds diff and download plan from two manifests', async () => {
    const oldSnapshot = await createSnapshot('old');
    const newSnapshot = await createSnapshot('new');

    await writeFile(
      join(oldSnapshot, 'simple', 'demo', 'index.html'),
      '<a href="../../packages/ab/cd/demo-1.0.0-py3-none-any.whl#sha256=oldhash">demo-1.0.0-py3-none-any.whl</a>',
      'utf8'
    );

    await writeFile(
      join(newSnapshot, 'simple', 'demo', 'index.html'),
      [
        '<a href="../../packages/ab/cd/demo-1.0.0-py3-none-any.whl#sha256=newhash">demo-1.0.0-py3-none-any.whl</a>',
        '<a href="../../packages/ef/gh/demo-1.0.1.tar.gz#sha256=src">demo-1.0.1.tar.gz</a>'
      ].join('\n'),
      'utf8'
    );

    const [oldManifest, newManifest] = await Promise.all([
      buildManifestFromSnapshot({
        snapshotRoot: oldSnapshot,
        simpleBaseUrl: 'https://pypi.tuna.tsinghua.edu.cn/simple/',
        writeOutputs: false
      }),
      buildManifestFromSnapshot({
        snapshotRoot: newSnapshot,
        simpleBaseUrl: 'https://pypi.tuna.tsinghua.edu.cn/simple/',
        writeOutputs: false
      })
    ]);

    const diff = diffSnapshotManifests(oldManifest, newManifest);
    const plan = buildDownloadPlan({
      mirrorRoot: '/tmp/mirror-root',
      newManifest,
      diff
    });

    expect(diff.added).toHaveLength(1);
    expect(diff.changed).toHaveLength(1);
    expect(plan.entries).toHaveLength(2);
    expect(plan.entries.map((entry) => entry.reason).sort()).toEqual(['added', 'changed']);
  });
});
