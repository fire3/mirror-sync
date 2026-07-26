package pypi

import (
	"testing"

	"github.com/user/mirror-sync/types"
)

// Real-world PyPI package filenames for smoke testing.
//
// These filenames are taken from actual PyPI releases of complex,
// multi-platform packages to verify the platform filter works correctly.

func TestDefaultFilter_SourceTarballs(t *testing.T) {
	// Source distributions should always be included when IncludeSource=true
	cases := []struct {
		pkg, file string
	}{
		{"numpy", "numpy-1.26.0.tar.gz"},
		{"numpy", "numpy-1.26.0.zip"},
		{"pandas", "pandas-2.1.0.tar.gz"},
		{"cryptography", "cryptography-41.0.0.tar.gz"},
		{"Pillow", "Pillow-10.1.0.tar.gz"},
		{"uwsgi", "uwsgi-2.0.23.tar.gz"},
		{"requests", "requests-2.31.0.tar.gz"},
		{"setuptools", "setuptools-68.2.0.tar.gz"},
		{"bcrypt", "bcrypt-4.1.0.tar.gz"},
		{"scipy", "scipy-1.11.3.tar.gz"},
		{"Apache-Airflow", "apache-airflow-2.8.0.tar.gz"},
		// Alternative source extensions
		{"lxml", "lxml-4.9.3.tar.bz2"},
		{"PyYAML", "PyYAML-6.0.1.tar.xz"},
		{"somepkg", "somepkg-1.0.tgz"},
	}
	for _, c := range cases {
		if !ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected source %s to be INCLUDED, but got EXCLUDED", c.file)
		}
	}
}

func TestDefaultFilter_PlatformAny(t *testing.T) {
	// py3-none-any wheels should be included
	cases := []struct {
		pkg, file string
	}{
		{"requests", "requests-2.31.0-py2.py3-none-any.whl"},
		{"certifi", "certifi-2023.7.22-py3-none-any.whl"},
		{"charset_normalizer", "charset_normalizer-3.3.2-py3-none-any.whl"},
		{"idna", "idna-3.4-py3-none-any.whl"},
		{"urllib3", "urllib3-2.0.7-py3-none-any.whl"},
		{"six", "six-1.16.0-py2.py3-none-any.whl"},
		{"pytz", "pytz-2023.3.post1-py3-none-any.whl"},
		{"colorama", "colorama-0.4.6-py2.py3-none-any.whl"},
	}
	for _, c := range cases {
		if !ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected platform-any %s to be INCLUDED, but got EXCLUDED", c.file)
		}
	}
}

