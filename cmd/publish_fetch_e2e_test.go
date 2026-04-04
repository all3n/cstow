package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedFakeRegistry is an in-memory registry that both publish and fetch
// talk to, enabling true publish-then-fetch end-to-end tests.
type sharedFakeRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte           // key -> artifact data
	manifests map[string]*registry.Manifest // "pkg@version" -> manifest
}

func newSharedFakeRegistry() *sharedFakeRegistry {
	return &sharedFakeRegistry{
		blobs:     make(map[string][]byte),
		manifests: make(map[string]*registry.Manifest),
	}
}

func (r *sharedFakeRegistry) artifactKey(pkg, version, abiTag, buildType, hashID string) string {
	return pkg + "/" + version + "/" + abiTag + "/" + buildType + "/" + hashID + ".tar.zst"
}

func (r *sharedFakeRegistry) Upload(_ context.Context, pkg, version, abiTag, buildType, hashID string, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.artifactKey(pkg, version, abiTag, buildType, hashID)
	r.blobs[key] = append([]byte(nil), data...)
	// Also store a simpler alias for non-hash downloads
	aliasKey := pkg + "/" + version + "/" + abiTag + "/" + buildType + ".tar.zst"
	r.blobs[aliasKey] = append([]byte(nil), data...)
	return nil
}

func (r *sharedFakeRegistry) UploadManifest(_ context.Context, pkg, version string, manifest *registry.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.manifests[pkg+"@"+version] = manifest
	return nil
}

func (r *sharedFakeRegistry) GetManifest(_ context.Context, pkg, version string) (*registry.Manifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.manifests[pkg+"@"+version]; ok {
		return m, nil
	}
	return nil, os.ErrNotExist
}

func (r *sharedFakeRegistry) Download(_ context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Try hash-based key first, then alias
	if hashID != "" {
		key := r.artifactKey(pkg, version, abiTag, buildType, hashID)
		if data, ok := r.blobs[key]; ok {
			return append([]byte(nil), data...), nil
		}
	}
	aliasKey := pkg + "/" + version + "/" + abiTag + "/" + buildType + ".tar.zst"
	if data, ok := r.blobs[aliasKey]; ok {
		return append([]byte(nil), data...), nil
	}
	return nil, os.ErrNotExist
}

func (r *sharedFakeRegistry) ListVersions(_ context.Context, pkg string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var versions []string
	seen := make(map[string]bool)
	for key := range r.manifests {
		parts := strings.SplitN(key, "@", 2)
		if len(parts) == 2 && parts[0] == pkg && !seen[parts[1]] {
			versions = append(versions, parts[1])
			seen[parts[1]] = true
		}
	}
	if len(versions) == 0 {
		return nil, os.ErrNotExist
	}
	return versions, nil
}

