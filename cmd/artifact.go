package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

var artifactCmd = &cobra.Command{
	Use:   "artifact",
	Short: "Inspect and repair the local artifact index",
}

var artifactListCmd = &cobra.Command{
	Use:   "list",
	Short: "List indexed local artifacts",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifactdb.OpenDefault()
		if err != nil {
			return err
		}
		defer store.Close()

		rows, err := store.List()
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No indexed artifacts found.")
			fmt.Fprintln(cmd.OutOrStdout(), "Run `cstow artifact sync` to scan the local cache.")
			return nil
		}

		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tVERSION\tABI\tTYPE\tORIGIN\tPATH\tUPDATED")
		for _, row := range rows {
			buildType := row.BuildType
			if buildType == "" {
				buildType = "default"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Name, row.Version, row.ABITag, buildType, row.Origin, row.InstallDir, row.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
		return tw.Flush()
	},
}

var artifactSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Rebuild the local artifact index from the cache directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifactdb.OpenDefault()
		if err != nil {
			return err
		}
		defer store.Close()

		cache := resolver.NewFSCache()
		stats, err := store.SyncFromCache(cache.Root)
		if err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "artifact sync complete")
		fmt.Fprintf(cmd.OutOrStdout(), "inserted: %d\n", stats.Inserted)
		fmt.Fprintf(cmd.OutOrStdout(), "updated: %d\n", stats.Updated)
		fmt.Fprintf(cmd.OutOrStdout(), "deleted: %d\n", stats.Deleted)
		return nil
	},
}

var artifactShowCmd = &cobra.Command{
	Use:   "show <hashid>",
	Short: "Show an indexed artifact by hash_id or unique hash prefix",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifactdb.OpenDefault()
		if err != nil {
			return err
		}
		defer store.Close()

		rec, err := store.FindByHashID(args[0])
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "name: %s\n", rec.Name)
		fmt.Fprintf(cmd.OutOrStdout(), "version: %s\n", rec.Version)
		fmt.Fprintf(cmd.OutOrStdout(), "abi: %s\n", rec.ABITag)
		fmt.Fprintf(cmd.OutOrStdout(), "build_type: %s\n", displayBuildType(rec.BuildType))
		fmt.Fprintf(cmd.OutOrStdout(), "hash_id: %s\n", rec.HashID)
		fmt.Fprintf(cmd.OutOrStdout(), "build_tags: %s\n", strings.Join(rec.BuildTags, ","))
		fmt.Fprintf(cmd.OutOrStdout(), "origin: %s\n", rec.Origin)
		fmt.Fprintf(cmd.OutOrStdout(), "path: %s\n", rec.InstallDir)
		return nil
	},
}

func init() {
	artifactCmd.AddCommand(artifactListCmd)
	artifactCmd.AddCommand(artifactSyncCmd)
	artifactCmd.AddCommand(artifactShowCmd)
	rootCmd.AddCommand(artifactCmd)
}
