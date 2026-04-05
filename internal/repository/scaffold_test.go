package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldPackage(t *testing.T) {
	tmp := t.TempDir()
	err := ScaffoldPackage(tmp, "mylib")
	if err != nil {
		t.Fatal(err)
	}

	pkgFile := filepath.Join(tmp, "m", "mylib", "package.toml")
	if _, err := os.Stat(pkgFile); err != nil {
		t.Errorf("package.toml not created: %v", err)
	}

	versDir := filepath.Join(tmp, "m", "mylib", "versions")
	if _, err := os.Stat(versDir); err != nil {
		t.Errorf("versions directory not created: %v", err)
	}

	// Verify content
	def, err := loadPackageDef(pkgFile)
	if err != nil {
		t.Fatal(err)
	}
	if def.Package.Name != "mylib" {
		t.Errorf("expected package name mylib, got %s", def.Package.Name)
	}
	if def.Build.System != "cmake" {
		t.Errorf("expected build system cmake, got %s", def.Build.System)
	}

	// Test "already exists" error
	err = ScaffoldPackage(tmp, "mylib")
	if err == nil {
		t.Error("expected error when package already exists, got nil")
	}
}
