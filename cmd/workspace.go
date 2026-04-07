package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/all3n/cstow/internal/cmakegen"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/workspace"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage multi-package workspace",
}

var workspaceInitCmd = &cobra.Command{
	Use:   "init [name]",
	Short: "Initialize a new workspace in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		name := "workspace"
		if len(args) > 0 {
			name = args[0]
		}

		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err == nil {
			return fmt.Errorf("cstow.toml already exists")
		}

		cfg := &config.Config{
			Package: config.Package{
				Name:    name,
				Version: "0.1.0",
			},
			Workspace: &config.Workspace{
				Members: []string{},
			},
		}

		if err := cfg.Save(cfgPath); err != nil {
			return err
		}

		fmt.Printf("Created workspace %s in %s\n", name, cfgPath)
		return nil
	},
}

var workspaceAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a member to the workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		dir, _ := os.Getwd()
		cfgPath := filepath.Join(dir, "cstow.toml")
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load workspace config: %w", err)
		}

		if cfg.Workspace == nil {
			return fmt.Errorf("cstow.toml is not a workspace (missing [workspace] section)")
		}

		// Check if member already exists
		relPath, err := filepath.Rel(dir, absPath)
		if err != nil {
			return err
		}

		for _, m := range cfg.Workspace.Members {
			if m == relPath {
				fmt.Printf("Member %s already exists in workspace\n", relPath)
				return nil
			}
		}

		// Verify member has cstow.toml
		memberCfgPath := filepath.Join(absPath, "cstow.toml")
		if _, err := os.Stat(memberCfgPath); err != nil {
			return fmt.Errorf("member %s has no cstow.toml", relPath)
		}

		cfg.Workspace.Members = append(cfg.Workspace.Members, relPath)
		if err := cfg.Save(cfgPath); err != nil {
			return err
		}

		fmt.Printf("Added member %s to workspace\n", relPath)
		return nil
	},
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspace members",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		ws, err := workspace.Load(dir)
		if err != nil {
			return err
		}

		showGraph, _ := cmd.Flags().GetBool("graph")

		fmt.Printf("Workspace: %s\n", ws.Root)

		if showGraph {
			order, err := ws.BuildOrder()
			if err != nil {
				return err
			}
			fmt.Printf("Build order (%d modules):\n", len(order))
			for i, p := range order {
				fmt.Printf("  %d. %s\n", i+1, filepath.Base(p))
			}
		} else {
			fmt.Printf("Members (%d):\n", len(ws.Members))
			for _, m := range ws.Members {
				rel, err := filepath.Rel(ws.Root, m)
				if err != nil {
					rel = m // fallback to whatever is stored
				}
				fmt.Printf("  - %s\n", rel)
			}
		}
		return nil
	},
}

var workspaceBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build all workspace members in dependency order",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		ws, err := workspace.Load(dir)
		if err != nil {
			return err
		}

		profile, _ := cmd.Flags().GetString("profile")
		toolchainName, _ := cmd.Flags().GetString("toolchain")
		autoFetch, _ := cmd.Flags().GetBool("fetch")
		jobs, _ := cmd.Flags().GetInt("jobs")

		modules, err := ws.LoadModules()
		if err != nil {
			return err
		}

		g, err := workspace.ComputeGraph(modules)
		if err != nil {
			return err
		}

		fmt.Printf(">> building workspace (%d modules, profile: %s, jobs: %d)\n", len(modules), profile, jobs)

		if autoFetch {
			fmt.Println(">> fetching all workspace dependencies...")
			deps, err := ws.AllDependencies()
			if err != nil {
				return fmt.Errorf("gather all dependencies: %w", err)
			}
			if len(deps) > 0 {
				// Create a virtual config representing all deps in workspace root
				cfg := &config.Config{
					Dependencies: deps,
					Registries:   ws.Config.Registries,
				}
				fetchErr := runFetch(cfg, fetchOptions{
					LockPath:       ws.RootLockPath(),
					DepsDir:        ws.RootDepsDir(),
					Profile:        profile,
					Toolchain:      toolchainName,
					SourceFallback: true,
					Stdout:         cmd.OutOrStdout(),
					Stderr:         cmd.ErrOrStderr(),
				})
				if fetchErr != nil {
					return fmt.Errorf("workspace auto-fetch failed: %w", fetchErr)
				}
			}
		}

		scheduler := workspace.NewScheduler(g, jobs)
		var (
			mu    sync.Mutex
			count int
			total = len(modules)
		)

		task := func(ctx context.Context, m *workspace.Module) error {
			mu.Lock()
			count++
			current := count
			mu.Unlock()

			var outBuf bytes.Buffer
			var stdout, stderr io.Writer

			if jobs > 1 {
				// Buffer output for parallel jobs
				stdout = &outBuf
				stderr = &outBuf
				fmt.Printf(">> [%d/%d] building %s...\n", current, total, m.Name)
			} else {
				// Direct output for sequential jobs
				stdout = cmd.OutOrStdout()
				stderr = cmd.ErrOrStderr()
				fmt.Printf("\n>> [%d/%d] building %s\n", current, total, m.Name)
			}

			// Pass autoFetch=false to individual builds because we've already fetched everything
			err := runBuild(m.Path, profile, toolchainName, false, stdout, stderr)
			if err != nil {
				if jobs > 1 {
					// Show the buffered output on failure so the user knows what happened
					fmt.Printf("\n!! build failed for %s\n", m.Name)
					fmt.Println(outBuf.String())
				}
				return fmt.Errorf("build %s failed: %w", m.Name, err)
			}

			if jobs > 1 {
				fmt.Printf(">> [%d/%d] %s complete\n", current, total, m.Name)
			}
			return nil
		}

		if err := scheduler.Run(cmd.Context(), task); err != nil {
			return err
		}

		fmt.Println("\n>> workspace build complete")
		return nil
	},
}

var workspaceCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean all workspace member build artifacts",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		ws, err := workspace.Load(dir)
		if err != nil {
			return err
		}

		ordered, err := ws.BuildOrder()
		if err != nil {
			return err
		}

		fmt.Printf(">> cleaning workspace (%d modules)\n", len(ordered))
		for _, modulePath := range ordered {
			fmt.Printf("  cleaning %s\n", filepath.Base(modulePath))
			cleanProjectDir(modulePath, false)
		}
		return nil
	},
}

var workspaceGenCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate CMake files for all workspace members",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		ws, err := workspace.Load(dir)
		if err != nil {
			return err
		}

		genCMakeLists, _ := cmd.Flags().GetBool("cmakelists")
		genPresets, _ := cmd.Flags().GetBool("presets")
		force, _ := cmd.Flags().GetBool("force")

		order, err := ws.BuildOrder()
		if err != nil {
			return err
		}

		fmt.Printf(">> generating CMake files for %d workspace members\n", len(order))
		for _, memberPath := range order {
			cfgPath := filepath.Join(memberPath, "cstow.toml")
			if _, err := os.Stat(cfgPath); err != nil {
				fmt.Printf("  skipping %s (no cstow.toml)\n", filepath.Base(memberPath))
				continue
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load %s: %w", cfgPath, err)
			}

			// Discover deps from member's cstow_deps/
			var deps []cmakegen.DepTarget
			depsDir := filepath.Join(memberPath, "cstow_deps")
			if entries, derr := os.ReadDir(depsDir); derr == nil && len(entries) > 0 {
				deps, _ = cmakegen.DiscoverDeps(depsDir)
			}

			buildType := cfg.Build.Type
			if buildType == "" {
				buildType = "executable"
			}

			opts := cmakegen.GenerateOptions{
				Name:      cfg.Package.Name,
				Type:      buildType,
				Std:       cfg.Package.Std,
				Sources:   cfg.Build.Sources,
				Include:   cfg.Build.Include,
				Defines:   cfg.Build.Defines,
				Deps:      deps,
				Profiles:  cfg.Profiles,
				Toolchain: cfg.Toolchain,
			}

			if genCMakeLists {
				content := cmakegen.GenerateCMakeLists(opts)
				target := filepath.Join(memberPath, "CMakeLists.txt")
				if err := writeFile(target, content, force); err != nil {
					fmt.Printf("  skip %s: %s\n", filepath.Base(memberPath), err)
				} else {
					fmt.Printf("  generated CMakeLists.txt for %s\n", cfg.Package.Name)
				}
			}

			if genPresets {
				content, gerr := cmakegen.GeneratePresets(opts)
				if gerr != nil {
					return fmt.Errorf("generate presets for %s: %w", cfg.Package.Name, gerr)
				}
				target := filepath.Join(memberPath, "CMakePresets.json")
				if err := writeFile(target, content, force); err != nil {
					fmt.Printf("  skip %s: %s\n", filepath.Base(memberPath), err)
				} else {
					fmt.Printf("  generated CMakePresets.json for %s\n", cfg.Package.Name)
				}
			}
		}

		fmt.Println(">> workspace gen complete")
		return nil
	},
}

var workspaceFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch all workspace dependencies into root cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		ws, err := workspace.Load(dir)
		if err != nil {
			return err
		}

		profile, _ := cmd.Flags().GetString("profile")
		toolchainName, _ := cmd.Flags().GetString("toolchain")
		sourceFallback, _ := cmd.Flags().GetBool("source-fallback")

		deps, err := ws.AllDependencies()
		if err != nil {
			return err
		}

		if len(deps) == 0 {
			fmt.Println("No dependencies in workspace")
			return nil
		}

		fmt.Printf(">> fetching workspace dependencies (%d total)\n", len(deps))

		// Create a virtual config representing all deps in workspace root
		cfg := &config.Config{
			Dependencies: deps,
			Registries:   ws.Config.Registries,
		}

		return runFetch(cfg, fetchOptions{
			LockPath:       ws.RootLockPath(),
			DepsDir:        ws.RootDepsDir(),
			Profile:        profile,
			Toolchain:      toolchainName,
			SourceFallback: sourceFallback,
			Stdout:         cmd.OutOrStdout(),
			Stderr:         cmd.ErrOrStderr(),
		})
	},
}

func init() {
	workspaceListCmd.Flags().Bool("graph", false, "show dependency-aware build order")
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceBuildCmd)
	workspaceCmd.AddCommand(workspaceCleanCmd)
	workspaceCmd.AddCommand(workspaceFetchCmd)
	workspaceCmd.AddCommand(workspaceInitCmd)
	workspaceCmd.AddCommand(workspaceAddCmd)

	workspaceBuildCmd.Flags().StringP("profile", "p", "debug", "build profile")
	workspaceBuildCmd.Flags().String("toolchain", "auto", "toolchain selection")
	workspaceBuildCmd.Flags().Bool("fetch", false, "automatically fetch missing dependencies before building")
	workspaceBuildCmd.Flags().IntP("jobs", "j", 1, "number of parallel build jobs")

	workspaceFetchCmd.Flags().StringP("profile", "p", "debug", "build profile")
	workspaceFetchCmd.Flags().String("toolchain", "auto", "toolchain selection")
	workspaceFetchCmd.Flags().Bool("source-fallback", true, "allow source builds")

	workspaceGenCmd.Flags().Bool("cmakelists", true, "generate CMakeLists.txt")
	workspaceGenCmd.Flags().Bool("presets", true, "generate CMakePresets.json")
	workspaceGenCmd.Flags().Bool("force", false, "overwrite existing files")
	workspaceCmd.AddCommand(workspaceGenCmd)
	rootCmd.AddCommand(workspaceCmd)
}
