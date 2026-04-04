package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publishFakeRegistryClient struct {
	uploadCalls int
	uploadedPkg string
	uploadedVer string
	uploadedABI string
	uploadedTyp string
	uploadedID  string
	uploadedRaw []byte

	manifestUploadCalls int
	manifestByKey       map[string]*registry.Manifest
	getManifestErr      error
}

func (f *publishFakeRegistryClient) Upload(_ context.Context, pkg, version, abiTag, buildType, hashID string, data []byte) error {
	f.uploadCalls++
	f.uploadedPkg = pkg
	f.uploadedVer = version
	f.uploadedABI = abiTag
	f.uploadedTyp = buildType
	f.uploadedID = hashID
	f.uploadedRaw = append([]byte(nil), data...)
	return nil
}

func (f *publishFakeRegistryClient) UploadManifest(_ context.Context, pkg, version string, manifest *registry.Manifest) error {
	f.manifestUploadCalls++
	if f.manifestByKey == nil {
		f.manifestByKey = map[string]*registry.Manifest{}
	}
	f.manifestByKey[pkg+"@"+version] = manifest
	return nil
}

func (f *publishFakeRegistryClient) GetManifest(_ context.Context, pkg, version string) (*registry.Manifest, error) {
	if f.getManifestErr != nil {
		return nil, f.getManifestErr
	}
	if got, ok := f.manifestByKey[pkg+"@"+version]; ok {
		return got, nil
	}
	return nil, os.ErrNotExist
}

func TestPublishLocalArtifactFromCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("binary"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	fake := &publishFakeRegistryClient{
		manifestByKey: map[string]*registry.Manifest{
			"googletest@1.14.0": {
				Name:    "googletest",
				Version: "1.14.0",
				Artifacts: []registry.Artifact{
					{
						ABITag:    "abi-1",
						BuildType: "static",
						HashID:    "existing-hash",
						BuildTags: []string{"compiler=gcc10"},
						SHA256:    "existing-sha",
						Size:      7,
					},
				},
			},
		},
	}

	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	stdout := captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0",
			"--abi-tag", "abi-1",
			"--build-type", "shared",
			"--build-tag", "compiler=gcc11",
		})
		require.NoError(t, rootCmd.Execute())
	})

	assert.Contains(t, stdout, "packaging googletest@1.14.0")
	assert.Equal(t, 1, fake.uploadCalls)
	assert.NotEmpty(t, fake.uploadedID)

	manifest := fake.manifestByKey["googletest@1.14.0"]
	require.NotNil(t, manifest)
	var sharedEntry *registry.Artifact
	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].ABITag == "abi-1" && manifest.Artifacts[i].BuildType == "shared" {
			sharedEntry = &manifest.Artifacts[i]
			break
		}
	}
	require.NotNil(t, sharedEntry)
	assert.Equal(t, []string{"compiler=gcc11"}, sharedEntry.BuildTags)

	rows, err := store.List()
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	var matched *artifactdb.Record
	for i := range rows {
		if rows[i].Name == "googletest" && rows[i].Version == "1.14.0" && rows[i].ABITag == "abi-1" && rows[i].BuildType == "shared" {
			matched = &rows[i]
			break
		}
	}
	require.NotNil(t, matched)
	assert.NotEmpty(t, matched.HashID)
	assert.Equal(t, []string{"compiler=gcc11"}, matched.BuildTags)
}

func TestPublishProjectModeStillWorks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "demo"
version = "0.1.0"
std = "c++17"

[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))
	require.NoError(t, os.MkdirAll("build/release/lib", 0o755))
	require.NoError(t, os.WriteFile("build/release/lib/libdemo.a", []byte("binary"), 0o644))

	rootCmd.SetArgs([]string{"publish", "--abi-tag", "abi-1", "--build-type", "static"})
	require.NoError(t, rootCmd.Execute())
	assert.Equal(t, 1, fake.uploadCalls)
	assert.Equal(t, "demo", fake.uploadedPkg)
	assert.Equal(t, "0.1.0", fake.uploadedVer)
}

func TestPublishProjectModeRefreshesManifestStd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	fake := &publishFakeRegistryClient{
		manifestByKey: map[string]*registry.Manifest{
			"demo@0.1.0": {
				Name:    "demo",
				Version: "0.1.0",
				Std:     "c++14",
			},
		},
	}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "demo"
version = "0.1.0"
std = "c++20"

[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))
	require.NoError(t, os.MkdirAll("build/release/lib", 0o755))
	require.NoError(t, os.WriteFile("build/release/lib/libdemo.a", []byte("binary"), 0o644))

	rootCmd.SetArgs([]string{"publish", "--abi-tag", "abi-1", "--build-type", "static"})
	require.NoError(t, rootCmd.Execute())

	manifest := fake.manifestByKey["demo@0.1.0"]
	require.NotNil(t, manifest)
	assert.Equal(t, "c++20", manifest.Std)
}

func TestPublishLocalArtifactRequiresExplicitBuildType(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.a"), []byte("binary"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "static",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
	})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--build-type must be explicitly provided")
	assert.Equal(t, 0, fake.uploadCalls)
}

func TestPublishProjectModeFailsOnManifestFetchError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	fake := &publishFakeRegistryClient{getManifestErr: errors.New("registry timeout")}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "demo"
version = "0.1.0"
std = "c++17"

