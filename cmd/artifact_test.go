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
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	require.NoError(t, rootCmd.Execute())
	return buf.String()
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
