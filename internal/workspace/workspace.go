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

// LoadModules loads each member's config and returns enriched Module entries.
func (w *Workspace) LoadModules() ([]*Module, error) {
	var modules []*Module
	for _, memberPath := range w.Members {
		cfgPath := filepath.Join(memberPath, "cstow.toml")
		if _, err := os.Stat(cfgPath); err != nil {
			continue
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", cfgPath, err)
		}
		modules = append(modules, &Module{
			Name: cfg.Package.Name,
			Path: memberPath,
			Cfg:  cfg,
		})
	}
	return modules, nil
}

// BuildOrder returns member paths in dependency order (dependencies first).
// Returns an error if a cycle is detected.
func (w *Workspace) BuildOrder() ([]string, error) {
	modules, err := w.LoadModules()
	if err != nil {
		return nil, err
	}
	return BuildGraph(modules)
}

// MergedDependencies returns the combined dependency list for a module,
// where the module's own declarations override root-level ones by name.
func (w *Workspace) MergedDependencies(moduleCfg *config.Config) []config.Dependency {
	seen := make(map[string]bool)
	var result []config.Dependency

	// Module deps first (higher priority)
	for _, dep := range moduleCfg.Dependencies {
		result = append(result, dep)
		seen[dep.Name] = true
	}

	// Root deps that are not overridden
	for _, dep := range w.Config.Dependencies {
		if !seen[dep.Name] {
			result = append(result, dep)
		}
	}

	return result
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
