package cmakegen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateCMakeLists_ExecutableNoDeps(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Type: "executable",
		Std:  "c++17",
	}
	got := GenerateCMakeLists(opts)

	assert.Contains(t, got, "cmake_minimum_required(VERSION 3.16)")
	assert.Contains(t, got, "project(myapp LANGUAGES CXX)")
	assert.Contains(t, got, "set(CMAKE_CXX_STANDARD 17)")
	assert.Contains(t, got, "set(CMAKE_CXX_STANDARD_REQUIRED ON)")
	assert.Contains(t, got, "file(GLOB_RECURSE SOURCES src/*.cpp)")
	assert.Contains(t, got, "add_executable(myapp ${SOURCES})")
	assert.NotContains(t, got, "find_package")
	assert.NotContains(t, got, "target_link_libraries")
}

func TestGenerateCMakeLists_LibraryNoDeps(t *testing.T) {
	opts := GenerateOptions{
		Name: "mylib",
		Type: "library",
		Std:  "c++20",
	}
	got := GenerateCMakeLists(opts)

	assert.Contains(t, got, "add_library(mylib ${SOURCES})")
	assert.Contains(t, got, "set(CMAKE_CXX_STANDARD 20)")
	assert.Contains(t, got, "file(GLOB_RECURSE SOURCES src/*.cpp)")
}

func TestGenerateCMakeLists_WithCMakeConfigDeps(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Type: "executable",
		Std:  "c++17",
		Deps: []DepTarget{
			{Name: "fmt", TargetName: "fmt::fmt", ConfigFile: "fmtConfig.cmake"},
			{Name: "spdlog", TargetName: "spdlog::spdlog", ConfigFile: "spdlogConfig.cmake"},
		},
	}
	got := GenerateCMakeLists(opts)

	assert.Contains(t, got, `list(APPEND CMAKE_PREFIX_PATH "${CMAKE_SOURCE_DIR}/cstow_deps/fmt")`)
	assert.Contains(t, got, `list(APPEND CMAKE_PREFIX_PATH "${CMAKE_SOURCE_DIR}/cstow_deps/spdlog")`)
	assert.Contains(t, got, "find_package(fmt REQUIRED)")
	assert.Contains(t, got, "find_package(spdlog REQUIRED)")
	assert.Contains(t, got, "target_link_libraries(myapp PRIVATE fmt::fmt spdlog::spdlog)")
}

func TestGenerateCMakeLists_WithFallbackDeps(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Type: "executable",
		Std:  "c++17",
		Deps: []DepTarget{
			{Name: "catch2", TargetName: "", ConfigFile: ""},
		},
	}
	got := GenerateCMakeLists(opts)

	assert.NotContains(t, got, "find_package(catch2")
	assert.Contains(t, got, `target_include_directories(myapp PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/catch2/include)`)
}

func TestGenerateCMakeLists_MixedDeps(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Type: "executable",
		Std:  "c++17",
		Deps: []DepTarget{
			{Name: "fmt", TargetName: "fmt::fmt", ConfigFile: "fmtConfig.cmake"},
			{Name: "catch2", TargetName: "", ConfigFile: ""},
		},
	}
	got := GenerateCMakeLists(opts)

	// CMake-config dep gets find_package
	assert.Contains(t, got, "find_package(fmt REQUIRED)")
	assert.Contains(t, got, `list(APPEND CMAKE_PREFIX_PATH "${CMAKE_SOURCE_DIR}/cstow_deps/fmt")`)
	// Fallback dep gets include directory
	assert.Contains(t, got, `target_include_directories(myapp PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/catch2/include)`)
	// link_libraries only includes cmake-config targets
	assert.Contains(t, got, "target_link_libraries(myapp PRIVATE fmt::fmt)")
}

func TestGenerateCMakeLists_HeaderOnly(t *testing.T) {
	opts := GenerateOptions{
		Name:    "mylib",
		Type:    "header-only",
		Std:     "c++17",
		Include: []string{"include"},
	}
	got := GenerateCMakeLists(opts)

	assert.Contains(t, got, "add_library(mylib INTERFACE)")
	assert.NotContains(t, got, "file(GLOB_RECURSE SOURCES")
	assert.Contains(t, got, "target_include_directories(mylib INTERFACE include)")
}

func TestGenerateCMakeLists_WithDefines(t *testing.T) {
	opts := GenerateOptions{
		Name:    "myapp",
		Type:    "executable",
		Std:     "c++17",
		Defines: []string{"MY_DEFINE=1", "OTHER=2"},
	}
	got := GenerateCMakeLists(opts)

	assert.Contains(t, got, "target_compile_definitions(myapp PRIVATE MY_DEFINE=1 OTHER=2)")
}

func TestGenerateCMakeLists_CMakeMinimumVersion(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Type: "executable",
		Std:  "c++17",
	}
	got := GenerateCMakeLists(opts)

	// Always present as first line
	lines := strings.SplitN(got, "\n", 2)
	assert.Equal(t, "cmake_minimum_required(VERSION 3.16)", lines[0])
}
