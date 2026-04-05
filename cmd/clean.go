package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/all3n/cstow/internal/workspace"
	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove build artifacts (build/ and cstow_deps/)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cache, _ := cmd.Flags().GetBool("cache")
		all, _ := cmd.Flags().GetBool("all")
		cleanLock, _ := cmd.Flags().GetBool("lock")
		dir, _ := os.Getwd()

		removeLock := cleanLock || all

		// Check if inside a workspace
		ws, wsErr := workspace.Load(dir)
		if wsErr == nil && len(ws.Members) > 0 {
			ordered, err := ws.BuildOrder()
			if err != nil {
				return err
			}
			for _, modulePath := range ordered {
				cleanProjectDir(modulePath, removeLock)
			}
			cleanProjectDir(ws.Root, removeLock)
		} else {
			cleanProjectDir(dir, removeLock)
		}

		if cache || all {
			cleanCache()
		}

		return nil
	},
}

func cleanProjectDir(dir string, removeLock bool) {
	targets := []string{
		filepath.Join(dir, "build"),
		filepath.Join(dir, "cstow_deps"),
	}
	if removeLock {
		targets = append(targets, filepath.Join(dir, "cstow.lock"))
	}
	for _, target := range targets {
		if _, err := os.Stat(target); err == nil {
			if err := os.RemoveAll(target); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to remove %s: %v\n", target, err)
			} else {
				fmt.Printf("  removed %s\n", target)
			}
		}
	}
}

func cleanCache() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine home dir: %v\n", err)
		return
	}
	cacheDir := filepath.Join(home, ".cstow", "cache")
	dbPath := filepath.Join(home, ".cstow", "cstow.db")

	if v := os.Getenv("CSTOW_CACHE_DIR"); v != "" {
		cacheDir = v
	}

	if _, err := os.Stat(cacheDir); err == nil {
		if err := os.RemoveAll(cacheDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove cache: %v\n", err)
		} else {
			fmt.Printf("  removed %s\n", cacheDir)
		}
	}

	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Remove(dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove artifact db: %v\n", err)
		} else {
			fmt.Printf("  removed %s\n", dbPath)
		}
	}
}

func init() {
	cleanCmd.Flags().Bool("lock", false, "also remove cstow.lock")
	cleanCmd.Flags().Bool("cache", false, "also purge global cache (~/.cstow/cache)")
	cleanCmd.Flags().Bool("all", false, "remove everything (lock + cache)")
	rootCmd.AddCommand(cleanCmd)
}
