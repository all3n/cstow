package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writePackageToml creates a minimal package.toml in the fake repository.
func writePackageToml(t *testing.T, root, name string, versions []string, extra string) {
	t.Helper()
	letter := string([]rune(name)[0:1])
	dir := filepath.Join(root, letter, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	content := "versions = ["
	for i, v := range versions {
		if i > 0 {
			content += ", "
		}
		content += "\"" + v + "\""
	}
	content += "]\n\n[package]\nname = \"" + name + "\"\n" + extra
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.toml"), []byte(content), 0o644))
}

// writeVersionToml creates a versions/<ver>.toml override file.
func writeVersionToml(t *testing.T, root, name, version, content string) {
	t.Helper()
	letter := string([]rune(name)[0:1])
	dir := filepath.Join(root, letter, name, "versions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, version+".toml"), []byte(content), 0o644))
}

func TestFinder_Found(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "fmt", []string{"10.2.1", "10.1.0"}, "")

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("fmt", "^10.0.0")
	require.NoError(t, err)
	assert.Equal(t, "10.2.1", pkg.Version)
	assert.Equal(t, root, pkg.RepoPath)
	assert.Nil(t, pkg.Override)
}

func TestFinder_ExactVersion(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "fmt", []string{"10.2.1"}, "")

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("fmt", "10.2.1")
	require.NoError(t, err)
	assert.Equal(t, "10.2.1", pkg.Version)
}

func TestFinder_VersionOverrideLoaded(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "googletest", []string{"1.14.0", "1.13.0"}, "")
	writeVersionToml(t, root, "googletest", "1.14.0", `patch = "1.14.0-fix.patch"`)

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("googletest", "^1.14")
	require.NoError(t, err)
	assert.Equal(t, "1.14.0", pkg.Version)
	require.NotNil(t, pkg.Override)
	assert.Equal(t, "1.14.0-fix.patch", pkg.Override.Patch)
}

func TestFinder_NoVersionOverride(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "spdlog", []string{"1.13.0"}, "")
	// no versions/ dir

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("spdlog", "*")
	require.NoError(t, err)
	assert.Nil(t, pkg.Override)
}

func TestFinder_NotFound(t *testing.T) {
	root := t.TempDir()

	f := NewFinderWithPaths([]string{root})
	_, err := f.Find("nonexistent", "^1.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "not found in any repository")
}

func TestFinder_NoMatchingVersion(t *testing.T) {
	root := t.TempDir()
	writePackageToml(t, root, "fmt", []string{"9.1.0"}, "")

	f := NewFinderWithPaths([]string{root})
	_, err := f.Find("fmt", "^10.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fmt")
}

func TestFinder_NonLetterPackage(t *testing.T) {
	root := t.TempDir()
	// package name starting with digit → goes under "_"
	letter := "_"
	dir := filepath.Join(root, letter, "7zip")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "versions = [\"22.0.0\"]\n\n[package]\nname = \"7zip\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.toml"), []byte(content), 0o644))

	f := NewFinderWithPaths([]string{root})
	pkg, err := f.Find("7zip", "22.0.0")
	require.NoError(t, err)
	assert.Equal(t, "22.0.0", pkg.Version)
}

func TestFinder_MultipleRoots_FirstWins(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	writePackageToml(t, root1, "fmt", []string{"11.0.0"}, "")
	writePackageToml(t, root2, "fmt", []string{"10.2.1"}, "")

	f := NewFinderWithPaths([]string{root1, root2})
	pkg, err := f.Find("fmt", ">=10.0.0")
	require.NoError(t, err)
	assert.Equal(t, "11.0.0", pkg.Version)
	assert.Equal(t, root1, pkg.RepoPath)
}
