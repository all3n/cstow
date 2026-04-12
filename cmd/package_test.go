package cmd

import (
	"path/filepath"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageAddCommand(t *testing.T) {
	tmp := t.TempDir()
	
	// Use --repo_dir flag to test
	rootCmd.SetArgs([]string{"package", "add", "testpkg", "--repo_dir", tmp})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	pkgFile := filepath.Join(tmp, "t", "testpkg", "package.toml")
	if _, err := os.Stat(pkgFile); err != nil {
		t.Errorf("package.toml not created via CLI: %v", err)
	}

	// Verify it can be found by Repository Finder
	// (Check if it follows the folder structure)
	letterDir := filepath.Join(tmp, "t")
	if fi, err := os.Stat(letterDir); err != nil || !fi.IsDir() {
		t.Errorf("letter directory 't' not created: %v", err)
	}
}

func TestPackageLintCommandPassesForScaffoldedRecipe(t *testing.T) {
	tmp := t.TempDir()

	rootCmd.SetArgs([]string{"package", "add", "fmt", "--repo_dir", tmp})
	require.NoError(t, rootCmd.Execute())

	output, err := executeRootForTestWithError(t, "package", "lint", "fmt", "--repo_dir", tmp)
	require.NoError(t, err)
	assert.Contains(t, output, "package lint OK")
}

func TestPackageLintCommandReportsRecipeErrors(t *testing.T) {
	tmp := t.TempDir()
	pkgDir := filepath.Join(tmp, "f", "fmt")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["1.0.0"]
[package]
name = "fmt"
[source]
type = "git"
url = ""
[build]
system = "make"
type = "static"
`), 0o644))

	output, err := executeRootForTestWithError(t, "package", "lint", "fmt", "--repo_dir", tmp)
	require.Error(t, err)
	assert.Contains(t, output, "package lint FAILED")
	assert.Contains(t, output, `source.url is required when source.type = "git"`)
	assert.Contains(t, output, `build.system "make" is not supported`)
}

func TestPackageLintCommandShowsWarningsForScaffoldedRecipe(t *testing.T) {
	tmp := t.TempDir()

	rootCmd.SetArgs([]string{"package", "add", "fmt", "--repo_dir", tmp})
	require.NoError(t, rootCmd.Execute())

	output, err := executeRootForTestWithError(t, "package", "lint", "fmt", "--repo_dir", tmp)
	require.NoError(t, err)
	assert.Contains(t, output, "package lint OK")
	assert.Contains(t, output, "warning:")
}

func TestPackageLintCommandAllLintsWholeRepository(t *testing.T) {
	tmp := t.TempDir()
	rootCmd.SetArgs([]string{"package", "add", "fmt", "--repo_dir", tmp})
	require.NoError(t, rootCmd.Execute())

	badDir := filepath.Join(tmp, "s", "spdlog")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "package.toml"), []byte(`
versions = ["1.0.0"]
[package]
name = "spdlog"
[source]
type = "git"
url = ""
[build]
system = "cmake"
type = "static"
`), 0o644))

	output, err := executeRootForTestWithError(t, "package", "lint", "--all", "--repo_dir", tmp)
	require.Error(t, err)
	assert.Contains(t, output, "package lint OK: fmt")
	assert.Contains(t, output, "package lint FAILED: spdlog")
}
