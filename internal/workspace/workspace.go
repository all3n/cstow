package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/all3n/cstow/internal/config"
)

// Workspace represents a multi-package workspace
type Workspace struct {
	Root    string
	Members []string // expanded member paths
	Config  *config.Config
}

// Load discovers and loads a workspace from the given directory.
// Walks up the directory tree to find a cstow.toml with [workspace] section.
func Load(dir string) (*Workspace, error) {
	searchDir := dir
	for {
		cfgPath := filepath.Join(searchDir, "cstow.toml")
		if _, err := os.Stat(cfgPath); err == nil {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", cfgPath, err)
			}
			if cfg.Workspace != nil && len(cfg.Workspace.Members) > 0 {
				members, err := expandMembers(searchDir, cfg.Workspace.Members)
				if err != nil {
					return nil, err
				}
				return &Workspace{
					Root:    searchDir,
					Members: members,
					Config:  cfg,
				}, nil
			}
		}
		parent := filepath.Dir(searchDir)
		if parent == searchDir {
			break
		}
		searchDir = parent
	}

	return nil, fmt.Errorf("no workspace found (no cstow.toml with [workspace] section)")
}

// MemberPackages returns the cstow.toml config for each member
func (w *Workspace) MemberPackages() ([]*config.Config, error) {
	var pkgs []*config.Config
	for _, member := range w.Members {
		cfgPath := filepath.Join(member, "cstow.toml")
		if _, err := os.Stat(cfgPath); err != nil {
			continue
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", cfgPath, err)
		}
		pkgs = append(pkgs, cfg)
	}
	return pkgs, nil
}

// expandMembers expands glob patterns in member list
func expandMembers(root string, patterns []string) ([]string, error) {
	var members []string
	for _, pattern := range patterns {
		if strings.Contains(pattern, "*") {
			matches, err := filepath.Glob(filepath.Join(root, pattern))
			if err != nil {
				return nil, fmt.Errorf("glob %s: %w", pattern, err)
			}
			for _, m := range matches {
				if _, err := os.Stat(filepath.Join(m, "cstow.toml")); err == nil {
					members = append(members, m)
				}
			}
		} else {
			members = append(members, filepath.Join(root, pattern))
		}
	}
	return members, nil
}
