package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/all3n/cstow/internal/config"
)

func mustConstraint(s string) *semver.Constraints {
	c, err := semver.NewConstraint(s)
	if err != nil {
		panic(err)
	}
	return c
}

func TestPickBest(t *testing.T) {
	versions := []string{"1.0.0", "1.1.0", "2.0.0", "1.2.0"}

	tests := []struct {
		name       string
		constraint string
		want       string
		wantErr    bool
	}{
		{"caret major", "^1.0.0", "1.2.0", false},
		{"exact", "2.0.0", "2.0.0", false},
		{"tilde minor", "~1.1.0", "1.1.0", false},
		{"any", "*", "2.0.0", false},
		{"no match", "^3.0.0", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickBest(versions, mustConstraint(tt.constraint))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestResolveBasic(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{Name: "fmt", Version: "^10.0.0", Source: "local"},
		{Name: "spdlog", Version: "^1.12.0", Source: "local"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, len(lf.Packages))
	assert.Equal(t, "fmt", lf.Packages[0].Name)
	assert.Equal(t, "spdlog", lf.Packages[1].Name)
}

func TestResolveNoDuplicate(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{Name: "fmt", Version: "^10.0.0", Source: "local"},
		{Name: "fmt", Version: "^10.0.0", Source: "local"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, len(lf.Packages))
}

func TestResolveConflict(t *testing.T) {
	r := New(nil, nil)
	_, err := r.Resolve([]config.Dependency{
		{Name: "fmt", Version: "^10.0.0", Source: "local"},
		{Name: "fmt", Version: "^9.0.0", Source: "local"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting")
}

func TestResolveWithRegistry(t *testing.T) {
	mock := &mockRegistry{
		versions: map[string][]string{
			"fmt": {"10.0.0", "10.1.0", "10.2.1", "11.0.0"},
		},
	}
	r := New(nil, mock)
	lf, err := r.Resolve([]config.Dependency{
		{Name: "fmt", Version: "^10.0.0", Source: "registry"},
	})
	require.NoError(t, err)
	assert.Equal(t, "10.2.1", lf.Packages[0].Version)
}

func TestResolvePropagatesBuildType(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{Name: "fmt", Version: "^10.0.0", Source: "local", BuildType: "shared"},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "shared", lf.Packages[0].BuildType)
}

func TestLockFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "cstow.lock")

	lf := &LockFile{
		Version: 1,
		Packages: []LockEntry{
			{Name: "fmt", Version: "10.2.1", Source: "registry:default", SHA256: "abc123", BuildType: "shared"},
			{Name: "spdlog", Version: "1.13.0", Source: "registry:default", Deps: []string{"fmt"}},
		},
	}

	require.NoError(t, SaveLock(lockPath, lf))
	loaded, err := LoadLock(lockPath)
	require.NoError(t, err)
	assert.Equal(t, lf.Version, loaded.Version)
	assert.Equal(t, len(lf.Packages), len(loaded.Packages))
	assert.Equal(t, "fmt", loaded.Packages[0].Name)
	assert.Equal(t, "10.2.1", loaded.Packages[0].Version)
	assert.Equal(t, "shared", loaded.Packages[0].BuildType)
}

func TestLoadLockLegacyWithoutBuildType(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "cstow.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte(`# cstow.lock — auto-generated, do not edit

version = 1

[[package]]
name = "fmt"
version = "10.2.1"
source = "registry:default"
sha256 = "abc123"
`), 0o644))

	loaded, err := LoadLock(lockPath)
	require.NoError(t, err)
	require.Len(t, loaded.Packages, 1)
	assert.Equal(t, "", loaded.Packages[0].BuildType)
}

func TestFSCache(t *testing.T) {
	dir := t.TempDir()
	cache := &FSCache{Root: dir}

	assert.False(t, cache.Has("fmt", "10.2.1", "gcc13-cxx17-x86_64", "shared"))

	p := cache.Path("fmt", "10.2.1", "gcc13-cxx17-x86_64", "shared")
	require.NoError(t, os.MkdirAll(p, 0o755))
	assert.True(t, cache.Has("fmt", "10.2.1", "gcc13-cxx17-x86_64", "shared"))
	assert.Equal(t, filepath.Join(dir, "fmt", "10.2.1", "gcc13-cxx17-x86_64", "shared"), p)
}

func TestFSCacheLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	cache := &FSCache{Root: dir}

	legacy := cache.LegacyPath("fmt", "10.2.1", "gcc13-cxx17-x86_64")
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	assert.True(t, cache.Has("fmt", "10.2.1", "gcc13-cxx17-x86_64", "shared"))
}

func TestNewFSCacheUsesGlobalCacheDirWhenEnvUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[cache]
dir = "~/configured-cache"
`), 0o644))

	cache := NewFSCache()
	assert.Equal(t, filepath.Join(home, "configured-cache"), cache.Root)
}

func TestNewFSCachePrefersEnvOverGlobalCacheDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", filepath.Join(home, "env-cache"))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[cache]
dir = "~/configured-cache"
`), 0o644))

	cache := NewFSCache()
	assert.Equal(t, filepath.Join(home, "env-cache"), cache.Root)
}

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

func TestResolveGitSourceEmptyVersionUsesRev(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:    "mylib",
			Version: "",
			Source:  "git",
			Git:     "https://github.com/user/mylib.git",
			Rev:     "v2.1.0",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "v2.1.0", lf.Packages[0].Version)
	assert.Equal(t, "git:https://github.com/user/mylib.git", lf.Packages[0].Source)
}

func TestResolveGitSourceWildcardVersionUsesRev(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:    "mylib",
			Version: "*",
			Source:  "git",
			Git:     "https://github.com/user/mylib.git",
			Rev:     "main",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "main", lf.Packages[0].Version)
}

func TestResolveGitSourceNoVersionNoRev(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:   "mylib",
			Source: "git",
			Git:    "https://github.com/user/mylib.git",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "0.0.0", lf.Packages[0].Version)
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

// mock registry

type mockRegistry struct {
	versions map[string][]string
}

func (m *mockRegistry) ListVersions(pkg string) ([]string, error) {
	if v, ok := m.versions[pkg]; ok {
		return v, nil
	}
	return nil, nil
}
