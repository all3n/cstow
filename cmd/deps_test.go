package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidateABITags(t *testing.T) {
	assert.Equal(t,
		[]string{"gcc13-cxx17-linux-x86_64", "default"},
		candidateABITags("gcc13-cxx17-linux-x86_64", ""),
	)

	assert.Equal(t,
		[]string{"clang17-cxx20-linux-x86_64", "default"},
		candidateABITags("", "clang17-cxx20-linux-x86_64"),
	)

	assert.Equal(t,
		[]string{"default"},
		candidateABITags("", ""),
	)

	assert.Equal(t,
		[]string{"gcc13-cxx17-linux-x86_64", "clang17-cxx20-linux-x86_64", "default"},
		candidateABITags("gcc13-cxx17-linux-x86_64", "clang17-cxx20-linux-x86_64"),
	)
}

func TestFindCachedPackage(t *testing.T) {
	cacheRoot := t.TempDir()
	cache := &resolver.FSCache{Root: cacheRoot}

	path := cache.Path("fmt", "10.2.1", "clang17-cxx20-linux-x86_64", "shared")
	require.NoError(t, os.MkdirAll(path, 0o755))

	resolvedPath, abiTag, ok := findCachedPackage(cache, "fmt", "10.2.1", []string{
		"gcc13-cxx17-linux-x86_64",
		"clang17-cxx20-linux-x86_64",
	}, "shared")
	require.True(t, ok)
	assert.Equal(t, path, resolvedPath)
	assert.Equal(t, "clang17-cxx20-linux-x86_64", abiTag)
}

func TestFindCachedPackageIgnoresLegacyPathWhenBuildTypeSpecified(t *testing.T) {
	cacheRoot := t.TempDir()
	cache := &resolver.FSCache{Root: cacheRoot}

	// Legacy path exists (no build type), but we're looking for shared.
	legacy := cache.LegacyPath("fmt", "10.2.1", "clang17-cxx20-linux-x86_64")
	require.NoError(t, os.MkdirAll(legacy, 0o755))

	_, _, ok := findCachedPackage(cache, "fmt", "10.2.1", []string{
		"clang17-cxx20-linux-x86_64",
	}, "shared")
	assert.False(t, ok, "should not match legacy path when buildType is specified")
}

func TestFindCachedPackageMatchesLegacyPathWhenNoBuildType(t *testing.T) {
	cacheRoot := t.TempDir()
	cache := &resolver.FSCache{Root: cacheRoot}

	legacy := cache.LegacyPath("fmt", "10.2.1", "clang17-cxx20-linux-x86_64")
	require.NoError(t, os.MkdirAll(legacy, 0o755))

	resolvedPath, abiTag, ok := findCachedPackage(cache, "fmt", "10.2.1", []string{
		"clang17-cxx20-linux-x86_64",
	}, "")
	require.True(t, ok)
	assert.Equal(t, legacy, resolvedPath)
	assert.Equal(t, "clang17-cxx20-linux-x86_64", abiTag)
}

func TestDesiredBuildTypePrefersLockThenConfigThenDefault(t *testing.T) {
	cfg := &config.Config{
		Dependencies: []config.Dependency{
			{Name: "fmt", BuildType: "shared"},
		},
	}

	assert.Equal(t, "static", desiredBuildType("fmt", resolver.LockEntry{
		Name:      "fmt",
		BuildType: "static",
	}, cfg))
	assert.Equal(t, "shared", desiredBuildType("fmt", resolver.LockEntry{
		Name: "fmt",
	}, cfg))
	assert.Equal(t, "static", desiredBuildType("spdlog", resolver.LockEntry{
		Name: "spdlog",
	}, cfg))
}

func TestFetchBuildTypeUsesConfiguredValueOnly(t *testing.T) {
	cfg := &config.Config{
		Dependencies: []config.Dependency{
			{Name: "fmt", BuildType: "shared"},
		},
	}

	assert.Equal(t, "static", fetchBuildType("fmt", resolver.LockEntry{
		Name:      "fmt",
		BuildType: "static",
	}, cfg))
	assert.Equal(t, "shared", fetchBuildType("fmt", resolver.LockEntry{
		Name: "fmt",
	}, cfg))
	assert.Equal(t, "", fetchBuildType("spdlog", resolver.LockEntry{
		Name: "spdlog",
	}, cfg))
}

func TestDependencyLinkTarget(t *testing.T) {
	cachePath := filepath.Join("/cache", "fmt", "10.2.1", "default")

	assert.Equal(t, cachePath, dependencyLinkTarget(resolver.LockEntry{
		Name:   "fmt",
		Source: "registry:default",
	}, cachePath))

	assert.Equal(t, "../myutil", dependencyLinkTarget(resolver.LockEntry{
		Name:   "myutil",
		Source: "local:../myutil",
		Path:   "../myutil",
	}, cachePath))
}
