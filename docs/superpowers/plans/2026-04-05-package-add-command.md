# `cstow package add` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `cstow package add` command to scaffold new package recipes in a repository.

**Architecture:**
- **Command Layer**: `cmd/package.go` defines the `package` root command and `add` subcommand.
- **Config Layer**: `internal/config/config.go` provides helpers to resolve repository paths by name or project default.
- **Repository Layer**: `internal/repository/scaffold.go` implements the core logic for creating directory structures and `package.toml`.

**Tech Stack:** Go, Cobra (CLI), TOML (BurntSushi/toml).

---

### Task 1: Repository Path Resolution in Config

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Add `RepoPathByName` and `DefaultRepoPath` to `Global` struct**

```go
// RepoPathByName returns the expanded path of a repository named 'name' in global config.
func (g *Global) RepoPathByName(name string) (string, bool) {
	home, _ := os.UserHomeDir()
	for _, r := range g.Repositories {
		if r.Name == name {
			p := r.Path
			if len(p) >= 2 && p[:2] == "~/" {
				p = filepath.Join(home, p[2:])
			}
			return p, true
		}
	}
	// Also check built-in "home" repo if name matches
	if name == "home" || name == "default" {
		return filepath.Join(home, ".cstow", "repository"), true
	}
	return "", false
}

// DefaultRepoPath returns the path to the current project's .cstow/repository.
func DefaultRepoPath() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, ".cstow", "repository")
}
```

- [ ] **Step 2: Add unit tests in `internal/config/config_test.go`**

```go
func TestGlobal_RepoPathByName(t *testing.T) {
	g := &Global{
		Repositories: []RepoSource{
			{Name: "local", Path: "/tmp/repo"},
		},
	}
	path, ok := g.RepoPathByName("local")
	if !ok || path != "/tmp/repo" {
		t.Errorf("expected /tmp/repo, got %s", path)
	}
	_, ok = g.RepoPathByName("missing")
	if ok {
		t.Error("expected not found")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add repo path resolution helpers"
```

---

### Task 2: Package Scaffolding Logic

**Files:**
- Create: `internal/repository/scaffold.go`
- Test: `internal/repository/scaffold_test.go`

- [ ] **Step 1: Implement `ScaffoldPackage` function**

```go
package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func ScaffoldPackage(repoDir, pkgName string) error {
	letter := indexLetter(pkgName)
	pkgDir := filepath.Join(repoDir, letter, pkgName)
	pkgFile := filepath.Join(pkgDir, "package.toml")

	if _, err := os.Stat(pkgFile); err == nil {
		return fmt.Errorf("package %s already exists in %s", pkgName, repoDir)
	}

	if err := os.MkdirAll(filepath.Join(pkgDir, "versions"), 0o755); err != nil {
		return fmt.Errorf("create package dirs: %w", err)
	}

	def := PackageDef{
		Package: PackageMeta{
			Name: pkgName,
		},
		Versions: []string{"0.1.0"},
		Source: SourceDef{
			Type:        "git",
			TagTemplate: "{version}",
		},
		Build: BuildDef{
			System: "cmake",
			Type:   "static",
		},
		Artifacts: ArtifactsDef{
			IncludeDirs: []string{"include"},
		},
	}

	f, err := os.Create(pkgFile)
	if err != nil {
		return fmt.Errorf("create package.toml: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(def); err != nil {
		return fmt.Errorf("write package.toml: %w", err)
	}

	return nil
}
```

- [ ] **Step 2: Add unit tests in `internal/repository/scaffold_test.go`**

```go
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
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/repository/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/repository/scaffold.go internal/repository/scaffold_test.go
git commit -m "feat(repository): add package scaffolding logic"
```

---

### Task 3: `cstow package add` Command

**Files:**
- Create: `cmd/package.go`
- Test: `cmd/package_test.go`

- [ ] **Step 1: Implement `cmd/package.go`**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/repository"
	"github.com/spf13/cobra"
)

var packageCmd = &cobra.Command{
	Use:   "package",
	Short: "Manage repository package recipes",
}

var packageAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new package recipe skeleton",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		repoDir, _ := cmd.Flags().GetString("repo_dir")
		repoName, _ := cmd.Flags().GetString("repo_name")

		var targetDir string
		if repoDir != "" {
			targetDir = repoDir
		} else if repoName != "" {
			g, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			var ok bool
			targetDir, ok = g.RepoPathByName(repoName)
			if !ok {
				return fmt.Errorf("repository %q not found in config", repoName)
			}
		} else {
			targetDir = config.DefaultRepoPath()
		}

		if err := repository.ScaffoldPackage(targetDir, name); err != nil {
			return err
		}

		fmt.Printf("Added package %s to %s\n", name, targetDir)
		return nil
	},
}

func init() {
	packageAddCmd.Flags().String("repo_dir", "", "Direct path to target repository")
	packageAddCmd.Flags().String("repo_name", "", "Repository name from global config")
	packageCmd.AddCommand(packageAddCmd)
	rootCmd.AddCommand(packageCmd)
}
```

- [ ] **Step 2: Add integration tests in `cmd/package_test.go`**

```go
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
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./cmd/... ./internal/...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/package.go cmd/package_test.go
git commit -m "feat(cli): add cstow package add command"
```
