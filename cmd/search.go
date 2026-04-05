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
		query := ""
		if len(args) > 0 {
			query = args[0]
		}

		finder, err := repository.NewFinder(findProjectRoot())
		if err != nil {
			return err
		}

		results, err := finder.Search(query)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			if query != "" {
				fmt.Printf("no packages matching %q\n", query)
			} else {
				fmt.Println("no packages found in any repository")
			}
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tVERSION\tDESCRIPTION\tREPO")
		for _, r := range results {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Version, r.Description, shortRepoPath(r.RepoPath))
		}
		tw.Flush()
		fmt.Printf("\n%d packages found\n", len(results))
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
