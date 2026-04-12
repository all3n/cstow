package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadGlobal_MissingFile(t *testing.T) {
	// Point to a nonexistent directory so config.toml is absent.
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	tmp := t.TempDir()
	os.Setenv("HOME", tmp)

	g, err := LoadGlobal()
	require.NoError(t, err)
	assert.Equal(t, "c++17", g.Defaults.Std)
	assert.Equal(t, "debug", g.Defaults.Profile)
	assert.Equal(t, "~/.cstow/cache", g.Cache.Dir)
}

func TestLoadGlobal_ParsesAllFields(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	tmp := t.TempDir()
	os.Setenv("HOME", tmp)
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".cstow"), 0o755))

	content := `
[defaults]
std = "c++20"
profile = "release"
jobs = 8
color = false

[cache]
dir = "/tmp/cstow-cache"
max_size_gb = 5
retention_days = 30

[[repositories]]
name = "team"
path = "/opt/cstow-pkgs"
priority = 10

[[repositories]]
name = "work"
path = "~/projects/pkgs"
priority = 20

[[registry]]
name = "default"
url = "s3://my-bucket/cstow"
provider = "cloudflare"
region = "auto"
endpoint_url = "https://example.r2.cloudflarestorage.com"
access_key = "cfg-key"
secret_key = "cfg-secret"

[toolchain]
prefer = "clang"
min_gcc = "12"
min_clang = "16"

[build.flags]
cxx_flags = ["-fstack-protector-strong"]
defines = ["FOO=1"]

[network]
proxy = "http://proxy:8080"
timeout_sec = 30
retries = 5
`
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, ".cstow", "config.toml"),
		[]byte(content), 0o644,
	))

	g, err := LoadGlobal()
	require.NoError(t, err)

	assert.Equal(t, "c++20", g.Defaults.Std)
	assert.Equal(t, "release", g.Defaults.Profile)
	assert.Equal(t, 8, g.Defaults.Jobs)
	assert.False(t, g.Defaults.Color)

	assert.Equal(t, "/tmp/cstow-cache", g.Cache.Dir)
	assert.Equal(t, 5, g.Cache.MaxSizeGB)
	assert.Equal(t, 30, g.Cache.RetentionDays)

	require.Len(t, g.Repositories, 2)
	assert.Equal(t, "team", g.Repositories[0].Name)
	assert.Equal(t, "/opt/cstow-pkgs", g.Repositories[0].Path)
	assert.Equal(t, 10, g.Repositories[0].Priority)

	assert.Equal(t, "clang", g.Toolchain.Prefer)
	assert.Equal(t, "12", g.Toolchain.MinGCC)

	require.Len(t, g.Registries, 1)
	assert.Equal(t, "default", g.Registries[0].Name)
	assert.Equal(t, "https://example.r2.cloudflarestorage.com", g.Registries[0].EndpointURL)
	assert.Equal(t, "cfg-key", g.Registries[0].AccessKey)
	assert.Equal(t, "cfg-secret", g.Registries[0].SecretKey)

	require.Len(t, g.Build.Flags.CXXFlags, 1)
	assert.Equal(t, "-fstack-protector-strong", g.Build.Flags.CXXFlags[0])

	assert.Equal(t, "http://proxy:8080", g.Network.Proxy)
	assert.Equal(t, 30, g.Network.Timeout)
	assert.Equal(t, 5, g.Network.Retries)
}

func TestResolveCacheDirPrefersEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", filepath.Join(home, "env-cache"))

	dir, err := ResolveCacheDir(&Global{
		Cache: GlobalCache{Dir: "~/configured-cache"},
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "env-cache"), dir)
}

func TestResolveCacheDirExpandsConfiguredHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	dir, err := ResolveCacheDir(&Global{
		Cache: GlobalCache{Dir: "~/configured-cache"},
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "configured-cache"), dir)
}

func TestResolveCacheDirFallsBackToDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	dir, err := ResolveCacheDir(nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".cstow", "cache"), dir)
}

func TestResolveArtifactDBPathFollowsResolvedCacheParent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	path, err := ResolveArtifactDBPath(&Global{
		Cache: GlobalCache{Dir: "~/configured-cache"},
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "cstow.db"), path)
}

func TestResolveArtifactDBPathFallsBackToDefaultStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	path, err := ResolveArtifactDBPath(nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".cstow", "cstow.db"), path)
}

