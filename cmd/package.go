package cmd

import (
	"fmt"
	"path/filepath"
	"unicode"

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
		targetDir, err := resolvePackageRepoTarget(cmd)
		if err != nil {
			return err
		}

		if err := repository.ScaffoldPackage(targetDir, name); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Added package %s to %s\n", name, targetDir)
		return nil
	},
}

var packageLintCmd = &cobra.Command{
	Use:   "lint [name]",
	Short: "Lint a package recipe",
	Args:  cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir, err := resolvePackageRepoTarget(cmd)
		if err != nil {
			return err
		}

		lintAll, _ := cmd.Flags().GetBool("all")
		var results []*repository.LintResult
		if lintAll {
			results, err = repository.LintRepositoryDir(targetDir)
			if err != nil {
				return err
			}
		} else {
			if len(args) != 1 {
				return fmt.Errorf("package name is required unless --all is set")
			}
			name := args[0]
			pkgDir := filepath.Join(targetDir, string(repositoryNameIndex(name)), name)
			result, err := repository.LintPackageDir(pkgDir)
			if err != nil {
				return err
			}
			results = []*repository.LintResult{result}
		}

		hasErrors := false
		for _, result := range results {
			if result.OK() {
				fmt.Fprintf(cmd.OutOrStdout(), "package lint OK: %s (%s)\n", result.Name, result.PackageDir)
			} else {
				hasErrors = true
				fmt.Fprintf(cmd.OutOrStdout(), "package lint FAILED: %s (%s)\n", result.Name, result.PackageDir)
			}
			for _, issue := range result.Errors {
				fmt.Fprintf(cmd.OutOrStdout(), "  - error: %s\n", issue)
			}
			for _, issue := range result.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "  - warning: %s\n", issue)
			}
		}

		if !hasErrors {
			return nil
		}
		return fmt.Errorf("package lint failed")
	},
}

func resolvePackageRepoTarget(cmd *cobra.Command) (string, error) {
	repoDir, _ := cmd.Flags().GetString("repo_dir")
	repoName, _ := cmd.Flags().GetString("repo_name")

	if repoDir != "" {
		return repoDir, nil
	}
	if repoName != "" {
		g, err := config.LoadGlobal()
		if err != nil {
			return "", err
		}
		targetDir, ok := g.RepoPathByName(repoName)
		if !ok {
			return "", fmt.Errorf("repository %q not found in config", repoName)
		}
		return targetDir, nil
	}
	return config.DefaultRepoPath(), nil
}

func repositoryNameIndex(name string) byte {
	if name == "" {
		return '_'
	}
	r := []rune(name)[0]
	if !unicode.IsLetter(r) {
		return '_'
	}
	return byte(unicode.ToLower(r))
}

func init() {
	packageAddCmd.Flags().String("repo_dir", "", "Direct path to target repository")
	packageAddCmd.Flags().String("repo_name", "", "Repository name from global config")
	packageCmd.AddCommand(packageAddCmd)
	packageLintCmd.Flags().String("repo_dir", "", "Direct path to target repository")
	packageLintCmd.Flags().String("repo_name", "", "Repository name from global config")
	packageLintCmd.Flags().Bool("all", false, "lint every package recipe under the target repository")
	packageCmd.AddCommand(packageLintCmd)
	rootCmd.AddCommand(packageCmd)
}
