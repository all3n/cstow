# Repository System Design

Date: 2026-03-31

## Summary

Implement a centralized package definition repository system for cstow. Build knowledge (how to compile a C++ library) lives in `~/.cstow/repository/<letter>/<pkg>/package.toml`, separate from project-level dependency declarations (`cstow.toml`). This allows teams and communities to share build recipes, with version-specific overrides and multi-repository search.

## Motivation

Currently `cstow add` only downloads pre-compiled packages from S3 registries. If a pre-compiled binary doesn't exist for your ABI/platform, you're stuck. The repository system provides:

1. **Build recipes** — How to fetch source, configure CMake, and compile any version of a library
2. **Shared knowledge** — Teams maintain one set of build configs that all projects reuse
3. **Version overrides** — Version-specific patches and flags without duplicating the whole recipe

## Phased Implementation

### Phase 1: Repository Data Layer (`internal/repository/`)

The core data model, TOML parsing, search, and config merge logic. No CLI changes yet.

#### Module Structure

```
internal/repository/
├── package.go      # PackageDef, SourceDef, BuildDef, ArtifactsDef, DepRef
├── version.go      # VersionOverride struct + loading logic
├── finder.go       # Finder: multi-path search + version matching
├── merge.go        # Merge: layered config composition
├── loader.go       # loadPackage(), loadVersionOverride() TOML parsing
└── package_test.go # Tests
```

#### Data Structures

```go
// PackageDef — full package.toml structure
type PackageDef struct {
    Package   PackageMeta         `toml:"package"`
    Versions  []string            `toml:"versions"`
    Source    SourceDef           `toml:"source"`
    Build     BuildDef            `toml:"build"`
    Artifacts ArtifactsDef        `toml:"artifacts"`
    Deps      []DepRef            `toml:"dependencies"`
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
    URLTemplate string            `toml:"url_template"`  // archive URL template
    SHA256Versions map[string]string `toml:"sha256_versions"`
}

type BuildDef struct {
    System   string                          `toml:"system"`   // cmake|make|meson|header-only|custom
    Type     string                          `toml:"type"`     // static|shared|header-only|both
    CMake    CMakeBuildDef                   `toml:"cmake"`
    Profiles map[string]ProfileOverride      `toml:"profile"`
    Compiler map[string]CompilerOverride     `toml:"compiler"`
    Platform map[string]PlatformOverride     `toml:"platform"`
}

type CMakeBuildDef struct {
    Defines         []string `toml:"defines"`
    InstallTargets  []string `toml:"install_targets"`
    CXXFlags        []string `toml:"cxx_flags"`
}

type ProfileOverride struct {
    Defines   []string `toml:"defines"`
    CXXFlags  []string `toml:"cxx_flags"`
}

type CompilerOverride struct {
    Defines   []string `toml:"defines"`
    CXXFlags  []string `toml:"cxx_flags"`
    Patch     string   `toml:"patch"`
}

type PlatformOverride struct {
    Defines   []string `toml:"defines"`
    CXXFlags  []string `toml:"cxx_flags"`
}

type ArtifactsDef struct {
    IncludeDirs []string `toml:"include_dirs"`
    Libs        []string `toml:"libs"`
}

type DepRef struct {
    Name    string `toml:"name"`
    Version string `toml:"version"`
}

// ResolvedPkg — Finder output, fully resolved package + optional version override
type ResolvedPkg struct {
    Def      *PackageDef
    Version  string
    Override *VersionOverride
    RepoPath string // which repository it came from
}

// VersionOverride — only stores differences from PackageDef, nil = inherit
type VersionOverride struct {
    Source *SourceOverride `toml:"source"`
    Build  *BuildOverride  `toml:"build"`
}

type SourceOverride struct {
    SHA256 string `toml:"sha256"`
}

type BuildOverride struct {
    CMake    *CMakeBuildOverride    `toml:"cmake"`
    Compiler map[string]CompilerOverride `toml:"compiler"`
}

type CMakeBuildOverride struct {
    Defines []string `toml:"defines"`
}
```

#### Finder Logic

- Input: package name + semver constraint
- Search paths from GlobalConfig, ordered by priority + builtin `~/.cstow/repository/` last
- Index rule: first lowercase letter of package name → subdirectory. Non-letter starts go to `_/`
- Version matching: use existing `Masterminds/semver/v3` — pick highest matching version
- Return first match across all search paths

```go
func (f *Finder) Find(name, version string) (*ResolvedPkg, error)
```

#### Merge Logic

Priority low → high:
1. `package.toml` base build config
2. Version override (versions/x.y.z.toml) — replaces matching fields
3. Profile overlay (debug/release)
4. Compiler adaptation (gcc/clang/msvc)
5. Platform adaptation (linux/macos/windows)
6. Global build flags (~/.cstow/config.toml)
7. Environment variables (CSTOW_CXX_FLAGS, CSTOW_DEFINES)

```go
type MergedBuildConfig struct {
    System       string
    CMakeDefines []string
    CXXFlags     []string
    LinkFlags    []string
    Profile      string
    Patch        string
}

func Merge(pkg *PackageDef, ver *VersionOverride, global *globalcfg.GlobalConfig,
    toolchain *toolchain.Toolchain, profile string) *MergedBuildConfig
```

#### Tests

- TOML round-trip: load package.toml → verify all fields → serialize → reload
- Finder: create temp repository structure, test search across multiple paths
- Merge: verify layer priority, version override replacement, env var injection
- Version matching: semver constraint resolution for "^1.14", ">=1.12,<2.0", "*"

---

### Phase 2: Global Config (`internal/globalcfg/`)

Parse `~/.cstow/config.toml` and provide repository search paths to Finder.

#### Module Structure

