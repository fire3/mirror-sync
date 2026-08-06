package cleanup

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/user/mirror-sync/types"
)

// sampleDiff builds a diff with one removed package (oldpkg, 2 files) and one
// artifact removed from a surviving package (sharedpkg).
func sampleDiff() types.SnapshotDiff {
	return types.SnapshotDiff{
		RemovedPackages: []string{"oldpkg"},
		Removed: []types.ArtifactRecord{
			{Package: "oldpkg", Filename: "oldpkg-1.0.tar.gz", RelativePath: "packages/aa/bb/cc/oldpkg-1.0.tar.gz"},
			{Package: "oldpkg", Filename: "oldpkg-1.0-py3-none-any.whl", RelativePath: "packages/aa/bb/cc/oldpkg-1.0-py3-none-any.whl"},
			{Package: "sharedpkg", Filename: "sharedpkg-0.1.tar.gz", RelativePath: "packages/de/f0/12/sharedpkg-0.1.tar.gz"},
		},
	}
}

func TestGenerateClassifiesReasons(t *testing.T) {
	res, err := Generate(Options{
		Diff:         sampleDiff(),
		Filter:       defaultFilterAll(),
		CleanupRoot:  "/data/mirror/pypi/pypi-2025-07-24",
		OldDate:      "2025-07-24",
		NewDate:      "2025-07-25",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	byPath := map[string]RemovedArtifactReason{}
	for _, r := range res.Removed {
		byPath[r.RelativePath] = r.Reason
	}
	if byPath["packages/aa/bb/cc/oldpkg-1.0.tar.gz"] != ReasonPackageRemoved {
		t.Errorf("oldpkg file reason = %q, want package-removed", byPath["packages/aa/bb/cc/oldpkg-1.0.tar.gz"])
	}
	if byPath["packages/de/f0/12/sharedpkg-0.1.tar.gz"] != ReasonArtifactRemoved {
		t.Errorf("sharedpkg file reason = %q, want artifact-removed", byPath["packages/de/f0/12/sharedpkg-0.1.tar.gz"])
	}
	if len(res.Removed) != 3 {
		t.Errorf("len(Removed) = %d, want 3", len(res.Removed))
	}
}

func TestGenerateAppliesFilter(t *testing.T) {
	diff := sampleDiff()
	// Exclude source packages: the .tar.gz files must not appear.
	res, err := Generate(Options{
		Diff:         diff,
		Filter:       defaultFilterNoSource(),
		CleanupRoot:  "/data/mirror/pypi/pypi-2025-07-24",
		OldDate:      "2025-07-24",
		NewDate:      "2025-07-25",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("len(Removed) = %d, want 1 (only the whl)", len(res.Removed))
	}
	if res.Removed[0].RelativePath != "packages/aa/bb/cc/oldpkg-1.0-py3-none-any.whl" {
		t.Errorf("Removed[0] = %q, want the whl", res.Removed[0].RelativePath)
	}
	if res.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (both source files filtered)", res.Skipped)
	}
}

func TestGenerateSkipsNonPackagesPaths(t *testing.T) {
	diff := types.SnapshotDiff{
		RemovedPackages: []string{"odd"},
		Removed: []types.ArtifactRecord{
			{Package: "odd", Filename: "odd-1.0.tar.gz", RelativePath: "files/odd/odd-1.0.tar.gz"},
			{Package: "odd", Filename: "odd-2.0.tar.gz", RelativePath: "../escape/odd-2.0.tar.gz"},
			{Package: "odd", Filename: "odd-3.0.tar.gz", RelativePath: "/abs/path/odd-3.0.tar.gz"},
			{Package: "odd", Filename: "odd-4.0.tar.gz", RelativePath: "packages/ab/cd/odd-4.0.tar.gz"},
		},
	}
	res, err := Generate(Options{
		Diff:        diff,
		Filter:      defaultFilterAll(),
		CleanupRoot: "/data/mirror/pypi/pypi-2025-07-24",
		OldDate:     "2025-07-24",
		NewDate:     "2025-07-25",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0].RelativePath != "packages/ab/cd/odd-4.0.tar.gz" {
		t.Fatalf("Removed = %v, want only the packages/ path", res.Removed)
	}
	if res.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3 (invalid paths)", res.Skipped)
	}
}

func TestGenerateScriptEscapesSpecialChars(t *testing.T) {
	diff := types.SnapshotDiff{
		RemovedPackages: []string{"we!rd$pkg"},
		Removed: []types.ArtifactRecord{
			{Package: "we!rd$pkg", Filename: "we!rd$pkg-1.0+local.tar.gz",
				RelativePath: "packages/aa/bb/cc/we!rd$pkg-1.0+local.tar.gz"},
		},
	}
	res, err := Generate(Options{
		Diff:         diff,
		Filter:       defaultFilterAll(),
		CleanupRoot:  `/data/mirror/pypi/pypi-2025-07-24'quote`,
		OldDate:      "2025-07-24",
		NewDate:      "2025-07-25",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := `rm -f -- "${CLEANUP_ROOT}/packages/aa/bb/cc/we!rd\$pkg-1.0+local.tar.gz"`
	if !strings.Contains(res.Script, want) {
		t.Errorf("script 缺少转义后的 rm 行\nwant: %s\nscript:\n%s", want, res.Script)
	}
	if strings.Contains(res.Script, "rm -rf") {
		t.Error("script 不得包含 rm -rf（仅文件级删除）")
	}
	// The special-char script must still pass bash syntax checking.
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(res.Script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n 校验失败（特殊字符脚本）: %v\n%s\nscript:\n%s", err, out, res.Script)
	}
}

func TestGenerateScriptPassesBashSyntaxCheck(t *testing.T) {
	res, err := Generate(Options{
		Diff:         sampleDiff(),
		Filter:       defaultFilterAll(),
		CleanupRoot:  "/data/mirror/pypi/pypi-2025-07-24",
		OldDate:      "2025-07-24",
		NewDate:      "2025-07-25",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(res.Script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n 校验失败: %v\n%s\nscript:\n%s", err, out, res.Script)
	}
}

func defaultFilterAll() types.PypiFilterOptions {
	f := types.PypiFilterOptions{
		IncludeSource:       true,
		IncludePlatformAny:  true,
		IncludeLinuxAmd64:   true,
		IncludeWindowsAmd64: true,
	}
	return f
}

func defaultFilterNoSource() types.PypiFilterOptions {
	f := defaultFilterAll()
	f.IncludeSource = false
	return f
}
