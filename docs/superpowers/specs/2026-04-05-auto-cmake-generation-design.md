# Auto CMake File Generation Design

Date: 2026-04-05

## Summary

Auto-generate `CMakeLists.txt` and `CMakePresets.json` from `cstow.toml` configuration, with automatic dependency detection from `cstow_deps/`. Triggered via a new `cstow gen` command and as a fallback during `cstow build`.

## Motivation

Currently `cstow init` generates a basic CMakeLists.txt, but when a project doesn't have one (or dependencies change), the user must manually write or update CMake files. This feature automates that by deriving CMake configuration from the cstow project manifest.

## Architecture

New package: `internal/cmakegen/`

```
internal/cmakegen/
├── cmakegen.go       # Public types and entry functions
├── cmakelists.go     # CMakeLists.txt generation
├── presets.go        # CMakePresets.json generation
├── discover.go       # cstow_deps dependency discovery
├── cmakegen_test.go
├── discover_test.go
└── presets_test.go
```

## Core Types

```go
// DepTarget describes a discovered dependency's CMake information.
type DepTarget struct {
    Name       string // dependency directory name
    ConfigFile string // path to found *Config.cmake file
    TargetName string // inferred CMake target (e.g. "fmt::fmt")
    Prefix     string // dependency install prefix path
}

// GenerateOptions holds all data needed to generate CMake files.
type GenerateOptions struct {
    Name       string
    Type       string                     // executable | library | header-only
    Std        string                     // c++17 etc
    Sources    []string
    Include    []string
    Defines    []string
    Deps       []DepTarget
    Profiles   map[string]config.Profile
    Toolchain  config.Toolchain
}
```

## Dependency Discovery (`discover.go`)

### Algorithm

1. Read entries in `cstow_deps/` directory
2. For each subdirectory, recursively search for `*Config.cmake` or `*-config.cmake` files
3. Extract package name by stripping the `Config.cmake` or `-config.cmake` suffix
4. Infer target name as `<package>::<package>` (e.g. `fmtConfig.cmake` → `fmt::fmt`)

### Fallback for header-only libraries

When no Config file is found, fall back to:
```cmake
target_include_directories(${PROJECT_NAME} PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/<name>/include)
```

### Function

```go
func DiscoverDeps(depsDir string) ([]DepTarget, error)
```

## CMakeLists.txt Generation (`cmakelists.go`)

### Output structure

```cmake
cmake_minimum_required(VERSION 3.16)
project(<name> LANGUAGES CXX)

set(CMAKE_CXX_STANDARD <std_num>)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
# If executable:
add_executable(<name> ${SOURCES})
# If library:
add_library(<name> ${SOURCES})

target_include_directories(${PROJECT_NAME} PUBLIC include)

# For each discovered dependency:
list(APPEND CMAKE_PREFIX_PATH "${CMAKE_SOURCE_DIR}/cstow_deps/<dep>")
find_package(<dep> REQUIRED)
target_link_libraries(${PROJECT_NAME} PRIVATE <dep>::<dep>)

# For fallback (no config file) dependencies:
target_include_directories(${PROJECT_NAME} PRIVATE
    ${CMAKE_SOURCE_DIR}/cstow_deps/<dep>/include)
```

### Function

```go
func GenerateCMakeLists(opts GenerateOptions) (string, error)
```

## CMakePresets.json Generation (`presets.go`)

### Mapping

| cstow.toml field | CMakePresets field |
|---|---|
| `profile.debug` | `CMAKE_BUILD_TYPE=Debug` |
| `profile.release` + `lto=true` | `CMAKE_INTERPROCEDURAL_OPTIMIZATION=ON` |
| `toolchain.compiler` | `CMAKE_CXX_COMPILER` |
| `build.std` | `CMAKE_CXX_STANDARD` |
| (always) | `CMAKE_EXPORT_COMPILE_COMMANDS=ON` |

### Output example

```json
{
  "version": 6,
  "configurePresets": [
    {
      "name": "debug",
      "binaryDir": "${sourceDir}/build/debug",
      "cacheVariables": {
        "CMAKE_BUILD_TYPE": "Debug",
        "CMAKE_CXX_STANDARD": "17",
        "CMAKE_EXPORT_COMPILE_COMMANDS": "ON"
      }
    },
    {
      "name": "release",
      "binaryDir": "${sourceDir}/build/release",
      "cacheVariables": {
        "CMAKE_BUILD_TYPE": "Release",
        "CMAKE_INTERPROCEDURAL_OPTIMIZATION": "ON",
        "CMAKE_CXX_STANDARD": "17"
      }
    }
  ],
  "buildPresets": [
    { "name": "debug", "configurePreset": "debug" },
    { "name": "release", "configurePreset": "release" }
  ]
}
```

### Function

```go
func GeneratePresets(opts GenerateOptions) (string, error)
```

## CLI Integration

### `cstow gen` command

```
cstow gen [--cmakelists] [--presets] [--force]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--cmakelists` | true | Generate CMakeLists.txt |
| `--presets` | true | Generate CMakePresets.json |
| `--force` | false | Overwrite existing files (default: warn and skip) |

**Flow**:
1. Load `cstow.toml` from current directory
2. Call `DiscoverDeps("cstow_deps")` if directory exists
3. Build `GenerateOptions` from config + discovered deps
4. Generate requested files
5. Write to current directory (skip existing unless `--force`)

### `cstow build` fallback

In `cmd/build.go`, when `CMakeLists.txt` does not exist:

1. Print `>> no CMakeLists.txt found, auto-generating from cstow.toml...`
2. Call `cmakegen.DiscoverDeps("cstow_deps")`
3. Call `cmakegen.GenerateCMakeLists(opts)` and write to `CMakeLists.txt`
4. Continue normal build flow

Similarly for CMakePresets.json: auto-generate if missing.

## Testing

- **Unit tests**: `cmakegen_test.go` — verify CMakeLists.txt output for executable, library, header-only types with and without dependencies
- **Discovery tests**: `discover_test.go` — create temp directory with fake `*Config.cmake` files, verify correct target inference
- **Presets tests**: `presets_test.go` — verify JSON output matches expected structure for various profile configurations
- **Integration**: `cmd/gen_test.go` — test the CLI command end-to-end with a temp project

## Scope

- MVP: CMakeLists.txt + CMakePresets.json generation with auto-dependency discovery
- Future: support for custom CMake templates, `.cmake` module generation, Meson presets
