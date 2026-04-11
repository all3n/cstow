package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchIncludesRepositoryOverridePaths(t *testing.T) {
	home := t.TempDir()
	overrideRepo := filepath.Join(home, "override-repo")
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	writeRepositoryDefinition(t, overrideRepo, "overridepkg", []string{"1.2.3"})

	var stdout bytes.Buffer
	prevOut := rootCmd.OutOrStdout()
	prevErr := rootCmd.ErrOrStderr()
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	rootCmd.SetArgs([]string{"search", "overridepkg", "--repository", overrideRepo})
	t.Cleanup(func() {
		rootCmd.SetOut(prevOut)
		rootCmd.SetErr(prevErr)
		rootCmd.SetArgs(nil)
	})

	require.NoError(t, rootCmd.Execute())
	assert.Contains(t, stdout.String(), "overridepkg")
	assert.Contains(t, stdout.String(), shortRepoPath(overrideRepo))
}

func TestSearchRepositoryOverrideSupplementsConfiguredRepositories(t *testing.T) {
	home := t.TempDir()
	globalRepo := filepath.Join(home, "global-repo")
	overrideRepo := filepath.Join(home, "override-repo")
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	writeRepositoryDefinition(t, globalRepo, "globalpkg", []string{"1.0.0"})
	require.NoError(t, os.MkdirAll(overrideRepo, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[[repositories]]
name = "global"
path = "`+globalRepo+`"
priority = 10
`), 0o644))

	var stdout bytes.Buffer
	prevOut := rootCmd.OutOrStdout()
	prevErr := rootCmd.ErrOrStderr()
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	rootCmd.SetArgs([]string{"search", "globalpkg", "--repository", overrideRepo})
	t.Cleanup(func() {
		rootCmd.SetOut(prevOut)
		rootCmd.SetErr(prevErr)
		rootCmd.SetArgs(nil)
	})

	require.NoError(t, rootCmd.Execute())
	assert.Contains(t, stdout.String(), "globalpkg")
	assert.Contains(t, stdout.String(), shortRepoPath(globalRepo))
}
