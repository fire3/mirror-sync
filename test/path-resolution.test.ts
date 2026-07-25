import {describe, expect, it} from 'vitest';

import {resolveArtifactPath} from '../src/providers/pypi/path-resolution.js';

describe('resolveArtifactPath', () => {
  it('keeps the packages path from a relative href', () => {
    const result = resolveArtifactPath(
      'numpy',
      'https://pypi.tuna.tsinghua.edu.cn/simple/numpy/',
      '../../packages/ab/cd/numpy-2.0.0.whl#sha256=abc'
    );

    expect(result.remoteUrl).toBe('https://pypi.tuna.tsinghua.edu.cn/packages/ab/cd/numpy-2.0.0.whl#sha256=abc');
    expect(result.relativePath).toBe('packages/ab/cd/numpy-2.0.0.whl');
    expect(result.warning).toBeUndefined();
  });

  it('falls back to a provider-owned path when packages is absent', () => {
    const result = resolveArtifactPath(
      'demo',
      'https://pypi.tuna.tsinghua.edu.cn/simple/demo/',
      'https://files.pythonhosted.org/demo/demo-1.0.0.zip'
    );

    expect(result.relativePath).toBe('files/demo/demo-1.0.0.zip');
    expect(result.warning).toContain('packages/');
  });
});
