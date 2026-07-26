package pypi

import (
	"net/url"
	"path"
	"strings"
)

// ResolvedArtifactPath contains the resolved remote URL and relative path for an artifact.
type ResolvedArtifactPath struct {
	RemoteURL    string
	RelativePath string
	Warning      *string
}

// ResolveArtifactPath resolves an artifact's href (from a simple index) to a remote URL
// and relative storage path.
func ResolveArtifactPath(packageName, simplePageURL, href string) ResolvedArtifactPath {
	base, err := url.Parse(simplePageURL)
	if err != nil {
		// Fallback: treat href as relative to simple page
		return ResolvedArtifactPath{
			RemoteURL:    simplePageURL + href,
			RelativePath: path.Join("files", packageName, lastSegment(href)),
		}
	}
	remoteURL, err := url.Parse(href)
	if err != nil {
		return ResolvedArtifactPath{
			RemoteURL:    simplePageURL + href,
			RelativePath: path.Join("files", packageName, lastSegment(href)),
		}
	}
	resolvedURL := base.ResolveReference(remoteURL)
	resolvedStr := resolvedURL.String()

	remotePathname := resolvedURL.Path
	if strings.HasPrefix(remotePathname, "/") {
		remotePathname = remotePathname[1:]
	}

	packagesIdx := strings.Index(remotePathname, "packages/")
	if packagesIdx >= 0 {
		return ResolvedArtifactPath{
			RemoteURL:    resolvedStr,
			RelativePath: remotePathname[packagesIdx:],
		}
	}

	hrefNoFrag := href
	if idx := strings.IndexByte(href, '#'); idx >= 0 {
		hrefNoFrag = href[:idx]
	}
	fallbackFilename := lastSegment(remotePathname)
	if fallbackFilename == "" {
		fallbackFilename = lastSegment(hrefNoFrag)
	}
	if fallbackFilename == "" {
		fallbackFilename = packageName + ".bin"
	}
	warning := "Artifact path does not contain packages/: " + href
	return ResolvedArtifactPath{
		RemoteURL:    resolvedStr,
		RelativePath: path.Join("files", packageName, fallbackFilename),
		Warning:      &warning,
	}
}
