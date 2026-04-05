package cmakegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverDeps_FindsConfigFile(t *testing.T) {
	dir := t.TempDir()
	// Create fmt/lib/cmake/fmt/fmtConfig.cmake
	cfgDir := filepath.Join(dir, "fmt", "lib", "cmake", "fmt")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "fmtConfig.cmake"), nil, 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 1)

	assert.Equal(t, "fmt", deps[0].Name)
	assert.Equal(t, "fmt::fmt", deps[0].TargetName)
	assert.Contains(t, deps[0].ConfigFile, "fmtConfig.cmake")
	assert.Equal(t, filepath.Join(dir, "fmt"), deps[0].Prefix)
}

func TestDiscoverDeps_LowercaseConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "protobuf", "lib", "cmake", "protobuf")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "protobuf-config.cmake"), nil, 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 1)

	assert.Equal(t, "protobuf", deps[0].Name)
	assert.Equal(t, "protobuf::protobuf", deps[0].TargetName)
	assert.Contains(t, deps[0].ConfigFile, "protobuf-config.cmake")
}

func TestDiscoverDeps_FallbackNoConfigFile(t *testing.T) {
	dir := t.TempDir()
	incDir := filepath.Join(dir, "expected", "include", "expected")
	require.NoError(t, os.MkdirAll(incDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(incDir, "expected.h"), nil, 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	require.Len(t, deps, 1)

	assert.Equal(t, "expected", deps[0].Name)
	assert.Empty(t, deps[0].ConfigFile)
	assert.Empty(t, deps[0].TargetName)
}

func TestDiscoverDeps_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	assert.Len(t, deps, 0)
}

func TestDiscoverDeps_NonexistentDir(t *testing.T) {
	deps, err := DiscoverDeps("/nonexistent/path/cstow_deps")
	require.NoError(t, err)
	assert.Nil(t, deps)
}

func TestDiscoverDeps_MultipleDeps(t *testing.T) {
	dir := t.TempDir()

	// fmt with config
	fmtCfg := filepath.Join(dir, "fmt", "lib", "cmake", "fmt")
	require.NoError(t, os.MkdirAll(fmtCfg, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fmtCfg, "fmtConfig.cmake"), nil, 0o644))

	// spdlog with config
	spdCfg := filepath.Join(dir, "spdlog", "lib", "cmake", "spdlog")
	require.NoError(t, os.MkdirAll(spdCfg, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(spdCfg, "spdlogConfig.cmake"), nil, 0o644))

	// expected header-only (no cmake config)
	expInc := filepath.Join(dir, "expected", "include", "expected")
	require.NoError(t, os.MkdirAll(expInc, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(expInc, "expected.h"), nil, 0o644))

	deps, err := DiscoverDeps(dir)
	require.NoError(t, err)
	assert.Len(t, deps, 3)

	// Build a map for order-independent lookup
	m := make(map[string]DepTarget, len(deps))
	for _, d := range deps {
		m[d.Name] = d
	}

	fmtDep := m["fmt"]
	assert.Equal(t, "fmt::fmt", fmtDep.TargetName)
	assert.NotEmpty(t, fmtDep.ConfigFile)

	spdDep := m["spdlog"]
	assert.Equal(t, "spdlog::spdlog", spdDep.TargetName)
	assert.NotEmpty(t, spdDep.ConfigFile)

	expDep := m["expected"]
	assert.Empty(t, expDep.ConfigFile)
	assert.Empty(t, expDep.TargetName)
}