// TestPublishFetchGoogletestStaticSharedRoundTrip verifies:
//  1. Publish googletest as static -> registry has artifact + manifest
//  2. Publish googletest as shared  -> registry manifest has both variants
//  3. Fetch resolves static via manifest -> correct hash_id, symlink works
//  4. Fetch resolves shared via manifest -> correct hash_id, symlink works
//  5. The local artifactdb records both with origin "registry"
func TestPublishFetchGoogletestStaticSharedRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	// Build separate install prefixes for static and shared so the tarballs
	// produce different hashes and avoid the UNIQUE constraint on hash_id.
	staticPrefix := filepath.Join(home, "install", "googletest-static")
	require.NoError(t, os.MkdirAll(filepath.Join(staticPrefix, "lib"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(staticPrefix, "include", "gtest"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staticPrefix, "include", "gtest", "gtest.h"), []byte("#pragma once"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(staticPrefix, "lib", "libgtest.a"), []byte("static-binary"), 0o644))

	sharedPrefix := filepath.Join(home, "install", "googletest-shared")
	require.NoError(t, os.MkdirAll(filepath.Join(sharedPrefix, "lib"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(sharedPrefix, "include", "gtest"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedPrefix, "include", "gtest", "gtest.h"), []byte("#pragma once"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sharedPrefix, "lib", "libgtest.so"), []byte("shared-binary"), 0o644))

	sharedReg := newSharedFakeRegistry()

	// Wire both publish and fetch to the same shared registry
	prevPubFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return sharedReg, nil
	}
	prevFetchFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return sharedReg, nil
	}
	t.Cleanup(func() {
		publishNewRegistryClient = prevPubFactory
		fetchNewRegistryClient = prevFetchFactory
	})

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// ---- Step 1: Seed the artifactdb with local artifacts ----
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "gcc13-cxx17-linux-x86_64",
		BuildType:  "static",
		InstallDir: staticPrefix,
		Origin:     "local",
	}))
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "gcc13-cxx17-linux-x86_64",
		BuildType:  "shared",
		InstallDir: sharedPrefix,
		Origin:     "local",
	}))

	// ---- Step 2: Publish static ----
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0",
			"--abi-tag", "gcc13-cxx17-linux-x86_64",
			"--build-type", "static",
			"--build-tag", "compiler=gcc13",
		})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify manifest has the static entry
	manifest, err := sharedReg.GetManifest(context.Background(), "googletest", "1.14.0")
	require.NoError(t, err)
	require.Len(t, manifest.Artifacts, 1)
	assert.Equal(t, "static", manifest.Artifacts[0].BuildType)
	staticHash := manifest.Artifacts[0].HashID
	require.NotEmpty(t, staticHash)

	// ---- Step 3: Publish shared ----
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0",
			"--abi-tag", "gcc13-cxx17-linux-x86_64",
			"--build-type", "shared",
			"--build-tag", "compiler=gcc13",
		})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify manifest now has both static and shared
	manifest, err = sharedReg.GetManifest(context.Background(), "googletest", "1.14.0")
	require.NoError(t, err)
	require.Len(t, manifest.Artifacts, 2)

	var staticEntry, sharedEntry *registry.Artifact
	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		switch a.BuildType {
		case "static":
			staticEntry = a
		case "shared":
			sharedEntry = a
		}
	}
	require.NotNil(t, staticEntry)
	require.NotNil(t, sharedEntry)
	assert.NotEqual(t, staticEntry.HashID, sharedEntry.HashID)

	// ---- Step 4: Write cstow.toml + cstow.lock for fetch ----
	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "test-project"
version = "0.1.0"

[[dependencies]]
name = "googletest"
version = "1.14.0"
source = "registry"
build_type = "static"
`), 0o644))

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "googletest"
version = "1.14.0"
source = "registry"
abi_tag = "gcc13-cxx17-linux-x86_64"
build_type = "static"
`), 0o644))

	// Remove the cached artifact so fetch has to download from registry
	cacheDir := filepath.Join(home, ".cstow", "cache")
	require.NoError(t, os.RemoveAll(cacheDir))
	require.NoError(t, os.RemoveAll("cstow_deps"))

	// ---- Step 5: Fetch static ----
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify the symlink and artifact content
	staticLink := filepath.Join("cstow_deps", "googletest")
	linkTarget, err := os.Readlink(staticLink)
	require.NoError(t, err)
	assert.Contains(t, linkTarget, "static")
	require.FileExists(t, filepath.Join(staticLink, "lib", "libgtest.a"))

	// ---- Step 6: Verify artifactdb was populated with hash_id ----
	rows, err := store.List()
	require.NoError(t, err)
	var staticRow *artifactdb.Record
	for i := range rows {
		if rows[i].Name == "googletest" && rows[i].BuildType == "static" {
			staticRow = &rows[i]
			break
		}
	}
	require.NotNil(t, staticRow)
	assert.Equal(t, staticHash, staticRow.HashID)
	assert.Equal(t, "registry", staticRow.Origin)
	assert.Equal(t, []string{"compiler=gcc13"}, staticRow.BuildTags)

	// ---- Step 7: Fetch shared via --artifact hash prefix ----
	require.NoError(t, os.RemoveAll("cstow_deps"))

	// Update lock to reference shared
	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "googletest"