[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))
	require.NoError(t, os.MkdirAll("build/release/lib", 0o755))
	require.NoError(t, os.WriteFile("build/release/lib/libdemo.a", []byte("binary"), 0o644))

	rootCmd.SetArgs([]string{"publish", "--abi-tag", "abi-1", "--build-type", "static"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get manifest")
	assert.Equal(t, 0, fake.uploadCalls, "upload should be skipped if GetManifest fails")
	assert.Equal(t, 0, fake.manifestUploadCalls)
}

func TestPublishLocalArtifactRequiresExplicitBuildTypeAcrossRepeatedExecutions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("binary"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
	})
	require.NoError(t, rootCmd.Execute())

	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
	})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--build-type must be explicitly provided")
}

func TestPublishLocalArtifactRepublishReplacesSameVariant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("binary-v1"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
	})
	require.NoError(t, rootCmd.Execute())
	firstHash := fake.uploadedID
	require.NotEmpty(t, firstHash)

	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("binary-v2"), 0o644))
	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
	})
	require.NoError(t, rootCmd.Execute())
	secondHash := fake.uploadedID
	require.NotEmpty(t, secondHash)
	assert.NotEqual(t, firstHash, secondHash)

	manifest := fake.manifestByKey["googletest@1.14.0"]
	require.NotNil(t, manifest)
	sharedCount := 0
	for i := range manifest.Artifacts {
		artifact := manifest.Artifacts[i]
		if artifact.ABITag == "abi-1" && artifact.BuildType == "shared" {
			sharedCount++
			assert.Equal(t, secondHash, artifact.HashID)
		}
	}
	assert.Equal(t, 1, sharedCount)
}

func TestPublishSkipsIfHashMatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("binary"), 0o644))

	// First publish to get the hash
	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
	})
	require.NoError(t, rootCmd.Execute())
	assert.Equal(t, 1, fake.uploadCalls)
	hashID := fake.uploadedID

	// Second publish should skip
	fake.uploadCalls = 0
	fake.manifestUploadCalls = 0
	stdout := captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0",
			"--abi-tag", "abi-1",
			"--build-type", "shared",
		})
		require.NoError(t, rootCmd.Execute())
	})
	assert.Equal(t, 0, fake.uploadCalls, "should skip upload")
	assert.Equal(t, 0, fake.manifestUploadCalls, "should skip manifest upload")
	assert.Contains(t, stdout, "already published with same content")

	// Third publish with --force should NOT skip
	fake.uploadCalls = 0
	fake.manifestUploadCalls = 0
	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
		"--force",
	})
	require.NoError(t, rootCmd.Execute())
	assert.Equal(t, 1, fake.uploadCalls, "should NOT skip upload with --force")
	assert.Equal(t, 1, fake.manifestUploadCalls)
	assert.Equal(t, hashID, fake.uploadedID)
}

func TestPublishUpdatesExistingArtifact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("v1"), 0o644))

	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// First publish
	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
	})
	require.NoError(t, rootCmd.Execute())
	hash1 := fake.uploadedID

	// Change content and publish again
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("v2"), 0o644))
	fake.uploadCalls = 0
	fake.manifestUploadCalls = 0
	stdout := captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0",
			"--abi-tag", "abi-1",
			"--build-type", "shared",
		})
		require.NoError(t, rootCmd.Execute())
	})
	hash2 := fake.uploadedID
	assert.NotEqual(t, hash1, hash2)
	assert.Equal(t, 1, fake.uploadCalls, "should upload when hash differs")
	assert.Equal(t, 1, fake.manifestUploadCalls)
	assert.Contains(t, stdout, "updating existing artifact")
}

func TestPublishLocalArtifactMarksIndexedOriginRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.so"), []byte("binary"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	fake := &publishFakeRegistryClient{}
	prevFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return fake, nil
	}
	t.Cleanup(func() { publishNewRegistryClient = prevFactory })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	rootCmd.SetArgs([]string{
		"publish", "googletest",
		"--version", "1.14.0",
		"--abi-tag", "abi-1",
		"--build-type", "shared",
	})
	require.NoError(t, rootCmd.Execute())

	rows, err := store.List()
	require.NoError(t, err)
	var row *artifactdb.Record
	for i := range rows {
		if rows[i].Name == "googletest" && rows[i].Version == "1.14.0" && rows[i].ABITag == "abi-1" && rows[i].BuildType == "shared" {
			row = &rows[i]
			break
		}
	}
	require.NotNil(t, row)
	assert.Equal(t, "registry", row.Origin)
}

func TestResolveLocalArtifactPrefixRejectsLegacyPathForTypedRequest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".cstow", "cache")
	legacyPath := filepath.Join(cacheRoot, "googletest", "1.14.0", "abi-1")
	require.NoError(t, os.MkdirAll(filepath.Join(legacyPath, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyPath, "lib", "libgtest.a"), []byte("legacy"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "abi-1",
		BuildType:  "",
		InstallDir: legacyPath,
		Origin:     "local",
	}))

	_, err = resolveLocalArtifactPrefix("googletest", "1.14.0", "abi-1", "shared")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local artifact not found")
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	outC := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.Bytes()
	}()

	fn()

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	return string(<-outC)
}
