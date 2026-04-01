package repository

import (
	"fmt"
	"os/exec"
	"strings"
)

// ExpandTagTemplate replaces "{version}" in a tag template.
// "v{version}" + "1.14.0" → "v1.14.0".
// If template is empty, returns the version as-is.
func ExpandTagTemplate(template, version string) string {
	if template == "" {
		return version
	}
	return strings.ReplaceAll(template, "{version}", version)
}

// FetchSource fetches source code for a package to destDir.
// Returns the path to the source root.
// Supports git (clone + checkout) and archive sources.
func FetchSource(srcDef SourceDef, version, destDir string) (string, error) {
	switch srcDef.Type {
	case "git":
		tag := ExpandTagTemplate(srcDef.TagTemplate, version)
		if err := FetchGit(srcDef.URL, tag, destDir); err != nil {
			return "", fmt.Errorf("git fetch %s@%s: %w", srcDef.URL, tag, err)
		}
		return destDir, nil
	case "archive":
		url := ExpandTagTemplate(srcDef.URLTemplate, version)
		if err := FetchArchive(url, destDir); err != nil {
			return "", fmt.Errorf("archive fetch %s: %w", url, err)
		}
		return destDir, nil
	default:
		return "", fmt.Errorf("unsupported source type %q", srcDef.Type)
	}
}

// FetchGit clones a repo and checks out the specified tag using a shallow clone.
func FetchGit(url, tag, destDir string) error {
	cmd := exec.Command("git", "clone", "--branch", tag, "--depth", "1", url, destDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %s: %w", string(out), err)
	}
	return nil
}

// FetchArchive downloads and extracts an archive to destDir.
func FetchArchive(url, destDir string) error {
	return fmt.Errorf("archive source not yet implemented")
}