version = "1.14.0"
source = "registry"
abi_tag = "gcc13-cxx17-linux-x86_64"
build_type = "shared"
`), 0o644))

	require.NoError(t, os.RemoveAll(cacheDir))

	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	sharedLink := filepath.Join("cstow_deps", "googletest")
	linkTarget, err = os.Readlink(sharedLink)
	require.NoError(t, err)
	assert.Contains(t, linkTarget, "shared")
	require.FileExists(t, filepath.Join(sharedLink, "lib", "libgtest.so"))

	// Verify shared in artifactdb
	rows, err = store.List()
	require.NoError(t, err)
	var sharedRow *artifactdb.Record
	for i := range rows {
		if rows[i].Name == "googletest" && rows[i].BuildType == "shared" {
			sharedRow = &rows[i]
			break
		}
	}
	require.NotNil(t, sharedRow)
	assert.Equal(t, sharedEntry.HashID, sharedRow.HashID)
	assert.Equal(t, "registry", sharedRow.Origin)

	// ---- Step 8: Verify fetch --artifact works with hash prefix ----
	require.NoError(t, os.RemoveAll("cstow_deps"))
	require.NoError(t, os.RemoveAll(cacheDir))
	// Clear artifactdb so hash lookup goes to registry
	require.NoError(t, os.Remove(filepath.Join(home, ".cstow", "cstow.db")))

	store2, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store2.Close()) })

	hashPrefix := sharedEntry.HashID[:8]
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--artifact", hashPrefix})
		require.NoError(t, rootCmd.Execute())
	})

	artifactLink := filepath.Join("cstow_deps", "googletest")
	require.FileExists(t, filepath.Join(artifactLink, "lib", "libgtest.so"))

	// Verify the downloaded artifact was indexed
	dlRows, err := store2.List()
	require.NoError(t, err)
	require.NotEmpty(t, dlRows)
	assert.Equal(t, sharedEntry.HashID, dlRows[0].HashID)
}

// TestPublishFetchVerifiesContentIntegrity ensures that the data round-tripped
// through publish -> registry -> fetch produces the same extracted files.
func TestPublishFetchVerifiesContentIntegrity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	// Create a realistic install prefix
	installPrefix := filepath.Join(home, "install", "googletest")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "include", "gtest"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib", "cmake", "gtest"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "include", "gtest", "gtest.h"), []byte("#pragma once\n#include <string>\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libgtest.a"), []byte("static-archive-content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "cmake", "gtest", "gtestConfig.cmake"), []byte("set(gtest_FOUND TRUE)\n"), 0o644))

	// Seed artifactdb
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "googletest",
		Version:    "1.14.0",
		ABITag:     "gcc13-cxx17",
		BuildType:  "static",
		InstallDir: installPrefix,
		Origin:     "local",
	}))

	sharedReg := newSharedFakeRegistry()
	prevPubFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return sharedReg, nil
	}
	prevFetchFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return sharedReg, nil
	}
	t.Cleanup(func() {
		publishNewRegistryClient = prevPubFactory
		fetchNewRegistryClient = prevFetchFactory
	})

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Publish
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0",
			"--abi-tag", "gcc13-cxx17",
			"--build-type", "static",
		})
		require.NoError(t, rootCmd.Execute())
	})

	// Remove source and cache
	require.NoError(t, os.RemoveAll(installPrefix))
	cacheDir := filepath.Join(home, ".cstow", "cache")
	require.NoError(t, os.RemoveAll(cacheDir))

	// Write config for fetch
	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "test-project"
version = "0.1.0"

[[dependencies]]
name = "googletest"
version = "1.14.0"
source = "registry"
build_type = "static"
`), 0o644))
	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "googletest"
