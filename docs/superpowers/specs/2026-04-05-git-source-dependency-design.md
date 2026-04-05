# Git Source Dependency Design

Date: 2026-04-05

## Goal

Allow users to add dependencies directly from Git/GitHub repositories via `cstow add --source git`, specifying a tag and build options. The dependency is then built from source during `cstow fetch` or `cstow install`.

## CLI Interface

### `cstow add` new flags

```bash
cstow add <name> --source git --git-url <url> --tag <tag> \
  [--build-type static|shared|header-only] \
  [--cmake-define KEY=VAL] [--cxx-flags "flags"] [--link-flags "flags"]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--git-url <url>` | yes (when `--source git`) | Git repository URL |
| `--tag <tag>` | no | Git tag or branch (default: `main`) |
| `--cmake-define KEY=VAL` | no | Repeatable CMake defines |
| `--cxx-flags "flags"` | no | Additional C++ compiler flags |
| `--link-flags "flags"` | no | Additional linker flags |
| `--build-type` | no | static/shared/header-only (existing) |

### Validation

- `--source git` requires `--git-url`
- Validate tag exists via `git ls-remote --tags <url> <tag>` (skip if tag is blank/default)
- `--cmake-define` values must contain `=`

## Data Model

### `cstow.toml` schema

```toml
[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "git"
build_type = "static"
git = "https://github.com/fmtlib/fmt.git"
rev = "10.2.1"

[dependencies.cmake]
defines = ["BUILD_SHARED_LIBS=OFF"]
cxx_flags = ["-fPIC", "-Wall"]
link_flags = []
```

### `cstow.lock` schema

```toml
[[package]]
name = "fmt"
version = "10.2.1"
source = "git:https://github.com/fmtlib/fmt.git"
build_type = "static"
abi_tag = "gcc14-cxx17"
git = "https://github.com/fmtlib/fmt.git"
rev = "10.2.1"
```

### Go struct changes

**`config.Dependency`** (existing fields `Git`/`Rev` already defined, add `CMake`):

```go
type Dependency struct {
    Name      string   `toml:"name"`
    Version   string   `toml:"version"`
    Source    string   `toml:"source"`
    BuildType string   `toml:"build_type"`
    Path      string   `toml:"path"`
    Git       string   `toml:"git"`
    Rev       string   `toml:"rev"`
    CMake     GitCMake `toml:"cmake,omitempty"`
}

type GitCMake struct {
    Defines   []string `toml:"defines"`
    CXXFlags  []string `toml:"cxx_flags"`
    LinkFlags []string `toml:"link_flags"`
}
```

**`resolver.LockEntry`** (add `Git`/`Rev`):

```go
type LockEntry struct {
    // existing fields...
    Git string `toml:"git,omitempty"`
    Rev string `toml:"rev,omitempty"`
}
```

## Data Flow

### `cstow add --source git`

1. Validate: `--git-url` required, optionally verify tag via `git ls-remote`
2. Construct `Dependency{Source:"git", Git:url, Rev:tag, CMake:{...}}`
3. `resolver.AddDependency` writes to `cstow.toml`
4. `resolver.Resolve` handles `source=git`: version = tag, source = `"git:"+url`
5. Save `cstow.lock`

### `cstow fetch`

In the per-package loop, add a new branch before registry lookup:

```
if source starts with "git:":
    → skip registry
    → git clone --branch <tag> --depth 1 <url> to temp dir
    → build with CMake using defines/flags from Dependency.CMake
    → install to cache: ~/.cstow/cache/<name>/<tag>/<abi_tag>/<build_type>/
    → symlink to cstow_deps/
```

### `cstow install`

When `cstow.toml` has a git dependency for the named package, `install` reads git URL/tag/cmake options from the dependency and performs clone+build directly (same path as fetch).

## Build Integration

For git-sourced dependencies, the build step:

1. Clones to a temp dir using `repository.FetchGit(url, tag, tmpDir)`
2. Constructs `builder.Options` with:
   - `CMakeDefines`: merged from `Dependency.CMake.Defines` + `BUILD_SHARED_LIBS` (derived from `BuildType`)
   - `CXXFlags`: from `Dependency.CMake.CXXFlags`
   - `LinkFlags`: from `Dependency.CMake.LinkFlags`
   - `BuildType`, `Toolchain`, `Profile`: same as existing flow
3. Calls `builder.Build(...)` — same builder as repository packages

No new build system support needed; all git deps use CMake.

## Error Handling

- `git` not on PATH: clear error message
- `git ls-remote` fails (network/auth): report and suggest manual verification
- Clone fails: report URL and tag, suggest checking access
- Build fails: same error handling as existing `install`

## Testing Plan

1. **Unit tests**: `parseGitDependency`, resolver git branch, Dependency serialization/deserialization with `CMake` field
2. **Integration test**: clone a small real GitHub repo (e.g., a header-only lib), build it
3. **E2E test**: `add --source git` → `fetch` → verify `cstow_deps/` symlink and cache entry

## Scope

- CMake-based projects only (consistent with existing builder)
- No recursive git dependency resolution in this MVP
- No git submodule initialization support in this MVP
- No authentication handling beyond what `git` CLI provides (SSH keys, credential helpers)
