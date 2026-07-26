package pypi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// linkRE matches <a> tags to extract package names from the simple index.
var linkRE = regexp.MustCompile(`<a\b[^>]*>([^<]+)</a>`)

func extractPackageNames(html string) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, match := range linkRE.FindAllStringSubmatch(html, -1) {
		if len(match) >= 2 {
			name := strings.TrimSpace(match[1])
			if name != "" {
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					names = append(names, name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// FetchPackageList fetches the PyPI simple index and extracts package names.
func FetchPackageList(simpleURL, userAgent string) ([]string, error) {
	req, err := http.NewRequest("GET", simpleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", simpleURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch package list from %s: %d", simpleURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	packages := extractPackageNames(string(body))
	return packages, nil
}

// FetchAndWritePackageList fetches the package list and writes it to a file.
func FetchAndWritePackageList(simpleURL, outputPath, userAgent string) ([]string, error) {
	packages, err := FetchPackageList(simpleURL, userAgent)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	content := strings.Join(packages, "\n") + "\n"
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	return packages, nil
}
