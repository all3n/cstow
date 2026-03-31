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

func TestLockFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "cstow.lock")

	lf := &LockFile{
		Version: 1,
		Packages: []LockEntry{
			{Name: "fmt", Version: "10.2.1", Source: "registry:default", SHA256: "abc123"},
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
}

func TestFSCache(t *testing.T) {
	dir := t.TempDir()
	cache := &FSCache{Root: dir}

	assert.False(t, cache.Has("fmt", "10.2.1", "gcc13-cxx17-x86_64"))

	p := cache.Path("fmt", "10.2.1", "gcc13-cxx17-x86_64")
	require.NoError(t, os.MkdirAll(p, 0o755))
	assert.True(t, cache.Has("fmt", "10.2.1", "gcc13-cxx17-x86_64"))
}

func TestAddDependency(t *testing.T) {
	cfg := &config.Config{}
	AddDependency(cfg, "fmt", "^10.0.0", "registry")
	assert.Equal(t, 1, len(cfg.Dependencies))
	assert.Equal(t, "fmt", cfg.Dependencies[0].Name)

	// Adding again should not duplicate
	AddDependency(cfg, "fmt", "^10.0.0", "registry")
	assert.Equal(t, 1, len(cfg.Dependencies))

	// Different package
	AddDependency(cfg, "spdlog", "^1.12.0", "registry")
	assert.Equal(t, 2, len(cfg.Dependencies))
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
