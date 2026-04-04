package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallFromRepositoryWithRecursiveDependencies(t *testing.T) {
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

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))

	// Create lib_b (dependency)
	repoB := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoB, "include", "lib_b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoB, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoB, "CMakeLists.txt"), []byte(`cmake_minimum_required(VERSION 3.15)
project(lib_b LANGUAGES CXX)
add_library(lib_b src/lib_b.cpp)
target_include_directories(lib_b PUBLIC
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<INSTALL_INTERFACE:include>
)
install(TARGETS lib_b ARCHIVE DESTINATION lib LIBRARY DESTINATION lib)
install(DIRECTORY include/ DESTINATION include)
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoB, "include", "lib_b", "lib_b.h"), []byte(`#pragma once
int b_func();
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoB, "src", "lib_b.cpp"), []byte(`#include "lib_b/lib_b.h"
int b_func() { return 100; }
`), 0o644))
	runGit(t, repoB, "init")
	runGit(t, repoB, "config", "user.email", "test@example.com")
	runGit(t, repoB, "config", "user.name", "cstow test")
	runGit(t, repoB, "add", ".")
	runGit(t, repoB, "commit", "-m", "initial")
	runGit(t, repoB, "tag", "1.0.0")

	// Create lib_a (depends on lib_b)
	repoA := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoA, "include", "lib_a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoA, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoA, "CMakeLists.txt"), []byte(`cmake_minimum_required(VERSION 3.15)
project(lib_a LANGUAGES CXX)

# find_library to simulate finding dependency
find_library(LIB_B_LIBRARY NAMES lib_b b PATH_SUFFIXES lib)
find_path(LIB_B_INCLUDE_DIR NAMES lib_b/lib_b.h PATH_SUFFIXES include)
if(NOT LIB_B_LIBRARY OR NOT LIB_B_INCLUDE_DIR)
  message(FATAL_ERROR "lib_b not found!")
endif()

add_library(lib_a src/lib_a.cpp)
target_include_directories(lib_a PUBLIC
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<INSTALL_INTERFACE:include>
)
target_include_directories(lib_a PRIVATE ${LIB_B_INCLUDE_DIR})
target_link_libraries(lib_a PRIVATE ${LIB_B_LIBRARY})

install(TARGETS lib_a ARCHIVE DESTINATION lib LIBRARY DESTINATION lib)
install(DIRECTORY include/ DESTINATION include)
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoA, "include", "lib_a", "lib_a.h"), []byte(`#pragma once
int a_func();
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoA, "src", "lib_a.cpp"), []byte(`#include "lib_a/lib_a.h"
#include "lib_b/lib_b.h"
int a_func() { return b_func() + 42; }
`), 0o644))
	runGit(t, repoA, "init")
	runGit(t, repoA, "config", "user.email", "test@example.com")
	runGit(t, repoA, "config", "user.name", "cstow test")
	runGit(t, repoA, "add", ".")
	runGit(t, repoA, "commit", "-m", "initial")
	runGit(t, repoA, "tag", "1.0.0")

	// Write repository packages
	writeRepositoryPackage(t, repoRoot, "lib_b", repoB, packageOptions{buildType: "static"})
	
	// write lib_a which declares dependency on lib_b
	pkgADir := filepath.Join(repoRoot, "l", "lib_a")
	require.NoError(t, os.MkdirAll(pkgADir, 0o755))
	contentA := fmt.Sprintf(`
versions = ["1.0.0"]

[package]
name = "lib_a"
description = "package a"

[source]
type = "git"
url = %q
tag_template = "{version}"

[build]
system = "cmake"
type = "static"

[[dependencies]]
name = "lib_b"
version = "1.0.0"
source = "repository"
build_type = "static"
`, repoA)
	require.NoError(t, os.WriteFile(filepath.Join(pkgADir, "package.toml"), []byte(contentA), 0o644))

	// Write global config
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

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc")
	require.NoError(t, err)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Install lib_a (which should trigger install of lib_b)
	result, err := installFromRepository("lib_a", "^1", repositoryInstallOptions{
		Context: ctx,
		Force:   true,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	require.NoErrorf(t, err, "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())

	// Verify lib_a was installed
	assert.Equal(t, "1.0.0", result.Version)
	assert.DirExists(t, result.InstallDir)
	assert.FileExists(t, filepath.Join(result.InstallDir, "include", "lib_a", "lib_a.h"))
	assert.FileExists(t, filepath.Join(result.InstallDir, "lib", "liblib_a.a"))
}