version = "1.14.0"
source = "registry"
abi_tag = "gcc13-cxx17"
build_type = "static"
`), 0o644))

	// Fetch
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify all files round-tripped correctly
	fetchedLink := filepath.Join("cstow_deps", "googletest")
	require.FileExists(t, filepath.Join(fetchedLink, "include", "gtest", "gtest.h"))

	hContent, err := os.ReadFile(filepath.Join(fetchedLink, "include", "gtest", "gtest.h"))
	require.NoError(t, err)
	assert.Equal(t, "#pragma once\n#include <string>\n", string(hContent))

	libContent, err := os.ReadFile(filepath.Join(fetchedLink, "lib", "libgtest.a"))
	require.NoError(t, err)
	assert.Equal(t, "static-archive-content", string(libContent))

	cmakeContent, err := os.ReadFile(filepath.Join(fetchedLink, "lib", "cmake", "gtest", "gtestConfig.cmake"))
	require.NoError(t, err)
	assert.Equal(t, "set(gtest_FOUND TRUE)\n", string(cmakeContent))
}

// TestPublishFetchSharedArtifactViaTarZst verifies the tar.zst round-trip
// for a shared library artifact end-to-end.
func TestPublishFetchSharedArtifactViaTarZst(t *testing.T) {
	// This test verifies the actual pack.CreateTarZst -> registry -> pack.ExtractTarZst cycle
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "src")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib", "libtest.so"), []byte("shared-lib-bytes"), 0o644))

	// Create tar.zst
	data, err := pack.CreateTarZst(srcDir)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Extract
	destDir := filepath.Join(tmp, "dest")
	require.NoError(t, os.MkdirAll(destDir, 0o755))
	require.NoError(t, pack.ExtractTarZst(data, destDir))

	content, err := os.ReadFile(filepath.Join(destDir, "lib", "libtest.so"))
	require.NoError(t, err)
	assert.Equal(t, "shared-lib-bytes", string(content))
}

// TestPublishFetchGoogletestBothVariantsInSingleManifest verifies that
// after publishing static and shared, the manifest carries both artifacts
// and fetch can select the right one based on build_type.
func TestPublishFetchGoogletestBothVariantsInSingleManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	// Create separate install dirs for static and shared
	staticPrefix := filepath.Join(home, "install", "gtest-static")
	require.NoError(t, os.MkdirAll(filepath.Join(staticPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staticPrefix, "lib", "libgtest.a"), []byte("static-content"), 0o644))

	sharedPrefix := filepath.Join(home, "install", "gtest-shared")
	require.NoError(t, os.MkdirAll(filepath.Join(sharedPrefix, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedPrefix, "lib", "libgtest.so"), []byte("shared-content"), 0o644))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.Upsert(artifactdb.Record{
		Name: "googletest", Version: "1.14.0", ABITag: "linux-x86_64",
		BuildType: "static", InstallDir: staticPrefix, Origin: "local",
	}))
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name: "googletest", Version: "1.14.0", ABITag: "linux-x86_64",
		BuildType: "shared", InstallDir: sharedPrefix, Origin: "local",
	}))

	sharedReg := newSharedFakeRegistry()
	prevPubFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return sharedReg, nil
	}
	prevFetchFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return sharedReg, nil
	}
	t.Cleanup(func() {
		publishNewRegistryClient = prevPubFactory
		fetchNewRegistryClient = prevFetchFactory
	})

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Publish static
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0", "--abi-tag", "linux-x86_64", "--build-type", "static",
		})
		require.NoError(t, rootCmd.Execute())
	})

	// Publish shared
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "googletest",
			"--version", "1.14.0", "--abi-tag", "linux-x86_64", "--build-type", "shared",
		})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify manifest has exactly 2 artifacts
	manifest, err := sharedReg.GetManifest(context.Background(), "googletest", "1.14.0")
	require.NoError(t, err)
	assert.Len(t, manifest.Artifacts, 2)

	// Clear cache and fetch static
	cacheDir := filepath.Join(home, ".cstow", "cache")
	require.NoError(t, os.RemoveAll(cacheDir))

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "t"
version = "0.1.0"
[[dependencies]]
name = "googletest"
version = "1.14.0"
source = "registry"
build_type = "static"
`), 0o644))
	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1
[[package]]
name = "googletest"
version = "1.14.0"
source = "registry"
abi_tag = "linux-x86_64"
build_type = "static"
`), 0o644))

	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	// Fetched content should be the static variant
	fetchedLib := filepath.Join("cstow_deps", "googletest", "lib", "libgtest.a")
	require.FileExists(t, fetchedLib)
	content, err := os.ReadFile(fetchedLib)
	require.NoError(t, err)
	assert.Equal(t, "static-content", string(content))

	// Now clear and fetch shared
	require.NoError(t, os.RemoveAll(cacheDir))
	require.NoError(t, os.RemoveAll("cstow_deps"))

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1
[[package]]
name = "googletest"
version = "1.14.0"
source = "registry"
abi_tag = "linux-x86_64"
build_type = "shared"
`), 0o644))

	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	// Fetched content should be the shared variant
	fetchedShared := filepath.Join("cstow_deps", "googletest", "lib", "libgtest.so")
	require.FileExists(t, fetchedShared)
	content, err = os.ReadFile(fetchedShared)
	require.NoError(t, err)
	assert.Equal(t, "shared-content", string(content))

	// The static .a should NOT be present in the shared fetch
	assert.NoFileExists(t, filepath.Join("cstow_deps", "googletest", "lib", "libgtest.a"))
}

