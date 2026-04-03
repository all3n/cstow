package artifactdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncFromCacheIndexesTypedAndLegacyArtifacts(t *testing.T) {
	cacheRoot := t.TempDir()
	typed := filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-1", "shared")
	legacy := filepath.Join(cacheRoot, "spdlog", "1.13.0", "abi-2")
	require.NoError(t, os.MkdirAll(typed, 0o755))
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(typed, "marker.txt"), []byte("typed"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "marker.txt"), []byte("legacy"), 0o644))

	store, err := Open(filepath.Join(t.TempDir(), "cstow.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	stats, err := store.SyncFromCache(cacheRoot)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Inserted)
	assert.Equal(t, 0, stats.Updated)
	assert.Equal(t, 0, stats.Deleted)

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "shared", rows[0].BuildType)
	assert.Equal(t, "", rows[1].BuildType)
}

func TestSyncFromCacheDeletesStaleRowsAndIgnoresEmptyDirectories(t *testing.T) {
	cacheRoot := t.TempDir()
	empty := filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-1", "shared")
	require.NoError(t, os.MkdirAll(empty, 0o755))

	store, err := Open(filepath.Join(t.TempDir(), "cstow.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.Upsert(Record{
		Name:       "obsolete",
		Version:    "1.0.0",
		ABITag:     "abi-old",
		BuildType:  "static",
		InstallDir: filepath.Join(cacheRoot, "obsolete", "1.0.0", "abi-old", "static"),
		Origin:     "unknown",
	}))

	stats, err := store.SyncFromCache(cacheRoot)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.Inserted)
	assert.Equal(t, 0, stats.Updated)
	assert.Equal(t, 1, stats.Deleted)

	rows, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, rows)
}
