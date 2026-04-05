package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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

		// Get build order (also detects cycles)
		ordered, err := ws.BuildOrder()
		if err != nil {
			return err
		}

		fmt.Printf(">> building workspace (%d modules, profile: %s)\n", len(ordered), profile)

		for i, modulePath := range ordered {
			fmt.Printf("\n>> [%d/%d] building %s\n", i+1, len(ordered), filepath.Base(modulePath))
			if err := runBuildInDir(modulePath, autoFetch); err != nil {
				return fmt.Errorf("build %s failed: %w", filepath.Base(modulePath), err)
			}
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

func init() {
	workspaceListCmd.Flags().Bool("graph", false, "show dependency-aware build order")
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceBuildCmd)
	workspaceCmd.AddCommand(workspaceCleanCmd)
	workspaceBuildCmd.Flags().StringP("profile", "p", "debug", "build profile")
	workspaceBuildCmd.Flags().Bool("fetch", false, "automatically fetch missing dependencies before building")
	rootCmd.AddCommand(workspaceCmd)
}
