package workspace

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/all3n/cstow/internal/config"
)

// Module represents a workspace member with its identity and dependencies.
type Module struct {
	Name string         // package name from cstow.toml
	Path string         // absolute path to module directory
	Cfg  *config.Config // parsed module config
}

// Graph represents the dependency relationship between modules.
type Graph struct {
	Order   []string            // One valid topological order (dependencies first)
	Edges   map[string][]string // Forward edges: A depends on [B, C] -> Edges[A] = [B, C]
	Rev     map[string][]string // Reverse edges: A is depended on by [B, C] -> Rev[A] = [B, C]
	InDeg   map[string]int      // Number of dependencies for each module
	Modules map[string]*Module  // Lookup by module name
}

// BuildGraph performs a topological sort over modules based on inter-module
// dependencies. It returns module paths in build order (dependencies first).
// If a cycle is detected it returns an error containing the cycle path.
func BuildGraph(modules []*Module) ([]string, error) {
	g, err := ComputeGraph(modules)
	if err != nil {
		return nil, err
	}

	// Convert ordered names back to paths
	result := make([]string, 0, len(g.Order))
	for _, name := range g.Order {
		result = append(result, g.Modules[name].Path)
	}
	return result, nil
}

// ComputeGraph builds the dependency graph and returns it.
func ComputeGraph(modules []*Module) (*Graph, error) {
	// Build lookup maps
	pathToName := make(map[string]string) // abs module path -> module name
	nameToModule := make(map[string]*Module)
	for _, m := range modules {
		pathToName[m.Path] = m.Name
		nameToModule[m.Name] = m
	}

	// Build adjacency list and in-degree map.
	// edges[A] = [B, C] means A depends on B and C.
	edges := make(map[string][]string)
	rev := make(map[string][]string)
	inDeg := make(map[string]int)

	for _, m := range modules {
		inDeg[m.Name] = 0 // ensure all modules are in inDeg map
		for _, dep := range m.Cfg.Dependencies {
			if !dep.IsLocal() || dep.Path == "" {
				continue
			}
			// Resolve relative path from module directory
			absDepPath := filepath.Clean(filepath.Join(m.Path, dep.Path))
			targetName, ok := pathToName[absDepPath]
			if !ok {
				// Not a workspace module — skip (could be external local dep)
				continue
			}
			edges[m.Name] = append(edges[m.Name], targetName)
			rev[targetName] = append(rev[targetName], m.Name)
			inDeg[m.Name]++
		}
	}

	// DFS-based topological sort with cycle detection
	const (
		white = 0 // unvisited
		gray  = 1 // in progress
		black = 2 // done
	)

	color := make(map[string]int)
	parent := make(map[string]string)
	var order []string

	var dfs func(name string) error
	dfs = func(name string) error {
		color[name] = gray
		for _, neighbor := range edges[name] {
			switch color[neighbor] {
			case gray:
				// Cycle detected — reconstruct path
				return fmt.Errorf("dependency cycle detected: %s", reconstructCycle(parent, name, neighbor))
			case white:
				parent[neighbor] = name
				if err := dfs(neighbor); err != nil {
					return err
				}
			}
			// black: already processed, skip
		}
		color[name] = black
		order = append(order, name)
		return nil
	}

	// Sort module names for deterministic iteration order
	names := make([]string, 0, len(modules))
	for name := range nameToModule {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if color[name] == white {
			if err := dfs(name); err != nil {
				return nil, err
			}
		}
	}

	return &Graph{
		Order:   order,
		Edges:   edges,
		Rev:     rev,
		InDeg:   inDeg,
		Modules: nameToModule,
	}, nil
}

// reconstructCycle traces the DFS parent chain from `from` back to `to`.
func reconstructCycle(parent map[string]string, from, to string) string {
	var path []string
	cur := from
	for cur != to {
		path = append(path, cur)
		cur = parent[cur]
		if cur == "" {
			// Should not happen, but guard against infinite loop
			break
		}
	}
	path = append(path, to)

	// Reverse to get chronological order and append the start again
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	path = append(path, path[0])

	result := path[0]
	for _, p := range path[1:] {
		result += " -> " + p
	}
	return result
}
