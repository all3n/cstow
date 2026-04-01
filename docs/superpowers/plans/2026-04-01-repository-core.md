# Repository Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `internal/repository/` — data structures, Finder, and Merge — and wire them into `cmd/add.go` and `cmd/build.go`.

**Architecture:** Strictly layered. `internal/repository/` is a pure data package with no knowledge of CLI or toolchain internals; callers (`cmd/`) pass in toolchain kind, profile, and GOOS as plain strings. `pickBestVersion` is an unexported helper in `finder.go` that mirrors the semver logic already in `resolver.go`.

**Tech Stack:** Go stdlib, `BurntSushi/toml`, `Masterminds/semver/v3`, `stretchr/testify`

---

### Task 1: Data structures

**Files:**
- Create: `internal/repository/package.go`

- [ ] **Step 1: Create `internal/repository/package.go` with all types**

```go
package repository

import "github.com/all3n/cstow/internal/config"

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
	Type        string            `toml:"type"`            // git | archive
	URL         string            `toml:"url"`
	TagTemplate string            `toml:"tag_template"`    // "v{version}"
	URLTemplate string            `toml:"url_template"`
	SHA256      map[string]string `toml:"sha256_versions"` // version -> sha256
}

type BuildDef struct {
	System   string                      `toml:"system"`   // cmake|make|header-only
	Type     string                      `toml:"type"`     // static|shared|header-only
	CMake    CMakeBuildDef               `toml:"cmake"`
	Profiles map[string]ProfileOverride  `toml:"profile"`
	Compiler map[string]CompilerOverride `toml:"compiler"` // gcc|clang|msvc
	Platform map[string]PlatformOverride `toml:"platform"` // linux|macos|windows
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
	CMake    *CMakeBuildDef              `toml:"cmake"`
	Compiler map[string]CompilerOverride `toml:"compiler"`
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/repository/
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/repository/package.go
git commit -m "feat(repository): add data structures"
```

---

### Task 2: Merge logic + tests

**Files:**
- Create: `internal/repository/merge.go`
- Create: `internal/repository/merge_test.go`

- [ ] **Step 1: Write failing tests in `internal/repository/merge_test.go`**