```
internal/globalcfg/
├── config.go       # GlobalConfig struct + Load() / Save()
└── config_test.go  # Tests
```

#### Data Structures

```go
type GlobalConfig struct {
    Defaults     DefaultsDef     `toml:"defaults"`
    Cache        CacheDef        `toml:"cache"`
    Repositories []RepoRef       `toml:"repositories"`
    Registry     []RegistryRef   `toml:"registry"`
    Toolchain    ToolchainDef    `toml:"toolchain"`
    Build        BuildFlagsDef   `toml:"build"`
    Network      NetworkDef      `toml:"network"`
}

type DefaultsDef struct {
    Std     string `toml:"std"`     // default: "c++17"
    Profile string `toml:"profile"` // default: "debug"
    Jobs    int    `toml:"jobs"`    // 0 = runtime.NumCPU()
    Color   bool   `toml:"color"`   // default: true
}

type CacheDef struct {
    Dir           string `toml:"dir"`             // default: "~/.cstow/cache"
    MaxSizeGB     int    `toml:"max_size_gb"`     // 0 = unlimited
    RetentionDays int    `toml:"retention_days"`  // 0 = forever
}

type RepoRef struct {
    Name       string `toml:"name"`
    Path       string `toml:"path"`
    Git        string `toml:"git"`
    Branch     string `toml:"branch"`
    Archive    string `toml:"archive"`
    Priority   int    `toml:"priority"`     // lower = higher priority, default: 50
    AutoUpdate bool   `toml:"auto_update"`
}

type RegistryRef struct {
    Name      string `toml:"name"`
    URL       string `toml:"url"`
    Provider  string `toml:"provider"`    // aws | cloudflare | minio | custom
    Region    string `toml:"region"`
    KeyEnv    string `toml:"key_env"`
    SecretEnv string `toml:"secret_env"`
    ReadOnly  bool   `toml:"read_only"`
}

type ToolchainDef struct {
    Prefer   string `toml:"prefer"`    // auto | gcc | clang | msvc
    MinGCC   string `toml:"min_gcc"`
    MinClang string `toml:"min_clang"`
}

type BuildFlagsDef struct {
    CXXFlags  []string `toml:"cxx_flags"`
    LinkFlags []string `toml:"link_flags"`
    Defines   []string `toml:"defines"`
}

type NetworkDef struct {
    Proxy      string   `toml:"proxy"`
    NoProxy    []string `toml:"no_proxy"`
    TimeoutSec int      `toml:"timeout_sec"`
    Retries    int      `toml:"retries"`
}
```

#### Key Functions

```go
// CstowHome returns ~/.cstow/, overridable via CSTOW_HOME env var
func CstowHome() string

// Load reads ~/.cstow/config.toml. Returns zero-value defaults if file missing (no error).
func Load() (*GlobalConfig, error)

// Save writes config to ~/.cstow/config.toml, creating directory if needed.
func Save(cfg *GlobalConfig) error

// RepoSearchPaths returns ordered search paths: user repos (by priority) + builtin repository
func (cfg *GlobalConfig) RepoSearchPaths() []string
```

#### Behavior

- Missing config file → return defaults, not an error
- `CSTOW_HOME` env var overrides `~/.cstow/`
- `RepoSearchPaths()` sorts by Priority ascending, appends builtin last
- Phase 2 only supports local path repositories. Git/Archive repo types are stored in config but not yet resolved.

---

### Phase 3: CLI Integration

Wire repository system into CLI commands and `cstow add` workflow.

#### New Commands

**`cstow repo add`** — Register a repository
```bash
cstow repo add --name team --path /mnt/shared/cstow-pkgs
cstow repo add --name community --git https://github.com/org/pkgs.git  # stored but not cloned yet
```

**`cstow repo list`** — Show registered repositories
```
[10] team         /mnt/shared/cstow-pkgs    (local)
[50] community    https://github.com/...     (git, main)
[99] builtin      ~/.cstow/repository       (local, always last)
```

**`cstow search <keyword>`** — Search packages across all repositories
```
[team]        googletest   1.14.0, 1.13.0, 1.12.1, 1.11.0
[builtin]     googletest   1.14.0, 1.13.0
[builtin]     fmt          11.0.0, 10.2.1
```

#### Modified `cstow add` Workflow

Current: `cstow add pkg@ver` → S3 registry fetch

New flow:
1. Load `~/.cstow/config.toml` → get search paths
2. `Finder.Find(name, version)` → `ResolvedPkg` (build recipe)
3. `Merge(pkg, override, globalCfg, toolchain, profile)` → `MergedBuildConfig`
4. Check local cache `~/.cstow/cache/<name>/<ver>/<abi_tag>/`
   - Hit → symlink to project, done
   - Miss → try S3 registry fetch (existing logic)
   - Still miss → source build using MergedBuildConfig
5. Write to cache + update `cstow.lock` + append `cstow.toml`

#### Files Changed

| File | Change |
|------|--------|
| `cmd/repo.go` | New: `cstow repo add/list/update` subcommands |
| `cmd/search.go` | New: `cstow search` command |
| `cmd/add.go` | Modified: integrate Finder + Merge before S3 fallback |
| `cmd/root.go` | Modified: add repo subcommand |
| `internal/resolver/resolver.go` | Modified: add FindInRepository method to Registry interface |

#### Fallback Behavior

If no repository entry exists for a package, `cstow add` falls back to the existing S3 registry behavior. This ensures backwards compatibility — projects that don't use the repository system continue to work identically.

---

## Out of Scope (Future Work)

- Git repository auto-clone/pull (stored in config but not resolved)
- Archive repository extraction
- `cstow repo update` (just prints "not yet implemented")
- Cache LRU eviction based on `max_size_gb` / `retention_days`
- Network proxy support for source fetching
- `cstow repo remove` command