func TestGlobal_RepositoryPaths_PriorityOrder(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	tmp := t.TempDir()
	os.Setenv("HOME", tmp)

	g := &Global{
		Repositories: []RepoSource{
			{Name: "low", Path: "/low", Priority: 90},
			{Name: "high", Path: "/high", Priority: 5},
			{Name: "mid", Path: "~/mid", Priority: 50},
		},
	}

	paths := g.RepositoryPaths()
	require.Len(t, paths, 4) // 3 configured + 1 built-in fallback
	assert.Equal(t, "/high", paths[0])
	assert.Equal(t, filepath.Join(tmp, "mid"), paths[1]) // ~ expanded
	assert.Equal(t, "/low", paths[2])
	assert.Contains(t, paths[3], ".cstow/repository") // built-in fallback
}

func TestGlobal_RepositoryPaths_DefaultPriority(t *testing.T) {
	g := &Global{
		Repositories: []RepoSource{
			{Name: "a", Path: "/a"}, // priority 0 → treated as 50
			{Name: "b", Path: "/b", Priority: 10},
		},
	}

	paths := g.RepositoryPaths()
	assert.Equal(t, "/b", paths[0]) // priority 10 wins over default 50
	assert.Equal(t, "/a", paths[1])
}

func TestGlobal_RepositoryPaths_Empty(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	tmp := t.TempDir()
	os.Setenv("HOME", tmp)

	g := &Global{}
	paths := g.RepositoryPaths()
	require.Len(t, paths, 1)
	assert.Contains(t, paths[0], ".cstow/repository")
}

func TestResolvePrimaryRegistry_UsesGlobalRegistryWhenProjectMissing(t *testing.T) {
	global := &Global{
		Registries: []Registry{
			{
				Name:        "global",
				URL:         "s3://bucket/prefix",
				EndpointURL: "https://example.com",
				Profile:     "cstow",
			},
		},
	}

	reg, err := ResolvePrimaryRegistry(nil, global)
	require.NoError(t, err)
	assert.Equal(t, "global", reg.Name)
	assert.Equal(t, "https://example.com", reg.EndpointURL)
	assert.Equal(t, "cstow", reg.Profile)
}

func TestResolvePrimaryRegistry_MergesMatchingGlobalRegistry(t *testing.T) {
	project := []Registry{
		{
			Name: "default",
			URL:  "s3://bucket/project",
		},
	}
	global := &Global{
		Registries: []Registry{
			{
				Name:        "default",
				URL:         "s3://bucket/global",
				EndpointURL: "https://example.com",
				Profile:     "cstow",
				AccessKey:   "cfg-key",
				SecretKey:   "cfg-secret",
			},
		},
	}

	reg, err := ResolvePrimaryRegistry(project, global)
	require.NoError(t, err)
	assert.Equal(t, "s3://bucket/project", reg.URL)
	assert.Equal(t, "https://example.com", reg.EndpointURL)
	assert.Equal(t, "cstow", reg.Profile)
	assert.Equal(t, "cfg-key", reg.AccessKey)
	assert.Equal(t, "cfg-secret", reg.SecretKey)
}

func TestResolvePrimaryRegistry_PrefersProjectFieldsOverGlobal(t *testing.T) {
	project := []Registry{
		{
			Name:        "default",
			URL:         "s3://bucket/project",
			EndpointURL: "https://project.example.com",
			Profile:     "project",
		},
	}
	global := &Global{
		Registries: []Registry{
			{
				Name:        "default",
				URL:         "s3://bucket/global",
				EndpointURL: "https://global.example.com",
				Profile:     "global",
			},
		},
	}

	reg, err := ResolvePrimaryRegistry(project, global)
	require.NoError(t, err)
	assert.Equal(t, "https://project.example.com", reg.EndpointURL)
	assert.Equal(t, "project", reg.Profile)
}

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

func TestGlobal_RepoPathByName(t *testing.T) {
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	tmp := t.TempDir()
	os.Setenv("HOME", tmp)

	g := &Global{
		Repositories: []RepoSource{
			{Name: "local", Path: "/tmp/repo"},
			{Name: "work", Path: "~/projects/pkgs"},
		},
	}

	// Case 1: Existing repo by name
	path, ok := g.RepoPathByName("local")
	assert.True(t, ok)
	assert.Equal(t, "/tmp/repo", path)

	// Case 2: Repo with ~ expansion
	path, ok = g.RepoPathByName("work")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(tmp, "projects/pkgs"), path)

	// Case 3: Built-in "home" repo
	path, ok = g.RepoPathByName("home")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(tmp, ".cstow", "repository"), path)

	// Case 4: Built-in "default" repo
	path, ok = g.RepoPathByName("default")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(tmp, ".cstow", "repository"), path)

	// Case 5: Missing repo
	_, ok = g.RepoPathByName("missing")
	assert.False(t, ok)
}

func TestDefaultRepoPath(t *testing.T) {
	cwd, _ := os.Getwd()
	expected := filepath.Join(cwd, ".cstow", "repository")
	assert.Equal(t, expected, DefaultRepoPath())
}
