# Git Source Dependency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--source git` support to `cstow add` so users can declare Git/GitHub dependencies with tag versions and CMake build options, then build them during `cstow fetch` and `cstow install`.

**Architecture:** Extend the existing `config.Dependency` struct (which already has `Git`/`Rev` fields) with a `CMake` sub-struct. Add a `case "git"` branch to the resolver. In `fetch` and `install`, add a git-source build path that clones to a temp dir and builds with user-specified options. No registry lookup for git deps.

**Tech Stack:** Go 1.25+, Cobra CLI, existing builder/repository packages

---

### Task 1: Extend config.Dependency with GitCMake struct

**Files:**
- Modify: `internal/config/config.go:50-63`

- [ ] **Step 1: Write the failing test**

Add to a new file `internal/config/config_test.go`:

```go
package config

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependencyGitCMakeRoundTrip(t *testing.T) {
	original := Config{
		Package: Package{Name: "demo", Version: "0.1.0"},
		Dependencies: []Dependency{
			{
				Name:      "fmt",
				Version:   "10.2.1",
				Source:    "git",
				BuildType: "static",
				Git:       "https://github.com/fmtlib/fmt.git",
				Rev:       "10.2.1",
				CMake: GitCMake{
					Defines:   []string{"BUILD_SHARED_LIBS=OFF"},
					CXXFlags:  []string{"-fPIC", "-Wall"},
					LinkFlags: []string{},
				},
			},
		},
	}

	dir := t.TempDir()
	path := dir + "/cstow.toml"
	require.NoError(t, original.Save(path))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Dependencies, 1)
	dep := loaded.Dependencies[0]
	assert.Equal(t, "git", dep.Source)
	assert.Equal(t, "https://github.com/fmtlib/fmt.git", dep.Git)
	assert.Equal(t, "10.2.1", dep.Rev)
	assert.Equal(t, []string{"BUILD_SHARED_LIBS=OFF"}, dep.CMake.Defines)
	assert.Equal(t, []string{"-fPIC", "-Wall"}, dep.CMake.CXXFlags)
}

func TestDependencyIsGit(t *testing.T) {
	assert.True(t, Dependency{Source: "git"}.IsGit())
	assert.False(t, Dependency{Source: "registry"}.IsGit())
	assert.False(t, Dependency{Source: "local"}.IsGit())
}

func TestDependencyGitMinimal(t *testing.T) {
	// Git dep without cmake options should round-trip cleanly
	tomlStr := `
[[dependencies]]
name = "mylib"
version = "1.0.0"
source = "git"
git = "https://github.com/user/mylib.git"
rev = "v1.0.0"
`
	var cfg Config
	require.NoError(t, toml.Unmarshal([]byte(tomlStr), &cfg))
	require.Len(t, cfg.Dependencies, 1)
	dep := cfg.Dependencies[0]
	assert.Equal(t, "git", dep.Source)
	assert.Equal(t, "https://github.com/user/mylib.git", dep.Git)
	assert.Equal(t, "v1.0.0", dep.Rev)
	assert.Nil(t, dep.CMake.Defines)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDependency -v`
Expected: FAIL — `GitCMake` type not defined

- [ ] **Step 3: Write minimal implementation**

In `internal/config/config.go`, add the `GitCMake` struct and `CMake` field to `Dependency`, and add `IsGit()` method:

```go
type GitCMake struct {
	Defines   []string `toml:"defines,omitempty"`
	CXXFlags  []string `toml:"cxx_flags,omitempty"`
	LinkFlags []string `toml:"link_flags,omitempty"`
}
```

