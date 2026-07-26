package pypi

import (
	"regexp"
	"strings"
)

// SimpleIndexEntry represents an entry from a simple index HTML page.
type SimpleIndexEntry struct {
	Filename       string
	Href           string
	RequiresPython *string
	Yanked         *string
}

// Go's RE2 regex engine doesn't support backreferences (\1, \2),
// so we split into two separate regexes for double vs single quotes.
var anchorDoubleRE = regexp.MustCompile(`<a\b([^>]*)href="([^"]*)"([^>]*)>([\s\S]*?)</a>`)
var anchorSingleRE = regexp.MustCompile(`<a\b([^>]*)href='([^']*)'([^>]*)>([\s\S]*?)</a>`)

// ParseSimpleIndexHTML parses a PyPI simple index HTML page and extracts file links.
func ParseSimpleIndexHTML(html string) []SimpleIndexEntry {
	var entries []SimpleIndexEntry

	entries = append(entries, parseAnchors(html, anchorDoubleRE, false)...)
	entries = append(entries, parseAnchors(html, anchorSingleRE, true)...)

	return entries
}

func parseAnchors(html string, re *regexp.Regexp, singleQuote bool) []SimpleIndexEntry {
	var entries []SimpleIndexEntry

	for _, match := range re.FindAllStringSubmatch(html, -1) {
		if len(match) < 5 {
			continue
		}
		beforeHref := match[1]
		href := decodeHTMLEntities(match[2])
		afterHref := match[3]
		innerHTML := stripHTMLTags(decodeHTMLEntities(match[4]))

		if href == "" {
			continue
		}

		entry := buildEntry(beforeHref, afterHref, href, innerHTML)
		if entry != nil {
			entries = append(entries, *entry)
		}
	}

	return entries
}

func buildEntry(beforeHref, afterHref, href, innerHTML string) *SimpleIndexEntry {
	attrs := beforeHref + " " + afterHref

	var requiresPython *string
	if rp := extractAttr(attrs, "data-requires-python"); rp != "" {
		requiresPython = &rp
	}

	var yanked *string
	if y := extractAttr(attrs, "data-yanked"); y != "" {
		yanked = &y
	} else if hasAttr(attrs, "data-yanked") {
		y := "true"
		yanked = &y
	}

	hrefNoFrag := href
	if idx := strings.IndexByte(href, '#'); idx >= 0 {
		hrefNoFrag = href[:idx]
	}

	fallbackFilename := lastSegment(hrefNoFrag)

	filename := innerHTML
	if filename == "" {
		filename = fallbackFilename
	}
	if filename == "" {
		return nil
	}

	return &SimpleIndexEntry{
		Filename:       filename,
		Href:           href,
		RequiresPython: requiresPython,
		Yanked:         yanked,
	}
}

func stripHTMLTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}

func decodeHTMLEntities(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return s
}

// extractAttr finds attribute="value" or attribute='value' without backreferences.
func extractAttr(attrs, name string) string {
	quoted := regexp.QuoteMeta(name)
	doubleRE := regexp.MustCompile(quoted + `="([^"]*)"`)
	if m := doubleRE.FindStringSubmatch(attrs); len(m) >= 2 {
		return m[1]
	}
	singleRE := regexp.MustCompile(quoted + `='([^']*)'`)
	if m := singleRE.FindStringSubmatch(attrs); len(m) >= 2 {
		return m[1]
	}
	return ""
}

func hasAttr(attrs, name string) bool {
	return strings.Contains(attrs, name)
}

func lastSegment(path string) string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}
