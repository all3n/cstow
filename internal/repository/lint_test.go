package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintPackageDir_ScaffoldedPackagePasses(t *testing.T) {
	repoDir := t.TempDir()
	require.NoError(t, ScaffoldPackage(repoDir, "mylib"))

	result, err := LintPackageDir(filepath.Join(repoDir, "m", "mylib"))
	require.NoError(t, err)
	assert.True(t, result.OK())
	assert.NotEmpty(t, result.Warnings)
}

func TestLintPackageDir_ReportsUnknownKeysAndMissingPatch(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "f", "fmt")
	require.NoError(t, os.MkdirAll(filepath.Join(pkgDir, "versions"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["1.0.0"]

[package]
name = "fmt"

[source]
type = "git"
url = "https://example.com/fmt.git"

[build]
system = "cmake"
type = "static"

[build.options]
foo = "bar"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "versions", "1.0.0.toml"), []byte(`
patch = "missing.patch"
`), 0o644))

	result, err := LintPackageDir(pkgDir)
	require.NoError(t, err)
	assert.False(t, result.OK())
	assert.Contains(t, result.Errors, "package.toml has unknown keys: build.options, build.options.foo")
	assert.Contains(t, result.Errors, `patch file "patches/missing.patch" does not exist`)
}

func TestLintPackageDir_ReportsUnsupportedBuildSystemAndUnknownOverrideVersion(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "z", "zlib")
	require.NoError(t, os.MkdirAll(filepath.Join(pkgDir, "versions"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["1.3.1"]

[package]
name = "zlib"

[source]
type = "archive"
url_template = "https://example.com/zlib-{version}.tar.gz"

[build]
system = "make"
type = "static"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "versions", "9.9.9.toml"), []byte(`
[build]
type = "static"
`), 0o644))

	result, err := LintPackageDir(pkgDir)
	require.NoError(t, err)
	assert.False(t, result.OK())
	assert.Contains(t, result.Errors, `build.system "make" is not supported (supported: cmake, autotools, header-only)`)
	assert.Contains(t, result.Errors, `versions/9.9.9.toml is not listed in package versions`)
}

func TestLintRepositoryDirReturnsAllPackages(t *testing.T) {
	repoDir := t.TempDir()
	require.NoError(t, ScaffoldPackage(repoDir, "fmt"))

	badDir := filepath.Join(repoDir, "s", "spdlog")
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

	results, err := LintRepositoryDir(repoDir)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "fmt", results[0].Name)
	assert.Equal(t, "spdlog", results[1].Name)
	assert.True(t, results[0].OK())
	assert.False(t, results[1].OK())
}
