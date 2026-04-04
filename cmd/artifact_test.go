package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeRootForTest(t *testing.T, args ...string) string {
	t.Helper()
	buf := new(bytes.Buffer)
	prevOut := rootCmd.OutOrStdout()
	prevErr := rootCmd.ErrOrStderr()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(prevOut)
		rootCmd.SetErr(prevErr)
		rootCmd.SetArgs(nil)
	})
	require.NoError(t, rootCmd.Execute())
	return buf.String()
}

func executeRootForTestWithError(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	prevOut := rootCmd.OutOrStdout()
	prevErr := rootCmd.ErrOrStderr()
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(prevOut)
		rootCmd.SetErr(prevErr)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestArtifactListEmptyState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	output := executeRootForTest(t, "artifact", "list")
	assert.Contains(t, output, "No indexed artifacts found.")
	assert.Contains(t, output, "cstow artifact sync")
}

func TestArtifactListDisplaysSortedRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "zlib",
		Version:    "1.3.1",
		ABITag:     "abi-z",
		BuildType:  "static",
		InstallDir: "/tmp/cache/zlib/1.3.1/abi-z/static",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi-f",
		BuildType:  "",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi-f",
		Origin:     "unknown",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	output := executeRootForTest(t, "artifact", "list")
	assert.Contains(t, output, "NAME")
	assert.True(t, bytes.Index([]byte(output), []byte("fmt")) < bytes.Index([]byte(output), []byte("zlib")))
	assert.Contains(t, output, "default")
}

func TestArtifactSyncScansCacheAndReportsCounters(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-f", "shared"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-f", "shared", "marker.txt"), []byte("x"), 0o644))

	output := executeRootForTest(t, "artifact", "sync")
	assert.Contains(t, output, "artifact sync complete")
	assert.Contains(t, output, "inserted: 1")
	assert.Contains(t, output, "updated: 0")
	assert.Contains(t, output, "deleted: 0")

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fmt", rows[0].Name)
}

func TestArtifactShowByHashID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC)
	fullHash := "f1a2b3c4d5e6f78900112233445566778899aabbccddeeff0011223344556677"
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "linux-x86_64-gcc13-cxx11",
		BuildType:  "",
		HashID:     fullHash,
		BuildTags:  []string{"cxx11", "lto"},
		InstallDir: "/tmp/cache/fmt/10.2.1/linux-x86_64-gcc13-cxx11",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	output := executeRootForTest(t, "artifact", "show", "f1a2b3c4")
	assert.Contains(t, output, "name: fmt")
	assert.Contains(t, output, "version: 10.2.1")
	assert.Contains(t, output, "abi: linux-x86_64-gcc13-cxx11")
	assert.Contains(t, output, "build_type: default")
	assert.Contains(t, output, "hash_id: "+fullHash)
	assert.Contains(t, output, "build_tags: cxx11,lto")
	assert.Contains(t, output, "origin: registry")
	assert.Contains(t, output, "path: /tmp/cache/fmt/10.2.1/linux-x86_64-gcc13-cxx11")
}

func TestArtifactShowAmbiguousHashPrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 4, 9, 30, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi-f",
		BuildType:  "shared",
		HashID:     "abc1234567890def111111111111111111111111111111111111111111111111",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi-f/shared",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "zlib",
		Version:    "1.3.1",
		ABITag:     "abi-z",
		BuildType:  "static",
		HashID:     "abc9999999999def222222222222222222222222222222222222222222222222",
		InstallDir: "/tmp/cache/zlib/1.3.1/abi-z/static",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	_, err = executeRootForTestWithError(t, "artifact", "show", "abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `hash_id prefix "abc" is ambiguous`)
}

func TestArtifactShowHashIDNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	_, err := executeRootForTestWithError(t, "artifact", "show", "doesnotexist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `artifact with hash_id prefix "doesnotexist" not found`)
	assert.NotContains(t, err.Error(), "sql: no rows in result set")
}

func TestArtifactShowRejectsEmptyHashID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "emptyhash",
		Version:    "1.0.0",
		ABITag:     "abi-empty",
		BuildType:  "",
		HashID:     "",
		InstallDir: "/tmp/cache/emptyhash/1.0.0/abi-empty",
		Origin:     "unknown",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	_, err = executeRootForTestWithError(t, "artifact", "show", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash_id must not be empty")
}
