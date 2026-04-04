package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFetchRegistryClient struct {
	manifest    *registry.Manifest
	manifestErr error
	archive     []byte
	downloadErr error
}

func (f *fakeFetchRegistryClient) GetManifest(ctx context.Context, pkg, version string) (*registry.Manifest, error) {
	if f.manifestErr != nil {
		return nil, f.manifestErr
	}
	return f.manifest, nil
}

func (f *fakeFetchRegistryClient) Download(ctx context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	return f.archive, nil
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
}

func writeFetchProject(t *testing.T, dir, cfg string, lock *resolver.LockFile) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cstow.toml"), []byte(cfg), 0o644))
	require.NoError(t, resolver.SaveLock(filepath.Join(dir, "cstow.lock"), lock))
}

func readIndexedRows(t *testing.T) []artifactdb.Record {
	t.Helper()
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	rows, err := store.List()
	require.NoError(t, err)
	return rows
}

func TestFetchIndexesCachedArtifact(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	workdir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	writeFetchProject(t, workdir, `
[package]
name = "demo"
version = "0.1.0"

[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"
build_type = "shared"
`, &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{{
			Name:      "fmt",
			Version:   "10.2.1",
			Source:    "registry:default",
			ABITag:    "abi-cache",
			BuildType: "shared",
		}},
	})
	withWorkingDir(t, workdir)

	cache := resolver.NewFSCache()
	installDir := cache.Path("fmt", "10.2.1", "abi-cache", "shared")
	require.NoError(t, os.MkdirAll(installDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installDir, "marker.txt"), []byte("cached"), 0o644))

	output := executeRootForTest(t, "fetch")
	assert.Contains(t, output, "[cached] fmt@10.2.1")

	rows := readIndexedRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "fmt", rows[0].Name)
	assert.Equal(t, "unknown", rows[0].Origin)
	assert.Equal(t, installDir, rows[0].InstallDir)
}

func TestFetchIndexesRegistryDownload(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	workdir := t.TempDir()
	payloadDir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(payloadDir, "include", "fmt"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(payloadDir, "include", "fmt", "format.h"), []byte("fmt"), 0o644))

	archive, err := pack.CreateTarZst(payloadDir)
	require.NoError(t, err)

	oldFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(ctx context.Context, reg config.Registry) (fetchRegistryClient, error) {
		return &fakeFetchRegistryClient{
			manifest: &registry.Manifest{
				Name:    "fmt",
				Version: "10.2.1",
				Artifacts: []registry.Artifact{{
					ABITag:    "abi-registry",
					BuildType: "shared",
				}},
			},
			archive: archive,
		}, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = oldFactory })

	writeFetchProject(t, workdir, `
[package]
name = "demo"
version = "0.1.0"

[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"
build_type = "shared"

[[registry]]
name = "default"
url = "s3://example/cstow"
provider = "custom"
region = "auto"
`, &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{{
			Name:      "fmt",
			Version:   "10.2.1",
			Source:    "registry:default",
			ABITag:    "abi-registry",
			BuildType: "shared",
		}},
	})
	withWorkingDir(t, workdir)

	output := executeRootForTest(t, "fetch")
	assert.Contains(t, output, "[done]  fmt@10.2.1")

	rows := readIndexedRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "registry", rows[0].Origin)
	assert.Equal(t, "abi-registry", rows[0].ABITag)
	assert.FileExists(t, filepath.Join(rows[0].InstallDir, "include", "fmt", "format.h"))
}

func TestFetchIndexesRepositoryFallbackArtifact(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("repository fallback integration is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	repoRoot := filepath.Join(home, "repository")
	workdir := t.TempDir()
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-fetch", sourceRepo, packageOptions{buildType: "static"})
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".cstow", "config.toml"),
		[]byte(fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)),
		0o644,
	))

	writeFetchProject(t, workdir, `
[package]
name = "demo"
version = "0.1.0"

[[dependencies]]
name = "mini-fetch"
version = "1.0.0"
source = "registry"
build_type = "static"
`, &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{{
			Name:      "mini-fetch",
			Version:   "1.0.0",
			Source:    "registry:default",
			BuildType: "static",
		}},
	})
	withWorkingDir(t, workdir)

	output := executeRootForTest(t, "fetch")
	assert.Contains(t, output, "[built] mini-fetch@1.0.0")

	rows := readIndexedRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "repository", rows[0].Origin)
	assert.DirExists(t, rows[0].InstallDir)
}