```go
package repository

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMerge_BaseOnly(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake: CMakeBuildDef{
				Defines:  []string{"BUILD_SHARED_LIBS=OFF"},
				CXXFlags: []string{"-Wall"},
			},
		},
		Artifacts: ArtifactsDef{
			IncludeDirs: []string{"include"},
			Libs:        []string{"libfoo.a"},
		},
	}
	got := Merge(pkg, nil, "gcc", "debug", "linux")
	assert.Equal(t, "cmake", got.System)
	assert.Equal(t, []string{"BUILD_SHARED_LIBS=OFF"}, got.CMakeDefines)
	assert.Equal(t, []string{"-Wall"}, got.CXXFlags)
	assert.Equal(t, []string{"include"}, got.IncludeDirs)
	assert.Equal(t, []string{"libfoo.a"}, got.Libs)
}

func TestMerge_ProfileAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Profiles: map[string]ProfileOverride{
				"release": {Defines: []string{"CMAKE_BUILD_TYPE=Release"}, CXXFlags: []string{"-O3"}},
			},
		},
	}
	got := Merge(pkg, nil, "gcc", "release", "linux")
	assert.Equal(t, []string{"BASE=1", "CMAKE_BUILD_TYPE=Release"}, got.CMakeDefines)
	assert.Equal(t, []string{"-O3"}, got.CXXFlags)
}

func TestMerge_CompilerAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Compiler: map[string]CompilerOverride{
				"msvc": {Defines: []string{"_CRT_SECURE_NO_WARNINGS=1"}, CXXFlags: []string{"/EHsc"}},
			},
		},
	}
	got := Merge(pkg, nil, "msvc", "debug", "windows")
	assert.Equal(t, []string{"BASE=1", "_CRT_SECURE_NO_WARNINGS=1"}, got.CMakeDefines)
	assert.Equal(t, []string{"/EHsc"}, got.CXXFlags)
}

func TestMerge_PlatformAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Platform: map[string]PlatformOverride{
				"linux": {Defines: []string{"LINUX=1"}},
			},
		},
	}
	got := Merge(pkg, nil, "gcc", "debug", "linux")
	assert.Equal(t, []string{"BASE=1", "LINUX=1"}, got.CMakeDefines)
}

func TestMerge_VersionOverrideReplaces(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"OLD=1"}},
		},
	}
	ver := &VersionOverride{
		Build: &BuildOverride{
			CMake: &CMakeBuildDef{Defines: []string{"NEW=1", "NEW=2"}},
		},
		Patch: "fix.patch",
	}
	got := Merge(pkg, ver, "gcc", "debug", "linux")
	// version override replaces, not appends
	assert.Equal(t, []string{"NEW=1", "NEW=2"}, got.CMakeDefines)
	assert.Equal(t, "fix.patch", got.Patch)
}

func TestMerge_VersionOverrideCompilerAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Compiler: map[string]CompilerOverride{
				"clang": {CXXFlags: []string{"-Wno-old"}},
			},
		},
	}
	ver := &VersionOverride{
		Build: &BuildOverride{
			Compiler: map[string]CompilerOverride{
				"clang": {CXXFlags: []string{"-Wno-new"}},
			},
		},
	}
	got := Merge(pkg, ver, "clang", "debug", "linux")
	// version compiler override appends to package compiler override
	assert.Equal(t, []string{"-Wno-old", "-Wno-new"}, got.CXXFlags)
}

func TestMerge_AllLayers(t *testing.T) {
	goos := "linux"
	if runtime.GOOS == "windows" {
		goos = "windows"
	}
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}, CXXFlags: []string{"-Wall"}},
			Profiles: map[string]ProfileOverride{
				"release": {Defines: []string{"NDEBUG=1"}},
			},
			Compiler: map[string]CompilerOverride{
				"gcc": {CXXFlags: []string{"-fstack-protector"}},
			},
			Platform: map[string]PlatformOverride{
				goos: {Defines: []string{"OS_DEFINE=1"}},
			},
		},
		Artifacts: ArtifactsDef{IncludeDirs: []string{"include"}, Libs: []string{"libfoo.a"}},
	}
	ver := &VersionOverride{
		Build: &BuildOverride{
			CMake: &CMakeBuildDef{Defines: []string{"OVERRIDE=1"}},
		},
	}
	got := Merge(pkg, ver, "gcc", "release", goos)
	// version override replaces cmake.defines entirely
	assert.Equal(t, []string{"OVERRIDE=1"}, got.CMakeDefines)
	// cxx_flags: base + release(none) + gcc + ver compiler(none)
	assert.Equal(t, []string{"-Wall", "-fstack-protector"}, got.CXXFlags)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/repository/ -run TestMerge -v 2>&1 | head -20
```

Expected: compilation error — `Merge` undefined.

- [ ] **Step 3: Implement `internal/repository/merge.go`**

```go
package repository

import "slices"

// MergedBuildConfig is the fully resolved build configuration for one dependency.
type MergedBuildConfig struct {
	System      string
	CMakeDefines []string
	CXXFlags    []string
	LinkFlags   []string
	IncludeDirs []string
	Libs        []string
	Patch       string
}

// Merge applies configuration layers in priority order (lowest → highest):
//  1. package-level cmake defines + cxx_flags
//  2. profile override (appends)
//  3. compiler override (appends)
//  4. platform override (appends)
//  5. version-specific cmake override (replaces defines if non-empty; compiler appends)
func Merge(pkg *PackageDef, ver *VersionOverride, toolchainKind, profile, goos string) *MergedBuildConfig {
	out := &MergedBuildConfig{
		System:      pkg.Build.System,
		IncludeDirs: slices.Clone(pkg.Artifacts.IncludeDirs),
		Libs:        slices.Clone(pkg.Artifacts.Libs),
	}

	// Layer 1: package base
	out.CMakeDefines = slices.Clone(pkg.Build.CMake.Defines)
	out.CXXFlags = slices.Clone(pkg.Build.CMake.CXXFlags)

	// Layer 2: profile
	if po, ok := pkg.Build.Profiles[profile]; ok {
		out.CMakeDefines = append(out.CMakeDefines, po.Defines...)
		out.CXXFlags = append(out.CXXFlags, po.CXXFlags...)
	}

	// Layer 3: compiler
	if co, ok := pkg.Build.Compiler[toolchainKind]; ok {
		out.CMakeDefines = append(out.CMakeDefines, co.Defines...)
		out.CXXFlags = append(out.CXXFlags, co.CXXFlags...)
	}

	// Layer 4: platform
	if po, ok := pkg.Build.Platform[goos]; ok {
		out.CMakeDefines = append(out.CMakeDefines, po.Defines...)
		out.CXXFlags = append(out.CXXFlags, po.CXXFlags...)
	}

	// Layer 5: version-specific override
	if ver != nil && ver.Build != nil {
		if ver.Build.CMake != nil && len(ver.Build.CMake.Defines) > 0 {
			out.CMakeDefines = slices.Clone(ver.Build.CMake.Defines) // full replacement
		}
		if ver.Build.CMake != nil {
			out.CXXFlags = append(out.CXXFlags, ver.Build.CMake.CXXFlags...)
		}
		if co, ok := ver.Build.Compiler[toolchainKind]; ok {
			out.CMakeDefines = append(out.CMakeDefines, co.Defines...)
			out.CXXFlags = append(out.CXXFlags, co.CXXFlags...)
		}
		out.Patch = ver.Patch
	}

	return out
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/repository/ -run TestMerge -v
```

