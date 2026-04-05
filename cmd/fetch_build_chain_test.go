package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFetchBuildChain verifies the fetch→build-dep-check data flow:
//  1. Write cstow.toml + cstow.lock with a dependency
//  2. Fetch from a mocked registry → populates cstow_deps
//  3. checkDependenciesReady succeeds
//  4. Verify artifact was downloaded and extracted correctly
func TestFetchBuildChain(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))

	workdir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Global config with registry
	require.NoError(t, os.WriteFile(filepath.Join(fakeHome, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	// Project config + lock
	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "myproject"
version = "0.1.0"

[[dependencies]]
name = "mylib"
version = "1.0.0"
source = "registry"
build_type = "static"
`), 0o644))

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "mylib"
version = "1.0.0"
source = "registry"
abi_tag = "gcc13-cxx17"
build_type = "static"
`), 0o644))

	// Seed fake registry
	artifactDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(artifactDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(artifactDir, "lib", "libmylib.a"), []byte("artifact-data"), 0o644))
	archiveData, err := pack.CreateTarZst(artifactDir)
	require.NoError(t, err)

	sharedReg := newSharedFakeRegistry()
	require.NoError(t, sharedReg.UploadManifest(context.Background(), "mylib", "1.0.0", &registry.Manifest{
		Name: "mylib", Version: "1.0.0",
		Artifacts: []registry.Artifact{
			{ABITag: "gcc13-cxx17", BuildType: "static"},
		},
	}))
	require.NoError(t, sharedReg.Upload(context.Background(), "mylib", "1.0.0", "gcc13-cxx17", "static", "", archiveData))

	prevFetchFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return sharedReg, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevFetchFactory })

	// Verify: build check fails before fetch
	err = checkDependenciesReady(".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependencies")

	// Fetch
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify: cstow_deps/mylib populated
	info, err := os.Stat(filepath.Join("cstow_deps", "mylib"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify artifact content
	content, err := os.ReadFile(filepath.Join("cstow_deps", "mylib", "lib", "libmylib.a"))
	require.NoError(t, err)
	assert.Equal(t, "artifact-data", string(content))

	// Verify: build check passes after fetch
	err = checkDependenciesReady(".")
	assert.NoError(t, err, "all deps should be ready after fetch")
}