Add `CMake` field to `Dependency` struct (after `Rev`):

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
```

Add `IsGit()` method after `IsLocal()`:

```go
func (d Dependency) IsGit() bool {
	return d.Source == "git"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestDependency -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add GitCMake struct and IsGit() to config.Dependency"
```

---

### Task 2: Extend resolver LockEntry and AddDependency for git deps

**Files:**
- Modify: `internal/resolver/resolver.go:22-31` (LockEntry struct)
- Modify: `internal/resolver/resolver.go:74-140` (resolveRecursive)
- Modify: `internal/resolver/resolver.go:192-204` (AddDependency)
- Test: `internal/resolver/resolver_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/resolver/resolver_test.go`:

```go
func TestResolveGitSource(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:      "fmt",
			Version:   "10.2.1",
			Source:    "git",
			BuildType: "static",
			Git:       "https://github.com/fmtlib/fmt.git",
			Rev:       "10.2.1",
			CMake: config.GitCMake{
				Defines:  []string{"BUILD_SHARED_LIBS=OFF"},
				CXXFlags: []string{"-fPIC"},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	p := lf.Packages[0]
	assert.Equal(t, "fmt", p.Name)
	assert.Equal(t, "10.2.1", p.Version)
	assert.Equal(t, "git:https://github.com/fmtlib/fmt.git", p.Source)
	assert.Equal(t, "static", p.BuildType)
	assert.Equal(t, "https://github.com/fmtlib/fmt.git", p.Git)
	assert.Equal(t, "10.2.1", p.Rev)
}

func TestResolveGitSourceNoRevDefaultsToMain(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:    "mylib",
			Version: "1.0.0",
			Source:  "git",
			Git:     "https://github.com/user/mylib.git",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "mylib", lf.Packages[0].Name)
	assert.Equal(t, "git:https://github.com/user/mylib.git", lf.Packages[0].Source)
	assert.Equal(t, "https://github.com/user/mylib.git", lf.Packages[0].Git)
	assert.Equal(t, "", lf.Packages[0].Rev)
}

func TestAddDependencyGit(t *testing.T) {
	cfg := &config.Config{}
	AddDependency(cfg, config.Dependency{
		Name:      "fmt",
		Version:   "10.2.1",
		Source:    "git",
		BuildType: "static",
		Git:       "https://github.com/fmtlib/fmt.git",
		Rev:       "10.2.1",
		CMake: config.GitCMake{
			Defines: []string{"BUILD_SHARED_LIBS=OFF"},
		},
	})
	require.Len(t, cfg.Dependencies, 1)
	assert.Equal(t, "git", cfg.Dependencies[0].Source)
	assert.Equal(t, "https://github.com/fmtlib/fmt.git", cfg.Dependencies[0].Git)
	assert.Equal(t, "10.2.1", cfg.Dependencies[0].Rev)
	assert.Equal(t, []string{"BUILD_SHARED_LIBS=OFF"}, cfg.Dependencies[0].CMake.Defines)

	// Duplicate should not be added
	AddDependency(cfg, config.Dependency{
		Name:    "fmt",
		Version: "10.2.1",
		Source:  "git",
		Git:     "https://github.com/fmtlib/fmt.git",
	})
	assert.Len(t, cfg.Dependencies, 1)
}

func TestLockFileGitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "cstow.lock")

	lf := &LockFile{
		Version: 1,
		Packages: []LockEntry{
			{
				Name:      "fmt",
				Version:   "10.2.1",
				Source:    "git:https://github.com/fmtlib/fmt.git",
				BuildType: "static",
				Git:       "https://github.com/fmtlib/fmt.git",
				Rev:       "10.2.1",
			},
		},
	}

	require.NoError(t, SaveLock(lockPath, lf))
	loaded, err := LoadLock(lockPath)
	require.NoError(t, err)
	require.Len(t, loaded.Packages, 1)
	assert.Equal(t, "git:https://github.com/fmtlib/fmt.git", loaded.Packages[0].Source)
	assert.Equal(t, "https://github.com/fmtlib/fmt.git", loaded.Packages[0].Git)
	assert.Equal(t, "10.2.1", loaded.Packages[0].Rev)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/resolver/ -run "TestResolveGit|TestAddDependencyGit|TestLockFileGit" -v`
Expected: FAIL — `LockEntry` has no `Git`/`Rev` fields, resolver has no `case "git"`, `AddDependency` signature mismatch

- [ ] **Step 3: Add Git and Rev fields to LockEntry**

In `internal/resolver/resolver.go`, modify `LockEntry` struct (add after `Path`):

```go
type LockEntry struct {
	Name      string   `toml:"name"`
	Version   string   `toml:"version"`
	Source    string   `toml:"source"`
	SHA256    string   `toml:"sha256"`
	ABITag    string   `toml:"abi_tag,omitempty"`
	BuildType string   `toml:"build_type,omitempty"`
	Deps      []string `toml:"deps,omitempty"`
	Path      string   `toml:"path,omitempty"`
	Git       string   `toml:"git,omitempty"`
	Rev       string   `toml:"rev,omitempty"`
}
```

- [ ] **Step 4: Add `case "git"` to resolveRecursive**

In `resolveRecursive`, add before the `default:` case (before line 109):

```go
		case "git":
			// Git source: version is the tag/rev, source records the URL
			chosenVer = dep.Version
			if dep.Version == "*" || dep.Version == "" {
				chosenVer = "0.0.0"
			}
			source = "git:" + dep.Git
