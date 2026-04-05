# Auto CMake File Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-generate `CMakeLists.txt` and `CMakePresets.json` from `cstow.toml` with dependency discovery from `cstow_deps/`.

**Architecture:** New `internal/cmakegen/` package with three responsibilities: dependency discovery (scanning `cstow_deps/` for `*Config.cmake` files), CMakeLists.txt generation, and CMakePresets.json generation. Exposed via a new `cstow gen` CLI command and as a fallback in `cstow build`.

**Tech Stack:** Go 1.25, `encoding/json` for presets, `testify` for tests, `path/filepath` for file discovery.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/cmakegen/cmakegen.go` | Public types (`DepTarget`, `GenerateOptions`) |
| Create | `internal/cmakegen/discover.go` | `DiscoverDeps(depsDir)` — scan cstow_deps for Config.cmake files |
| Create | `internal/cmakegen/cmakelists.go` | `GenerateCMakeLists(opts)` — produce CMakeLists.txt content |
| Create | `internal/cmakegen/presets.go` | `GeneratePresets(opts)` — produce CMakePresets.json content |
| Create | `internal/cmakegen/discover_test.go` | Tests for dependency discovery |
| Create | `internal/cmakegen/cmakegen_test.go` | Tests for CMakeLists.txt and presets generation |
| Create | `cmd/gen.go` | `cstow gen` CLI command |
| Modify | `cmd/build.go:89-96` | Add auto-generation fallback when CMakeLists.txt missing |

---

### Task 1: Create cmakegen package types

**Files:**
- Create: `internal/cmakegen/cmakegen.go`

- [ ] **Step 1: Create the package with types**

```go
package cmakegen

import "github.com/all3n/cstow/internal/config"

// DepTarget describes a discovered dependency's CMake information.
type DepTarget struct {
	Name       string // dependency directory name
	ConfigFile string // path to found *Config.cmake file (empty if fallback)
	TargetName string // inferred CMake target (e.g. "fmt::fmt")
	Prefix     string // dependency install prefix path
}

// GenerateOptions holds all data needed to generate CMake files.
type GenerateOptions struct {
	Name      string
	Type      string // executable | library | header-only
	Std       string // c++17 etc
	Sources   []string
	Include   []string
	Defines   []string
	Deps      []DepTarget
	Profiles  map[string]config.Profile
	Toolchain config.Toolchain
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/cmakegen/`
Expected: success (no output)

- [ ] **Step 3: Commit**

```bash
git add internal/cmakegen/cmakegen.go
git commit -m "feat(cmakegen): add package types for DepTarget and GenerateOptions"
```

---

### Task 2: Implement dependency discovery

**Files:**
- Create: `internal/cmakegen/discover.go`
- Create: `internal/cmakegen/discover_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cmakegen/discover_test.go`:

```go
package cmakegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverDeps_FindsConfigFile(t *testing.T) {
	dir := t.TempDir()
	// Simulate fmt dependency with fmtConfig.cmake
	fmtDir := filepath.Join(dir, "fmt", "lib", "cmake", "fmt")
	require.NoError(t, os.MkdirAll(fmtDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fmtDir, "fmtConfig.cmake"), []byte(""), 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "fmt", deps[0].Name)
	assert.Equal(t, "fmt::fmt", deps[0].TargetName)
	assert.Contains(t, deps[0].ConfigFile, "fmtConfig.cmake")
	assert.Contains(t, deps[0].Prefix, filepath.Join(dir, "fmt"))
}

func TestDiscoverDeps_LowercaseConfigFile(t *testing.T) {
	dir := t.TempDir()
	// Simulate protobuf with protobuf-config.cmake
	pbDir := filepath.Join(dir, "protobuf", "lib", "cmake", "protobuf")
	require.NoError(t, os.MkdirAll(pbDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pbDir, "protobuf-config.cmake"), []byte(""), 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "protobuf", deps[0].Name)
	assert.Equal(t, "protobuf::protobuf", deps[0].TargetName)
}

func TestDiscoverDeps_FallbackNoConfigFile(t *testing.T) {
	dir := t.TempDir()
	// Simulate header-only library with no cmake config
	incDir := filepath.Join(dir, "expected", "include", "expected")
	require.NoError(t, os.MkdirAll(incDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(incDir, "expected.h"), []byte(""), 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "expected", deps[0].Name)
	assert.Equal(t, "", deps[0].ConfigFile)
	assert.Equal(t, "", deps[0].TargetName)
}

func TestDiscoverDeps_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	assert.Len(t, deps, 0)
}

func TestDiscoverDeps_NonexistentDir(t *testing.T) {
	deps, err := DiscoverDeps("/nonexistent/path")
	require.NoError(t, err)
	assert.Len(t, deps, 0)
}

func TestDiscoverDeps_MultipleDeps(t *testing.T) {
	dir := t.TempDir()

	// fmt with config
	fmtDir := filepath.Join(dir, "fmt", "lib", "cmake", "fmt")
	require.NoError(t, os.MkdirAll(fmtDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fmtDir, "fmtConfig.cmake"), []byte(""), 0o644))

	// spdlog with config
	spdDir := filepath.Join(dir, "spdlog", "lib", "cmake", "spdlog")
	require.NoError(t, os.MkdirAll(spdDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(spdDir, "spdlogConfig.cmake"), []byte(""), 0o644))

	// header-only expected
	incDir := filepath.Join(dir, "expected", "include")
	require.NoError(t, os.MkdirAll(incDir, 0o755))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 3)

	names := make(map[string]DepTarget)
	for _, d := range deps {
		names[d.Name] = d
	}
	assert.Equal(t, "fmt::fmt", names["fmt"].TargetName)
	assert.Equal(t, "spdlog::spdlog", names["spdlog"].TargetName)
	assert.Equal(t, "", names["expected"].TargetName)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cmakegen/ -run TestDiscoverDeps -v`
Expected: FAIL — `DiscoverDeps` not defined

- [ ] **Step 3: Implement DiscoverDeps**

Create `internal/cmakegen/discover.go`:

```go
package cmakegen

