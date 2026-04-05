package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDependenciesReady_NoLockFile(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	err = checkDependenciesReady(".")
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

	err = checkDependenciesReady(".")
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

	err = checkDependenciesReady(".")
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

	err = checkDependenciesReady(".")
	assert.NoError(t, err)
}

func TestAppendCMakeConfigArgs_Defines(t *testing.T) {
	cfg := &config.Config{
		Build: config.Build{
			Defines: []string{"FOO=BAR", "ENABLE_TESTS=ON"},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, "debug")
	assert.Contains(t, args, "-DFOO=BAR")
	assert.Contains(t, args, "-DENABLE_TESTS=ON")
}

func TestAppendCMakeConfigArgs_IncludePaths(t *testing.T) {
	cfg := &config.Config{
		Build: config.Build{
			Include: []string{"vendor/include", "third_party/include"},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, "debug")
	found := false
	for _, a := range args {
		if len(a) > len("-DCMAKE_INCLUDE_PATH=") && a[:len("-DCMAKE_INCLUDE_PATH=")] == "-DCMAKE_INCLUDE_PATH=" {
			assert.Contains(t, a, "vendor/include")
			assert.Contains(t, a, "third_party/include")
			found = true
		}
	}
	assert.True(t, found, "expected CMAKE_INCLUDE_PATH in args")
}

func TestAppendCMakeConfigArgs_ProfileLTO(t *testing.T) {
	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"release": {LTO: true},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, "release")
	assert.Contains(t, args, "-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON")
}

func TestAppendCMakeConfigArgs_ProfileNoLTO(t *testing.T) {
	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"debug": {LTO: false},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, "debug")
	assert.NotContains(t, args, "-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON")
}

func TestAppendCMakeConfigArgs_NoExtras(t *testing.T) {
	cfg := &config.Config{}
	args := appendCMakeConfigArgs(nil, cfg, "debug")
	assert.Empty(t, args)
}