func TestDefaultFilter_LinuxAmd64(t *testing.T) {
	// manylinux / linux x86_64 wheels should be included
	cases := []struct {
		pkg, file string
	}{
		{"numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
		{"numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
		{"pandas", "pandas-2.1.0-cp312-cp312-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
		{"cryptography", "cryptography-41.0.0-cp312-cp312-manylinux_2_28_x86_64.whl"},
		{"Pillow", "Pillow-10.1.0-cp312-cp312-manylinux_2_28_x86_64.whl"},
		{"scipy", "scipy-1.11.3-cp312-cp312-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
		{"bcrypt", "bcrypt-4.1.0-cp312-cp312-manylinux_2_28_x86_64.whl"},
		{"lxml", "lxml-4.9.3-cp312-cp312-manylinux_2_28_x86_64.whl"},
		{"uwsgi", "uwsgi-2.0.23-cp312-cp312-linux_x86_64.whl"},
	}
	for _, c := range cases {
		if !ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected linux-amd64 %s to be INCLUDED, but got EXCLUDED", c.file)
		}
	}
}

func TestDefaultFilter_WindowsAmd64(t *testing.T) {
	// win_amd64 wheels should be included
	cases := []struct {
		pkg, file string
	}{
		{"numpy", "numpy-1.26.0-cp312-cp312-win_amd64.whl"},
		{"pandas", "pandas-2.1.0-cp312-cp312-win_amd64.whl"},
		{"cryptography", "cryptography-41.0.0-cp312-cp312-win_amd64.whl"},
		{"Pillow", "Pillow-10.1.0-cp312-cp312-win_amd64.whl"},
		{"scipy", "scipy-1.11.3-cp312-cp312-win_amd64.whl"},
		{"bcrypt", "bcrypt-4.1.0-cp312-cp312-win_amd64.whl"},
		{"lxml", "lxml-4.9.3-cp312-cp312-win_amd64.whl"},
	}
	for _, c := range cases {
		if !ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected win-amd64 %s to be INCLUDED, but got EXCLUDED", c.file)
		}
	}
}

// ── Exclusions ──────────────────────────────────────────────────────────────

func TestDefaultFilter_ExcludeMusllinux(t *testing.T) {
	// musllinux wheels should be excluded
	cases := []struct {
		pkg, file string
	}{
		{"cryptography", "cryptography-41.0.0-cp312-cp312-musllinux_1_1_x86_64.whl"},
		{"cryptography", "cryptography-41.0.0-cp312-cp312-musllinux_1_1_aarch64.whl"},
		{"bcrypt", "bcrypt-4.1.0-cp312-cp312-musllinux_1_1_x86_64.whl"},
		{"lxml", "lxml-4.9.3-cp312-cp312-musllinux_1_1_x86_64.whl"},
	}
	for _, c := range cases {
		if ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected musllinux %s to be EXCLUDED, but got INCLUDED", c.file)
		}
	}
}

func TestDefaultFilter_ExcludeMacos(t *testing.T) {
	// macOS wheels should be excluded
	cases := []struct {
		pkg, file string
	}{
		{"numpy", "numpy-1.26.0-cp312-cp312-macosx_10_9_x86_64.whl"},
		{"numpy", "numpy-1.26.0-cp312-cp312-macosx_11_0_arm64.whl"},
		{"pandas", "pandas-2.1.0-cp312-cp312-macosx_11_0_arm64.whl"},
		{"cryptography", "cryptography-41.0.0-cp312-cp312-macosx_10_9_x86_64.whl"},
		{"Pillow", "Pillow-10.1.0-cp312-cp312-macosx_10_9_x86_64.whl"},
		{"scipy", "scipy-1.11.3-cp312-cp312-macosx_12_0_arm64.whl"},
		{"bcrypt", "bcrypt-4.1.0-cp312-cp312-macosx_10_9_x86_64.whl"},
		{"lxml", "lxml-4.9.3-cp312-cp312-macosx_10_9_x86_64.whl"},
	}
	for _, c := range cases {
		if ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected macOS %s to be EXCLUDED, but got INCLUDED", c.file)
		}
	}
}

func TestDefaultFilter_ExcludeArm(t *testing.T) {
	// ARM wheels (aarch64/armv7) should be excluded, even on linux
	cases := []struct {
		pkg, file string
	}{
		// Linux aarch64
		{"numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_aarch64.manylinux2014_aarch64.whl"},
		{"pandas", "pandas-2.1.0-cp312-cp312-manylinux_2_17_aarch64.manylinux2014_aarch64.whl"},
		{"scipy", "scipy-1.11.3-cp312-cp312-manylinux_2_17_aarch64.manylinux2014_aarch64.whl"},
		// Linux armv7
		{"somepkg", "somepkg-1.0-cp312-cp312-linux_armv7l.whl"},
	}
	for _, c := range cases {
		if ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected ARM %s to be EXCLUDED, but got INCLUDED", c.file)
		}
	}
}

