import {describe, expect, it} from 'vitest';

import {buildSnapshotId, defaultConfig, normalizeConfig} from '../src/app/config.js';

describe('normalizeConfig', () => {
  it('normalizes url, paths and numeric fields', () => {
    const config = normalizeConfig({
      ...defaultConfig(),
      simpleUrl: 'https://example.com/simple',
      metadataRoot: './tmp/meta',
      mirrorRoot: './tmp/mirror',
      concurrency: 3.7,
      retry: -1,
      timeoutMs: 0
    });

    expect(config.simpleUrl).toBe('https://example.com/simple/');
    expect(config.metadataRoot).toContain('/tmp/meta');
    expect(config.mirrorRoot).toContain('/tmp/mirror');
    expect(config.concurrency).toBe(3);
    expect(config.retry).toBe(2);
    expect(config.timeoutMs).toBe(60000);
  });
});

describe('buildSnapshotId', () => {
  it('creates a filesystem-friendly timestamp', () => {
    const snapshotId = buildSnapshotId(new Date('2026-07-25T12:34:56.789Z'));
    expect(snapshotId).toBe('2026-07-25T12-34-56-789Z');
  });
});
