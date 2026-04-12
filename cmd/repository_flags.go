package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/repository"
	"github.com/spf13/cobra"
)

func effectiveRepositoryPaths(projectRoot string, extraPaths []string) ([]string, error) {
	global, err := config.LoadGlobal()
	if err != nil {
		return nil, fmt.Errorf("load global config: %w", err)
	}
	basePaths, err := configuredRepositoryPaths(global, projectRoot)
	if err != nil {
		return nil, err
	}
	return mergeRepositoryPaths(basePaths, extraPaths), nil
}

func mergeRepositoryPaths(basePaths, extraPaths []string) []string {
	seen := make(map[string]struct{}, len(basePaths)+len(extraPaths))
	out := make([]string, 0, len(basePaths)+len(extraPaths))
	addPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = expandUserPath(path)
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}

	for _, path := range extraPaths {
		addPath(path)
	}
	for _, path := range basePaths {
		addPath(path)
	}
	return out
}

func expandUserPath(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func configuredRepositoryPaths(global *config.Global, projectRoot string) ([]string, error) {
	if global == nil {
		global = &config.Global{}
	}

	paths := make([]string, 0, len(global.Repositories)+2)
	if projectRoot != "" {
		projectRepo := filepath.Join(projectRoot, ".cstow", "repository")
		if fi, err := os.Stat(projectRepo); err == nil && fi.IsDir() {
			paths = append(paths, projectRepo)
		}
	}

	for _, repo := range sortedRepositorySources(global.Repositories) {
		path, err := resolveConfiguredRepositoryPath(global, repo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}

	stateDir, err := config.ResolveStateDir(global)
	if err != nil {
		return nil, err
	}
	homeRepo := filepath.Join(stateDir, "repository")
	paths = append(paths, homeRepo)
	return paths, nil
}

func sortedRepositorySources(repos []config.RepoSource) []config.RepoSource {
	sorted := make([]config.RepoSource, len(repos))
	copy(sorted, repos)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi := sorted[i].Priority
		pj := sorted[j].Priority
		if pi == 0 {
			pi = 50
		}
		if pj == 0 {
			pj = 50
		}
		return pi < pj
	})
	return sorted
}

func resolveConfiguredRepositoryPath(global *config.Global, repo config.RepoSource) (string, error) {
	if strings.TrimSpace(repo.Path) != "" {
		return expandUserPath(strings.TrimSpace(repo.Path)), nil
	}
	if strings.TrimSpace(repo.Git) != "" {
		stateDir, err := config.ResolveStateDir(global)
		if err != nil {
			return "", err
		}
		destDir := filepath.Join(stateDir, "repositories", managedRepositoryName(repo))
		if err := repository.SyncGitRepositoryWithOptions(repo.Git, repo.Branch, destDir, repo.AutoUpdate, repository.FetchOptions{
			Network: globalNetworkConfig(global),
		}); err != nil {
			return "", fmt.Errorf("sync repository source %q: %w", repo.Git, err)
		}
		return destDir, nil
	}
	if strings.TrimSpace(repo.Archive) != "" {
		stateDir, err := config.ResolveStateDir(global)
		if err != nil {
			return "", err
		}
		destDir := filepath.Join(stateDir, "repositories", managedRepositoryName(repo))
		if err := repository.SyncArchiveRepositoryWithOptions(repo.Archive, destDir, repo.AutoUpdate, repository.FetchOptions{
			Network: globalNetworkConfig(global),
		}); err != nil {
			return "", fmt.Errorf("sync repository source %q: %w", repo.Archive, err)
		}
		return destDir, nil
	}
	return "", nil
}

func managedRepositoryName(repo config.RepoSource) string {
	base := repo.Name
	if strings.TrimSpace(base) == "" {
		base = strings.TrimSuffix(filepath.Base(strings.TrimSpace(repo.Git)), ".git")
	}
	if strings.TrimSpace(base) == "" {
		base = strings.TrimSuffix(filepath.Base(strings.TrimSpace(repo.Archive)), ".tar.gz")
		base = strings.TrimSuffix(base, ".tgz")
		base = strings.TrimSuffix(base, ".zip")
		base = strings.TrimSuffix(base, ".tar")
	}
	if strings.TrimSpace(base) == "" {
		base = "repository"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(strings.ToLower(b.String()), "-")
}

func resetRootFlagState(cmd *cobra.Command) {
	resetFlagState(cmd, "profile")
	resetFlagState(cmd, "repository")
	resetFlagState(cmd, "registry")
}

func resetFlagState(cmd *cobra.Command, name string) {
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		return
	}
	if replacer, ok := flag.Value.(interface{ Replace([]string) error }); ok {
		_ = replacer.Replace(nil)
	} else {
		_ = flag.Value.Set(flag.DefValue)
	}
	flag.Changed = false
}
