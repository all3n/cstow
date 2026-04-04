# Repository Core Design

**Date:** 2026-04-01  
**Scope:** `internal/repository/` package — Finder + Merge + data structures  
**Out of scope:** CLI commands (`cstow repo`), global config (`~/.cstow/config.toml`), git/archive repository sources

---

## Goal

Separate "build knowledge" (how to compile googletest) from "dependency declarations" (I want googletest). Package build configurations live in `~/.cstow/repository/`, project `cstow.toml` only declares version constraints. When a package is not found in any repository, `cstow add` fails with a clear error.

---

## Directory Layout

```
~/.cstow/repository/
  <first-letter>/
    <package-name>/
      package.toml          # shared build config for all versions
      versions/             # optional per-version overrides
        1.14.0.toml
      patches/              # optional patch files
        1.14.0-fix-msvc.patch
```

Index rule: first lowercase letter of package name; non-letter names go under `_`.

---

## Data Structures (`internal/repository/package.go`)

```go
type PackageDef struct {
    Package   PackageMeta         `toml:"package"`
    Versions  []string            `toml:"versions"`
    Source    SourceDef           `toml:"source"`
    Build     BuildDef            `toml:"build"`
    Artifacts ArtifactsDef        `toml:"artifacts"`
    Deps      []config.Dependency `toml:"dependencies"`
}

type PackageMeta struct {
    Name        string `toml:"name"`
    Description string `toml:"description"`
    Homepage    string `toml:"homepage"`
    License     string `toml:"license"`
}

type SourceDef struct {
    Type        string            `toml:"type"`          // git | archive
    URL         string            `toml:"url"`
    TagTemplate string            `toml:"tag_template"`  // "v{version}"
    URLTemplate string            `toml:"url_template"`
    SHA256      map[string]string `toml:"sha256_versions"` // version -> sha256
}

type BuildDef struct {
    System   string                      `toml:"system"`   // cmake|make|header-only
    Type     string                      `toml:"type"`     // static|shared|header-only
    CMake    CMakeBuildDef               `toml:"cmake"`
    Profiles map[string]ProfileOverride  `toml:"profile"`
    Compiler map[string]CompilerOverride `toml:"compiler"`
    Platform map[string]PlatformOverride `toml:"platform"`
}

type CMakeBuildDef struct {
    Defines        []string `toml:"defines"`
    CXXFlags       []string `toml:"cxx_flags"`
    InstallTargets []string `toml:"install_targets"`
}

type ProfileOverride struct {
    Defines  []string `toml:"defines"`
    CXXFlags []string `toml:"cxx_flags"`
}

type CompilerOverride struct {
    Defines  []string `toml:"defines"`
    CXXFlags []string `toml:"cxx_flags"`
}

type PlatformOverride struct {
    Defines  []string `toml:"defines"`
    CXXFlags []string `toml:"cxx_flags"`
}

type ArtifactsDef struct {
    IncludeDirs []string `toml:"include_dirs"`
    Libs        []string `toml:"libs"`
}

// VersionOverride: only fields that differ from package.toml
type VersionOverride struct {
    Source *SourceOverride `toml:"source"`
    Build  *BuildOverride  `toml:"build"`
    Patch  string          `toml:"patch"`
}

type SourceOverride struct {
    SHA256 string `toml:"sha256"`
}

type BuildOverride struct {
    CMake    *CMakeBuildDef               `toml:"cmake"`
    Compiler map[string]CompilerOverride  `toml:"compiler"`
}
```

---

## Finder (`internal/repository/finder.go`)

```go
type Finder struct {
    searchPaths []string // ordered, highest priority first
}

// NewFinder uses ~/.cstow/repository/ as the single search path.
// Future: accept extra paths from global config.
func NewFinder() *Finder

type ResolvedPkg struct {
    Def      *PackageDef
    Version  string           // resolved concrete version
    Override *VersionOverride // nil if no version-specific override
    RepoPath string           // which repository dir matched (for debug messages)
}

// Find returns the first matching package definition across all search paths.
// Returns an error (not nil+empty) when not found — callers must fail hard.
func (f *Finder) Find(name, versionConstraint string) (*ResolvedPkg, error)
```

**Find algorithm:**
1. Compute `letter = toLower(name[0])`, non-letter → `_`
2. For each `root` in `searchPaths`:
   - Check `root/<letter>/<name>/package.toml`; skip if missing
   - Load `PackageDef`, pick best version from `def.Versions` matching `versionConstraint` (reuse semver logic)
   - If no version matches, continue to next root
   - Load `versions/<matched>.toml` if it exists
   - Return `ResolvedPkg`
3. If no root matched: `return nil, fmt.Errorf("package %q not found in any repository (constraint: %s)", name, versionConstraint)`

Version selection uses the same highest-matching-semver logic as `resolver.pickBest`.

---

## Merge (`internal/repository/merge.go`)

```go
type MergedBuildConfig struct {
    System      string   // cmake|make|header-only
    CMakeDefines []string
    CXXFlags    []string
    LinkFlags   []string
    IncludeDirs []string
    Libs        []string
    Patch       string
}

// Merge applies layers in priority order (lowest → highest):
//   1. package-level CMake defines + cxx_flags
//   2. profile override (debug/release)
//   3. compiler override (gcc/clang/msvc)
//   4. platform override (linux/macos/windows)
//   5. version-specific override (full replacement of cmake.defines if non-empty)
func Merge(pkg *PackageDef, ver *VersionOverride,
           toolchainKind, profile, goos string) *MergedBuildConfig
```

Layer 5 (version override) **replaces** rather than appends `cmake.defines` — matching `README.md` spec. All other layers append.

---

## Integration Points

### `cmd/add.go`
After parsing `name@version`, call `finder.Find(name, version)` before writing `cstow.toml`. If not found, return the error immediately (no partial state written).

### `cmd/build.go`
After `toolchain.Detect()`, for each dependency in the lock file call `finder.Find()` + `Merge()`. Inject `MergedBuildConfig.CMakeDefines` as `-D` flags and `CXXFlags` as `CMAKE_CXX_FLAGS`. If a dep has no repository entry, warn but continue (build may fail at cmake level — the package might be header-only or managed differently).

---

## Testing

**`finder_test.go`**
- Use `t.TempDir()` as fake repository root; no real `~/.cstow/` touched
- Cases: found exact version, found via semver range, version override loaded, package not found (error message check), non-letter package name (`_` dir)

**`merge_test.go`**
- Table-driven; each row specifies profile/compiler/platform inputs and expected output slices
- Cases: profile appends, compiler appends, platform appends, version override replaces defines, all layers combined
