package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
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

	paths, err := ctx.repositoryPaths("")
	require.NoError(t, err)
	require.Len(t, paths, 3)
	assert.Equal(t, filepath.Join(home, "override"), paths[0])
	assert.Equal(t, "/opt/global", paths[1])
	assert.Equal(t, filepath.Join(home, ".cstow", "repository"), paths[2])
}

func TestConfiguredRepositoryPathsClonesManagedGitRepositorySource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	repoDir := t.TempDir()
	pkgDir := filepath.Join(repoDir, "f", "fmt")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["1.0.0"]
[package]
name = "fmt"
`), 0o644))
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "branch", "-M", "main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "cstow test")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	paths, err := configuredRepositoryPaths(&config.Global{
		Repositories: []config.RepoSource{
			{Name: "team", Git: repoDir, Priority: 10},
		},
	}, "")
	require.NoError(t, err)
	require.Len(t, paths, 2)
	assert.FileExists(t, filepath.Join(paths[0], "f", "fmt", "package.toml"))
}

func TestConfiguredRepositoryPathsAutoUpdateRefreshesManagedGitRepository(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	repoDir := t.TempDir()
	pkgDir := filepath.Join(repoDir, "f", "fmt")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["1.0.0"]
[package]
name = "fmt"
`), 0o644))
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "branch", "-M", "main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "cstow test")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	global := &config.Global{
		Repositories: []config.RepoSource{
			{Name: "team", Git: repoDir, Priority: 10},
		},
	}
	paths, err := configuredRepositoryPaths(global, "")
	require.NoError(t, err)
	require.Len(t, paths, 2)

	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(`
versions = ["2.0.0"]
[package]
name = "fmt"
`), 0o644))
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "update")

	// auto_update=false keeps the original clone untouched
	paths, err = configuredRepositoryPaths(global, "")
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(paths[0], "f", "fmt", "package.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `["1.0.0"]`)

	global.Repositories[0].AutoUpdate = true
	paths, err = configuredRepositoryPaths(global, "")
	require.NoError(t, err)
	data, err = os.ReadFile(filepath.Join(paths[0], "f", "fmt", "package.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `["2.0.0"]`)
}

func TestConfiguredRepositoryPathsDownloadsManagedArchiveRepositorySource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(repositoryArchiveBytes(t, "1.0.0"))
	}))
	defer ts.Close()

	paths, err := configuredRepositoryPaths(&config.Global{
		Repositories: []config.RepoSource{
			{Name: "team-archive", Archive: ts.URL + "/repo.tar.gz", Priority: 10},
		},
	}, "")
	require.NoError(t, err)
	require.Len(t, paths, 2)
	assert.FileExists(t, filepath.Join(paths[0], "f", "fmt", "package.toml"))
}

func TestConfiguredRepositoryPathsAutoUpdateRefreshesManagedArchiveRepository(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	var archiveData []byte
	archiveData = repositoryArchiveBytes(t, "1.0.0")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer ts.Close()

	global := &config.Global{
		Repositories: []config.RepoSource{
			{Name: "team-archive", Archive: ts.URL + "/repo.tar.gz", Priority: 10},
		},
	}
	paths, err := configuredRepositoryPaths(global, "")
	require.NoError(t, err)
	require.Len(t, paths, 2)

	archiveData = repositoryArchiveBytes(t, "2.0.0")

	paths, err = configuredRepositoryPaths(global, "")
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(paths[0], "f", "fmt", "package.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `["1.0.0"]`)

	global.Repositories[0].AutoUpdate = true
	paths, err = configuredRepositoryPaths(global, "")
	require.NoError(t, err)
	data, err = os.ReadFile(filepath.Join(paths[0], "f", "fmt", "package.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `["2.0.0"]`)
}

func repositoryArchiveBytes(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := "versions = [\"" + version + "\"]\n[package]\nname = \"fmt\"\n"
	files := map[string]string{
		"repo-root/f/fmt/package.toml": content,
	}
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0644, Size: int64(len(body))}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func TestExpandUserPathLeavesNonHomePathsUntouched(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, cwd, expandUserPath(cwd))
}
