package cmd

import (
	"fmt"

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
