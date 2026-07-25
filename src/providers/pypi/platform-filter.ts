import type {ArtifactRecord, PypiFilterOptions} from '../../shared/types.js';

const SOURCE_EXTENSIONS = ['.tar.gz', '.zip', '.tar.bz2', '.tar.xz', '.tgz'];

export const defaultPypiFilterOptions: PypiFilterOptions = {
  includeSource: true,
  includePlatformAny: true,
  includeLinuxAmd64: true,
  includeWindowsAmd64: true,
  excludeMusllinux: true,
  excludeMacos: true,
  excludeArm: true
};

type WheelPlatform = {
  platform: 'any' | 'linux' | 'win' | 'macos' | 'other';
  arch: 'any' | 'amd64' | 'x86' | 'arm64' | 'arm' | 'other';
  rawTag: string;
};

export function parseWheelPlatform(filename: string): WheelPlatform | undefined {
  if (!filename.endsWith('.whl')) {
    return undefined;
  }

  const parts = filename.slice(0, -4).split('-');
  const rawTag = parts.at(-1);
  if (!rawTag) {
    return undefined;
  }

  const lowerTag = rawTag.toLowerCase();

  if (lowerTag === 'any') {
    return {platform: 'any', arch: 'any', rawTag: lowerTag};
  }

  const arch =
    lowerTag.includes('x86_64') || lowerTag.includes('amd64')
      ? 'amd64'
      : lowerTag.includes('aarch64') || lowerTag.includes('arm64')
        ? 'arm64'
        : lowerTag.includes('armv7') || lowerTag.includes('arm')
          ? 'arm'
          : lowerTag.includes('i686') || lowerTag.includes('win32')
            ? 'x86'
            : 'other';

  const platform =
    lowerTag.includes('manylinux') || lowerTag.includes('linux') || lowerTag.includes('musllinux')
      ? 'linux'
      : lowerTag.includes('win')
        ? 'win'
        : lowerTag.includes('macosx')
          ? 'macos'
          : 'other';

  return {platform, arch, rawTag: lowerTag};
}

export function shouldIncludeFilename(
  packageName: string,
  filename: string,
  options: PypiFilterOptions = defaultPypiFilterOptions
): boolean {
  if (options.includePackages && options.includePackages.length > 0 && !options.includePackages.includes(packageName)) {
    return false;
  }

  if (options.excludePackages?.includes(packageName)) {
    return false;
  }

  const lowerFilename = filename.toLowerCase();

  if (SOURCE_EXTENSIONS.some((extension) => lowerFilename.endsWith(extension))) {
    return options.includeSource;
  }

  const wheel = parseWheelPlatform(lowerFilename);
  if (!wheel) {
    return false;
  }

  if (wheel.rawTag.includes('musllinux') && options.excludeMusllinux) {
    return false;
  }

  if (wheel.platform === 'macos' && options.excludeMacos) {
    return false;
  }

  if ((wheel.arch === 'arm64' || wheel.arch === 'arm') && options.excludeArm) {
    return false;
  }

  if (wheel.platform === 'any') {
    return options.includePlatformAny;
  }

  if (wheel.platform === 'linux' && wheel.arch === 'amd64') {
    return options.includeLinuxAmd64;
  }

  if (wheel.platform === 'win' && wheel.arch === 'amd64') {
    return options.includeWindowsAmd64;
  }

  return false;
}

export function shouldIncludeArtifact(
  artifact: Pick<ArtifactRecord, 'package' | 'filename'>,
  options: PypiFilterOptions = defaultPypiFilterOptions
): boolean {
  return shouldIncludeFilename(artifact.package, artifact.filename, options);
}