Expected: all TestMerge_* PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/merge.go internal/repository/merge_test.go
git commit -m "feat(repository): add Merge with layer priority + tests"
```

---

### Task 3: Finder + tests

**Files:**
- Create: `internal/repository/finder.go`
- Create: `internal/repository/finder_test.go`

- [ ] **Step 1: Write failing tests in `internal/repository/finder_test.go`**

```go
package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writePackageToml creates a minimal package.toml in the fake repository.
func writePackageToml(t *testing.T, root, name string, versions []string, extra string) {
	t.Helper()
	letter := string([]rune(name)[0:1])
	dir := filepath.Join(root, letter, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	content := "[package]\nname = \"" + name + "\"\n\nversions = ["
	for i, v := range versions {
		if i > 0 {
			content += ", "
		}
		content += "\"" + v + "\""
	}
	content += "]\n" + extra
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.toml"), []byte(content), 0o644))
}

// writeVersionToml creates a versions/<ver>.toml override file.
func writeVersionToml(t *testing.T, root, name, version, content string) {
	t.Helper()
	letter := string([]rune(name)[0:1])
	dir := filepath.Join(root, letter, name, "versions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, version+".toml"), []byte(content), 0o644))
}

func TestFinder_Found(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "fmt", []string{"10.2.1", "10.1.0"}, "")

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("fmt", "^10.0.0")
	require.NoError(t, err)
	assert.Equal(t, "10.2.1", pkg.Version)
	assert.Equal(t, root, pkg.RepoPath)
	assert.Nil(t, pkg.Override)
}

func TestFinder_ExactVersion(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "fmt", []string{"10.2.1"}, "")

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("fmt", "10.2.1")
	require.NoError(t, err)
	assert.Equal(t, "10.2.1", pkg.Version)
}

func TestFinder_VersionOverrideLoaded(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "googletest", []string{"1.14.0", "1.13.0"}, "")
	writeVersionToml(t, root, "googletest", "1.14.0", `patch = "1.14.0-fix.patch"`)

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("googletest", "^1.14")
	require.NoError(t, err)
	assert.Equal(t, "1.14.0", pkg.Version)
	require.NotNil(t, pkg.Override)
	assert.Equal(t, "1.14.0-fix.patch", pkg.Override.Patch)
}

func TestFinder_NoVersionOverride(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "spdlog", []string{"1.13.0"}, "")
	// no versions/ dir

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("spdlog", "*")
	require.NoError(t, err)
	assert.Nil(t, pkg.Override)
}

func TestFinder_NotFound(t *testing.T) {
	root := t.TempDir()

	f := NewFinderWithPaths([]string{root})
	_, err := f.Find("nonexistent", "^1.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "not found in any repository")
}

func TestFinder_NoMatchingVersion(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "fmt", []string{"9.1.0"}, "")

	f := NewFinderWithPaths([]string{root})
	_, err := f.Find("fmt", "^10.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fmt")
}

func TestFinder_NonLetterPackage(t *testing.T) {
	root := t.TempDir()
	// package name starting with digit → goes under "_"
	letter := "_"
	dir := filepath.Join(root, letter, "7zip")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "[package]\nname = \"7zip\"\n\nversions = [\"22.0.0\"]\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.toml"), []byte(content), 0o644))

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("7zip", "22.0.0")
	require.NoError(t, err)
	assert.Equal(t, "22.0.0", pkg.Version)
}

