export interface SimpleIndexEntry {
  filename: string;
  href: string;
  requiresPython?: string | undefined;
  yanked?: string | undefined;
}

function stripHtmlTags(value: string): string {
  return value.replace(/<[^>]+>/g, '').trim();
}

function decodeHtmlEntities(value: string): string {
  return value
    .replaceAll('&amp;', '&')
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&quot;', '"')
    .replaceAll('&#39;', "'");
}

export function parseSimpleIndexHtml(html: string): SimpleIndexEntry[] {
  const anchorRegex =
    /<a\b([^>]*)href=(['"])(.*?)\2([^>]*)>([\s\S]*?)<\/a>/gi;
  const entries: SimpleIndexEntry[] = [];

  for (const match of html.matchAll(anchorRegex)) {
    const beforeHref = match[1] ?? '';
    const href = decodeHtmlEntities(match[3] ?? '');
    const afterHref = match[4] ?? '';
    const innerHtml = decodeHtmlEntities(stripHtmlTags(match[5] ?? ''));
    const attrs = `${beforeHref} ${afterHref}`;
    const requiresPython = attrs.match(/data-requires-python=(['"])(.*?)\1/i)?.[2];
    const yanked = attrs.match(/data-yanked(?:=(['"])(.*?)\1)?/i)?.[2];

    if (!href) {
      continue;
    }

      const hrefWithoutFragment = href.split('#', 1)[0] ?? '';
      const fallbackFilename = hrefWithoutFragment.split('/').filter(Boolean).at(-1);
    const filename = innerHtml || fallbackFilename;
    if (!filename) {
      continue;
    }

    entries.push({
      filename,
      href,
      requiresPython,
      yanked
    });
  }

  return entries;
}