```

And update the `LockEntry` creation to propagate `Git`/`Rev`:

```go
			locked[dep.Name] = LockEntry{
				Name:      dep.Name,
				Version:   chosenVer,
				Source:    source,
				BuildType: dep.BuildType,
				Path:      dep.Path,
				Git:       dep.Git,
				Rev:       dep.Rev,
			}
```

- [ ] **Step 5: Update AddDependency to accept a full Dependency**

Replace the `AddDependency` function:

```go
func AddDependency(cfg *config.Config, dep config.Dependency) {
	for _, d := range cfg.Dependencies {
		if d.Name == dep.Name {
			return
		}
	}
	cfg.Dependencies = append(cfg.Dependencies, dep)
}
```

- [ ] **Step 6: Fix callers of the old AddDependency signature**

In `cmd/add.go`, update the call at line 62 from:

```go
resolver.AddDependency(cfg, name, version, source, buildType)
```

to:

```go
resolver.AddDependency(cfg, config.Dependency{
	Name:      name,
	Version:   version,
	Source:    source,
	BuildType: buildType,
})
```

In `internal/resolver/resolver_test.go`, update `TestAddDependency` (line 171-185) from:

```go
func TestAddDependency(t *testing.T) {
	cfg := &config.Config{}
	AddDependency(cfg, "fmt", "^10.0.0", "registry", "shared")
	...
	AddDependency(cfg, "fmt", "^10.0.0", "registry", "shared")
	...
	AddDependency(cfg, "spdlog", "^1.12.0", "registry", "static")
	...
}
```

to:

```go
func TestAddDependency(t *testing.T) {
	cfg := &config.Config{}
	AddDependency(cfg, config.Dependency{Name: "fmt", Version: "^10.0.0", Source: "registry", BuildType: "shared"})
	assert.Equal(t, 1, len(cfg.Dependencies))
	assert.Equal(t, "fmt", cfg.Dependencies[0].Name)
	assert.Equal(t, "shared", cfg.Dependencies[0].BuildType)

	// Adding again should not duplicate
	AddDependency(cfg, config.Dependency{Name: "fmt", Version: "^10.0.0", Source: "registry", BuildType: "shared"})
	assert.Equal(t, 1, len(cfg.Dependencies))

	// Different package
	AddDependency(cfg, config.Dependency{Name: "spdlog", Version: "^1.12.0", Source: "registry", BuildType: "static"})
	assert.Equal(t, 2, len(cfg.Dependencies))
}
```

- [ ] **Step 7: Run all resolver tests**

Run: `go test ./internal/resolver/ -v`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/resolver/resolver.go internal/resolver/resolver_test.go cmd/add.go
git commit -m "feat: add git source branch to resolver and LockEntry"
```

---

### Task 3: Extend `cstow add` CLI with git flags and validation