func TestFinder_MultipleRoots_FirstWins(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	writePackageToml(t, root1, "fmt", []string{"11.0.0"}, "")
	writePackageToml(t, root2, "fmt", []string{"10.2.1"}, "")

	f := NewFinderWithPaths([]string{root1, root2})
	pkg, err := f.Find("fmt", ">=10.0.0")
	require.NoError(t, err)
	assert.Equal(t, "11.0.0", pkg.Version)
	assert.Equal(t, root1, pkg.RepoPath)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/repository/ -run TestFinder -v 2>&1 | head -20
```

Expected: compilation error — `NewFinderWithPaths` undefined.

- [ ] **Step 3: Implement `internal/repository/finder.go`**

```go
package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
)

// Finder searches ordered repository directories for package definitions.
type Finder struct {
	searchPaths []string
}

// NewFinder returns a Finder using ~/.cstow/repository/ as the single search path.
func NewFinder() *Finder {
	home, _ := os.UserHomeDir()
	return &Finder{searchPaths: []string{filepath.Join(home, ".cstow", "repository")}}
}

// NewFinderWithPaths returns a Finder with explicit search paths (used in tests).
func NewFinderWithPaths(paths []string) *Finder {
	return &Finder{searchPaths: paths}
}

// ResolvedPkg is the result of a successful Find call.
type ResolvedPkg struct {
	Def      *PackageDef
	Version  string           // resolved concrete version satisfying the constraint
	Override *VersionOverride // nil if no version-specific override file exists
	RepoPath string           // which repository root matched
}

// Find searches all repository paths for name matching versionConstraint.
// Returns a descriptive error when not found — callers must fail hard.
func (f *Finder) Find(name, versionConstraint string) (*ResolvedPkg, error) {
	letter := indexLetter(name)

	for _, root := range f.searchPaths {
		pkgFile := filepath.Join(root, letter, name, "package.toml")
		if _, err := os.Stat(pkgFile); err != nil {
			continue
		}

		def, err := loadPackageDef(pkgFile)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", pkgFile, err)
		}

		matched, err := pickBestVersion(def.Versions, versionConstraint)
		if err != nil {
			continue // no matching version in this root, try next
		}

		override := loadVersionOverride(filepath.Join(root, letter, name, "versions"), matched)

		return &ResolvedPkg{
			Def:      def,
			Version:  matched,
			Override: override,
			RepoPath: root,
		}, nil
	}

	return nil, fmt.Errorf("package %q not found in any repository (constraint: %s)", name, versionConstraint)
}

// indexLetter returns the first-letter directory name for a package.
func indexLetter(name string) string {
	if len(name) == 0 {
		return "_"
	}
	r := []rune(name)[0]
	if !unicode.IsLetter(r) {
		return "_"
	}
	return string(unicode.ToLower(r))
}

