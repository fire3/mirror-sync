import {describe, expect, it} from 'vitest';

import {defaultPypiFilterOptions, shouldIncludeFilename} from '../src/providers/pypi/platform-filter.js';

describe('shouldIncludeFilename', () => {
  it('keeps source, any, linux amd64 and windows amd64 artifacts by default', () => {
    expect(shouldIncludeFilename('demo', 'demo-1.0.0.tar.gz')).toBe(true);
    expect(shouldIncludeFilename('demo', 'demo-1.0.0-py3-none-any.whl')).toBe(true);
    expect(shouldIncludeFilename('demo', 'demo-1.0.0-cp312-cp312-manylinux_2_17_x86_64.whl')).toBe(true);
    expect(shouldIncludeFilename('demo', 'demo-1.0.0-cp312-cp312-win_amd64.whl')).toBe(true);
  });

  it('drops musllinux, macos and arm wheels by default', () => {
    expect(shouldIncludeFilename('demo', 'demo-1.0.0-cp312-cp312-musllinux_1_2_x86_64.whl')).toBe(false);
    expect(shouldIncludeFilename('demo', 'demo-1.0.0-cp312-cp312-macosx_14_0_x86_64.whl')).toBe(false);
    expect(shouldIncludeFilename('demo', 'demo-1.0.0-cp312-cp312-manylinux_2_17_aarch64.whl')).toBe(false);
  });

  it('respects package include filters', () => {
    expect(
      shouldIncludeFilename('numpy', 'numpy-2.0.0.tar.gz', {
        ...defaultPypiFilterOptions,
        includePackages: ['pandas']
      })
    ).toBe(false);
  });
});
