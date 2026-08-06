package planner

import (
	"slices"
	"testing"

	"github.com/user/mirror-sync/types"
)

func TestDiffSnapshotManifestsPackageLevel(t *testing.T) {
	oldManifest := types.SnapshotManifest{
		Packages: []types.PackageRecord{
			{Name: "pkgA"},
			{Name: "pkgB"},
		},
		Artifacts: []types.ArtifactRecord{
			{Package: "pkgA", Filename: "pkgA-1.0.tar.gz", RelativePath: "packages/aa/bb/cc/pkgA-1.0.tar.gz"},
			{Package: "pkgB", Filename: "pkgB-1.0.whl", RelativePath: "packages/ab/cd/ef/pkgB-1.0.whl"},
		},
	}
	newManifest := types.SnapshotManifest{
		Packages: []types.PackageRecord{
			{Name: "pkgB"},
			{Name: "pkgC"},
		},
		Artifacts: []types.ArtifactRecord{
			{Package: "pkgB", Filename: "pkgB-1.0.whl", RelativePath: "packages/ab/cd/ef/pkgB-1.0.whl"},
			{Package: "pkgC", Filename: "pkgC-1.0.tar.gz", RelativePath: "packages/de/f0/12/pkgC-1.0.tar.gz"},
		},
	}

	diff := DiffSnapshotManifests(oldManifest, newManifest)

	if !slices.Equal(diff.AddedPackages, []string{"pkgC"}) {
		t.Errorf("AddedPackages = %v, want [pkgC]", diff.AddedPackages)
	}
	if !slices.Equal(diff.RemovedPackages, []string{"pkgA"}) {
		t.Errorf("RemovedPackages = %v, want [pkgA]", diff.RemovedPackages)
	}
	// Artifact level: pkgA's file and pkgC's file differ; pkgB's file unchanged.
	if len(diff.Added) != 1 || diff.Added[0].Package != "pkgC" {
		t.Errorf("Added = %v, want [pkgC artifact]", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].Package != "pkgA" {
		t.Errorf("Removed = %v, want [pkgA artifact]", diff.Removed)
	}
	if len(diff.Changed) != 0 {
		t.Errorf("Changed = %v, want []", diff.Changed)
	}
	if len(diff.Unchanged) != 1 || diff.Unchanged[0].Package != "pkgB" {
		t.Errorf("Unchanged = %v, want [pkgB artifact]", diff.Unchanged)
	}
}

func TestDiffSnapshotManifestsPackageFromArtifacts(t *testing.T) {
	// Manifests without Packages records derive package names from artifacts.
	oldManifest := types.SnapshotManifest{
		Artifacts: []types.ArtifactRecord{{Package: "pkgA", RelativePath: "packages/a/1/pkgA.whl"}},
	}
	newManifest := types.SnapshotManifest{
		Artifacts: []types.ArtifactRecord{{Package: "pkgB", RelativePath: "packages/b/2/pkgB.whl"}},
	}

	diff := DiffSnapshotManifests(oldManifest, newManifest)

	if !slices.Equal(diff.AddedPackages, []string{"pkgB"}) {
		t.Errorf("AddedPackages = %v, want [pkgB]", diff.AddedPackages)
	}
	if !slices.Equal(diff.RemovedPackages, []string{"pkgA"}) {
		t.Errorf("RemovedPackages = %v, want [pkgA]", diff.RemovedPackages)
	}
}

func TestDiffSnapshotManifestsSortedOutput(t *testing.T) {
	oldManifest := types.SnapshotManifest{
		Packages: []types.PackageRecord{{Name: "z"}, {Name: "a"}},
	}
	newManifest := types.SnapshotManifest{
		Packages: []types.PackageRecord{{Name: "b"}, {Name: "z"}},
	}

	diff := DiffSnapshotManifests(oldManifest, newManifest)

	if !slices.Equal(diff.AddedPackages, []string{"b"}) {
		t.Errorf("AddedPackages = %v, want [b]", diff.AddedPackages)
	}
	if !slices.Equal(diff.RemovedPackages, []string{"a"}) {
		t.Errorf("RemovedPackages = %v, want [a]", diff.RemovedPackages)
	}
}