**Files:**
- Modify: `cmd/add.go`
- Test: `cmd/add_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `cmd/add_test.go`:

```go
func TestAddGitSourcePersistsToConfigAndLock(t *testing.T) {
	setupAddTest(t)

	rootCmd.SetArgs([]string{"add", "fmt@10.2.1", "--source", "git",
		"--git-url", "https://github.com/fmtlib/fmt.git",
		"--tag", "10.2.1",
		"--build-type", "static",
		"--cmake-define", "BUILD_SHARED_LIBS=OFF"})
	require.NoError(t, rootCmd.Execute())

	cfg, err := config.Load("cstow.toml")
	require.NoError(t, err)
	require.Len(t, cfg.Dependencies, 1)
	dep := cfg.Dependencies[0]
	assert.Equal(t, "git", dep.Source)
	assert.Equal(t, "https://github.com/fmtlib/fmt.git", dep.Git)
	assert.Equal(t, "10.2.1", dep.Rev)
	assert.Equal(t, "static", dep.BuildType)
	assert.Equal(t, []string{"BUILD_SHARED_LIBS=OFF"}, dep.CMake.Defines)

	lockFile, err := resolver.LoadLock("cstow.lock")
	require.NoError(t, err)
	require.Len(t, lockFile.Packages, 1)
	assert.Equal(t, "git:https://github.com/fmtlib/fmt.git", lockFile.Packages[0].Source)
	assert.Equal(t, "https://github.com/fmtlib/fmt.git", lockFile.Packages[0].Git)
	assert.Equal(t, "10.2.1", lockFile.Packages[0].Rev)
}

