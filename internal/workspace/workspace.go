package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	searchDir, _ := filepath.Abs(dir)
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

// RootLockPath returns the path to cstow.lock at the workspace root.
func (w *Workspace) RootLockPath() string {
	return filepath.Join(w.Root, "cstow.lock")
}

// RootDepsDir returns the path to cstow_deps directory at the workspace root.
func (w *Workspace) RootDepsDir() string {
	return filepath.Join(w.Root, "cstow_deps")
}

// AllDependencies returns a merged list of all unique dependencies in the workspace.
// This is used to resolve and fetch all dependencies into the root at once.
func (w *Workspace) AllDependencies() ([]config.Dependency, error) {
	seen := make(map[string]config.Dependency)

	// Collect from root
	for _, dep := range w.Config.Dependencies {
		if dep.Source == "local" && dep.Path != "" && !filepath.IsAbs(dep.Path) {
			dep.Path = filepath.Join(w.Root, dep.Path)
		}
		seen[dep.Name] = dep
	}

	// Collect from each module
	modules, err := w.LoadModules()
	if err != nil {
		return nil, fmt.Errorf("load modules for all-deps: %w", err)
	}

	for _, m := range modules {
		for _, dep := range m.Cfg.Dependencies {
			if dep.Source == "local" && dep.Path != "" && !filepath.IsAbs(dep.Path) {
				dep.Path = filepath.Join(m.Path, dep.Path)
			}
			// In a workspace, module-level dependencies take precedence
			// if they have the same name.
			seen[dep.Name] = dep
		}
	}

	// Convert map to slice
	var deps []config.Dependency
	for _, d := range seen {
		deps = append(deps, d)
	}

	// Sort for determinism
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
	})

	return deps, nil
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
