import {describe, expect, it} from 'vitest';

import {
  buildManifestPath,
  buildMirrorOutputRoot,
  buildPlanPathFromMetadataDate,
  buildSnapshotId,
  DEFAULT_BROWSER_USER_AGENT,
  DEFAULT_CONFIG_PATH,
  defaultConfig,
  normalizeConfig,
  taskLabel
} from '../src/app/config.js';

describe('normalizeConfig', () => {
  it('normalizes base config, keeps provider and trims task config fields', () => {
    const config = normalizeConfig({
      ...defaultConfig(),
      base: {
        ...defaultConfig().base,
        simpleUrl: 'https://example.com/simple',
        metadataRoot: './tmp/meta',
        mirrorRoot: './tmp/mirror',
        concurrency: 3.7,
        retry: -1,
        timeoutMs: 0
      },
      pypi: {
        ...defaultConfig().pypi,
        metadataSync: {
          snapshotDate: ' 2026-07-25 '
        },
        artifactDownload: {
          metadataDate: ' 2026-07-24 ',
          outputDate: ' '
        },
        incrementalDownload: {
          oldMetadataDate: ' 2026-07-23 ',
          newMetadataDate: ' 2026-07-24 ',
          outputDate: ' 2026-07-25 '
        }
      }
    });

    expect(config.base.provider).toBe('pypi');
    expect(config.base.simpleUrl).toBe('https://example.com/simple/');
    expect(config.base.metadataRoot).toContain('/tmp/meta');
    expect(config.base.mirrorRoot).toContain('/tmp/mirror');
    expect(config.base.concurrency).toBe(3);
    expect(config.base.retry).toBe(2);
    expect(config.base.timeoutMs).toBe(60000);
    expect(config.pypi.metadataSync.snapshotDate).toBe('2026-07-25');
    expect(config.pypi.artifactDownload.metadataDate).toBe('2026-07-24');
    expect(config.pypi.artifactDownload.outputDate.length).toBeGreaterThan(0);
    expect(config.pypi.incrementalDownload.oldMetadataDate).toBe('2026-07-23');
    expect(config.pypi.incrementalDownload.newMetadataDate).toBe('2026-07-24');
    expect(config.pypi.incrementalDownload.outputDate).toBe('2026-07-25');
  });
});

describe('path builders', () => {
  it('creates filesystem-friendly timestamp and derived paths', () => {
    const snapshotId = buildSnapshotId(new Date('2026-07-25T12:34:56.789Z'));
    expect(snapshotId).toBe('2026-07-25T12-34-56-789Z');
    expect(buildManifestPath('/data/meta/pypi', '2026-07-25')).toBe('/data/meta/pypi/snapshots/pypi-2026-07-25/manifest.json');
    expect(buildPlanPathFromMetadataDate('/data/meta/pypi', '2026-07-25')).toBe('/data/meta/pypi/snapshots/pypi-2026-07-25/download-plan.json');
    expect(buildMirrorOutputRoot('/data/mirror/pypi', '2026-07-26')).toBe('/data/mirror/pypi/pypi-2026-07-26');
  });
});

describe('taskLabel', () => {
  it('returns labels for all task types', () => {
    expect(taskLabel('metadata-sync')).toBe('下载元数据');
    expect(taskLabel('artifact-download')).toBe('按单日元数据下载包');
    expect(taskLabel('incremental-download')).toBe('按两日元数据增量下载');
  });
});

describe('DEFAULT_CONFIG_PATH', () => {
  it('stores config under home directory', () => {
    expect(DEFAULT_CONFIG_PATH).toContain('/.mirror-sync/config.json');
  });
});

describe('DEFAULT_BROWSER_USER_AGENT', () => {
  it('uses a browser-style user agent', () => {
    expect(DEFAULT_BROWSER_USER_AGENT).toContain('Mozilla/5.0');
    expect(DEFAULT_BROWSER_USER_AGENT).toContain('Chrome/');
  });
});