func TestDefaultFilter_ExcludeLessCommon(t *testing.T) {
	// Less common platform/arch combos should be excluded
	cases := []struct {
		pkg, file string
	}{
		// win32 (32-bit Windows)
		{"numpy", "numpy-1.26.0-cp312-cp312-win32.whl"},
		// linux i686 (32-bit Linux)
		{"uwsgi", "uwsgi-2.0.23-cp312-cp312-linux_i686.whl"},
		// Linux with unknown arch (ppc64le, s390x)
		{"numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_ppc64le.manylinux2014_ppc64le.whl"},
		{"scipy", "scipy-1.11.3-cp312-cp312-manylinux_2_17_s390x.manylinux2014_s390x.whl"},
		// FreeBSD
		{"somepkg", "somepkg-1.0-cp312-cp312-freebsd_12_x86_64.whl"},
		// NetBSD
		{"somepkg", "somepkg-1.0-cp312-cp312-netbsd_9_x86_64.whl"},
		// Non-wheel, non-source (should return false via parseWheelPlatform returning nil)
		{"numpy", "numpy-1.26.0-1.x86_64.rpm"},
		{"numpy", "numpy-1.26.0.deb"},
	}
	for _, c := range cases {
		if ShouldIncludeFilename(c.pkg, c.file, DefaultFilterOptions) {
			t.Errorf("expected less-common %s to be EXCLUDED, but got INCLUDED", c.file)
		}
	}
}

// ── Package name filtering ──────────────────────────────────────────────────

func TestFilter_IncludePackages(t *testing.T) {
	opts := DefaultFilterOptions
	opts.IncludePackages = []string{"numpy", "pandas"}

	// numpy and pandas should be included
	if !ShouldIncludeFilename("numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.whl", opts) {
		t.Error("expected numpy file to be INCLUDED with IncludePackages=[numpy,pandas]")
	}
	if !ShouldIncludeFilename("pandas", "pandas-2.1.0-cp312-cp312-manylinux_2_17_x86_64.whl", opts) {
		t.Error("expected pandas file to be INCLUDED with IncludePackages=[numpy,pandas]")
	}
	// scipy should be excluded
	if ShouldIncludeFilename("scipy", "scipy-1.11.3-cp312-cp312-manylinux_2_17_x86_64.whl", opts) {
		t.Error("expected scipy file to be EXCLUDED with IncludePackages=[numpy,pandas]")
	}
}

func TestFilter_ExcludePackages(t *testing.T) {
	opts := DefaultFilterOptions
	opts.ExcludePackages = []string{"badpkg", "oldpkg"}

	// badpkg should be excluded
	if ShouldIncludeFilename("badpkg", "badpkg-1.0-cp312-cp312-manylinux_2_17_x86_64.whl", opts) {
		t.Error("expected badpkg file to be EXCLUDED")
	}
	// normal package still included
	if !ShouldIncludeFilename("numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.whl", opts) {
		t.Error("expected numpy file to be INCLUDED")
	}
}

// ── Edge cases ──────────────────────────────────────────────────────────────

func TestFilter_DisableSource(t *testing.T) {
	opts := DefaultFilterOptions
	opts.IncludeSource = false

	if ShouldIncludeFilename("numpy", "numpy-1.26.0.tar.gz", opts) {
		t.Error("expected source tarball to be EXCLUDED when IncludeSource=false")
	}
	// Wheels should still work
	if !ShouldIncludeFilename("numpy", "numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.whl", opts) {
		t.Error("expected wheel to still be INCLUDED when IncludeSource=false")
	}
}

func TestFilter_DisableAll(t *testing.T) {
	opts := types.PypiFilterOptions{
		IncludeSource:       false,
		IncludePlatformAny:  false,
		IncludeLinuxAmd64:   false,
		IncludeWindowsAmd64: false,
	}

	// Everything should be excluded
	cases := []string{
		"numpy-1.26.0.tar.gz",
		"requests-2.31.0-py3-none-any.whl",
		"numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.whl",
		"numpy-1.26.0-cp312-cp312-win_amd64.whl",
	}
	for _, f := range cases {
		if ShouldIncludeFilename("numpy", f, opts) {
			t.Errorf("expected %s to be EXCLUDED with all filters disabled", f)
		}
	}
}

