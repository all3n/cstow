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