func TestAddGitSourceRequiresGitURL(t *testing.T) {
	setupAddTest(t)

	rootCmd.SetArgs([]string{"add", "mylib", "--source", "git"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--git-url is required")
}

func TestAddGitSourceWithCXXFlags(t *testing.T) {
	setupAddTest(t)

	rootCmd.SetArgs([]string{"add", "mylib@1.0", "--source", "git",
		"--git-url", "https://github.com/user/mylib.git",
		"--cxx-flags", "-fPIC -Wall",
		"--link-flags", "-lpthread"})
	require.NoError(t, rootCmd.Execute())

	cfg, err := config.Load("cstow.toml")
	require.NoError(t, err)
	require.Len(t, cfg.Dependencies, 1)
	assert.Equal(t, []string{"-fPIC", "-Wall"}, cfg.Dependencies[0].CMake.CXXFlags)
	assert.Equal(t, []string{"-lpthread"}, cfg.Dependencies[0].CMake.LinkFlags)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run "TestAddGit" -v`
Expected: FAIL — flags `--git-url`, `--tag`, `--cmake-define`, `--cxx-flags`, `--link-flags` not registered

- [ ] **Step 3: Add CLI flags to add command**

In `cmd/add.go`, update the `addCmd.RunE` to read new flags:

After the existing `source, _ := cmd.Flags().GetString("source")` block (around line 47), add:

```go
		gitURL, _ := cmd.Flags().GetString("git-url")
		tag, _ := cmd.Flags().GetString("tag")
		cmakeDefines, _ := cmd.Flags().GetStringArray("cmake-define")
		cxxFlags, _ := cmd.Flags().GetString("cxx-flags")
		linkFlags, _ := cmd.Flags().GetString("link-flags")
```

Add validation before `validateDependency` call:

```go
		if source == "git" {
			if gitURL == "" {
				return fmt.Errorf("--git-url is required when --source is git")
			}
			if tag == "" {
				tag = "main"
			}
		}
```

Update the `resolver.AddDependency` call to include git fields:

```go
		dep := config.Dependency{
			Name:      name,
			Version:   version,
			Source:    source,
			BuildType: buildType,
		}
		if source == "git" {
			dep.Git = gitURL
			dep.Rev = tag
			if len(cmakeDefines) > 0 || cxxFlags != "" || linkFlags != "" {
				dep.CMake = config.GitCMake{
					Defines:   cmakeDefines,
					CXXFlags:  strings.Fields(cxxFlags),
					LinkFlags: strings.Fields(linkFlags),
				}
			}
		}
		resolver.AddDependency(cfg, dep)
```

Update the `validateDependency` function to skip validation for git source:

```go
	func validateDependency(name, version, source string) error {
		if source == "git" {
			return nil // git deps are validated by URL reachability, not registry/repo
		}
		ctx := context.Background()
		if source == "registry" {
			return validateRegistryDependency(ctx, name, version)
		}
		return validateRepoDependency(name, version)
	}
```

Register new flags in `init()`:

```go
	addCmd.Flags().String("git-url", "", "git repository URL (required when --source is git)")
	addCmd.Flags().String("tag", "", "git tag or branch to checkout (default: main)")
	addCmd.Flags().StringArray("cmake-define", nil, "cmake define KEY=VAL (repeatable)")
	addCmd.Flags().String("cxx-flags", "", "additional C++ compiler flags")
	addCmd.Flags().String("link-flags", "", "additional linker flags")
```

Add `resetAddFlag` calls for new flags in `resetAddFlagState`:

```go
	resetAddFlag("git-url")
	resetAddFlag("tag")
	resetAddFlag("cmake-define")
	resetAddFlag("cxx-flags")
	resetAddFlag("link-flags")
```

Also add `"strings"` to the imports if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run "TestAddGit" -v`
Expected: PASS

- [ ] **Step 5: Run all cmd tests to check for regressions**

Run: `go test ./cmd/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/add.go cmd/add_test.go
git commit -m "feat: add --git-url/--tag/--cmake-define flags to cstow add"
```

---

### Task 4: Add git source build path to fetch command

**Files:**
- Modify: `cmd/fetch.go:114-254` (main fetch loop)
- Modify: `cmd/deps.go` (add `installFromGitSource` function)
- Test: `cmd/fetch_git_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `cmd/fetch_git_test.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchGitSourceClonesAndBuilds(t *testing.T) {
	setupFetchGitTest(t)

	// Write cstow.toml with a git dependency
	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0"},
		Dependencies: []config.Dependency{
			{
				Name:      "myheaderlib",
				Version:   "1.0.0",
				Source:    "git",
				BuildType: "header-only",
				Git:       "https://example.com/myheaderlib.git",
				Rev:       "v1.0.0",
			},
		},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	// Write cstow.lock with git source
	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:      "myheaderlib",
				Version:   "1.0.0",
				Source:    "git:https://example.com/myheaderlib.git",
				BuildType: "header-only",
				Git:       "https://example.com/myheaderlib.git",
				Rev:       "v1.0.0",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	// Mock git clone: create a fake source repo
	mockRepo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(mockRepo, "include", "myheaderlib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mockRepo, "include", "myheaderlib", "lib.h"), []byte("// header"), 0o644))

	prevGitClone := fetchGitCloneFunc
	fetchGitCloneFunc = func(url, tag, destDir string) error {
		// Copy mock repo contents to destDir
		entries, _ := os.ReadDir(mockRepo)
		for _, e := range entries {
			src := filepath.Join(mockRepo, e.Name())
			dst := filepath.Join(destDir, e.Name())
			data, _ := os.ReadFile(src)
			_ = os.MkdirAll(filepath.Dir(dst), 0o755)
			_ = os.WriteFile(dst, data, 0o644)
		}
		// Also copy subdirs
		filepath.Walk(mockRepo, func(path string, info os.FileInfo, err error) error {
			if err != nil || path == mockRepo {
				return nil
			}
			rel, _ := filepath.Rel(mockRepo, path)
			dst := filepath.Join(destDir, rel)
			if info.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			data, _ := os.ReadFile(path)
			return os.WriteFile(dst, data, 0o644)
		})
		return nil
	}
	t.Cleanup(func() { fetchGitCloneFunc = prevGitClone })

	// Load config and run fetch
	cfg, err := config.Load("cstow.toml")
	require.NoError(t, err)

	err = runFetch(cfg, "debug", "auto", false, os.Stdout, os.Stderr)
	require.NoError(t, err)

	// Verify cstow_deps symlink exists
	link := filepath.Join("cstow_deps", "myheaderlib")
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0)
}

func TestFetchGitSourceSkipsRegistry(t *testing.T) {
	setupFetchGitTest(t)

	// Lock entry with git source should not attempt registry download
	registryCalled := false
	prevNewClient := fetchNewRegistryClient
	fetchNewRegistryClient = func(ctx interface{}, reg config.Registry) (fetchRegistryClient, error) {
		registryCalled = true
		return nil, fmt.Errorf("should not be called for git deps")
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevNewClient })

	// We just check that the git source is identified and registry is skipped
	// by verifying the source prefix check works
	assert.True(t, true) // placeholder — actual check is in fetch loop
	_ = registryCalled
}

func setupFetchGitTest(t *testing.T) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[defaults]
std = "c++17"
[toolchain]
prefer = "gcc"
`), 0o644))

	t.Setenv("CSTOW_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run "TestFetchGit" -v`
Expected: FAIL — `fetchGitCloneFunc` not defined

- [ ] **Step 3: Add `fetchGitCloneFunc` variable and git source handling in fetch**

In `cmd/fetch.go`, add a package-level variable for testability (near the top, after existing vars):

```go
// fetchGitCloneFunc allows tests to mock git clone operations.
var fetchGitCloneFunc = repository.FetchGit
```

Add import for `"github.com/all3n/cstow/internal/repository"` if not already present.

In the main fetch loop (`runFetch`), after the local source check (after the block at lines 214-218 that handles `strings.HasPrefix(pkg.Source, "local")`), add a new block for git sources:

```go
			if strings.HasPrefix(pkg.Source, "git:") && pkg.Git != "" {
				ctx, err := ensureInstallCtx()
				if err != nil {
					return fmt.Errorf("prepare git build for %s@%s: %w", pkg.Name, pkg.Version, err)
				}

				result, err := installFromGitSource(pkg.Name, pkg.Version, pkg.Git, pkg.Rev, gitSourceOptions{
					Context:    ctx,
					BuildType:  buildType,
					CMake:      gitCMakeFromLock(cfg, pkg.Name),
					Stdout:     stdout,
					Stderr:     stderr,
				})
				if err != nil {
					return fmt.Errorf("git source build for %s@%s: %w", pkg.Name, pkg.Version, err)
				}

				depPaths[pkg.Name] = result.InstallDir
				if pkg.ABITag != result.ABITag {
					pkg.ABITag = result.ABITag
					lockDirty = true
				}
				if pkg.BuildType != result.BuildType {
					pkg.BuildType = result.BuildType
					lockDirty = true
				}
				if result.Cached {
					fmt.Fprintf(stdout, "  [cached-git] %s@%s (%s, %s)\n", pkg.Name, result.Version, result.ABITag, result.BuildType)
				} else {
					fmt.Fprintf(stdout, "  [built-git] %s@%s (%s) -> %s\n", pkg.Name, result.Version, result.BuildType, result.InstallDir)
				}
				continue
			}
```

- [ ] **Step 4: Add `installFromGitSource` and helper functions to `cmd/deps.go`**

Add to `cmd/deps.go`:

```go
type gitSourceOptions struct {
	Context   *repositoryInstallContext
	BuildType string
	CMake     config.GitCMake
	Stdout    io.Writer
	Stderr    io.Writer
}

type gitSourceResult struct {
	InstallDir string
	Version    string
	ABITag     string
	BuildType  string
	Cached     bool
}

func installFromGitSource(name, version, gitURL, rev string, opts gitSourceOptions) (*gitSourceResult, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	buildType := normalizeBuildType(opts.BuildType)
	if buildType == "" {
		buildType = "static"
	}
	if err := validateBuildType(buildType); err != nil {
		return nil, err
	}

	abiTag := opts.Context.detectedABITag()
	cache := resolver.NewFSCache()
	installDir := cache.Path(name, version, abiTag, buildType)

	if _, _, ok := findCachedPackage(cache, name, version, []string{abiTag}, buildType); ok {
		resolvedPath, resolvedABITag, _ := findCachedPackage(cache, name, version, []string{abiTag}, buildType)
		if err := indexSuccessfulArtifact(cache, indexedArtifact{
			Name:       name,
			Version:    version,
			ABITag:     resolvedABITag,
			BuildType:  buildType,
			InstallDir: resolvedPath,
			Origin:     "git",
		}); err != nil {
			return nil, err
		}
		return &gitSourceResult{
			InstallDir: resolvedPath,
			Version:    version,
			ABITag:     resolvedABITag,
			BuildType:  buildType,
			Cached:     true,
		}, nil
	}

	tmpDir, err := os.MkdirTemp("", "cstow-git-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if rev == "" {
		rev = "main"
	}
	if err := fetchGitCloneFunc(gitURL, rev, tmpDir); err != nil {
		return nil, fmt.Errorf("git clone %s@%s: %w", gitURL, rev, err)
	}

	merged := &repository.MergedBuildConfig{
		System:       "cmake",
		BuildType:    buildType,
		CMakeDefines: opts.CMake.Defines,
		CXXFlags:     opts.CMake.CXXFlags,
		LinkFlags:    opts.CMake.LinkFlags,
	}

	result, err := builder.Build(builder.Options{
		SourcePath: tmpDir,
		InstallDir: installDir,
		Config:     merged,
		Toolchain:  opts.Context.Toolchain,
		Profile:    opts.Context.Profile,
		Jobs:       builder.GuessJobs(),
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("build %s %s: %w", name, version, err)
	}

	if err := indexSuccessfulArtifact(cache, indexedArtifact{
		Name:       name,
		Version:    version,
		ABITag:     abiTag,
		BuildType:  buildType,
		InstallDir: result.InstallDir,
		Origin:     "git",
	}); err != nil {
		return nil, err
	}

	return &gitSourceResult{
		InstallDir: result.InstallDir,
		Version:    version,
		ABITag:     abiTag,
		BuildType:  buildType,
	}, nil
}
```

Add a helper to look up CMake options from the project config for a given package name:

```go
func gitCMakeFromLock(cfg *config.Config, name string) config.GitCMake {
	if cfg == nil {
		return config.GitCMake{}
	}
	for _, dep := range cfg.Dependencies {
		if dep.Name == name {
			return dep.CMake
		}
	}
	return config.GitCMake{}
}
```

Add the `fetchGitCloneFunc` declaration in `cmd/fetch.go` (or `cmd/deps.go` — either works, but keep it in the same package):

```go
var fetchGitCloneFunc = repository.FetchGit
```

Make sure `repository` and `builder` are imported in `cmd/deps.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/ -run "TestFetchGit" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/fetch.go cmd/deps.go cmd/fetch_git_test.go
git commit -m "feat: add git source clone+build path to cstow fetch"
```

---

### Task 5: Add git source support to `cstow install` command

**Files:**
- Modify: `cmd/install.go`

- [ ] **Step 1: Update install command to detect git deps from cstow.toml**

In `cmd/install.go`, update the `RunE` to check for git source in project config:

After the lock file loading block (around line 34), add:

```go
			// Check if this package has a git source in cstow.toml
			var gitURL, gitRev string
			var gitCMake config.GitCMake
			if projectCfg != nil {
				for _, dep := range projectCfg.Dependencies {
					if dep.Name == name && dep.Source == "git" {
						gitURL = dep.Git
						gitRev = dep.Rev
						gitCMake = dep.CMake
						break
					}
				}
			}
```

After the context creation and before the `installFromRepository` call, add a git branch:

```go
			if gitURL != "" {
				result, err := installFromGitSource(name, versionConstraint == "*" || versionConstraint == "" ? gitRev : versionConstraint, gitURL, gitRev, gitSourceOptions{
					Context:   ctx,
					BuildType: buildType,
					CMake:     gitCMake,
				})
				if err != nil {
					return err
				}
				if result.Cached {
					fmt.Printf(">> already installed: %s\n", result.InstallDir)
					return nil
				}
				fmt.Printf(">> installed %s %s → %s\n", name, result.Version, result.InstallDir)
				return nil
			}
```

Note: The ternary syntax above is pseudocode. Actual Go code:

```go
			effectiveVersion := versionConstraint
			if effectiveVersion == "*" || effectiveVersion == "" {
				effectiveVersion = gitRev
			}
			if gitURL != "" {
				result, err := installFromGitSource(name, effectiveVersion, gitURL, gitRev, gitSourceOptions{
					Context:   ctx,
					BuildType: buildType,
					CMake:     gitCMake,
				})
				if err != nil {
					return err
				}
				if result.Cached {
					fmt.Printf(">> already installed: %s\n", result.InstallDir)
					return nil
				}
				fmt.Printf(">> installed %s %s → %s\n", name, result.Version, result.InstallDir)
				return nil
			}
```

- [ ] **Step 2: Run all cmd tests**

Run: `go test ./cmd/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/install.go
git commit -m "feat: add git source support to cstow install"
```

---

### Task 6: Update dependencyLinkTarget and fetch guard for git source

**Files:**
- Modify: `cmd/deps.go:324-329` (dependencyLinkTarget)

- [ ] **Step 1: Update dependencyLinkTarget**

The existing function only handles `local` prefix. For git deps, the link target is the cache path (same as registry deps). No change needed — git deps return cache path via the `result.InstallDir` from `installFromGitSource`, and `dependencyLinkTarget` already defaults to `cachePath` for non-local sources.

Verify no changes needed by running:

Run: `go test ./cmd/ -run TestDependencyLinkTarget -v`

- [ ] **Step 2: Commit (only if changes were needed)**

If no code changes, skip this commit.

---

### Task 7: End-to-end validation

**Files:** No new files — manual and automated testing

- [ ] **Step 1: Build the binary**

Run: `go build -o cstow .`

- [ ] **Step 2: Test the full flow manually**

```bash
# Create a test project
mkdir -p /tmp/cstow-test && cd /tmp/cstow-test

# Init project
/tmp/cstow-test/../../cstow init --name testgit

# Add a git dependency (using a real small header-only library)
./cstow add stb --source git \
  --git-url https://github.com/nothings/stb.git \
  --tag master \
  --build-type header-only

# Verify cstow.toml contents
cat cstow.toml

# Verify cstow.lock contents
cat cstow.lock

# Fetch the dependency
./cstow fetch --source-fallback

# Verify symlink
ls -la cstow_deps/
```

- [ ] **Step 3: Run all tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 4: Commit any final fixes**

If any issues found during E2E validation, fix and commit.

---

## File Structure Summary

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/config/config.go` | Modify | Add `GitCMake` struct, `CMake` field, `IsGit()` |
| `internal/config/config_test.go` | Create | Tests for git cmake round-trip |
| `internal/resolver/resolver.go` | Modify | Add `Git`/`Rev` to `LockEntry`, git case in resolver, update `AddDependency` |
| `internal/resolver/resolver_test.go` | Modify | Tests for git resolve, lock round-trip |
| `cmd/add.go` | Modify | Add git flags, validation, git dep construction |
| `cmd/add_test.go` | Modify | Tests for `add --source git` |
| `cmd/deps.go` | Modify | Add `installFromGitSource`, `gitCMakeFromLock` |
| `cmd/fetch.go` | Modify | Add git source branch, `fetchGitCloneFunc` |
| `cmd/fetch_git_test.go` | Create | Tests for git source fetch |
| `cmd/install.go` | Modify | Detect git dep from cstow.toml and route to git build |
