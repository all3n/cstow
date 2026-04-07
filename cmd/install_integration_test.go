package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallFromRepositoryBuildsStaticAndSharedLibraries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shared/static install verification is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	fakeHome := t.TempDir()
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	repoRoot := filepath.Join(fakeHome, "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)

	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-static", sourceRepo, packageOptions{buildType: "static"})
	writeRepositoryPackage(t, repoRoot, "mini-shared", sourceRepo, packageOptions{buildType: "shared"})

	globalConfig := fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeHome, ".cstow", "config.toml"),
		[]byte(globalConfig),
		0o644,
	))

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc", nil)
	require.NoError(t, err)

	tests := []struct {
		name         string
		pkg          string
		expectedGlob string
	}{
		{
			name:         "static library",
			pkg:          "mini-static",
			expectedGlob: filepath.Join("lib", "libmini.a"),
		},
		{
			name:         "shared library",
			pkg:          "mini-shared",
			expectedGlob: filepath.Join("lib", sharedLibraryPattern("mini")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			result, err := installFromRepository(tt.pkg, "^1", repositoryInstallOptions{
				Context: ctx,
				Force:   true,
				Stdout:  &stdout,
				Stderr:  &stderr,
			})
			require.NoErrorf(t, err, "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())

			assert.Equal(t, "1.0.0", result.Version)
			assert.DirExists(t, result.InstallDir)
			assert.FileExists(t, filepath.Join(result.InstallDir, "include", "mini", "mini.h"))

			matches, globErr := filepath.Glob(filepath.Join(result.InstallDir, tt.expectedGlob))
			require.NoError(t, globErr)
			require.NotEmptyf(t, matches, "expected installed artifact matching %q", tt.expectedGlob)
		})
	}
}

func TestInstallFromRepositoryBuildTypeOverrideBeatsConflictingRecipeDefine(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shared/static install verification is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	fakeHome := t.TempDir()
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	repoRoot := filepath.Join(fakeHome, "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)

	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-googletest-style", sourceRepo, packageOptions{
		buildType: "static",
		defines:   []string{"BUILD_SHARED_LIBS=OFF"},
	})

	globalConfig := fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeHome, ".cstow", "config.toml"),
		[]byte(globalConfig),
		0o644,
	))

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc", nil)
	require.NoError(t, err)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	result, err := installFromRepository("mini-googletest-style", "^1", repositoryInstallOptions{
		Context:   ctx,
		BuildType: "shared",
		Force:     true,
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	require.NoErrorf(t, err, "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())

	matches, globErr := filepath.Glob(filepath.Join(result.InstallDir, "lib", sharedLibraryPattern("mini")))
	require.NoError(t, globErr)
	require.NotEmpty(t, matches, "expected BuildType override to produce a shared library")
	assert.NoFileExists(t, filepath.Join(result.InstallDir, "lib", "libmini.a"))
}

func TestInstallFromRepositoryCachesStaticAndSharedSeparately(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shared/static install verification is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	fakeHome := t.TempDir()
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	repoRoot := filepath.Join(fakeHome, "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)

	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-split", sourceRepo, packageOptions{
		buildType: "static",
		defines:   []string{"BUILD_SHARED_LIBS=OFF"},
	})

	globalConfig := fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeHome, ".cstow", "config.toml"),
		[]byte(globalConfig),
		0o644,
	))

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc", nil)
	require.NoError(t, err)

	staticResult, err := installFromRepository("mini-split", "^1", repositoryInstallOptions{
		Context:   ctx,
		BuildType: "static",
		Force:     true,
	})
	require.NoError(t, err)

	sharedResult, err := installFromRepository("mini-split", "^1", repositoryInstallOptions{
		Context:   ctx,
		BuildType: "shared",
		Force:     true,
	})
	require.NoError(t, err)

	assert.NotEqual(t, staticResult.InstallDir, sharedResult.InstallDir)
	assert.DirExists(t, staticResult.InstallDir)
	assert.DirExists(t, sharedResult.InstallDir)
	assert.FileExists(t, filepath.Join(staticResult.InstallDir, "lib", "libmini.a"))

	matches, globErr := filepath.Glob(filepath.Join(sharedResult.InstallDir, "lib", sharedLibraryPattern("mini")))
	require.NoError(t, globErr)
	require.NotEmpty(t, matches)
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found on PATH", name)
	}
}

func createTaggedLibraryRepo(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "include", "mini"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "src"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "CMakeLists.txt"), []byte(`cmake_minimum_required(VERSION 3.15)
project(mini LANGUAGES CXX)

add_library(mini src/mini.cpp)
target_include_directories(mini PUBLIC
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<INSTALL_INTERFACE:include>
)
target_compile_features(mini PUBLIC cxx_std_17)

install(TARGETS mini
  ARCHIVE DESTINATION lib
  LIBRARY DESTINATION lib
  RUNTIME DESTINATION bin
)
install(DIRECTORY include/ DESTINATION include)
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "include", "mini", "mini.h"), []byte(`#pragma once

int mini();
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "src", "mini.cpp"), []byte(`#include "mini/mini.h"

int mini() {
	return 42;
}
`), 0o644))

	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "cstow test")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")
	runGit(t, repoDir, "tag", "1.0.0")

	return repoDir
}

type packageOptions struct {
	buildType string
	defines   []string
}

func writeRepositoryPackage(t *testing.T, repoRoot, name, sourceRepo string, opts packageOptions) {
	t.Helper()

	pkgDir := filepath.Join(repoRoot, string(name[0]), name)
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	content := fmt.Sprintf(`
versions = ["1.0.0"]

[package]
name = %q
description = "integration-test package"

[source]
type = "git"
url = %q
tag_template = "{version}"

[build]
system = "cmake"
type = %q
`, name, sourceRepo, opts.buildType)
	if len(opts.defines) > 0 {
		content += "\n[build.cmake]\ndefines = ["
		for i, d := range opts.defines {
			if i > 0 {
				content += ", "
			}
			content += fmt.Sprintf("%q", d)
		}
		content += "]\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(content), 0o644))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, string(out))
}

func sharedLibraryPattern(base string) string {
	switch runtime.GOOS {
	case "darwin":
		return "lib" + base + ".dylib*"
	default:
		return "lib" + base + ".so*"
	}
}

func TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shared/static install verification is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	fakeHome := t.TempDir()
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	repoRoot := filepath.Join(fakeHome, "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-indexed", sourceRepo, packageOptions{buildType: "static"})
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeHome, ".cstow", "config.toml"),
		[]byte(fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)),
		0o644,
	))

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc", nil)
	require.NoError(t, err)

	result, err := installFromRepository("mini-indexed", "^1", repositoryInstallOptions{
		Context: ctx,
		Force:   true,
	})
	require.NoError(t, err)

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, result.InstallDir, rows[0].InstallDir)
	assert.Equal(t, "repository", rows[0].Origin)

	require.NoError(t, store.Close())
	require.NoError(t, os.Remove(filepath.Join(fakeHome, ".cstow", "cstow.db")))

	_, err = installFromRepository("mini-indexed", "^1", repositoryInstallOptions{
		Context: ctx,
		Force:   false,
	})
	require.NoError(t, err)

	store, err = artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	rows, err = store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "repository", rows[0].Origin)
}
