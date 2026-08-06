package cleanup

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/user/mirror-sync/internal/pypi"
	"github.com/user/mirror-sync/types"
)

// RemovedArtifactReason distinguishes why an artifact is being removed.
type RemovedArtifactReason string

const (
	// ReasonPackageRemoved marks artifacts of a package that no longer exists
	// in the new snapshot.
	ReasonPackageRemoved RemovedArtifactReason = "package-removed"
	// ReasonArtifactRemoved marks artifacts removed from a package that still
	// exists in the new snapshot.
	ReasonArtifactRemoved RemovedArtifactReason = "artifact-removed"
)

// RemovedArtifact is one file-level removal entry, pointing at a
// packages/... path relative to the old mirror root.
type RemovedArtifact struct {
	Package      string               `json:"package"`
	Filename     string               `json:"filename"`
	RelativePath string               `json:"relativePath"`
	Reason       RemovedArtifactReason `json:"reason"`
}

// Options configures cleanup script generation.
type Options struct {
	// Diff carries RemovedPackages and Removed (old→new removals, artifact
	// level). Removed is expected to include the artifacts of removed
	// packages as well as artifacts removed from surviving packages.
	Diff types.SnapshotDiff
	// Filter applies the same artifact filter used for downloads, so the
	// script only targets files that should exist in the (filtered) mirror.
	Filter types.PypiFilterOptions
	// CleanupRoot is the absolute path of the old mirror directory the
	// script removes files from. Written as CLEANUP_ROOT in the script.
	CleanupRoot string
	// OldDate / NewDate appear in the script header for traceability.
	OldDate    string
	NewDate    string
	GeneratedAt time.Time
}

// Result of Generate: the bash script content plus the structured removal
// list (also serialized as removed-artifacts.jsonl by the caller).
type Result struct {
	Script  string
	Removed []RemovedArtifact
	// Skipped counts removed artifacts that were excluded from the script
	// (filtered out, or non-packages/ paths).
	Skipped int
}

// Generate builds the cleanup script (bash, inline rm commands, never
// executed) and the flat removal list. Deletions are file-level only: one
// rm per packages/... path, no rm -rf / rmdir.
func Generate(opts Options) (Result, error) {
	removedPkgs := make(map[string]struct{}, len(opts.Diff.RemovedPackages))
	for _, name := range opts.Diff.RemovedPackages {
		removedPkgs[name] = struct{}{}
	}

	// Classify, filter, and validate every removed artifact.
	var removed []RemovedArtifact
	var skipped int
	for _, a := range opts.Diff.Removed {
		if !pypi.ShouldIncludeArtifact(a.Package, a.Filename, opts.Filter) {
			skipped++
			continue
		}
		if err := validateRelativePath(a.RelativePath); err != nil {
			skipped++
			continue
		}
		reason := ReasonArtifactRemoved
		if _, ok := removedPkgs[a.Package]; ok {
			reason = ReasonPackageRemoved
		}
		removed = append(removed, RemovedArtifact{
			Package:      a.Package,
			Filename:     a.Filename,
			RelativePath: a.RelativePath,
			Reason:       reason,
		})
	}

	sort.Slice(removed, func(i, j int) bool {
		if removed[i].Reason != removed[j].Reason {
			return removed[i].Reason < removed[j].Reason
		}
		return removed[i].RelativePath < removed[j].RelativePath
	})

	script := buildScript(opts, removed)
	return Result{Script: script, Removed: removed, Skipped: skipped}, nil
}

// validateRelativePath guards the script against path traversal and
// non-standard layouts: only relative paths under packages/ are accepted.
func validateRelativePath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return fmt.Errorf("non-relative path: %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path traversal: %q", p)
		}
	}
	if !strings.HasPrefix(p, "packages/") {
		return fmt.Errorf("non-packages path: %q", p)
	}
	return nil
}

// shellDoubleQuote wraps s in double quotes, escaping bash specials inside.
func shellDoubleQuote(s string) string {
	return `"` + escapeForDoubleQuotes(s) + `"`
}

// escapeForDoubleQuotes escapes bash specials for use inside "..." context.
func escapeForDoubleQuotes(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '$', '`', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func buildScript(opts Options, removed []RemovedArtifact) string {
	var pkgRemoved, artifactRemoved []RemovedArtifact
	for _, r := range removed {
		if r.Reason == ReasonPackageRemoved {
			pkgRemoved = append(pkgRemoved, r)
		} else {
			artifactRemoved = append(artifactRemoved, r)
		}
	}

	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("# mirror-sync incremental-download 生成的删除脚本（不会自动执行）\n")
	fmt.Fprintf(&b, "# 用途: 清理 %s -> %s 快照对比中已删除的内容\n", opts.OldDate, opts.NewDate)
	b.WriteString("# 目标根目录: CLEANUP_ROOT（下方变量，可修改后执行以重定向删除目标）\n")
	fmt.Fprintf(&b, "# 生成时间: %s\n", generatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "# 统计: 删除文件 %d 个（其中删除包 %d 个文件 / 共有包删除 %d 个文件）\n",
		len(removed), len(pkgRemoved), len(artifactRemoved))
	b.WriteString("# 注意: 执行前请人工审查；可先运行 bash -n 本脚本做语法检查\n")
	fmt.Fprintf(&b, "CLEANUP_ROOT=%s\n\n", shellDoubleQuote(opts.CleanupRoot))

	if len(pkgRemoved) > 0 {
		fmt.Fprintf(&b, "# ---- removed packages (%d packages, %d files) ----\n",
			countPackages(pkgRemoved), len(pkgRemoved))
		for _, r := range pkgRemoved {
			writeRm(&b, r.RelativePath)
		}
		b.WriteString("\n")
	}
	if len(artifactRemoved) > 0 {
		fmt.Fprintf(&b, "# ---- removed artifacts of existing packages (%d files) ----\n", len(artifactRemoved))
		for _, r := range artifactRemoved {
			writeRm(&b, r.RelativePath)
		}
		b.WriteString("\n")
	}

	b.WriteString("# 如需清理 packages/ 下空目录（人工执行）:\n")
	b.WriteString("# find \"${CLEANUP_ROOT}/packages\" -type d -empty -delete\n")
	return b.String()
}

func countPackages(removed []RemovedArtifact) int {
	set := make(map[string]struct{})
	for _, r := range removed {
		set[r.Package] = struct{}{}
	}
	return len(set)
}

func writeRm(b *strings.Builder, relativePath string) {
	// File-level deletion only. The root is referenced via ${CLEANUP_ROOT}
	// (relative path escaped separately), so operators can redirect the
	// script by editing the CLEANUP_ROOT variable.
	fmt.Fprintf(b, "rm -f -- \"${CLEANUP_ROOT}/%s\"\n", escapeForDoubleQuotes(relativePath))
}
