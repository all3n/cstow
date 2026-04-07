package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAddRegistryValidator implements addRegistryValidator for tests.
type fakeAddRegistryValidator struct {
	getManifestFunc  func(ctx context.Context, pkg, version string) (*registry.Manifest, error)
	listVersionsFunc func(ctx context.Context, pkg string) ([]string, error)
}

func (f *fakeAddRegistryValidator) GetManifest(ctx context.Context, pkg, version string) (*registry.Manifest, error) {
	if f.getManifestFunc != nil {
		return f.getManifestFunc(ctx, pkg, version)
	}
	return nil, errors.New("not found")
}

func (f *fakeAddRegistryValidator) ListVersions(ctx context.Context, pkg string) ([]string, error) {
	if f.listVersionsFunc != nil {
		return f.listVersionsFunc(ctx, pkg)
	}
	return nil, errors.New("not found")
}

func setupAddTest(t *testing.T) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "demo"
version = "0.1.0"
`), 0o644))

	// Write global config with registry
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))
}

func TestAddCommandPersistsBuildTypeToConfigAndLock(t *testing.T) {
	setupAddTest(t)

	// Mock registry validator to accept any dependency
	prevValidator := addNewRegistryValidator
	addNewRegistryValidator = func(_ context.Context, _ config.Registry) (addRegistryValidator, error) {
		return &fakeAddRegistryValidator{
			getManifestFunc: func(_ context.Context, _, _ string) (*registry.Manifest, error) {
				return &registry.Manifest{Name: "googletest", Version: "1.14.0"}, nil
			},
		}, nil
	}
	t.Cleanup(func() { addNewRegistryValidator = prevValidator })

	rootCmd.SetArgs([]string{"add", "googletest@1.14.0", "--source", "registry", "--build-type", "shared"})
	require.NoError(t, rootCmd.Execute())

	cfg, err := config.Load("cstow.toml")
	require.NoError(t, err)
	require.Len(t, cfg.Dependencies, 1)
	assert.Equal(t, "googletest", cfg.Dependencies[0].Name)
	assert.Equal(t, "shared", cfg.Dependencies[0].BuildType)

	lockFile, err := resolver.LoadLock("cstow.lock")
	require.NoError(t, err)
	require.Len(t, lockFile.Packages, 1)
	assert.Equal(t, "shared", lockFile.Packages[0].BuildType)
}

func TestAddValidatesRegistrySpecificVersion(t *testing.T) {
	setupAddTest(t)

	prevValidator := addNewRegistryValidator
	addNewRegistryValidator = func(_ context.Context, _ config.Registry) (addRegistryValidator, error) {
		return &fakeAddRegistryValidator{
			getManifestFunc: func(_ context.Context, pkg, version string) (*registry.Manifest, error) {
				if pkg == "mylib" && version == "1.0.0" {
					return &registry.Manifest{Name: pkg, Version: version}, nil
				}
				return nil, errors.New("not found")
			},
		}, nil
	}
	t.Cleanup(func() { addNewRegistryValidator = prevValidator })

	rootCmd.SetArgs([]string{"add", "mylib@1.0.0", "--source", "registry"})
	require.NoError(t, rootCmd.Execute())
}

func TestAddRejectsUnknownRegistryPackage(t *testing.T) {
	setupAddTest(t)

	prevValidator := addNewRegistryValidator
	addNewRegistryValidator = func(_ context.Context, _ config.Registry) (addRegistryValidator, error) {
		return &fakeAddRegistryValidator{
			getManifestFunc: func(_ context.Context, _, _ string) (*registry.Manifest, error) {
				return nil, errors.New("not found")
			},
		}, nil
	}
	t.Cleanup(func() { addNewRegistryValidator = prevValidator })

	rootCmd.SetArgs([]string{"add", "nonexistent@1.0.0", "--source", "registry"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in registry")
}

func TestAddValidatesRegistryWildcard(t *testing.T) {
	setupAddTest(t)

	prevValidator := addNewRegistryValidator
	addNewRegistryValidator = func(_ context.Context, _ config.Registry) (addRegistryValidator, error) {
		return &fakeAddRegistryValidator{
			listVersionsFunc: func(_ context.Context, pkg string) ([]string, error) {
				if pkg == "mylib" {
					return []string{"1.0.0", "2.0.0"}, nil
				}
				return nil, errors.New("not found")
			},
		}, nil
	}
	t.Cleanup(func() { addNewRegistryValidator = prevValidator })

	rootCmd.SetArgs([]string{"add", "mylib", "--source", "registry"})
	require.NoError(t, rootCmd.Execute())
}

func TestAddRejectsRegistryWildcardNoVersions(t *testing.T) {
	setupAddTest(t)

	prevValidator := addNewRegistryValidator
	addNewRegistryValidator = func(_ context.Context, _ config.Registry) (addRegistryValidator, error) {
		return &fakeAddRegistryValidator{
			listVersionsFunc: func(_ context.Context, _ string) ([]string, error) {
				return []string{}, nil
			},
		}, nil
	}
	t.Cleanup(func() { addNewRegistryValidator = prevValidator })

	rootCmd.SetArgs([]string{"add", "mylib", "--source", "registry"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no versions")
}

func TestAddValidatesRepoDependency(t *testing.T) {
	setupAddTest(t)

	prevFinder := addNewRepoFinder
	addNewRepoFinder = func(extraPaths []string) (*repository.Finder, error) {
		return repository.NewFinderWithPaths([]string{"/nonexistent"}), nil
	}
	t.Cleanup(func() { addNewRepoFinder = prevFinder })

	rootCmd.SetArgs([]string{"add", "nonexistent@1.0.0", "--source", "local"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in repository")
}

func TestAddRejectsInvalidBuildType(t *testing.T) {
	setupAddTest(t)

	prevValidator := addNewRegistryValidator
	addNewRegistryValidator = func(_ context.Context, _ config.Registry) (addRegistryValidator, error) {
		return &fakeAddRegistryValidator{}, nil
	}
	t.Cleanup(func() { addNewRegistryValidator = prevValidator })

	rootCmd.SetArgs([]string{"add", "mylib@1.0.0", "--build-type", "invalid"})
	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported build type")
}

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
