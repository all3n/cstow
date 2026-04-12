package cmakegen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGenerateCMakeLists_WithCompileAndLinkFlags(t *testing.T) {
	opts := GenerateOptions{
		Name:      "myapp",
		Type:      "executable",
		Std:       "c++17",
		CXXFlags:  []string{"-Wall", "-Wextra"},
		LinkFlags: []string{"-lpthread", "-ldl"},
	}
	got := GenerateCMakeLists(opts)

	assert.Contains(t, got, "target_compile_options(myapp PRIVATE -Wall -Wextra)")
	assert.Contains(t, got, "target_link_options(myapp PRIVATE -lpthread -ldl)")
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

func TestGeneratePresets_DebugAndRelease(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Std:  "c++17",
		Profiles: map[string]config.Profile{
			"debug":   {Optimize: "0", Debug: true},
			"release": {Optimize: "3", LTO: true},
		},
	}
	got, err := GeneratePresets(opts)
	require.NoError(t, err)

	// Valid JSON
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got), &result))

	// Version 6
	assert.Equal(t, float64(6), result["version"])

	// Configure presets
	presets := result["configurePresets"].([]interface{})
	presetMap := make(map[string]map[string]interface{})
	for _, p := range presets {
		pm := p.(map[string]interface{})
		presetMap[pm["name"].(string)] = pm
	}

	debugPreset := presetMap["debug"]
	releasePreset := presetMap["release"]
	require.NotNil(t, debugPreset)
	require.NotNil(t, releasePreset)

	assert.Equal(t, "${sourceDir}/build/debug", debugPreset["binaryDir"])
	assert.Equal(t, "${sourceDir}/build/release", releasePreset["binaryDir"])

	debugCache := debugPreset["cacheVariables"].(map[string]interface{})
	releaseCache := releasePreset["cacheVariables"].(map[string]interface{})

	assert.Equal(t, "Debug", debugCache["CMAKE_BUILD_TYPE"])
	assert.Equal(t, "Release", releaseCache["CMAKE_BUILD_TYPE"])
	assert.Equal(t, "ON", debugCache["CMAKE_EXPORT_COMPILE_COMMANDS"])
	assert.Equal(t, "ON", releaseCache["CMAKE_EXPORT_COMPILE_COMMANDS"])
	assert.Equal(t, "17", debugCache["CMAKE_CXX_STANDARD"])
	assert.Equal(t, "17", releaseCache["CMAKE_CXX_STANDARD"])

	// LTO only on release
	assert.Nil(t, debugCache["CMAKE_INTERPROCEDURAL_OPTIMIZATION"])
	assert.Equal(t, "ON", releaseCache["CMAKE_INTERPROCEDURAL_OPTIMIZATION"])
}

func TestGeneratePresets_DefaultProfiles(t *testing.T) {
	opts := GenerateOptions{
		Name:     "myapp",
		Std:      "c++17",
		Profiles: map[string]config.Profile{}, // empty
	}
	got, err := GeneratePresets(opts)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got), &result))

	presets := result["configurePresets"].([]interface{})
	presetMap := make(map[string]map[string]interface{})
	for _, p := range presets {
		pm := p.(map[string]interface{})
		presetMap[pm["name"].(string)] = pm
	}

	_, hasDebug := presetMap["debug"]
	_, hasRelease := presetMap["release"]
	assert.True(t, hasDebug, "expected default debug preset")
	assert.True(t, hasRelease, "expected default release preset")
}

func TestGeneratePresets_WithToolchain(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Std:  "c++17",
		Profiles: map[string]config.Profile{
			"debug": {Debug: true},
		},
		Toolchain: config.Toolchain{Compiler: "clang"},
	}
	got, err := GeneratePresets(opts)
	require.NoError(t, err)

	assert.Contains(t, got, "CMAKE_CXX_COMPILER")
	assert.Contains(t, got, "clang++")
}

func TestGeneratePresets_BuildPresets(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Std:  "c++17",
		Profiles: map[string]config.Profile{
			"debug":   {Debug: true},
			"release": {Optimize: "3"},
		},
	}
	got, err := GeneratePresets(opts)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(got), &result))

	buildPresets := result["buildPresets"].([]interface{})
	buildMap := make(map[string]map[string]interface{})
	for _, p := range buildPresets {
		pm := p.(map[string]interface{})
		buildMap[pm["name"].(string)] = pm
	}

	assert.Equal(t, "debug", buildMap["debug"]["configurePreset"])
	assert.Equal(t, "release", buildMap["release"]["configurePreset"])
}

func TestGeneratePresets_ValidJSON(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Std:  "c++20",
		Profiles: map[string]config.Profile{
			"release": {Optimize: "3", LTO: true},
		},
	}
	got, err := GeneratePresets(opts)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal([]byte(got), &result)
	assert.NoError(t, err, "output should be valid JSON")
	assert.Contains(t, got, "\"version\"")
	assert.Contains(t, got, "\"configurePresets\"")
}