// Verify that the sharedFakeRegistry also satisfies fetchRegistryClient
var _ fetchRegistryClient = (*sharedFakeRegistry)(nil)
var _ publishRegistryClient = (*sharedFakeRegistry)(nil)

// helper to check sharedFakeRegistry satisfies fetchRegistryClient interface at compile time
func init() {
	_ = func() fetchRegistryClient { return &sharedFakeRegistry{} }
	_ = func() publishRegistryClient { return &sharedFakeRegistry{} }
}

// TestFetchStoresHashIDFromManifest verifies that a fresh fetch (no prior publish to
// artifactdb) correctly stores hash_id and build_tags from the manifest into the local
// artifact database. This catches a regression where the fetch code forgot to propagate
// artifact.HashID and artifact.BuildTags after manifest-based download.
func TestFetchStoresHashIDFromManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[registry]]
name = "default"
url = "s3://bucket/prefix"
`), 0o644))

	installPrefix := filepath.Join(home, "install", "mylib")
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "lib"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(installPrefix, "include"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "include", "mylib.h"), []byte("#pragma once"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(installPrefix, "lib", "libmylib.a"), []byte("mylib-static"), 0o644))

	sharedReg := newSharedFakeRegistry()

	// Publish to populate the fake registry with a manifest containing hash_id.
	// Seed artifactdb so publish can find the install dir.
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "mylib",
		Version:    "2.0.0",
		ABITag:     "gcc13-cxx17",
		BuildType:  "static",
		InstallDir: installPrefix,
		Origin:     "local",
	}))
	require.NoError(t, store.Close())

	prevPubFactory := publishNewRegistryClient
	publishNewRegistryClient = func(_ context.Context, _ config.Registry) (publishRegistryClient, error) {
		return sharedReg, nil
	}
	prevFetchFactory := fetchNewRegistryClient
	fetchNewRegistryClient = func(_ context.Context, _ config.Registry) (fetchRegistryClient, error) {
		return sharedReg, nil
	}
	t.Cleanup(func() {
		publishNewRegistryClient = prevPubFactory
		fetchNewRegistryClient = prevFetchFactory
	})

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Publish to seed the registry
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{
			"publish", "mylib",
			"--version", "2.0.0",
			"--abi-tag", "gcc13-cxx17",
			"--build-type", "static",
			"--build-tag", "os=linux",
		})
		require.NoError(t, rootCmd.Execute())
	})

	// Get the hash from the registry manifest
	manifest, err := sharedReg.GetManifest(context.Background(), "mylib", "2.0.0")
	require.NoError(t, err)
	require.Len(t, manifest.Artifacts, 1)
	expectedHash := manifest.Artifacts[0].HashID
	require.NotEmpty(t, expectedHash)

	// Delete the artifactdb so fetch starts completely fresh
	require.NoError(t, os.Remove(filepath.Join(home, ".cstow", "cstow.db")))
	// Remove cache so fetch has to download
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".cstow", "cache")))

	// Write config for fetch
	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "test-project"
version = "0.1.0"

[[dependencies]]
name = "mylib"
version = "2.0.0"
source = "registry"
build_type = "static"
`), 0o644))
	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "mylib"
version = "2.0.0"
source = "registry"
abi_tag = "gcc13-cxx17"
build_type = "static"
`), 0o644))

	// Fetch
	_ = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"fetch", "--toolchain", "gcc"})
		require.NoError(t, rootCmd.Execute())
	})

	// Verify artifactdb has the hash_id and build_tags
	freshStore, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, freshStore.Close()) })

	rows, err := freshStore.List()
	require.NoError(t, err)

	var mylibRow *artifactdb.Record
	for i := range rows {
		if rows[i].Name == "mylib" && rows[i].BuildType == "static" {
			mylibRow = &rows[i]
			break
		}
	}
	require.NotNil(t, mylibRow, "mylib static should be in artifactdb after fetch")
	assert.Equal(t, expectedHash, mylibRow.HashID, "hash_id should be populated from manifest")
	assert.Equal(t, []string{"os=linux"}, mylibRow.BuildTags, "build_tags should be populated from manifest")
	assert.Equal(t, "registry", mylibRow.Origin)
}
