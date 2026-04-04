package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fetchFakeDownloader struct {
	gotPkg       string
	gotVersion   string
	gotABITag    string
	gotBuildType string
	gotHashID    string
}

func (f *fetchFakeDownloader) Download(_ context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
	f.gotPkg = pkg
	f.gotVersion = version
	f.gotABITag = abiTag
	f.gotBuildType = buildType
	f.gotHashID = hashID
	return []byte("ok"), nil
}

func TestDownloadFromManifestArtifactUsesHashID(t *testing.T) {
	fake := &fetchFakeDownloader{}
	artifact := registry.Artifact{
		ABITag:    "abi-1",
		BuildType: "shared",
		HashID:    "abcdef0123456789",
	}

	data, err := downloadFromManifestArtifact(context.Background(), fake, "googletest", "1.14.0", artifact)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(data))
	assert.Equal(t, "googletest", fake.gotPkg)
	assert.Equal(t, "1.14.0", fake.gotVersion)
	assert.Equal(t, "abi-1", fake.gotABITag)
	assert.Equal(t, "shared", fake.gotBuildType)
	assert.Equal(t, "abcdef0123456789", fake.gotHashID)
}

type fetchFakeRegistryClient struct {
	listVersionsFunc func(context.Context, string) ([]string, error)
	getManifestFunc  func(context.Context, string, string) (*registry.Manifest, error)
	downloadFunc     func(context.Context, string, string, string, string, string) ([]byte, error)
	getManifestCalls []string
	downloadCalls    int
}

func (f *fetchFakeRegistryClient) ListVersions(ctx context.Context, pkg string) ([]string, error) {
	if f.listVersionsFunc == nil {
		return nil, nil
	}
	return f.listVersionsFunc(ctx, pkg)
}

func (f *fetchFakeRegistryClient) GetManifest(ctx context.Context, pkg, version string) (*registry.Manifest, error) {
	f.getManifestCalls = append(f.getManifestCalls, pkg+"@"+version)
	if f.getManifestFunc == nil {
		return nil, os.ErrNotExist
	}
	return f.getManifestFunc(ctx, pkg, version)
}

func (f *fetchFakeRegistryClient) Download(ctx context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
	f.downloadCalls++
	if f.downloadFunc == nil {
		return nil, os.ErrNotExist
	}
	return f.downloadFunc(ctx, pkg, version, abiTag, buildType, hashID)
}

func TestFetchByHashIDUsesLocalArtifactHit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	localPrefix := filepath.Join(workdir, "prefix", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(localPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(localPrefix, "lib", "libgtest.a"), []byte("local"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-local",
		BuildType:  "static",
		HashID:     "abc1234567890def111111111111111111111111111111111111111111111111",
		InstallDir: localPrefix,
		Origin:     "registry",
		CreatedAt:  time.Date(2026, 4, 4, 8, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 4, 8, 0, 0, 0, time.UTC),
		LastSeenAt: time.Date(2026, 4, 4, 8, 0, 0, 0, time.UTC),
	}))

	_ = executeRootForTest(t, "fetch", "--artifact", "abc12345")

	depLink := filepath.Join(workdir, "cstow_deps", "googletest")
	gotTarget, err := os.Readlink(depLink)
	require.NoError(t, err)
	assert.Equal(t, localPrefix, gotTarget)

	rec, err := store.FindByHashID("abc12345")
	require.NoError(t, err)
	assert.True(t, rec.LastSeenAt.After(time.Date(2026, 4, 4, 8, 0, 0, 0, time.UTC)))
}

func TestFetchByHashIDDownloadsFromManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile(filepath.Join(workdir, "cstow.toml"), []byte(`
[[dependencies]]
name = "googletest"
version = "1.14.0"
source = "registry"
`), 0o644))
	require.NoError(t, resolver.SaveLock(filepath.Join(workdir, "cstow.lock"), &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{Name: "googletest", Version: "1.14.0", Source: "registry:default"},
		},
	}))

	sourceDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "lib", "libgtest.so"), []byte("remote"), 0o644))
	archive, err := pack.CreateTarZst(sourceDir)
	require.NoError(t, err)

	hashSum := sha256.Sum256(archive)
	fullHash := hex.EncodeToString(hashSum[:])
	fake := &fetchFakeRegistryClient{
		getManifestFunc: func(_ context.Context, pkg, version string) (*registry.Manifest, error) {
			if pkg != "googletest" || version != "1.14.0" {
				return nil, os.ErrNotExist
			}
			return &registry.Manifest{
				Name:    pkg,
				Version: version,
				Artifacts: []registry.Artifact{
					{
						ABITag:    "abi-remote",
						BuildType: "shared",
						HashID:    fullHash,
					},
				},
			}, nil
		},
		downloadFunc: func(_ context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
			if pkg != "googletest" || version != "1.14.0" || abiTag != "abi-remote" || buildType != "shared" || hashID != fullHash {
				return nil, os.ErrNotExist
			}
			return append([]byte(nil), archive...), nil
		},
	}

	prevFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevFactory })

	output := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--artifact", fullHash[:8]})
		require.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, output, "downloaded")

	cache := resolver.NewFSCache()
	expectedInstallDir := cache.Path("googletest", "1.14.0", "abi-remote", "shared")
	_, statErr := os.Stat(filepath.Join(expectedInstallDir, "lib", "libgtest.so"))
	require.NoError(t, statErr)

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	rec, err := store.FindByHashID(fullHash[:8])
	require.NoError(t, err)
	assert.Equal(t, fullHash, rec.HashID)
	assert.Equal(t, expectedInstallDir, rec.InstallDir)

	depLink := filepath.Join(workdir, "cstow_deps", "googletest")
	gotTarget, err := os.Readlink(depLink)
	require.NoError(t, err)
	assert.Equal(t, expectedInstallDir, gotTarget)
}

