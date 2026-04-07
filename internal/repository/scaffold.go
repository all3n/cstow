package repository

import (
	"fmt"
	"os"
	"path/filepath"
)

// ScaffoldPackage creates a new package recipe skeleton in the specified repository.
// It follows the directory structure: repoDir/<letter>/<pkgName>/package.toml
// and creates an empty versions/ subdirectory.
func ScaffoldPackage(repoDir, pkgName string) error {
	letter := indexLetter(pkgName)
	pkgDir := filepath.Join(repoDir, letter, pkgName)
	pkgFile := filepath.Join(pkgDir, "package.toml")

	if _, err := os.Stat(pkgFile); err == nil {
		return fmt.Errorf("package %s already exists in %s", pkgName, repoDir)
	}

	if err := os.MkdirAll(filepath.Join(pkgDir, "versions"), 0o755); err != nil {
		return fmt.Errorf("create package dirs: %w", err)
	}

	template := fmt.Sprintf(`# cstow package definition: %s

[package]
name = "%s"
description = ""
homepage = ""
license = ""

versions = ["0.1.0"]

[source]
# type: git | archive
type = "git"
url = ""
# for git:
tag_template = "v{version}"
# for archive:
# url_template = "https://example.com/%s-{version}.tar.gz"

[build]
# system: cmake | autotools | header-only
system = "cmake"
# type: static | shared | header-only
type = "static"

[build.options]
# BUILD_SHARED_LIBS = "ON"

[artifacts]
include_dirs = ["include"]
# install_targets = ["lib/lib%s.a"]
`, pkgName, pkgName, pkgName, pkgName)

	if err := os.WriteFile(pkgFile, []byte(template), 0o644); err != nil {
		return fmt.Errorf("write package.toml template: %w", err)
	}

	// Create a dummy version override example
	versionExample := `[source]
# override url or rev for this specific version
# rev = "commit-hash"
`
	if err := os.WriteFile(filepath.Join(pkgDir, "versions", "0.1.0.toml"), []byte(versionExample), 0o644); err != nil {
		return fmt.Errorf("write version override template: %w", err)
	}

	return nil
}
