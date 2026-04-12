package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
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
	loadedCfg, err := config.Load("cstow.toml")
	require.NoError(t, err)

	err = runFetch(loadedCfg, fetchOptions{
		Profile:        "debug",
		Toolchain:      "auto",
		SourceFallback: false,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	})
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
	fetchNewRegistryClient = func(ctx context.Context, reg config.Registry) (fetchRegistryClient, error) {
		registryCalled = true
		return nil, fmt.Errorf("should not be called for git deps")
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevNewClient })

	// Write cstow.toml with a git dependency
	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0"},
		Dependencies: []config.Dependency{
			{
				Name:      "mylib",
				Version:   "1.0.0",
				Source:    "git",
				Git:       "https://example.com/mylib.git",
				Rev:       "v1.0.0",
			},
		},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:    "mylib",
				Version: "1.0.0",
				Source:  "git:https://example.com/mylib.git",
				Git:     "https://example.com/mylib.git",
				Rev:     "v1.0.0",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	// Mock git clone to succeed
	prevGitClone := fetchGitCloneFunc
	fetchGitCloneFunc = func(url, tag, destDir string) error {
		return nil
	}
	t.Cleanup(func() { fetchGitCloneFunc = prevGitClone })

	loadedCfg, _ := config.Load("cstow.toml")
	_ = runFetch(loadedCfg, fetchOptions{
		Profile:        "debug",
		Toolchain:      "auto",
		SourceFallback: false,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	})

	assert.False(t, registryCalled)
}

func TestRunFetchGitSourceFailureIncludesStageContext(t *testing.T) {
	setupFetchGitTest(t)

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

	prevGitClone := fetchGitCloneFunc
	fetchGitCloneFunc = func(url, tag, destDir string) error {
		return errors.New("clone boom")
	}
	t.Cleanup(func() { fetchGitCloneFunc = prevGitClone })

	loadedCfg, err := config.Load("cstow.toml")
	require.NoError(t, err)

	err = runFetch(loadedCfg, fetchOptions{
		Profile:        "debug",
		Toolchain:      "auto",
		SourceFallback: false,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git source build for myheaderlib@1.0.0")
	assert.Contains(t, err.Error(), "git dependency myheaderlib@1.0.0[header-only]: clone source")
	assert.Contains(t, err.Error(), "clone boom")
}

func TestRunFetchSourceFallbackFailureIncludesStageContext(t *testing.T) {
	setupFetchGitTest(t)
	requireTool(t, "git")

	repoRoot := filepath.Join(t.TempDir(), "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	writeRepositoryPackage(t, repoRoot, "cycle-a", sourceRepo, packageOptions{
		buildType: "header-only",
		dependencies: []config.Dependency{
			{Name: "cycle-b", Version: "^1", BuildType: "header-only"},
		},
	})
	writeRepositoryPackage(t, repoRoot, "cycle-b", sourceRepo, packageOptions{
		buildType: "header-only",
		dependencies: []config.Dependency{
			{Name: "cycle-a", Version: "^1", BuildType: "header-only"},
		},
	})

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(fmt.Sprintf(`
[defaults]
std = "c++17"

[toolchain]
prefer = "gcc"

[[repositories]]
name = "local"
path = %q
priority = 10
`, repoRoot)), 0o644))

	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0"},
		Dependencies: []config.Dependency{
			{
				Name:      "cycle-a",
				Version:   "1.0.0",
				Source:    "registry",
				BuildType: "header-only",
			},
		},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:      "cycle-a",
				Version:   "1.0.0",
				Source:    "registry",
				BuildType: "header-only",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	loadedCfg, err := config.Load("cstow.toml")
	require.NoError(t, err)

	err = runFetch(loadedCfg, fetchOptions{
		Profile:        "debug",
		Toolchain:      "auto",
		SourceFallback: true,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no registry is configured for cycle-a@1.0.0")
	assert.Contains(t, err.Error(), "source fallback for cycle-a@1.0.0")
	assert.Contains(t, err.Error(), "repository package cycle-a@1.0.0[header-only]: dependency cycle-b@^1")
	assert.Contains(t, err.Error(), "repository dependency cycle detected")
}

func TestRunFetchSourceFallbackFailureIncludesPrebuiltSelectionCause(t *testing.T) {
	setupFetchGitTest(t)
	requireTool(t, "git")

	repoRoot := filepath.Join(t.TempDir(), "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	writeRepositoryPackage(t, repoRoot, "cycle-a", sourceRepo, packageOptions{
		buildType: "header-only",
		dependencies: []config.Dependency{
			{Name: "cycle-b", Version: "^1", BuildType: "header-only"},
		},
	})
	writeRepositoryPackage(t, repoRoot, "cycle-b", sourceRepo, packageOptions{
		buildType: "header-only",
		dependencies: []config.Dependency{
			{Name: "cycle-a", Version: "^1", BuildType: "header-only"},
		},
	})

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(fmt.Sprintf(`
[defaults]
std = "c++17"

[toolchain]
prefer = "gcc"
`,)), 0o644))

	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0"},
		Dependencies: []config.Dependency{
			{
				Name:      "cycle-a",
				Version:   "1.0.0",
				Source:    "registry",
				BuildType: "header-only",
			},
		},
		Registries: []config.Registry{{
			Name: "default",
			URL:  "s3://example/cstow",
		}},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:      "cycle-a",
				Version:   "1.0.0",
				Source:    "registry:default",
				ABITag:    "abi-registry",
				BuildType: "header-only",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	prevNewClient := fetchNewRegistryClient
	fetchNewRegistryClient = func(ctx context.Context, reg config.Registry) (fetchRegistryClient, error) {
		return &fakeFetchRegistryClient{
			manifest: &registry.Manifest{
				Name:    "cycle-a",
				Version: "1.0.0",
				Artifacts: []registry.Artifact{{
					ABITag:    "abi-registry",
					BuildType: "static",
				}},
			},
		}, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevNewClient })

	loadedCfg, err := config.Load("cstow.toml")
	require.NoError(t, err)

	err = runFetch(loadedCfg, fetchOptions{
		Profile:        "debug",
		Toolchain:      "auto",
		SourceFallback: true,
		ExtraRepos:     []string{repoRoot},
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "select manifest artifact")
	assert.Contains(t, err.Error(), "source fallback for cycle-a@1.0.0 after prebuilt artifact unavailable")
	assert.Contains(t, err.Error(), "repository dependency cycle detected")
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
