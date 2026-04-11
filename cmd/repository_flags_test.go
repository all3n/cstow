package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeRepositoryPathsPrependsExtrasAndDeduplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths := mergeRepositoryPaths(
		[]string{filepath.Join(home, ".cstow", "repository"), "/opt/global"},
		[]string{"~/custom", "/opt/global", "  "},
	)

	require.Len(t, paths, 3)
	assert.Equal(t, filepath.Join(home, "custom"), paths[0])
	assert.Equal(t, "/opt/global", paths[1])
	assert.Equal(t, filepath.Join(home, ".cstow", "repository"), paths[2])
}

func TestRepositoryInstallContextRepositoryPathsSupplementsConfiguredRepos(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx := &repositoryInstallContext{
		Global: &config.Global{
			Repositories: []config.RepoSource{
				{Name: "global", Path: "/opt/global", Priority: 10},
			},
		},
		ExtraRepos: []string{"~/override"},
	}

	paths := ctx.repositoryPaths("")
	require.Len(t, paths, 3)
	assert.Equal(t, filepath.Join(home, "override"), paths[0])
	assert.Equal(t, "/opt/global", paths[1])
	assert.Equal(t, filepath.Join(home, ".cstow", "repository"), paths[2])
}

func TestExpandUserPathLeavesNonHomePathsUntouched(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, cwd, expandUserPath(cwd))
}
