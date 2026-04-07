package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallAbseilCppFromRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("integration test not yet supported on Windows")
	}
	
	// Check for required tools
	requireTool(t, "cmake")
	requireTool(t, "g++")
	
	// Check for network connectivity (Abseil is large, better to skip if no internet)
	if err := exec.Command("git", "ls-remote", "https://github.com/abseil/abseil-cpp.git", "HEAD").Run(); err != nil {
		t.Skip("Abseil-cpp repository unreachable, skipping test")
	}

	fakeHome := t.TempDir()
	repoRoot := filepath.Join(fakeHome, "repository")
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	
	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)
	
	// Setup repository recipe
	pkgDir := filepath.Join(repoRoot, "a", "abseil-cpp")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["20260107.1"]

[package]
name = "abseil-cpp"
description = "Abseil Common Libraries (C++)"

[source]
type = "archive"
url_template = "https://github.com/abseil/abseil-cpp/archive/refs/tags/{version}.tar.gz"

[build]
system = "cmake"
type = "static"

[build.cmake]
defines = [
    "ABSL_PROPAGATE_CXX_STD=ON",
    "CMAKE_CXX_STANDARD=17",
    "ABSL_ENABLE_INSTALL=ON"
]

[artifacts]
include_dirs = ["include"]
libs = ["libabsl_*.a"]
`), 0o644))

	// Setup global config
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeHome, ".cstow", "config.toml"), []byte(`
[[repositories]]
name = "local"
path = "`+repoRoot+`"
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`), 0o644))

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc", nil)
	require.NoError(t, err)

	// Run install
	result, err := installFromRepository("abseil-cpp", "20260107.1", repositoryInstallOptions{
		Context: ctx,
		Force:   true,
	})
	require.NoError(t, err)

	// Verify installation directory
	assert.Equal(t, "20260107.1", result.Version)
	assert.DirExists(t, result.InstallDir)
	
	// Verify headers
	assert.FileExists(t, filepath.Join(result.InstallDir, "include", "absl", "base", "config.h"))
	
	// Verify libraries (glob)
	matches, _ := filepath.Glob(filepath.Join(result.InstallDir, "lib", "libabsl_base.a"))
	assert.NotEmpty(t, matches, "expected at least libabsl_base.a to be installed")

	// Verify ArtifactDB indexing
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	defer store.Close()
	
	rows, err := store.List()
	require.NoError(t, err)
	found := false
	for _, row := range rows {
		if row.Name == "abseil-cpp" && row.Version == "20260107.1" {
			found = true
			break
		}
	}
	assert.True(t, found, "abseil-cpp should be indexed in artifactdb")
}