// TestNumpySmokeCheck uses real numpy 1.26.0 filenames to verify the
// default filter produces the expected set of downloads.
func TestNumpySmokeCheck(t *testing.T) {
	// Real numpy 1.26.0 artifacts from PyPI
	artifacts := map[string]bool{
		// Should be included
		"numpy-1.26.0.tar.gz":                                                  true,
		"numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.manylinux2014_x86_64.whl": true,
		"numpy-1.26.0-cp312-cp312-win_amd64.whl":                               true,
		// Should be excluded
		"numpy-1.26.0-cp312-cp312-macosx_10_9_x86_64.whl":                                                   false,
		"numpy-1.26.0-cp312-cp312-macosx_11_0_arm64.whl":                                                    false,
		"numpy-1.26.0-cp312-cp312-manylinux_2_17_aarch64.manylinux2014_aarch64.whl":                         false,
		"numpy-1.26.0-cp312-cp312-manylinux_2_17_ppc64le.manylinux2014_ppc64le.whl":                         false,
		"numpy-1.26.0-cp312-cp312-manylinux_2_17_s390x.manylinux2014_s390x.whl":                             false,
		"numpy-1.26.0-cp312-cp312-win32.whl":                                                                false,
		"numpy-1.26.0-cp312-cp312-musllinux_1_1_x86_64.whl":                                                false,
	}

	for file, expectIncluded := range artifacts {
		result := ShouldIncludeFilename("numpy", file, DefaultFilterOptions)
		if result != expectIncluded {
			status := "EXCLUDED"
			if expectIncluded {
				status = "INCLUDED"
			}
			t.Errorf("numpy artifact %s: expected %s, got opposite", file, status)
		}
	}
}

// TestPandasSmokeCheck — similar check for pandas 2.1.0
func TestPandasSmokeCheck(t *testing.T) {
	artifacts := map[string]bool{
		"pandas-2.1.0.tar.gz":                                                     true,
		"pandas-2.1.0-py3-none-any.whl":                                           true,
		"pandas-2.1.0-cp312-cp312-manylinux_2_17_x86_64.manylinux2014_x86_64.whl": true,
		"pandas-2.1.0-cp312-cp312-win_amd64.whl":                                  true,
		// Excluded
		"pandas-2.1.0-cp312-cp312-macosx_11_0_arm64.whl":                          false,
		"pandas-2.1.0-cp312-cp312-manylinux_2_17_aarch64.manylinux2014_aarch64.whl": false,
		"pandas-2.1.0-cp312-cp312-win32.whl":                                       false,
	}

	for file, expectIncluded := range artifacts {
		result := ShouldIncludeFilename("pandas", file, DefaultFilterOptions)
		if result != expectIncluded {
			status := "INCLUDED"
			if expectIncluded {
				status = "EXCLUDED"
			}
			t.Errorf("pandas artifact %s: expected %s, got opposite", file, status)
		}
	}
}

