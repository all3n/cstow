package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageAddCommand(t *testing.T) {
	tmp := t.TempDir()
	
	// Use --repo_dir flag to test
	rootCmd.SetArgs([]string{"package", "add", "testpkg", "--repo_dir", tmp})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	pkgFile := filepath.Join(tmp, "t", "testpkg", "package.toml")
	if _, err := os.Stat(pkgFile); err != nil {
		t.Errorf("package.toml not created via CLI: %v", err)
	}

	// Verify it can be found by Repository Finder
	// (Check if it follows the folder structure)
	letterDir := filepath.Join(tmp, "t")
	if fi, err := os.Stat(letterDir); err != nil || !fi.IsDir() {
		t.Errorf("letter directory 't' not created: %v", err)
	}
}
