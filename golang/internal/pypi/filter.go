package pypi

import (
	"strings"

	"github.com/user/mirror-sync/types"
)

// Source extensions for source distributions.
var sourceExtensions = []string{".tar.gz", ".zip", ".tar.bz2", ".tar.xz", ".tgz"}

// DefaultFilterOptions are the default PyPI filter options.
var DefaultFilterOptions = types.PypiFilterOptions{
	IncludeSource:       true,
	IncludePlatformAny:  true,
	IncludeLinuxAmd64:   true,
	IncludeWindowsAmd64: true,
	ExcludeMusllinux:    true,
	ExcludeMacos:        true,
	ExcludeArm:          true,
}

type wheelPlatform struct {
	platform string // "any", "linux", "win", "macos", "other"
	arch     string // "any", "amd64", "x86", "arm64", "arm", "other"
	rawTag   string
}

func parseWheelPlatform(filename string) *wheelPlatform {
	if !strings.HasSuffix(filename, ".whl") {
		return nil
	}
	base := filename[:len(filename)-4]
	parts := strings.Split(base, "-")
	if len(parts) == 0 {
		return nil
	}
	rawTag := strings.ToLower(parts[len(parts)-1])

	if rawTag == "any" {
		return &wheelPlatform{platform: "any", arch: "any", rawTag: rawTag}
	}

	var arch string
	switch {
	case strings.Contains(rawTag, "x86_64") || strings.Contains(rawTag, "amd64"):
		arch = "amd64"
	case strings.Contains(rawTag, "aarch64") || strings.Contains(rawTag, "arm64"):
		arch = "arm64"
	case strings.Contains(rawTag, "armv7") || strings.Contains(rawTag, "arm"):
		arch = "arm"
	case strings.Contains(rawTag, "i686") || strings.Contains(rawTag, "win32"):
		arch = "x86"
	default:
		arch = "other"
	}

	var platform string
	switch {
	case strings.Contains(rawTag, "manylinux") || strings.Contains(rawTag, "linux") || strings.Contains(rawTag, "musllinux"):
		platform = "linux"
	case strings.Contains(rawTag, "win"):
		platform = "win"
	case strings.Contains(rawTag, "macosx"):
		platform = "macos"
	default:
		platform = "other"
	}

	return &wheelPlatform{platform: platform, arch: arch, rawTag: rawTag}
}

// ShouldIncludeFilename checks if a filename should be included based on filter options.
func ShouldIncludeFilename(packageName, filename string, opts types.PypiFilterOptions) bool {
	if len(opts.IncludePackages) > 0 {
		found := false
		for _, p := range opts.IncludePackages {
			if p == packageName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, p := range opts.ExcludePackages {
		if p == packageName {
			return false
		}
	}

	lowerFilename := strings.ToLower(filename)

	for _, ext := range sourceExtensions {
		if strings.HasSuffix(lowerFilename, ext) {
			return opts.IncludeSource
		}
	}

	wheel := parseWheelPlatform(lowerFilename)
	if wheel == nil {
		return false
	}

	if wheel.rawTag != "any" && strings.Contains(wheel.rawTag, "musllinux") && opts.ExcludeMusllinux {
		return false
	}

	if wheel.platform == "macos" && opts.ExcludeMacos {
		return false
	}

	if (wheel.arch == "arm64" || wheel.arch == "arm") && opts.ExcludeArm {
		return false
	}

	if wheel.platform == "any" {
		return opts.IncludePlatformAny
	}

	if wheel.platform == "linux" && wheel.arch == "amd64" {
		return opts.IncludeLinuxAmd64
	}

	if wheel.platform == "win" && wheel.arch == "amd64" {
		return opts.IncludeWindowsAmd64
	}

	return false
}

// ShouldIncludeArtifact checks if an artifact should be included.
func ShouldIncludeArtifact(packageName, filename string, opts types.PypiFilterOptions) bool {
	return ShouldIncludeFilename(packageName, filename, opts)
}
