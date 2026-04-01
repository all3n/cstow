package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_GlobalConfigToFinderToMerge exercises the full pipeline:
//
//	~/.cstow/config.toml → RepositoryPaths → Finder.Find → Merge
func TestIntegration_GlobalConfigToFinderToMerge(t *testing.T) {
	// ── Setup: fake HOME with two repos ──────────────────────────────
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	fakeHome := t.TempDir()
	os.Setenv("HOME", fakeHome)
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))

	// Repo A: team (priority 10, higher) — has googletest 1.14.0 + 1.13.0
	repoA := filepath.Join(fakeHome, "team-repo")
	pkgDirA := filepath.Join(repoA, "g", "googletest")
	require.NoError(t, os.MkdirAll(pkgDirA, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDirA, "package.toml"), []byte(`
versions = ["1.14.0", "1.13.0"]

[package]
name = "googletest"
description = "Google C++ Testing Framework"

[source]
type = "git"
url = "https://github.com/google/googletest.git"

[build]
system = "cmake"
type = "static"

[build.cmake]
defines = ["BUILD_SHARED_LIBS=OFF", "INSTALL_GTEST=OFF"]
install_targets = ["gtest", "gmock"]

[build.profile.debug]
defines = ["CMAKE_BUILD_TYPE=Debug"]
cxx_flags = ["-g", "-O0"]

[build.profile.release]
defines = ["CMAKE_BUILD_TYPE=Release"]
cxx_flags = ["-O3"]

[build.compiler.msvc]
defines = ["_SILENCE_TR1_NAMESPACE_DEPRECATION_WARNING=1"]
cxx_flags = ["/EHsc"]

[build.platform.windows]
defines = ["GTEST_OS_WINDOWS=1"]

[artifacts]
include_dirs = ["googletest/include", "googlemock/include"]
libs = ["libgtest.a", "libgmock.a"]
`), 0o644))

	// Version override for 1.14.0
	verDirA := filepath.Join(pkgDirA, "versions")
	require.NoError(t, os.MkdirAll(verDirA, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(verDirA, "1.14.0.toml"), []byte(`
patch = "1.14.0-fix-msvc.patch"

[build.cmake]
defines = ["BUILD_SHARED_LIBS=OFF", "INSTALL_GTEST=OFF", "GTEST_HAS_ABSL=OFF"]
`), 0o644))

	// Repo B: builtin (priority 50, lower) — has fmt 10.2.1
	repoB := filepath.Join(fakeHome, "builtin-repo")
	pkgDirB := filepath.Join(repoB, "f", "fmt")
	require.NoError(t, os.MkdirAll(pkgDirB, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDirB, "package.toml"), []byte(`
versions = ["10.2.1"]

[package]
name = "fmt"

[build]
system = "cmake"
type = "static"

[build.cmake]
defines = ["FMT_INSTALL=ON"]
cxx_flags = ["-Wall"]
`), 0o644))

	// ── Global config pointing to both repos ─────────────────────────
	globalCfg := `
[[repositories]]
name = "team"
path = "` + repoA + `"
priority = 10

[[repositories]]
name = "builtin"
path = "` + repoB + `"
priority = 50

[defaults]
std = "c++17"
profile = "debug"

[build.flags]
cxx_flags = ["-fstack-protector-strong"]
`
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeHome, ".cstow", "config.toml"),
		[]byte(globalCfg), 0o644,
	))

	// ── Step 1: LoadGlobal ───────────────────────────────────────────
	g, err := config.LoadGlobal()
	require.NoError(t, err)
	assert.Equal(t, "c++17", g.Defaults.Std)
	assert.Equal(t, "debug", g.Defaults.Profile)

	// ── Step 2: RepositoryPaths (priority order + fallback) ──────────
	paths := g.RepositoryPaths()
	require.Len(t, paths, 3)
	assert.Equal(t, repoA, paths[0]) // priority 10
	assert.Equal(t, repoB, paths[1]) // priority 50
	assert.Contains(t, paths[2], ".cstow/repository") // built-in fallback

	// ── Step 3: Finder.Find (googletest from high-priority repo) ─────
	finder := NewFinderWithPaths(paths)
	pkg, err := finder.Find("googletest", "^1.14")
	require.NoError(t, err)
	assert.Equal(t, "1.14.0", pkg.Version)
	assert.Equal(t, repoA, pkg.RepoPath)
	require.NotNil(t, pkg.Override)
	assert.Equal(t, "1.14.0-fix-msvc.patch", pkg.Override.Patch)

	// ── Step 4: Merge (all layers) ───────────────────────────────────
	merged := Merge(pkg.Def, pkg.Override, "gcc", "debug", "linux")

	assert.Equal(t, "cmake", merged.System)
	// Version override replaces defines (layer 5): version override's full list
	assert.Contains(t, merged.CMakeDefines, "GTEST_HAS_ABSL=OFF")
	assert.Contains(t, merged.CMakeDefines, "BUILD_SHARED_LIBS=OFF")
	// Profile defines were wiped by version override replace; profile cxx_flags kept
	assert.NotContains(t, merged.CMakeDefines, "CMAKE_BUILD_TYPE=Debug")
	assert.Contains(t, merged.CXXFlags, "-g")
	assert.Contains(t, merged.CXXFlags, "-O0")
	// Artifacts
	assert.Contains(t, merged.IncludeDirs, "googletest/include")
	assert.Contains(t, merged.Libs, "libgtest.a")
	assert.Equal(t, "1.14.0-fix-msvc.patch", merged.Patch)

	// ── Step 5: Finder.Find (fmt from lower-priority repo) ───────────
	pkgFmt, err := finder.Find("fmt", "^10")
	require.NoError(t, err)
	assert.Equal(t, "10.2.1", pkgFmt.Version)
	assert.Equal(t, repoB, pkgFmt.RepoPath)

	mergedFmt := Merge(pkgFmt.Def, pkgFmt.Override, "clang", "release", "macos")
	assert.Equal(t, "cmake", mergedFmt.System)
	assert.Contains(t, mergedFmt.CMakeDefines, "FMT_INSTALL=ON")
	assert.Contains(t, mergedFmt.CXXFlags, "-Wall") // base flags preserved

	// ── Step 6: Verify search order — first repo wins ────────────────
	// Add a cheaper version of fmt in repo A (higher priority)
	pkgDirFmtA := filepath.Join(repoA, "f", "fmt")
	require.NoError(t, os.MkdirAll(pkgDirFmtA, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDirFmtA, "package.toml"), []byte(`
versions = ["11.0.0"]

[package]
name = "fmt"

[build]
system = "cmake"
`), 0o644))

	pkgFmtAgain, err := finder.Find("fmt", ">=10")
	require.NoError(t, err)
	assert.Equal(t, "11.0.0", pkgFmtAgain.Version)
	assert.Equal(t, repoA, pkgFmtAgain.RepoPath) // found in higher-priority repo A

	// ── Step 7: Not found in any repo ────────────────────────────────
	_, err = finder.Find("nonexistent", "^1.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in any repository")
}

// TestIntegration_DefaultGlobalConfig tests the fallback when ~/.cstow/config.toml
// does not exist — Finder should still work with the built-in path only.
func TestIntegration_DefaultGlobalConfig(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	fakeHome := t.TempDir()
	os.Setenv("HOME", fakeHome)

	// No ~/.cstow/config.toml — should use defaults
	g, err := config.LoadGlobal()
	require.NoError(t, err)
	assert.Equal(t, "c++17", g.Defaults.Std)

	paths := g.RepositoryPaths()
	require.Len(t, paths, 1)
	assert.Contains(t, paths[0], ".cstow/repository")

	// Finder with only the built-in path (empty repo — nothing to find)
	finder := NewFinderWithPaths(paths)
	_, err = finder.Find("fmt", "^10")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in any repository")
}
