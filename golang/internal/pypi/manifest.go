package pypi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/user/mirror-sync/internal/core/storage"
	"github.com/user/mirror-sync/types"
)

// BuildManifestOptions configures manifest building.
type BuildManifestOptions struct {
	SnapshotRoot  string
	SimpleBaseURL string
	WriteOutputs  bool
}

type jsonFileRecord struct {
	Filename       string             `json:"filename"`
	URL            string             `json:"url"`
	Hashes         map[string]string  `json:"hashes,omitempty"`
	RequiresPython *string            `json:"requires-python,omitempty"`
	Yanked         interface{}        `json:"yanked,omitempty"`
	UploadTime     *string            `json:"upload_time,omitempty"`
}

type jsonIndexPayload struct {
	Files []jsonFileRecord `json:"files,omitempty"`
}

// BuildManifestFromSnapshot builds a snapshot manifest by reading snapshot directory.
func BuildManifestFromSnapshot(opts BuildManifestOptions) (types.SnapshotManifest, error) {
	snapshotID := snapshotIDFromPath(opts.SnapshotRoot)
	simpleRoot := filepath.Join(opts.SnapshotRoot, "simple")
	writeOutputs := opts.WriteOutputs

	packagesPath := filepath.Join(opts.SnapshotRoot, "manifests", "packages.jsonl")
	artifactsPath := filepath.Join(opts.SnapshotRoot, "manifests", "artifacts.jsonl")

	entries, err := os.ReadDir(simpleRoot)
	if err != nil {
		return types.SnapshotManifest{}, fmt.Errorf("read simple root: %w", err)
	}

	var pkgDirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			pkgDirs = append(pkgDirs, entry.Name())
		}
	}
	sort.Strings(pkgDirs)

	var packages []types.PackageRecord
	artifactsMap := make(map[string]types.ArtifactRecord)
	packagesWithHTML := 0
	packagesWithJSON := 0
	artifactsTotal := 0

	if writeOutputs {
		// Clear output files
		_ = os.MkdirAll(filepath.Dir(packagesPath), 0755)
		_ = os.WriteFile(packagesPath, []byte{}, 0644)
		_ = os.WriteFile(artifactsPath, []byte{}, 0644)
	}

	for _, pkgName := range pkgDirs {
		pkgRoot := filepath.Join(simpleRoot, pkgName)
		htmlPath := filepath.Join(pkgRoot, "index.html")
		jsonPath := filepath.Join(pkgRoot, "index_v1.json")
		simplePageURL := buildPackageURL(opts.SimpleBaseURL, pkgName)

		pkgArtifacts := make(map[string]types.ArtifactRecord)

		// Try JSON first
		if jsonData, err := os.ReadFile(jsonPath); err == nil {
			var payload jsonIndexPayload
			if err := json.Unmarshal(jsonData, &payload); err == nil {
				for _, file := range payload.Files {
					resolved := ResolveArtifactPath(pkgName, simplePageURL, file.URL)
					hash := firstHash(file.Hashes)

					record := types.ArtifactRecord{
						Package:      pkgName,
						Filename:     file.Filename,
						RelativePath: resolved.RelativePath,
						URL:          resolved.RemoteURL,
						Source:       types.ArtifactSourceJSON,
						SnapshotID:   snapshotID,
					}
					if hash != "" {
						record.Hash = &hash
					}
					if file.RequiresPython != nil {
						record.RequiresPython = file.RequiresPython
					}
					if file.Yanked != nil {
						record.Yanked = file.Yanked
					}
					if file.UploadTime != nil {
						record.UploadTime = file.UploadTime
					}
					pkgArtifacts[resolved.RelativePath] = record
					artifactsMap[resolved.RelativePath] = record
				}
			}
		}

		// Try HTML for entries not already found in JSON
		if htmlData, err := os.ReadFile(htmlPath); err == nil {
			for _, entry := range ParseSimpleIndexHTML(string(htmlData)) {
				resolved := ResolveArtifactPath(pkgName, simplePageURL, entry.Href)
				if _, exists := pkgArtifacts[resolved.RelativePath]; exists {
					continue
				}

				hash := ""
				if idx := strings.IndexByte(entry.Href, '#'); idx >= 0 {
					hashStr := entry.Href[idx+1:]
					hash = strings.ReplaceAll(hashStr, "=", ":")
				}

				record := types.ArtifactRecord{
					Package:      pkgName,
					Filename:     entry.Filename,
					RelativePath: resolved.RelativePath,
					URL:          resolved.RemoteURL,
					Source:       types.ArtifactSourceHTML,
					SnapshotID:   snapshotID,
				}
				if hash != "" {
					record.Hash = &hash
				}
				if entry.RequiresPython != nil {
					record.RequiresPython = entry.RequiresPython
				}
				if entry.Yanked != nil {
					record.Yanked = entry.Yanked
				}
				pkgArtifacts[resolved.RelativePath] = record
				artifactsMap[resolved.RelativePath] = record
			}
		}

		htmlPresent := false
		jsonPresent := false
		if _, err := os.Stat(htmlPath); err == nil {
			htmlPresent = true
			packagesWithHTML++
		}
		if _, err := os.Stat(jsonPath); err == nil {
			jsonPresent = true
			packagesWithJSON++
		}

		pkgRecord := types.PackageRecord{
			Name:          pkgName,
			SnapshotID:    snapshotID,
			HTMLPresent:   htmlPresent,
			JSONPresent:   jsonPresent,
			ArtifactCount: len(pkgArtifacts),
		}
		artifactsTotal += len(pkgArtifacts)

		if writeOutputs {
			if err := storage.AppendJSONLine(packagesPath, pkgRecord); err != nil {
				return types.SnapshotManifest{}, err
			}
			for _, a := range pkgArtifacts {
				if err := storage.AppendJSONLine(artifactsPath, a); err != nil {
					return types.SnapshotManifest{}, err
				}
			}
		} else {
			packages = append(packages, pkgRecord)
		}
	}

	var sortedArtifacts []types.ArtifactRecord
	if !writeOutputs {
		for _, a := range artifactsMap {
			sortedArtifacts = append(sortedArtifacts, a)
		}
		sort.Slice(sortedArtifacts, func(i, j int) bool {
			return sortedArtifacts[i].RelativePath < sortedArtifacts[j].RelativePath
		})
	}

	stats := types.SnapshotStats{
		PackagesTotal:    len(pkgDirs),
		PackagesWithHTML: packagesWithHTML,
		PackagesWithJSON: packagesWithJSON,
		ArtifactsTotal:   artifactsTotal,
	}

	manifest := types.SnapshotManifest{
		SnapshotID: snapshotID,
		Packages:   packages,
		Artifacts:  sortedArtifacts,
		Stats:      stats,
	}

	if writeOutputs {
		if err := storage.WriteJSONFile(filepath.Join(opts.SnapshotRoot, "stats.json"), stats); err != nil {
			return types.SnapshotManifest{}, err
		}
		if err := storage.WriteJSONFile(filepath.Join(opts.SnapshotRoot, "manifest.json"), manifest); err != nil {
			return types.SnapshotManifest{}, err
		}
	}

	return manifest, nil
}

func snapshotIDFromPath(snapshotRoot string) string {
	parts := strings.Split(strings.TrimRight(snapshotRoot, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "snapshot"
}

func firstHash(hashes map[string]string) string {
	for algorithm, value := range hashes {
		return algorithm + ":" + value
	}
	return ""
}

func buildPackageURL(simpleBaseURL, packageName string) string {
	normalized := simpleBaseURL
	if !strings.HasSuffix(normalized, "/") {
		normalized += "/"
	}
	// URL-encode the package name
	encodedName := urlPathEscape(packageName)
	return normalized + encodedName + "/"
}

func urlPathEscape(s string) string {
	// Simple percent-encoding for URL path segment
	var result strings.Builder
	for _, b := range []byte(s) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '_' || b == '.' || b == '~' {
			result.WriteByte(b)
		} else {
			result.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return result.String()
}
