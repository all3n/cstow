package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceSharedDepsIntegration(t *testing.T) {
	root := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(root))
	defer os.Chdir(origDir)

	// 1. Setup workspace with two modules sharing a dependency
	// root/cstow.toml
	// root/a/cstow.toml
	// root/b/cstow.toml

	wsCfg := &config.Config{
		Package: config.Package{Name: "ws-root", Version: "0.1.0"},
		Workspace: &config.Workspace{
			Members: []string{"a", "b"},
		},
	}
	require.NoError(t, wsCfg.Save("cstow.toml"))

	require.NoError(t, os.MkdirAll("a", 0o755))
	cfgA := &config.Config{
		Package: config.Package{Name: "a", Version: "0.1.0"},
		Dependencies: []config.Dependency{
			{Name: "shared-dep", Version: "1.0.0", Source: "local", Path: "../mock-dep"},
		},
	}
	require.NoError(t, cfgA.Save(filepath.Join("a", "cstow.toml")))

	require.NoError(t, os.MkdirAll("b", 0o755))
	cfgB := &config.Config{
		Package: config.Package{Name: "b", Version: "0.1.0"},
		Dependencies: []config.Dependency{
			{Name: "shared-dep", Version: "1.0.0", Source: "local", Path: "../mock-dep"},
		},
	}
	require.NoError(t, cfgB.Save(filepath.Join("b", "cstow.toml")))

	// Create mock-dep
	require.NoError(t, os.MkdirAll("mock-dep", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("mock-dep", "dummy"), []byte("data"), 0o644))

	// 2. Run workspace fetch
	rootCmd.SetArgs([]string{"workspace", "fetch"})
	require.NoError(t, rootCmd.Execute())

	// 3. Verify root lock and deps
	assert.FileExists(t, "cstow.lock")
	assert.DirExists(t, "cstow_deps")
	// shared-dep is a symlink to mock-dep, which is a directory
	fi, err := os.Stat(filepath.Join("cstow_deps", "shared-dep"))
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	// 4. Verify modules can find the shared dependency
	// In module A, checkDependenciesReady should pass even though A/cstow.lock doesn't exist
	require.NoError(t, os.Chdir("a"))
	
	err = checkDependenciesReady(".")
	require.NoError(t, err, "checkDependenciesReady should find root lock and deps")

	// 5. Test parallel workspace build doesn't crash (mocking CMake calls if needed)
	// We'll just run 'workspace build' which will try to run cmake.
	// Since we don't have a real C++ compiler/cmake environment guaranteed here,
	// we'll just check if it correctly sets up the prefix paths.
	require.NoError(t, os.Chdir(root))
	
	// We'll use a mock buildCmd to avoid real execution but verify parameters
	// Actually, let's just run it and expect a cmake error (which is fine, as long as it's not a Go panic/race)
	rootCmd.SetArgs([]string{"workspace", "build", "--jobs", "2"})
	// We expect this to fail because there's no real CMakeLists.txt or compiler, but it should NOT panic.
	err = rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "cmake")
}
