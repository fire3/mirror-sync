import {basename, posix} from 'node:path';

export interface ResolvedArtifactPath {
  remoteUrl: string;
  relativePath: string;
  warning?: string | undefined;
}

function normalizePathname(pathname: string): string {
  const decoded = decodeURIComponent(pathname);
  return decoded.startsWith('/') ? decoded.slice(1) : decoded;
}

export function resolveArtifactPath(
  packageName: string,
  simplePageUrl: string,
  href: string
): ResolvedArtifactPath {
  const remoteUrl = new URL(href, simplePageUrl).toString();
  const remotePathname = normalizePathname(new URL(remoteUrl).pathname);
  const packagesIndex = remotePathname.indexOf('packages/');

  if (packagesIndex >= 0) {
    return {
      remoteUrl,
      relativePath: remotePathname.slice(packagesIndex)
    };
  }

  const hrefWithoutFragment = href.split('#', 1)[0] ?? '';
  const fallbackFilename = basename(remotePathname) || basename(hrefWithoutFragment) || `${packageName}.bin`;
  return {
    remoteUrl,
    relativePath: posix.join('files', packageName, fallbackFilename),
    warning: `Artifact path does not contain packages/: ${href}`
  };
}
