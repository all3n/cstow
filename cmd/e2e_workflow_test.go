package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2EWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("E2E workflow is currently verified on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	oldWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { os.Chdir(oldWd) })

	fakeHome := t.TempDir()
	repoRoot := filepath.Join(fakeHome, "repository")
	// Let cstow init create the project directory
	projectRoot := filepath.Join(fakeHome, "project")

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", filepath.Join(fakeHome, ".cstow", "cache"))
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	require.NoError(t, os.MkdirAll(repoRoot, 0o755))

	// 1. Setup a library in the repository
	libDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "include", "hello"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "include", "hello", "hello.h"), []byte(`
#pragma once
void say_hello();
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "src", "hello.cpp"), []byte(`
#include <iostream>
#include "hello/hello.h"
void say_hello() { std::cout << "Hello from lib_hello!" << std::endl; }
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "CMakeLists.txt"), []byte(`
cmake_minimum_required(VERSION 3.15)
project(lib_hello LANGUAGES CXX)
add_library(lib_hello src/hello.cpp)
target_include_directories(lib_hello PUBLIC 
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>
)
install(TARGETS lib_hello ARCHIVE DESTINATION lib LIBRARY DESTINATION lib)
install(DIRECTORY include/ DESTINATION include)
`), 0o644))

	runGit(t, libDir, "init")
	runGit(t, libDir, "config", "user.email", "test@example.com")
	runGit(t, libDir, "config", "user.name", "cstow test")
	runGit(t, libDir, "add", ".")
	runGit(t, libDir, "commit", "-m", "initial")
	runGit(t, libDir, "tag", "1.0.0")

	// Write repository package definition
	writeRepositoryPackage(t, repoRoot, "lib_hello", libDir, packageOptions{buildType: "static"})

	// Setup global config
	globalConfig := fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)
	require.NoError(t, os.WriteFile(filepath.Join(fakeHome, ".cstow", "config.toml"), []byte(globalConfig), 0o644))

	// 2. cstow init
	os.Chdir(fakeHome)
	rootCmd.SetArgs([]string{"init", "project"})
	require.NoError(t, rootCmd.Execute())

	os.Chdir(projectRoot)

	// 3. cstow add
	rootCmd.SetArgs([]string{"add", "lib_hello@1.0.0", "--source", "repository"})
	require.NoError(t, rootCmd.Execute())

	// 4. Setup consumer source
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "main.cpp"), []byte(`
#include "hello/hello.h"
int main() {
    say_hello();
    return 0;
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectRoot, "CMakeLists.txt"), []byte(`
cmake_minimum_required(VERSION 3.15)
project(consumer LANGUAGES CXX)
add_executable(consumer main.cpp)
find_library(HELLO_LIB lib_hello)
find_path(HELLO_INC hello/hello.h)
target_link_libraries(consumer PRIVATE ${HELLO_LIB})
target_include_directories(consumer PRIVATE ${HELLO_INC})
`), 0o644))

// 5. Add post-build hook
cfg, err := config.Load("cstow.toml")
require.NoError(t, err)
cfg.Hooks = &config.Hooks{
	PostBuild: "touch post-build-done",
}
require.NoError(t, cfg.Save("cstow.toml"))

// 6. cstow build --fetch
rootCmd.SetArgs([]string{"build", "--fetch"})
require.NoError(t, rootCmd.Execute())

// 7. Verify output and hooks
assert.FileExists(t, filepath.Join(projectRoot, "post-build-done"))

binary := filepath.Join(projectRoot, "build", "debug", "consumer")
require.FileExists(t, binary)
	cmd := exec.Command(binary)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(out), "Hello from lib_hello!")
}

func createNewRoot() *cobra.Command {
	return rootCmd
}
