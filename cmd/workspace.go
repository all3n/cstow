package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/all3n/cstow/internal/cmakegen"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/workspace"
	"github.com/spf13/cobra"
)

// runBuildInDir changes to workDir and executes the build command.
func runBuildInDir(workDir string, autoFetch bool) error {
	origDir, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("chdir to %s: %w", workDir, err)
	}
	defer os.Chdir(origDir)

	// Propagate autoFetch to buildCmd flags
	if autoFetch {
		buildCmd.Flags().Set("fetch", "true")
	}

	return buildCmd.RunE(buildCmd, []string{})
}

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage multi-package workspace",
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
				fmt.Printf("  - %s\n", m)
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

		scheduler := workspace.NewScheduler(g, jobs)
		var (
			mu      sync.Mutex
			count   int
			total   = len(modules)
		)

		task := func(ctx context.Context, m *workspace.Module) error {
			mu.Lock()
			count++
			current := count
			mu.Unlock()

			fmt.Printf("\n>> [%d/%d] building %s\n", current, total, m.Name)
			if err := runBuildInDir(m.Path, autoFetch); err != nil {
				return fmt.Errorf("build %s failed: %w", m.Name, err)
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

func init() {
	workspaceListCmd.Flags().Bool("graph", false, "show dependency-aware build order")
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceBuildCmd)
	workspaceCmd.AddCommand(workspaceCleanCmd)
	workspaceBuildCmd.Flags().StringP("profile", "p", "debug", "build profile")
	workspaceBuildCmd.Flags().Bool("fetch", false, "automatically fetch missing dependencies before building")
	workspaceBuildCmd.Flags().IntP("jobs", "j", 1, "number of parallel build jobs")
	workspaceGenCmd.Flags().Bool("cmakelists", true, "generate CMakeLists.txt")
	workspaceGenCmd.Flags().Bool("presets", true, "generate CMakePresets.json")
	workspaceGenCmd.Flags().Bool("force", false, "overwrite existing files")
	workspaceCmd.AddCommand(workspaceGenCmd)
	rootCmd.AddCommand(workspaceCmd)
}
