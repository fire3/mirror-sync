package pypi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/user/mirror-sync/types"
)

// BuildDownloadPlanFromSnapshotOptions configures plan building from a snapshot.
type BuildDownloadPlanFromSnapshotOptions struct {
	SnapshotRoot   string
	SimpleBaseURL  string
	MirrorRoot     string
	Filter         *types.PypiFilterOptions
	CompletedPaths map[string]struct{}
	NotFoundPaths  map[string]struct{}
}

type jsonFileRecordSimple struct {
	Filename string            `json:"filename"`
	URL      string            `json:"url"`
	Hashes   map[string]string `json:"hashes,omitempty"`
}

type jsonIndexPayloadSimple struct {
	Files []jsonFileRecordSimple `json:"files,omitempty"`
}

// BuildDownloadPlanFromSnapshot builds a download plan by iterating over the snapshot directory.
func BuildDownloadPlanFromSnapshot(opts BuildDownloadPlanFromSnapshotOptions) (types.DownloadPlan, error) {
	filter := opts.Filter
	if filter == nil {
		f := DefaultFilterOptions
		filter = &f
	}
	completedPaths := opts.CompletedPaths
	if completedPaths == nil {
		completedPaths = make(map[string]struct{})
	}
	notFoundPaths := opts.NotFoundPaths
	if notFoundPaths == nil {
		notFoundPaths = make(map[string]struct{})
	}

	var (
		skippedExisting   []string
		skippedCheckpoint []string
		skippedNotFound   []string
		entries           []types.DownloadPlanEntry
	)

	simpleRoot := filepath.Join(opts.SnapshotRoot, "simple")
	dirEntries, err := os.ReadDir(simpleRoot)
	if err != nil {
		return types.DownloadPlan{}, fmt.Errorf("read simple root: %w", err)
	}

	var pkgDirs []string
	for _, entry := range dirEntries {
		if entry.IsDir() {
			pkgDirs = append(pkgDirs, entry.Name())
		}
	}
	sort.Strings(pkgDirs)

	for _, pkgName := range pkgDirs {
		pkgRoot := filepath.Join(simpleRoot, pkgName)
		htmlPath := filepath.Join(pkgRoot, "index.html")
		jsonPath := filepath.Join(pkgRoot, "index_v1.json")
		simplePageURL := buildPackageURL(opts.SimpleBaseURL, pkgName)
		seenPaths := make(map[string]struct{})

		// Read JSON metadata
		if jsonData, err := os.ReadFile(jsonPath); err == nil {
			var payload jsonIndexPayloadSimple
			if err := json.Unmarshal(jsonData, &payload); err == nil {
				for _, file := range payload.Files {
					resolved := ResolveArtifactPath(pkgName, simplePageURL, file.URL)
					seenPaths[resolved.RelativePath] = struct{}{}

					entry := types.DownloadPlanEntry{
						Package:         pkgName,
						Filename:        file.Filename,
						RelativePath:    resolved.RelativePath,
						DestinationPath: filepath.Join(opts.MirrorRoot, resolved.RelativePath),
						URL:             resolved.RemoteURL,
						Reason:          "full-sync",
					}
					if h := firstHash(file.Hashes); h != "" {
						entry.Hash = &h
					}

					addEntry(&entry, *filter, completedPaths, notFoundPaths,
						&skippedExisting, &skippedCheckpoint, &skippedNotFound, &entries)
				}
			}
		}

		// Read HTML metadata for additional entries
		if htmlData, err := os.ReadFile(htmlPath); err == nil {
			for _, item := range ParseSimpleIndexHTML(string(htmlData)) {
				resolved := ResolveArtifactPath(pkgName, simplePageURL, item.Href)
				if _, seen := seenPaths[resolved.RelativePath]; seen {
					continue
				}

				hash := ""
				if idx := strings.IndexByte(item.Href, '#'); idx >= 0 {
					hashStr := item.Href[idx+1:]
					hash = strings.ReplaceAll(hashStr, "=", ":")
				}

				entry := types.DownloadPlanEntry{
					Package:         pkgName,
					Filename:        item.Filename,
					RelativePath:    resolved.RelativePath,
					DestinationPath: filepath.Join(opts.MirrorRoot, resolved.RelativePath),
					URL:             resolved.RemoteURL,
					Reason:          "full-sync",
				}
				if hash != "" {
					entry.Hash = &hash
				}

				addEntry(&entry, *filter, completedPaths, notFoundPaths,
					&skippedExisting, &skippedCheckpoint, &skippedNotFound, &entries)
			}
		}
	}

	return types.DownloadPlan{
		Entries:           entries,
		SkippedExisting:   skippedExisting,
		SkippedCheckpoint: skippedCheckpoint,
		SkippedNotFound:   skippedNotFound,
	}, nil
}

func addEntry(entry *types.DownloadPlanEntry, filter types.PypiFilterOptions,
	completedPaths, notFoundPaths map[string]struct{},
	skippedExisting, skippedCheckpoint, skippedNotFound *[]string,
	entries *[]types.DownloadPlanEntry) {

	if !ShouldIncludeArtifact(entry.Package, entry.Filename, filter) {
		return
	}
	if _, ok := completedPaths[entry.RelativePath]; ok {
		*skippedCheckpoint = append(*skippedCheckpoint, entry.RelativePath)
		return
	}
	if _, ok := notFoundPaths[entry.RelativePath]; ok {
		*skippedNotFound = append(*skippedNotFound, entry.RelativePath)
		return
	}
	if _, err := os.Stat(entry.DestinationPath); err == nil {
		*skippedExisting = append(*skippedExisting, entry.RelativePath)
		return
	}
	*entries = append(*entries, *entry)
}
