package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/all3n/cstow/internal/config"
	"github.com/spf13/cobra"
)

func effectiveRepositoryPaths(projectRoot string, extraPaths []string) ([]string, error) {
	global, err := config.LoadGlobal()
	if err != nil {
		return nil, fmt.Errorf("load global config: %w", err)
	}
	return mergeRepositoryPaths(global.RepositoryPaths(projectRoot), extraPaths), nil
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
