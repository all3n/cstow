package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/repository"
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

func TestApplyGlobalBuildFlagsToMergedConfigPrependsDefaults(t *testing.T) {
	merged := &repository.MergedBuildConfig{
		System:       "cmake",
		BuildType:    "static",
		CMakeDefines: []string{"PROJECT_DEFINE=1"},
		CXXFlags:     []string{"-Wall"},
		LinkFlags:    []string{"-lpthread"},
	}
	global := &config.Global{
		Build: config.GlobalBuild{
			Flags: config.GlobalBuildFlags{
				Defines:   []string{"GLOBAL_DEFINE=1"},
				CXXFlags:  []string{"-fstack-protector-strong"},
				LinkFlags: []string{"-ldl"},
			},
		},
	}

	got := applyGlobalBuildFlagsToMergedConfig(merged, global)
	assert.Equal(t, []string{"GLOBAL_DEFINE=1", "PROJECT_DEFINE=1"}, got.CMakeDefines)
	assert.Equal(t, []string{"-fstack-protector-strong", "-Wall"}, got.CXXFlags)
	assert.Equal(t, []string{"-ldl", "-lpthread"}, got.LinkFlags)
}
