package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCommandPersistsBuildTypeToConfigAndLock(t *testing.T) {
	origWD, err := os.Getwd()
	require.NoError(t, err)

	workdir := t.TempDir()
	require.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	require.NoError(t, os.WriteFile("cstow.toml", []byte(`
[package]
name = "demo"
version = "0.1.0"
`), 0o644))

	rootCmd.SetArgs([]string{"add", "googletest@1.14.0", "--source", "registry", "--build-type", "shared"})
	require.NoError(t, rootCmd.Execute())

	cfg, err := config.Load(filepath.Join(workdir, "cstow.toml"))
	require.NoError(t, err)
	require.Len(t, cfg.Dependencies, 1)
	assert.Equal(t, "googletest", cfg.Dependencies[0].Name)
	assert.Equal(t, "shared", cfg.Dependencies[0].BuildType)

	lockFile, err := resolver.LoadLock(filepath.Join(workdir, "cstow.lock"))
	require.NoError(t, err)
	require.Len(t, lockFile.Packages, 1)
	assert.Equal(t, "shared", lockFile.Packages[0].BuildType)
}