func TestFetchByHashIDReusesStaleTupleWhenInstallDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	sourceDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "lib", "libgtest.so"), []byte("remote"), 0o644))
	archive, err := pack.CreateTarZst(sourceDir)
	require.NoError(t, err)

	hashSum := sha256.Sum256(archive)
	archiveHash := hex.EncodeToString(hashSum[:])

	missingDir := filepath.Join(workdir, "missing-prefix")
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-old",
		BuildType:  "shared",
		HashID:     archiveHash,
		InstallDir: missingDir,
		Origin:     "registry",
	}))

	fake := &fetchFakeRegistryClient{
		getManifestFunc: func(_ context.Context, pkg, version string) (*registry.Manifest, error) {
			if pkg == "googletest" && version == "1.14.0" {
				return &registry.Manifest{
					Name:    pkg,
					Version: version,
					Artifacts: []registry.Artifact{
						{ABITag: "abi-old", BuildType: "shared", HashID: archiveHash},
					},
				}, nil
			}
			return nil, os.ErrNotExist
		},
		downloadFunc: func(_ context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
			if pkg != "googletest" || version != "1.14.0" || abiTag != "abi-old" || buildType != "shared" || hashID != archiveHash {
				return nil, os.ErrNotExist
			}
			return archive, nil
		},
	}

	prevFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevFactory })

	rootCmd.SetArgs([]string{"fetch", "--artifact", archiveHash[:8]})
	require.NoError(t, rootCmd.Execute())

	assert.Contains(t, fake.getManifestCalls, "googletest@1.14.0")
}

func TestFetchManifestCandidatesUnionLockAndConfig(t *testing.T) {
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	cfg := &config.Config{
		Dependencies: []config.Dependency{
			{Name: "fmt", Version: "10.2.1"},
			{Name: "spdlog", Version: "1.12.0"},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{Name: "fmt", Version: "10.2.1"},
			{Name: "zlib", Version: "1.3.1"},
		},
	}))

	got, err := fetchManifestCandidates(cfg)
	require.NoError(t, err)
	assert.ElementsMatch(t, []fetchManifestCandidate{
		{Name: "fmt", Version: "10.2.1"},
		{Name: "zlib", Version: "1.3.1"},
		{Name: "spdlog", Version: "1.12.0"},
	}, got)
}

func TestFetchByHashIDReturnsCrossManifestAmbiguity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"

[[dependencies]]
name = "spdlog"
version = "1.12.0"
source = "registry"
`), 0o644))

	fake := &fetchFakeRegistryClient{
		getManifestFunc: func(_ context.Context, pkg, version string) (*registry.Manifest, error) {
			switch pkg + "@" + version {
			case "fmt@10.2.1":
				return &registry.Manifest{
					Name:    pkg,
					Version: version,
					Artifacts: []registry.Artifact{
						{ABITag: "abi-a", BuildType: "shared", HashID: "abc1234567890aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
					},
				}, nil
			case "spdlog@1.12.0":
				return &registry.Manifest{
					Name:    pkg,
					Version: version,
					Artifacts: []registry.Artifact{
						{ABITag: "abi-b", BuildType: "static", HashID: "abc9999999999bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
					},
				}, nil
			default:
				return nil, os.ErrNotExist
			}
		},
	}

	prevFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevFactory })

	rootCmd.SetArgs([]string{"fetch", "--artifact", "abc"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `hash_id prefix "abc" is ambiguous`)
}

func TestFetchByHashIDSurfacesManifestLoadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"
`), 0o644))

	fake := &fetchFakeRegistryClient{
		getManifestFunc: func(_ context.Context, pkg, version string) (*registry.Manifest, error) {
			return nil, assert.AnError
		},
	}

	prevFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevFactory })

	rootCmd.SetArgs([]string{"fetch", "--artifact", "ffff1111"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest")
}

func TestFetchByHashIDRejectsMatchWhenManifestScanIncomplete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"

[[dependencies]]
name = "spdlog"
version = "1.12.0"
source = "registry"
`), 0o644))

	fake := &fetchFakeRegistryClient{
		getManifestFunc: func(_ context.Context, pkg, version string) (*registry.Manifest, error) {
			switch pkg + "@" + version {
			case "fmt@10.2.1":
				return &registry.Manifest{
					Name:    pkg,
					Version: version,
					Artifacts: []registry.Artifact{
						{ABITag: "abi-a", BuildType: "shared", HashID: "fff1234567890aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
					},
				}, nil
			case "spdlog@1.12.0":
				return nil, assert.AnError
			default:
				return nil, os.ErrNotExist
			}
		},
	}

	prevFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevFactory })

	rootCmd.SetArgs([]string{"fetch", "--artifact", "fff1234"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolution incomplete")
	assert.Contains(t, err.Error(), "manifest")
	assert.Equal(t, 0, fake.downloadCalls)
}
