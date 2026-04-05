package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGraph_LinearChain(t *testing.T) {
	root := t.TempDir()

	// C (no deps) -> B (depends on C) -> A (depends on B)
	modules := createTestModules(root, map[string][]string{
		"a": {"../b"},
		"b": {"../c"},
		"c": {},
	})

	order, err := BuildGraph(modules)
	require.NoError(t, err)

	names := pathsToNames(order)
	// C must come before B, B must come before A
	assert.True(t, index(names, "c") < index(names, "b"), "c should build before b")
	assert.True(t, index(names, "b") < index(names, "a"), "b should build before a")
}

func TestBuildGraph_Diamond(t *testing.T) {
	root := t.TempDir()

	// A depends on B and C, both B and C depend on D
	modules := createTestModules(root, map[string][]string{
		"a": {"../b", "../c"},
		"b": {"../d"},
		"c": {"../d"},
		"d": {},
	})

	order, err := BuildGraph(modules)
	require.NoError(t, err)

	names := pathsToNames(order)
	// D must come before B and C, both must come before A
	assert.True(t, index(names, "d") < index(names, "b"))
	assert.True(t, index(names, "d") < index(names, "c"))
	assert.True(t, index(names, "b") < index(names, "a"))
	assert.True(t, index(names, "c") < index(names, "a"))
}

func TestBuildGraph_NoDeps(t *testing.T) {
	root := t.TempDir()

	modules := createTestModules(root, map[string][]string{
		"x": {},
		"y": {},
		"z": {},
	})

	order, err := BuildGraph(modules)
	require.NoError(t, err)
	assert.Len(t, order, 3)
}

func TestBuildGraph_SingleModule(t *testing.T) {
	root := t.TempDir()

	modules := createTestModules(root, map[string][]string{
		"only": {},
	})

	order, err := BuildGraph(modules)
	require.NoError(t, err)
	assert.Len(t, order, 1)
	assert.Contains(t, order[0], "only")
}

func TestBuildGraph_CycleSimple(t *testing.T) {
	root := t.TempDir()

	modules := createTestModules(root, map[string][]string{
		"a": {"../b"},
		"b": {"../a"},
	})

	_, err := BuildGraph(modules)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestBuildGraph_CycleThreeWay(t *testing.T) {
	root := t.TempDir()

	modules := createTestModules(root, map[string][]string{
		"a": {"../b"},
		"b": {"../c"},
		"c": {"../a"},
	})

	_, err := BuildGraph(modules)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestBuildGraph_CycleTransitive(t *testing.T) {
	root := t.TempDir()

	// A -> B -> C -> B (cycle between B and C, A is outside)
	modules := createTestModules(root, map[string][]string{
		"a": {"../b"},
		"b": {"../c"},
		"c": {"../b"},
	})

	_, err := BuildGraph(modules)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestBuildGraph_NonModuleLocalDepIgnored(t *testing.T) {
	root := t.TempDir()

	// "a" has a local dep pointing outside the workspace — should be ignored
	aDir := filepath.Join(root, "a")
	require.NoError(t, os.MkdirAll(aDir, 0o755))
	aCfg := &config.Config{
		Package: config.Package{Name: "a"},
		Dependencies: []config.Dependency{
			{Name: "external", Source: "local", Path: "/some/external/path"},
		},
	}
	require.NoError(t, aCfg.Save(filepath.Join(aDir, "cstow.toml")))

	bDir := filepath.Join(root, "b")
	require.NoError(t, os.MkdirAll(bDir, 0o755))
	bCfg := &config.Config{Package: config.Package{Name: "b"}}
	require.NoError(t, bCfg.Save(filepath.Join(bDir, "cstow.toml")))

	modules := []*Module{
		{Name: "a", Path: aDir, Cfg: aCfg},
		{Name: "b", Path: bDir, Cfg: bCfg},
	}

	order, err := BuildGraph(modules)
	require.NoError(t, err)
	assert.Len(t, order, 2)
}

// helpers

func createTestModules(root string, deps map[string][]string) []*Module {
	var modules []*Module
	for name, paths := range deps {
		dir := filepath.Join(root, name)
		os.MkdirAll(dir, 0o755)

		var depList []config.Dependency
		for _, p := range paths {
			depList = append(depList, config.Dependency{
				Name: filepath.Base(p), Source: "local", Path: p,
			})
		}
		cfg := &config.Config{
			Package:      config.Package{Name: name},
			Dependencies: depList,
		}
		cfg.Save(filepath.Join(dir, "cstow.toml"))
		modules = append(modules, &Module{Name: name, Path: dir, Cfg: cfg})
	}
	return modules
}

func pathsToNames(paths []string) []string {
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return names
}

func index(slice []string, val string) int {
	for i, v := range slice {
		if v == val {
			return i
		}
	}
	return -1
}
