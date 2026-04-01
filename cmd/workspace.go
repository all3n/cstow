package cmd

import (
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/workspace"
	"github.com/spf13/cobra"
)

// runBuildInDir runs the build command logic in the given working directory.
// This avoids os.Chdir which is not goroutine-safe.
func runBuildInDir(cmd *cobra.Command, workDir string) error {
	origDir, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("chdir to %s: %w", workDir, err)
	}
	defer os.Chdir(origDir)
	return buildCmd.RunE(cmd, []string{})
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

		fmt.Printf("Workspace: %s\n", ws.Root)
		fmt.Printf("Members (%d):\n", len(ws.Members))
		for _, m := range ws.Members {
			fmt.Printf("  - %s\n", m)
		}
		return nil
	},
}

var workspaceBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build all workspace members",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		ws, err := workspace.Load(dir)
		if err != nil {
			return err
		}

		profile, _ := cmd.Flags().GetString("profile")
		fmt.Printf(">> building workspace (%d members, profile: %s)\n", len(ws.Members), profile)

		for _, member := range ws.Members {
			fmt.Printf("\n>> building %s\n", member)
			// Change to member dir and run build
			err := runBuildInDir(cmd, member)
			if err != nil {
				return fmt.Errorf("build %s failed: %w", member, err)
			}
		}

		fmt.Println(">> workspace build complete")
		return nil
	},
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceBuildCmd)
	workspaceBuildCmd.Flags().StringP("profile", "p", "debug", "build profile")
	rootCmd.AddCommand(workspaceCmd)
}
