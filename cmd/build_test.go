package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDependenciesReady_NoLockFile(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	err = checkDependenciesReady()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cstow.lock not found")
	assert.Contains(t, err.Error(), "cstow add")
}

func TestCheckDependenciesReady_AllPresent(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "fmt"
version = "10.2.1"
source = "registry"
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join("cstow_deps", "fmt"), 0o755))

	err = checkDependenciesReady()
	assert.NoError(t, err)
}

func TestCheckDependenciesReady_MissingDeps(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "fmt"
version = "10.2.1"
source = "registry"

[[package]]
name = "spdlog"
version = "1.13.0"
source = "registry"
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join("cstow_deps", "fmt"), 0o755))
	// spdlog dir intentionally NOT created

	err = checkDependenciesReady()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependencies")
	assert.Contains(t, err.Error(), "spdlog@1.13.0")
	assert.Contains(t, err.Error(), "cstow fetch")
	assert.NotContains(t, err.Error(), "fmt@10.2.1")
}

func TestCheckDependenciesReady_EmptyLock(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1
`), 0o644))

	err = checkDependenciesReady()
	assert.NoError(t, err)
}
