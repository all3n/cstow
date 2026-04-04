package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

type addRegistryValidator interface {
	GetManifest(ctx context.Context, pkg, version string) (*registry.Manifest, error)
	ListVersions(ctx context.Context, pkg string) ([]string, error)
}

var addNewRegistryValidator = func(ctx context.Context, reg config.Registry) (addRegistryValidator, error) {
	return registry.NewS3Client(ctx, reg)
}

var addNewRepoFinder = func() (*repository.Finder, error) {
	return repository.NewFinder()
}

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
		buildType, _ := cmd.Flags().GetString("build-type")
		if buildType != "" {
			if err := validateBuildType(buildType); err != nil {
				return err
			}
		}

		// Validate dependency before writing
		if err := validateDependency(name, version, source); err != nil {
			return err
		}

		resolver.AddDependency(cfg, name, version, source, buildType)

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

func validateDependency(name, version, source string) error {
	ctx := context.Background()

	if source == "registry" {
		return validateRegistryDependency(ctx, name, version)
	}
	return validateRepoDependency(name, version)
}

func validateRegistryDependency(ctx context.Context, name, version string) error {
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}

	// Load project config to get project-level registries
	cfg, err := config.Load("cstow.toml")
	if err != nil {
		return fmt.Errorf("load project config: %w", err)
	}

	reg, err := config.ResolvePrimaryRegistry(cfg.Registries, globalCfg)
	if err != nil {
		return fmt.Errorf("no registry configured: %w", err)
	}

	client, err := addNewRegistryValidator(ctx, reg)
	if err != nil {
		return fmt.Errorf("connect to registry: %w", err)
	}

	// For specific versions, check manifest directly
	if version != "*" && version != "" {
		_, err := client.GetManifest(ctx, name, version)
		if err != nil {
			return fmt.Errorf("package %s@%s not found in registry: %w", name, version, err)
		}
		return nil
	}

	// For wildcard, just check that the package exists
	versions, err := client.ListVersions(ctx, name)
	if err != nil {
		return fmt.Errorf("package %q not found in registry: %w", name, err)
	}
	if len(versions) == 0 {
		return fmt.Errorf("package %q has no versions in registry", name)
	}
	return nil
}

func validateRepoDependency(name, version string) error {
	finder, err := addNewRepoFinder()
	if err != nil {
		return fmt.Errorf("load repository config: %w", err)
	}

	_, err = finder.Find(name, version)
	if err != nil {
		return fmt.Errorf("package %q not found in repository: %w", name, err)
	}
	return nil
}

func init() {
	addCmd.Flags().String("source", "registry", "dependency source (registry|local|git)")
	addCmd.Flags().String("build-type", "", "dependency build type (static|shared|header-only)")
	rootCmd.AddCommand(addCmd)
}