// loadPackageDef reads and parses a package.toml file.
func loadPackageDef(path string) (*PackageDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def PackageDef
	if err := toml.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// loadVersionOverride reads versions/<version>.toml if it exists; returns nil otherwise.
func loadVersionOverride(versionsDir, version string) *VersionOverride {
	path := filepath.Join(versionsDir, version+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var vo VersionOverride
	if err := toml.Unmarshal(data, &vo); err != nil {
		return nil
	}
	return &vo
}

// pickBestVersion selects the highest version from candidates satisfying constraint.
func pickBestVersion(candidates []string, constraint string) (string, error) {
	// "*" or "" means any version — pick the latest
	if constraint == "*" || constraint == "" {
		if len(candidates) == 0 {
			return "", fmt.Errorf("no versions available")
		}
		var versions []*semver.Version
		for _, c := range candidates {
			if sv, err := semver.NewVersion(c); err == nil {
				versions = append(versions, sv)
			}
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("no valid semver versions")
		}
		sort.Sort(sort.Reverse(semver.Collection(versions)))
		return versions[0].Original(), nil
	}

	c, err := semver.NewConstraint(constraint)
	if err != nil {
		// treat as exact version
		for _, v := range candidates {
			if v == constraint {
				return v, nil
			}
		}
		return "", fmt.Errorf("version %q not in list", constraint)
	}

	var matched []*semver.Version
	for _, v := range candidates {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if c.Check(sv) {
			matched = append(matched, sv)
		}
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("no version matching %q", constraint)
	}
	sort.Sort(sort.Reverse(semver.Collection(matched)))
	return matched[0].Original(), nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/repository/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/finder.go internal/repository/finder_test.go
git commit -m "feat(repository): add Finder with semver lookup + tests"
```

---

### Task 4: Wire into `cmd/add.go`

**Files:**
- Modify: `cmd/add.go`

- [ ] **Step 1: Add Finder call after parsing package spec**

Replace the block starting at `resolver.AddDependency(cfg, name, version, source)` in `cmd/add.go`:

```go
import (
    // existing imports ...
    "github.com/all3n/cstow/internal/repository"
)

// Inside RunE, after `source, _ := cmd.Flags().GetString("source")`:

// Verify package exists in repository before modifying any files
if source == "registry" || source == "" {
    finder := repository.NewFinder()
    if _, err := finder.Find(name, version); err != nil {
        return fmt.Errorf("cstow add: %w\nHint: create ~/.cstow/repository/%s/%s/package.toml first", err, string(name[0]), name)
    }
}

resolver.AddDependency(cfg, name, version, source)
```

The full updated `cmd/add.go` `RunE` body:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    cfgPath := "cstow.toml"
    if _, err := os.Stat(cfgPath); err != nil {
        return fmt.Errorf("cstow.toml not found in current directory")
    }

    cfg, err := config.Load(cfgPath)
    if err != nil {
        return fmt.Errorf("load config: %w", err)
    }

    name, version := parsePackageSpec(args[0])
    source, _ := cmd.Flags().GetString("source")
    if source == "" {
        source = "registry"
    }

    // Verify package exists in repository before modifying any files
    if source == "registry" || source == "" {
        finder := repository.NewFinder()
        if _, err := finder.Find(name, version); err != nil {
            return fmt.Errorf("cstow add: %w\nHint: create ~/.cstow/repository/%s/%s/package.toml first",
                err, string([]rune(name)[0]), name)
        }
    }

    resolver.AddDependency(cfg, name, version, source)

    // Resolve first — only persist if resolution succeeds
    cache := resolver.NewFSCache()
    r := resolver.New(cache, nil)
    lf, err := r.Resolve(cfg.Dependencies)
    if err != nil {
        return fmt.Errorf("resolve dependencies: %w", err)
    }

    if err := cfg.Save(cfgPath); err != nil {
        return fmt.Errorf("save config: %w", err)
    }

    if err := resolver.SaveLock("cstow.lock", lf); err != nil {
        return fmt.Errorf("save lock file: %w", err)
    }

    fmt.Printf("Added %s@%s (source: %s)\n", name, version, source)
    return nil
},
```

- [ ] **Step 2: Build to verify no compilation errors**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/add.go
git commit -m "feat(cmd/add): verify package exists in repository before adding"
```

---

### Task 5: Wire into `cmd/build.go`

**Files:**
- Modify: `cmd/build.go`

- [ ] **Step 1: Add repository lookup + cmake flag injection**

After the `abiTag` detection block (line ~46) and before the `pre-build hook` call in `cmd/build.go`, insert dependency cmake flag collection:

```go
import (
    // existing imports ...
    "runtime"
    "github.com/all3n/cstow/internal/repository"
    "github.com/all3n/cstow/internal/resolver"
)
```

Insert after `fmt.Printf(">> abi: %s\n", abiTag.String())`:

```go
// Collect cmake flags from repository build configs for each dependency
var depCMakeDefines []string
var depCXXFlags []string
if len(cfg.Dependencies) > 0 {
    finder := repository.NewFinder()
    lockPath := "cstow.lock"
    lf, lockErr := resolver.LoadLock(lockPath)
    if lockErr == nil {
        for _, entry := range lf.Packages {
            resolved, findErr := finder.Find(entry.Name, entry.Version)
            if findErr != nil {
                fmt.Printf(">> warning: %s — skipping cmake flags\n", findErr)
                continue
            }
            merged := repository.Merge(resolved.Def, resolved.Override, tc.Kind, profile, runtime.GOOS)
            depCMakeDefines = append(depCMakeDefines, merged.CMakeDefines...)
            depCXXFlags = append(depCXXFlags, merged.CXXFlags...)
        }
    }
}
```

Then in the `cmakeArgs` assembly, after `cmakeArgs = append(cmakeArgs, tc.CMakeFlags()...)`, add:

```go
// Inject per-dependency cmake defines from repository build configs
for _, d := range depCMakeDefines {
    cmakeArgs = append(cmakeArgs, "-D"+d)
}
if len(depCXXFlags) > 0 {
    cmakeArgs = append(cmakeArgs, fmt.Sprintf("-DCMAKE_CXX_FLAGS=%s", strings.Join(depCXXFlags, " ")))
}
```

- [ ] **Step 2: Build to verify no compilation errors**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run all tests**

```bash
go test ./... -count=1
```

Expected: all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/build.go
git commit -m "feat(cmd/build): inject per-dependency cmake flags from repository"
```
