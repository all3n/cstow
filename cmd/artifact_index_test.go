package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexSuccessfulArtifactUsesRealCacheLayout(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	t.Setenv("HOME", home)

	cache := &resolver.FSCache{Root: cacheRoot}
	legacyPath := cache.LegacyPath("fmt", "10.2.1", "abi-1")
	require.NoError(t, os.MkdirAll(legacyPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyPath, "marker.txt"), []byte("legacy"), 0o644))

	require.NoError(t, indexSuccessfulArtifact(cache, indexedArtifact{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: legacyPath,
		Origin:     "unknown",
	}))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].BuildType)
	assert.Equal(t, legacyPath, rows[0].InstallDir)
}
