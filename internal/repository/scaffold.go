package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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

	def := PackageDef{
		Package: PackageMeta{
			Name: pkgName,
		},
		Versions: []string{"0.1.0"},
		Source: SourceDef{
			Type:        "git",
			TagTemplate: "{version}",
		},
		Build: BuildDef{
			System: "cmake",
			Type:   "static",
		},
		Artifacts: ArtifactsDef{
			IncludeDirs: []string{"include"},
		},
	}

	f, err := os.Create(pkgFile)
	if err != nil {
		return fmt.Errorf("create package.toml: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(def); err != nil {
		return fmt.Errorf("write package.toml: %w", err)
	}

	return nil
}