import (
	"os"
	"path/filepath"
	"strings"
)

// DiscoverDeps scans depsDir for installed dependencies, looking for
// CMake config files (*Config.cmake, *-config.cmake) to determine
// CMake package names and targets.
func DiscoverDeps(depsDir string) ([]DepTarget, error) {
	entries, err := os.ReadDir(depsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var deps []DepTarget
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		depPath := filepath.Join(depsDir, e.Name())
		target := discoverDep(e.Name(), depPath)
		deps = append(deps, target)
	}
	return deps, nil
}

func discoverDep(name, dir string) DepTarget {
	configFile := findCMakeConfig(dir)
	if configFile == "" {
		return DepTarget{
			Name:   name,
			Prefix: dir,
		}
	}

	pkgName := extractPackageName(filepath.Base(configFile))
	targetName := pkgName + "::" + pkgName
	return DepTarget{
		Name:       name,
		ConfigFile: configFile,
		TargetName: targetName,
		Prefix:     dir,
	}
}

// findCMakeConfig recursively searches for a *Config.cmake or *-config.cmake file.
func findCMakeConfig(dir string) string {
	var found string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, "Config.cmake") || strings.HasSuffix(name, "-config.cmake") {
			found = path
		}
		return nil
	})
	return found
}

// extractPackageName derives the CMake package name from a config filename.
// "fmtConfig.cmake" → "fmt", "protobuf-config.cmake" → "protobuf".
func extractPackageName(filename string) string {
	if strings.HasSuffix(filename, "Config.cmake") {
		return strings.TrimSuffix(filename, "Config.cmake")
	}
	if strings.HasSuffix(filename, "-config.cmake") {
		return strings.TrimSuffix(filename, "-config.cmake")
	}
	return strings.TrimSuffix(filename, ".cmake")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cmakegen/ -run TestDiscoverDeps -v`
Expected: All 6 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmakegen/discover.go internal/cmakegen/discover_test.go
git commit -m "feat(cmakegen): implement dependency discovery from cstow_deps"
```

---

### Task 3: Implement CMakeLists.txt generation

**Files:**
- Create: `internal/cmakegen/cmakelists.go`
- Modify: `internal/cmakegen/cmakegen_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/cmakegen/cmakegen_test.go`:

```go
package cmakegen

import (
	"strings"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCMakeLists_ExecutableNoDeps(t *testing.T) {
	opts := GenerateOptions{
		Name:    "myapp",
		Type:    "executable",
		Std:     "c++17",
		Sources: []string{"src/**/*.cpp"},
		Include: []string{"include"},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `project(myapp LANGUAGES CXX)`)
	assert.Contains(t, out, `set(CMAKE_CXX_STANDARD 17)`)
	assert.Contains(t, out, `add_executable(myapp ${SOURCES})`)
	assert.Contains(t, out, `target_include_directories(${PROJECT_NAME} PUBLIC include)`)
	assert.NotContains(t, out, "find_package")
}

func TestGenerateCMakeLists_LibraryNoDeps(t *testing.T) {
	opts := GenerateOptions{
		Name:    "mylib",
		Type:    "library",
		Std:     "c++20",
		Sources: []string{"src/**/*.cpp"},
		Include: []string{"include"},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `add_library(mylib ${SOURCES})`)
	assert.Contains(t, out, `set(CMAKE_CXX_STANDARD 20)`)
}

func TestGenerateCMakeLists_WithCMakeConfigDeps(t *testing.T) {
	opts := GenerateOptions{
		Name:    "myapp",
		Type:    "executable",
		Std:     "c++17",
		Sources: []string{"src/**/*.cpp"},
		Include: []string{"include"},
		Deps: []DepTarget{
			{Name: "fmt", TargetName: "fmt::fmt", Prefix: "/path/to/cstow_deps/fmt"},
			{Name: "spdlog", TargetName: "spdlog::spdlog", Prefix: "/path/to/cstow_deps/spdlog"},
		},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `find_package(fmt REQUIRED)`)
	assert.Contains(t, out, `find_package(spdlog REQUIRED)`)
	assert.Contains(t, out, `target_link_libraries(myapp PRIVATE fmt::fmt spdlog::spdlog)`)
	assert.Contains(t, out, `list(APPEND CMAKE_PREFIX_PATH`)
}

func TestGenerateCMakeLists_WithFallbackDeps(t *testing.T) {
	opts := GenerateOptions{
		Name:    "myapp",
		Type:    "executable",
		Std:     "c++17",
		Sources: []string{"src/**/*.cpp"},
		Include: []string{"include"},
		Deps: []DepTarget{
			{Name: "expected", Prefix: "/path/to/cstow_deps/expected"},
		},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.NotContains(t, out, "find_package")
	assert.Contains(t, out, `target_include_directories(myapp PRIVATE`)
	assert.Contains(t, out, `cstow_deps/expected`)
}

func TestGenerateCMakeLists_MixedDeps(t *testing.T) {
	opts := GenerateOptions{
		Name:    "myapp",
		Type:    "executable",
		Std:     "c++17",
		Sources: []string{"src/**/*.cpp"},
		Include: []string{"include"},
		Deps: []DepTarget{
			{Name: "fmt", TargetName: "fmt::fmt", Prefix: "/path/to/cstow_deps/fmt"},
			{Name: "expected", Prefix: "/path/to/cstow_deps/expected"},
		},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `find_package(fmt REQUIRED)`)
	assert.Contains(t, out, `target_include_directories(myapp PRIVATE`)
}

func TestGenerateCMakeLists_HeaderOnly(t *testing.T) {
	opts := GenerateOptions{
		Name:    "myheader",
		Type:    "header-only",
		Std:     "c++17",
		Sources: []string{},
		Include: []string{"include"},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `add_library(myheader INTERFACE)`)
	assert.Contains(t, out, `target_include_directories(myheader INTERFACE include)`)
	assert.NotContains(t, out, "file(GLOB_RECURSE")
}

func TestGenerateCMakeLists_WithDefines(t *testing.T) {
	opts := GenerateOptions{
		Name:     "myapp",
		Type:     "executable",
		Std:      "c++17",
		Sources:  []string{"src/**/*.cpp"},
		Include:  []string{"include"},
		Defines:  []string{"MY_DEFINE=1", "OTHER=2"},
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `target_compile_definitions(myapp PRIVATE MY_DEFINE=1 OTHER=2)`)
}

func TestGenerateCMakeLists_CMakeMinimumVersion(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Type: "executable",
		Std:  "c++17",
	}
	out, err := GenerateCMakeLists(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `cmake_minimum_required(VERSION 3.16)`)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cmakegen/ -run TestGenerateCMakeLists -v`
Expected: FAIL — `GenerateCMakeLists` not defined

- [ ] **Step 3: Implement GenerateCMakeLists**

Create `internal/cmakegen/cmakelists.go`:

```go
package cmakegen

import (
	"fmt"
	"path/filepath"
	"strings"
)

// stdToNumber converts a C++ standard string like "c++17" to the numeric version "17".
func stdToNumber(std string) string {
	return strings.TrimPrefix(std, "c++")
}

// GenerateCMakeLists produces a CMakeLists.txt content string from the given options.
func GenerateCMakeLists(opts GenerateOptions) string {
	var b strings.Builder

	cxxStd := stdToNumber(opts.Std)
	if cxxStd == "" {
		cxxStd = "17"
	}

	// Header
	b.WriteString(fmt.Sprintf("cmake_minimum_required(VERSION 3.16)\n"))
	b.WriteString(fmt.Sprintf("project(%s LANGUAGES CXX)\n\n", opts.Name))
	b.WriteString(fmt.Sprintf("set(CMAKE_CXX_STANDARD %s)\n", cxxStd))
	b.WriteString("set(CMAKE_CXX_STANDARD_REQUIRED ON)\n\n")

	// Sources and target
	if opts.Type == "header-only" {
		b.WriteString(fmt.Sprintf("add_library(%s INTERFACE)\n", opts.Name))
		if len(opts.Include) > 0 {
			incPaths := strings.Join(opts.Include, " ")
			b.WriteString(fmt.Sprintf("target_include_directories(%s INTERFACE %s)\n", opts.Name, incPaths))
		}
	} else {
		b.WriteString("file(GLOB_RECURSE SOURCES src/*.cpp)\n")
		if opts.Type == "executable" {
			b.WriteString(fmt.Sprintf("add_executable(%s ${SOURCES})\n", opts.Name))
		} else {
			b.WriteString(fmt.Sprintf("add_library(%s ${SOURCES})\n", opts.Name))
		}
		if len(opts.Include) > 0 {
			incPaths := strings.Join(opts.Include, " ")
			b.WriteString(fmt.Sprintf("target_include_directories(${PROJECT_NAME} PUBLIC %s)\n", incPaths))
		}
	}

	// Defines
	if len(opts.Defines) > 0 {
		b.WriteString(fmt.Sprintf("target_compile_definitions(%s PRIVATE %s)\n", opts.Name, strings.Join(opts.Defines, " ")))
	}

	// Dependencies with CMake config files
	var cmakeDeps []DepTarget
	var fallbackDeps []DepTarget
	for _, dep := range opts.Deps {
		if dep.TargetName != "" {
			cmakeDeps = append(cmakeDeps, dep)
		} else {
			fallbackDeps = append(fallbackDeps, dep)
		}
	}

	if len(cmakeDeps) > 0 {
		b.WriteString("\n# Dependencies (CMake config)\n")
		for _, dep := range cmakeDeps {
			relPath := filepath.Join("cstow_deps", dep.Name)
			b.WriteString(fmt.Sprintf("list(APPEND CMAKE_PREFIX_PATH \"${CMAKE_SOURCE_DIR}/%s\")\n", relPath))
			b.WriteString(fmt.Sprintf("find_package(%s REQUIRED)\n", dep.Name))
		}
		var targets []string
		for _, dep := range cmakeDeps {
			targets = append(targets, dep.TargetName)
		}
		b.WriteString(fmt.Sprintf("target_link_libraries(%s PRIVATE %s)\n", opts.Name, strings.Join(targets, " ")))
	}

	// Fallback dependencies (header-only without CMake config)
	if len(fallbackDeps) > 0 {
		b.WriteString("\n# Dependencies (header-only fallback)\n")
		var incPaths []string
		for _, dep := range fallbackDeps {
			incPaths = append(incPaths, fmt.Sprintf("${CMAKE_SOURCE_DIR}/cstow_deps/%s/include", dep.Name))
		}
		b.WriteString(fmt.Sprintf("target_include_directories(%s PRIVATE %s)\n", opts.Name, strings.Join(incPaths, " ")))
	}

	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cmakegen/ -run TestGenerateCMakeLists -v`
Expected: All 8 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmakegen/cmakelists.go internal/cmakegen/cmakegen_test.go
git commit -m "feat(cmakegen): implement CMakeLists.txt generation"
```

---

### Task 4: Implement CMakePresets.json generation

**Files:**
- Create: `internal/cmakegen/presets.go`
- Modify: `internal/cmakegen/cmakegen_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cmakegen/cmakegen_test.go`:

```go
func TestGeneratePresets_DebugAndRelease(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Std:  "c++17",
		Profiles: map[string]config.Profile{
			"debug":   {Optimize: "0", Debug: true},
			"release": {Optimize: "3", LTO: true},
		},
	}
	out, err := GeneratePresets(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `"version": 6`)
	assert.Contains(t, out, `"name": "debug"`)
	assert.Contains(t, out, `"CMAKE_BUILD_TYPE": "Debug"`)
	assert.Contains(t, out, `"name": "release"`)
	assert.Contains(t, out, `"CMAKE_BUILD_TYPE": "Release"`)
	assert.Contains(t, out, `"CMAKE_INTERPROCEDURAL_OPTIMIZATION": "ON"`)
	assert.Contains(t, out, `"CMAKE_EXPORT_COMPILE_COMMANDS": "ON"`)
	assert.Contains(t, out, `"CMAKE_CXX_STANDARD": "17"`)
}

func TestGeneratePresets_DefaultProfiles(t *testing.T) {
	opts := GenerateOptions{
		Name:     "myapp",
		Std:      "c++20",
		Profiles: map[string]config.Profile{},
	}
	out, err := GeneratePresets(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `"CMAKE_BUILD_TYPE": "Debug"`)
	assert.Contains(t, out, `"CMAKE_BUILD_TYPE": "Release"`)
	assert.Contains(t, out, `"CMAKE_CXX_STANDARD": "20"`)
}

func TestGeneratePresets_WithToolchain(t *testing.T) {
	opts := GenerateOptions{
		Name:      "myapp",
		Std:       "c++17",
		Toolchain: config.Toolchain{Compiler: "clang"},
		Profiles: map[string]config.Profile{
			"debug": {Debug: true},
		},
	}
	out, err := GeneratePresets(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `"CMAKE_CXX_COMPILER"`)
}

func TestGeneratePresets_BuildPresets(t *testing.T) {
	opts := GenerateOptions{
		Name: "myapp",
		Std:  "c++17",
		Profiles: map[string]config.Profile{
			"debug":   {Debug: true},
			"release": {LTO: true},
		},
	}
	out, err := GeneratePresets(opts)
	require.NoError(t, err)
	assert.Contains(t, out, `"configurePreset": "debug"`)
	assert.Contains(t, out, `"configurePreset": "release"`)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cmakegen/ -run TestGeneratePresets -v`
Expected: FAIL — `GeneratePresets` not defined

- [ ] **Step 3: Implement GeneratePresets**

Create `internal/cmakegen/presets.go`:

```go
package cmakegen

import (
	"encoding/json"
	"fmt"
	"sort"
)

type cmakePresetFile struct {
	Version           int               `json:"version"`
	ConfigurePresets  []configurePreset `json:"configurePresets"`
	BuildPresets      []buildPreset     `json:"buildPresets"`
}

type configurePreset struct {
	Name          string                 `json:"name"`
	BinaryDir     string                 `json:"binaryDir"`
	CacheVariables map[string]interface{} `json:"cacheVariables"`
}

type buildPreset struct {
	Name             string `json:"name"`
	ConfigurePreset  string `json:"configurePreset"`
}

// GeneratePresets produces a CMakePresets.json content string from the given options.
func GeneratePresets(opts GenerateOptions) (string, error) {
	cxxStd := stdToNumber(opts.Std)
	if cxxStd == "" {
		cxxStd = "17"
	}

	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = map[string]config.Profile{
			"debug":   {Optimize: "0", Debug: true},
			"release": {Optimize: "3", LTO: true},
		}
	}

	var configPresets []configurePreset
	var buildPresets []buildPreset

	// Sort profile names for deterministic output
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		p := profiles[name]
		buildType := "Debug"
		if name == "release" || p.Optimize == "3" || p.Optimize == "2" || p.Optimize == "s" || p.Optimize == "z" {
			buildType = "Release"
		}

		cacheVars := map[string]interface{}{
			"CMAKE_BUILD_TYPE":            buildType,
			"CMAKE_CXX_STANDARD":          cxxStd,
			"CMAKE_EXPORT_COMPILE_COMMANDS": "ON",
		}

		if p.LTO {
			cacheVars["CMAKE_INTERPROCEDURAL_OPTIMIZATION"] = "ON"
		}

		if opts.Toolchain.Compiler != "" && opts.Toolchain.Compiler != "auto" {
			cacheVars["CMAKE_CXX_COMPILER"] = fmt.Sprintf("CMAKE_CXX_COMPILER-%s", opts.Toolchain.Compiler)
		}

		configPresets = append(configPresets, configurePreset{
			Name:           name,
			BinaryDir:      fmt.Sprintf("${sourceDir}/build/%s", name),
			CacheVariables: cacheVars,
		})

		buildPresets = append(buildPresets, buildPreset{
			Name:            name,
			ConfigurePreset: name,
		})
	}

	presetFile := cmakePresetFile{
		Version:          6,
		ConfigurePresets: configPresets,
		BuildPresets:     buildPresets,
	}

	data, err := json.MarshalIndent(presetFile, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal presets: %w", err)
	}
	return string(data) + "\n", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cmakegen/ -run TestGeneratePresets -v`
Expected: All 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cmakegen/presets.go internal/cmakegen/cmakegen_test.go
git commit -m "feat(cmakegen): implement CMakePresets.json generation"
```

---

### Task 5: Run all cmakegen tests

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite for the package**

Run: `go test ./internal/cmakegen/ -v`
Expected: All tests PASS (DiscoverDeps 6 + GenerateCMakeLists 8 + GeneratePresets 4 = 18 tests)

- [ ] **Step 2: Run with race detector**

Run: `go test -race ./internal/cmakegen/`
Expected: PASS, no race conditions

---

### Task 6: Implement `cstow gen` CLI command

**Files:**
- Create: `cmd/gen.go`

- [ ] **Step 1: Write the gen command**

Create `cmd/gen.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/cmakegen"
	"github.com/all3n/cstow/internal/config"
	"github.com/spf13/cobra"
)

var genCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate CMake files from cstow.toml",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found in current directory")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		genCMake, _ := cmd.Flags().GetBool("cmakelists")
		genPresets, _ := cmd.Flags().GetBool("presets")
		force, _ := cmd.Flags().GetBool("force")

		// Discover dependencies
		var deps []cmakegen.DepTarget
		if entries, err := os.ReadDir("cstow_deps"); err == nil && len(entries) > 0 {
			deps, err = cmakegen.DiscoverDeps("cstow_deps")
			if err != nil {
				return fmt.Errorf("discover deps: %w", err)
			}
		}

		opts := cmakegen.GenerateOptions{
			Name:      cfg.Package.Name,
			Type:      cfg.Build.Type,
			Std:       cfg.Package.Std,
			Sources:   cfg.Build.Sources,
			Include:   cfg.Build.Include,
			Defines:   cfg.Build.Defines,
			Deps:      deps,
			Profiles:  cfg.Profiles,
			Toolchain: cfg.Toolchain,
		}

		if genCMake {
			content, err := cmakegen.GenerateCMakeLists(opts)
			if err != nil {
				return fmt.Errorf("generate CMakeLists.txt: %w", err)
			}
			if err := writeFile("CMakeLists.txt", content, force); err != nil {
				return err
			}
			fmt.Println(">> generated CMakeLists.txt")
		}

		if genPresets {
			content, err := cmakegen.GeneratePresets(opts)
			if err != nil {
				return fmt.Errorf("generate CMakePresets.json: %w", err)
			}
			if err := writeFile("CMakePresets.json", content, force); err != nil {
				return err
			}
			fmt.Println(">> generated CMakePresets.json")
		}

		return nil
	},
}

func writeFile(path, content string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists — use --force to overwrite", path)
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func init() {
	genCmd.Flags().Bool("cmakelists", true, "generate CMakeLists.txt")
	genCmd.Flags().Bool("presets", true, "generate CMakePresets.json")
	genCmd.Flags().Bool("force", false, "overwrite existing files")
	rootCmd.AddCommand(genCmd)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build -o cstow .`
Expected: success

- [ ] **Step 3: Test the command manually**

```bash
cd /tmp && mkdir test-gen && cd test-gen
cat > cstow.toml << 'EOF'
[package]
name = "testapp"
version = "0.1.0"
std = "c++17"

[build]
type = "executable"
sources = ["src/**/*.cpp"]
include = ["include"]

[profile.debug]
optimize = "0"
debug = true

[profile.release]
optimize = "3"
lto = true

[toolchain]
compiler = "auto"
EOF
/path/to/cstow gen
cat CMakeLists.txt
cat CMakePresets.json
```

Expected: Both files generated with correct content.

- [ ] **Step 4: Commit**

```bash
git add cmd/gen.go
git commit -m "feat: add cstow gen command for CMake file generation"
```

---

### Task 7: Add build fallback for auto-generation

**Files:**
- Modify: `cmd/build.go:89-96`

- [ ] **Step 1: Modify build.go to auto-generate CMakeLists.txt when missing**

In `cmd/build.go`, replace the source directory detection block (lines 88-96):

```go
			// Determine source directory
			sourceDir := "."
			if _, err := os.Stat("CMakeLists.txt"); err != nil {
				if cfg.Legacy != nil && cfg.Legacy.Root != "" {
					sourceDir = cfg.Legacy.Root
				} else if len(cfg.Build.Sources) > 0 && !strings.Contains(cfg.Build.Sources[0], "*") {
					// Use Build.Sources[0] only if it's a plain path (no globs)
					sourceDir = cfg.Build.Sources[0]
				}
			}
```

With:

```go
			// Determine source directory
			sourceDir := "."
			hasCMakeLists := true
			if _, err := os.Stat("CMakeLists.txt"); err != nil {
				hasCMakeLists = false
				if cfg.Legacy != nil && cfg.Legacy.Root != "" {
					sourceDir = cfg.Legacy.Root
				} else if len(cfg.Build.Sources) > 0 && !strings.Contains(cfg.Build.Sources[0], "*") {
					sourceDir = cfg.Build.Sources[0]
				}
			}
```

Then add after the dependency injection block (after the `cstow_deps` path injection, around line 118), before `cmakeArgs = append(cmakeArgs, tc.CMakeFlags()...)`:

Add this import to the top of `build.go`: `"github.com/all3n/cstow/internal/cmakegen"`

And add auto-generation logic just before `fmt.Printf(">> cmake configure (%s)\n", profile)`:

```go
			// Auto-generate CMakeLists.txt if missing
			if !hasCMakeLists && sourceDir == "." {
				fmt.Println(">> no CMakeLists.txt found, auto-generating from cstow.toml...")
				var deps []cmakegen.DepTarget
				if entries, derr := os.ReadDir("cstow_deps"); derr == nil && len(entries) > 0 {
					deps, _ = cmakegen.DiscoverDeps("cstow_deps")
				}
				genOpts := cmakegen.GenerateOptions{
					Name:      cfg.Package.Name,
					Type:      cfg.Build.Type,
					Std:       cfg.Package.Std,
					Sources:   cfg.Build.Sources,
					Include:   cfg.Build.Include,
					Defines:   cfg.Build.Defines,
					Deps:      deps,
					Profiles:  cfg.Profiles,
					Toolchain: cfg.Toolchain,
				}
				content, gerr := cmakegen.GenerateCMakeLists(genOpts)
				if gerr != nil {
					return fmt.Errorf("auto-generate CMakeLists.txt: %w", gerr)
				}
				if err := os.WriteFile("CMakeLists.txt", []byte(content), 0o644); err != nil {
					return fmt.Errorf("write CMakeLists.txt: %w", err)
				}
			}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build -o cstow .`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add cmd/build.go
git commit -m "feat: auto-generate CMakeLists.txt in build when missing"
```

---

### Task 8: Run full project test suite

**Files:** None (verification only)

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: All tests PASS

- [ ] **Step 2: Run with race detector**

Run: `go test -race ./...`
Expected: All tests PASS, no races

- [ ] **Step 3: Verify CLI help includes gen**

Run: `./cstow gen --help`
Expected: Shows help for gen command with `--cmakelists`, `--presets`, `--force` flags

---

## Self-Review

### Spec coverage
- [x] Dependency discovery from cstow_deps — Task 2
- [x] CMakeLists.txt generation with find_package — Task 3
- [x] CMakePresets.json generation — Task 4
- [x] `cstow gen` CLI command — Task 6
- [x] `cstow build` auto-generation fallback — Task 7
- [x] Unit tests — Tasks 2, 3, 4
- [x] Full test suite — Task 8

### Placeholder scan
No TBD/TODO/placeholders found.

### Type consistency
- `DepTarget` struct used consistently across discover.go, cmakelists.go, and gen.go
- `GenerateOptions` struct fields match usage in cmakelists.go and presets.go
- `stdToNumber` defined in cmakelists.go, used in presets.go (same package, accessible)
