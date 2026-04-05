package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadWorkspace(t *testing.T) {
	root := t.TempDir()

	// Create workspace root cstow.toml
	wsCfg := &config.Config{
		Package: config.Package{Name: "workspace-root", Version: "0.1.0"},
		Workspace: &config.Workspace{
			Members: []string{"engine", "renderer", "tools/*"},
		},
	}
	require.NoError(t, wsCfg.Save(filepath.Join(root, "cstow.toml")))

	// Create member projects
	for _, name := range []string{"engine", "renderer"} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		cfg := &config.Config{
			Package: config.Package{Name: name, Version: "0.1.0"},
		}
		require.NoError(t, cfg.Save(filepath.Join(dir, "cstow.toml")))
	}

	// Create tools sub-members
	toolsDir := filepath.Join(root, "tools", "codegen")
	require.NoError(t, os.MkdirAll(toolsDir, 0o755))
	toolCfg := &config.Config{
		Package: config.Package{Name: "codegen", Version: "0.1.0"},
	}
	require.NoError(t, toolCfg.Save(filepath.Join(toolsDir, "cstow.toml")))

	// Test loading from workspace root
	ws, err := Load(root)
	require.NoError(t, err)
	assert.Equal(t, root, ws.Root)

	pkgs, err := ws.MemberPackages()
	require.NoError(t, err)
	assert.Equal(t, 3, len(pkgs))
}

func TestLoadNoWorkspace(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	assert.Error(t, err)
}

func TestExpandMembersGlob(t *testing.T) {
	root := t.TempDir()

	// Create dirs
	for _, name := range []string{"a", "b", "c"} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		// Only a and b have cstow.toml
		if name != "c" {
			cfg := &config.Config{Package: config.Package{Name: name}}
			require.NoError(t, cfg.Save(filepath.Join(dir, "cstow.toml")))
		}
	}

	members, err := expandMembers(root, []string{"*"})
	require.NoError(t, err)
	// Only a and b should be included (c has no cstow.toml)
	assert.Equal(t, 2, len(members))
}

func TestAllDependencies(t *testing.T) {
	root := t.TempDir()

	// Root config with some deps
	wsCfg := &config.Config{
		Package: config.Package{Name: "root"},
		Workspace: &config.Workspace{
			Members: []string{"a", "b"},
		},
		Dependencies: []config.Dependency{
			{Name: "dep1", Version: "1.0.0"},
			{Name: "shared", Version: "1.0.0"},
		},
	}
	require.NoError(t, wsCfg.Save(filepath.Join(root, "cstow.toml")))

	// Module A config overriding 'shared'
	dirA := filepath.Join(root, "a")
	require.NoError(t, os.MkdirAll(dirA, 0o755))
	cfgA := &config.Config{
		Package: config.Package{Name: "a"},
		Dependencies: []config.Dependency{
			{Name: "shared", Version: "2.0.0"},
			{Name: "depA", Version: "0.1.0"},
		},
	}
	require.NoError(t, cfgA.Save(filepath.Join(dirA, "cstow.toml")))

	// Module B config
	dirB := filepath.Join(root, "b")
	require.NoError(t, os.MkdirAll(dirB, 0o755))
	cfgB := &config.Config{
		Package: config.Package{Name: "b"},
		Dependencies: []config.Dependency{
			{Name: "depB", Version: "0.1.0"},
		},
	}
	require.NoError(t, cfgB.Save(filepath.Join(dirB, "cstow.toml")))

	ws, err := Load(root)
	require.NoError(t, err)

	deps, err := ws.AllDependencies()
	require.NoError(t, err)

	// dep1 (root), shared (v2.0.0 from A), depA, depB
	assert.Equal(t, 4, len(deps))

	foundShared := false
	for _, d := range deps {
		if d.Name == "shared" {
			assert.Equal(t, "2.0.0", d.Version)
			foundShared = true
		}
	}
	assert.True(t, foundShared)

	assert.Equal(t, filepath.Join(root, "cstow.lock"), ws.RootLockPath())
	assert.Equal(t, filepath.Join(root, "cstow_deps"), ws.RootDepsDir())
}
