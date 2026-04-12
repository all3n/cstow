package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanCacheUsesConfiguredGlobalCacheDir(t *testing.T) {
	home := t.TempDir()
	configuredCacheDir := filepath.Join(home, "configured-cache")
	configuredDBPath := filepath.Join(home, "cstow.db")

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[cache]
dir = "~/configured-cache"
`), 0o644))

	require.NoError(t, os.MkdirAll(configuredCacheDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configuredCacheDir, "marker.txt"), []byte("x"), 0o644))

	require.NoError(t, os.WriteFile(configuredDBPath, []byte("db"), 0o644))

	cleanCache()

	_, err := os.Stat(configuredCacheDir)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(configuredDBPath)
	assert.True(t, os.IsNotExist(err))
}