// TestNumpyRealFilenames uses ACTUAL filenames fetched live from PyPI
// (numpy 2.5.1) to verify the filter against real-world data.
func TestNumpyRealFilenames(t *testing.T) {
	// These are the real filenames for numpy 2.5.1 fetched from
	// https://pypi.org/simple/numpy/ on 2026-07-26.
	artifacts := map[string]bool{
		// ── Should be INCLUDED ──
		"numpy-2.5.1-cp312-cp312-manylinux_2_27_x86_64.manylinux_2_28_x86_64.whl": true,
		"numpy-2.5.1-cp313-cp313-manylinux_2_27_x86_64.manylinux_2_28_x86_64.whl": true,
		"numpy-2.5.1-cp314-cp314-manylinux_2_27_x86_64.manylinux_2_28_x86_64.whl": true,
		"numpy-2.5.1-cp314-cp314t-manylinux_2_27_x86_64.manylinux_2_28_x86_64.whl": true,
		"numpy-2.5.1-cp312-cp312-win_amd64.whl":  true,
		"numpy-2.5.1-cp313-cp313-win_amd64.whl":  true,
		"numpy-2.5.1-cp314-cp314-win_amd64.whl":  true,
		"numpy-2.5.1-cp314-cp314t-win_amd64.whl": true,
		"numpy-2.5.1.tar.gz": true,

		// ── Should be EXCLUDED ──
		// macOS
		"numpy-2.5.1-cp313-cp313-macosx_10_13_x86_64.whl": false,
		"numpy-2.5.1-cp313-cp313-macosx_11_0_arm64.whl":   false,
		"numpy-2.5.1-cp313-cp313-macosx_14_0_arm64.whl":   false,
		"numpy-2.5.1-cp313-cp313-macosx_14_0_x86_64.whl":  false,
		"numpy-2.5.1-cp314-cp314-macosx_10_15_x86_64.whl": false,
		"numpy-2.5.1-cp314-cp314-macosx_11_0_arm64.whl":   false,
		"numpy-2.5.1-cp314-cp314-macosx_14_0_arm64.whl":   false,
		"numpy-2.5.1-cp314-cp314-macosx_14_0_x86_64.whl":  false,
		"numpy-2.5.1-cp314-cp314t-macosx_11_0_arm64.whl":  false,
		"numpy-2.5.1-cp314-cp314t-macosx_14_0_arm64.whl":  false,
		"numpy-2.5.1-cp314-cp314t-macosx_14_0_x86_64.whl": false,
		// ARM linux
		"numpy-2.5.1-cp312-cp312-manylinux_2_27_aarch64.manylinux_2_28_aarch64.whl": false,
		"numpy-2.5.1-cp313-cp313-manylinux_2_27_aarch64.manylinux_2_28_aarch64.whl": false,
		"numpy-2.5.1-cp314-cp314-manylinux_2_27_aarch64.manylinux_2_28_aarch64.whl": false,
		"numpy-2.5.1-cp314-cp314t-manylinux_2_27_aarch64.manylinux_2_28_aarch64.whl": false,
		// musllinux
		"numpy-2.5.1-cp312-cp312-musllinux_1_2_x86_64.whl": false,
		"numpy-2.5.1-cp313-cp313-musllinux_1_2_x86_64.whl": false,
		"numpy-2.5.1-cp314-cp314-musllinux_1_2_x86_64.whl": false,
		"numpy-2.5.1-cp314-cp314t-musllinux_1_2_x86_64.whl": false,
		// ARM musllinux
		"numpy-2.5.1-cp312-cp312-musllinux_1_2_aarch64.whl": false,
		"numpy-2.5.1-cp313-cp313-musllinux_1_2_aarch64.whl": false,
		"numpy-2.5.1-cp314-cp314-musllinux_1_2_aarch64.whl": false,
		"numpy-2.5.1-cp314-cp314t-musllinux_1_2_aarch64.whl": false,
		// win32 (32-bit)
		"numpy-2.5.1-cp312-cp312-win32.whl":  false,
		"numpy-2.5.1-cp313-cp313-win32.whl":  false,
		"numpy-2.5.1-cp314-cp314-win32.whl":  false,
		"numpy-2.5.1-cp314-cp314t-win32.whl": false,
		// win_arm64
		"numpy-2.5.1-cp312-cp312-win_arm64.whl":  false,
		"numpy-2.5.1-cp313-cp313-win_arm64.whl":  false,
		"numpy-2.5.1-cp314-cp314-win_arm64.whl":  false,
		"numpy-2.5.1-cp314-cp314t-win_arm64.whl": false,
	}

	for file, expectIncluded := range artifacts {
		result := ShouldIncludeFilename("numpy", file, DefaultFilterOptions)
		if result != expectIncluded {
			status := "INCLUDED"
			if expectIncluded {
				status = "EXCLUDED"
			}
			t.Errorf("numpy-2.5.1 %s: expected %s, got opposite", file, status)
		}
	}
}
