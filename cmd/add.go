package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <package>[@<version>]",
	Short: "Add a dependency to the project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found in current directory")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		name, version := parsePackageSpec(args[0])
		source, _ := cmd.Flags().GetString("source")
		if source == "" {
			source = "registry"
		}

		resolver.AddDependency(cfg, name, version, source)

		// Resolve first — only persist if resolution succeeds
		cache := resolver.NewFSCache()
		r := resolver.New(cache, nil)
		lf, err := r.Resolve(cfg.Dependencies)
		if err != nil {
			return fmt.Errorf("resolve dependencies: %w", err)
		}

		if err := cfg.Save(cfgPath); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		if err := resolver.SaveLock("cstow.lock", lf); err != nil {
			return fmt.Errorf("save lock file: %w", err)
		}

		fmt.Printf("Added %s@%s (source: %s)\n", name, version, source)
		return nil
	},
}

func parsePackageSpec(spec string) (name, version string) {
	parts := strings.SplitN(spec, "@", 2)
	name = parts[0]
	if len(parts) == 2 {
		version = parts[1]
	} else {
		version = "*"
	}
	return
}

func init() {
	addCmd.Flags().String("source", "registry", "dependency source (registry|local|git)")
	rootCmd.AddCommand(addCmd)
}
