package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/all3n/cstow/internal/repository"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search packages across all configured repositories",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		defer resetRootFlagState(cmd)
		query := ""
		if len(args) > 0 {
			query = args[0]
		}

		extraRepos, _ := cmd.Flags().GetStringSlice("repository")
		repoPaths, err := effectiveRepositoryPaths(findProjectRoot(), extraRepos)
		if err != nil {
			return err
		}
		finder := repository.NewFinderWithPaths(repoPaths)

		results, err := finder.Search(query)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			if query != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "no packages matching %q\n", query)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "no packages found in any repository")
			}
			return nil
		}

		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tVERSION\tDESCRIPTION\tREPO")
		for _, r := range results {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Version, r.Description, shortRepoPath(r.RepoPath))
		}
		tw.Flush()
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d packages found\n", len(results))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

// shortRepoPath replaces $HOME with ~ for display.
func shortRepoPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if len(p) >= len(home) && p[:len(home)] == home {
		return "~" + p[len(home):]
	}
	return p
}
