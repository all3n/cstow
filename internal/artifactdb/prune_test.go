package artifactdb

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorePruneAge(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	defer store.Close()

	cacheDir := t.TempDir()
	
	// Artifact 1: Old
	oldDir := filepath.Join(cacheDir, "old")
	require.NoError(t, os.MkdirAll(oldDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "f"), []byte("data"), 0644))
	oldTime := time.Now().AddDate(0, 0, -10).UTC()
	require.NoError(t, store.Upsert(Record{
		Name: "old", Version: "1.0", ABITag: "abi", BuildType: "static",
		InstallDir: oldDir, LastSeenAt: oldTime,
	}))

	// Artifact 2: New
	newDir := filepath.Join(cacheDir, "new")
	require.NoError(t, os.MkdirAll(newDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(newDir, "f"), []byte("data"), 0644))
	newTime := time.Now().UTC()
	require.NoError(t, store.Upsert(Record{
		Name: "new", Version: "1.0", ABITag: "abi", BuildType: "static",
		InstallDir: newDir, LastSeenAt: newTime,
	}))

	// Prune older than 5 days
	stats, err := store.PruneWithLimits(5, 0, false)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.RecordsDeleted)
	assert.True(t, stats.BytesFreed > 0)

	// Verify old artifact is gone from DB and filesystem
	rows, _ := store.List()
	assert.Len(t, rows, 1)
	assert.Equal(t, "new", rows[0].Name)
	_, err = os.Stat(oldDir)
	assert.True(t, os.IsNotExist(err))

	// Verify new artifact is still there
	_, err = os.Stat(newDir)
	assert.NoError(t, err)
}

func TestStorePruneSize(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	defer store.Close()

	cacheDir := t.TempDir()
	
	// Create multiple artifacts
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		dir := filepath.Join(cacheDir, name)
		require.NoError(t, os.MkdirAll(dir, 0755))
		// Each artifact 1KB (approx)
		data := make([]byte, 1024)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), data, 0644))
		
		// Access times: a (oldest) to e (newest)
		lastSeen := time.Now().Add(time.Duration(i-5) * time.Hour).UTC()
		require.NoError(t, store.Upsert(Record{
			Name: name, Version: "1.0", ABITag: "abi", BuildType: "static",
			InstallDir: dir, LastSeenAt: lastSeen,
		}))
	}

	// Total size is approx 5KB. Prune to 2KB. Should keep 2 newest artifacts ('d', 'e').
	stats, err := store.PruneWithLimits(0, 2048, false)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.RecordsDeleted)
	assert.Equal(t, int64(3072), stats.BytesFreed)

	// Verify DB state
	rows, _ := store.List()
	assert.Len(t, rows, 2)
	assert.Equal(t, "d", rows[0].Name)
	assert.Equal(t, "e", rows[1].Name)

	// Verify filesystem
	for i := 0; i < 3; i++ {
		_, err := os.Stat(filepath.Join(cacheDir, string(rune('a'+i))))
		assert.True(t, os.IsNotExist(err))
	}
	for i := 3; i < 5; i++ {
		_, err := os.Stat(filepath.Join(cacheDir, string(rune('a'+i))))
		assert.NoError(t, err)
	}
}

func TestStorePruneDryRun(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	defer store.Close()

	cacheDir := t.TempDir()
	dir := filepath.Join(cacheDir, "old")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("data"), 0644))
	
	oldTime := time.Now().AddDate(0, 0, -10).UTC()
	require.NoError(t, store.Upsert(Record{
		Name: "old", Version: "1.0", ABITag: "abi", BuildType: "static",
		InstallDir: dir, LastSeenAt: oldTime,
	}))

	// Dry run prune
	stats, err := store.PruneWithLimits(5, 0, true)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.RecordsDeleted)
	assert.True(t, stats.BytesFreed > 0)

	// Verify nothing actually deleted
	rows, _ := store.List()
	assert.Len(t, rows, 1)
	_, err = os.Stat(dir)
	assert.NoError(t, err)
}
