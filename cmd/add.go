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

var addNewRepoFinder = func(extraPaths []string) (*repository.Finder, error) {
	paths := extraPaths
	if len(paths) == 0 {
		paths = findProjectRootPaths()
	}
	return repository.NewFinderWithPaths(paths), nil
}

func findProjectRootPaths() []string {
	root := findProjectRoot()
	global, _ := config.LoadGlobal()
	if global != nil {
		return global.RepositoryPaths(root)
	}
	return []string{root}
}

var addCmd = &cobra.Command{
	Use:   "add <package>[@<version>]",
	Short: "Add a dependency to the project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		defer resetAddFlagState(cmd)
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

		gitURL, _ := cmd.Flags().GetString("git-url")
		tag, _ := cmd.Flags().GetString("tag")
		cmakeDefines, _ := cmd.Flags().GetStringArray("cmake-define")
		cxxFlags, _ := cmd.Flags().GetString("cxx-flags")
		linkFlags, _ := cmd.Flags().GetString("link-flags")

		// Persistent flags
		extraRepos, _ := cmd.Flags().GetStringSlice("repository")
		overrideRegistry, _ := cmd.Flags().GetString("registry")

		if source == "git" {
			if gitURL == "" {
				return fmt.Errorf("--git-url is required when --source is git")
			}
			if tag == "" {
				tag = "main"
			}
			// If no explicit version provided, use tag as version
			if version == "*" || version == "" {
				version = tag
			}
		}

		// Validate dependency before writing
		if err := validateDependency(name, version, source, overrideRegistry, extraRepos); err != nil {
			return err
		}

		dep := config.Dependency{
			Name:      name,
			Version:   version,
			Source:    source,
			BuildType: buildType,
		}
		if source == "git" {
			dep.Git = gitURL
			dep.Rev = tag
			if len(cmakeDefines) > 0 || cxxFlags != "" || linkFlags != "" {
				dep.CMake = config.GitCMake{
					Defines:   cmakeDefines,
					CXXFlags:  strings.Fields(cxxFlags),
					LinkFlags: strings.Fields(linkFlags),
				}
			}
		}
		updated := resolver.AddDependency(cfg, dep)

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

		action := "Added"
		if updated {
			action = "Updated"
		}
		fmt.Printf("%s %s@%s (source: %s)\n", action, name, version, source)
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

func validateDependency(name, version, source, registryOverride string, repoOverrides []string) error {
	if source == "git" {
		return nil // git deps are validated by URL reachability, not registry/repo
	}
	ctx := context.Background()

	if source == "registry" {
		return validateRegistryDependency(ctx, name, version, registryOverride)
	}
	return validateRepoDependency(name, version, repoOverrides)
}

func validateRegistryDependency(ctx context.Context, name, version, registryOverride string) error {
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
		if registryOverride == "" {
			return fmt.Errorf("no registry configured: %w", err)
		}
		// Use manual override if no registry configured
		reg = config.Registry{URL: registryOverride}
	} else if registryOverride != "" {
		reg.URL = registryOverride
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

func validateRepoDependency(name, version string, repoOverrides []string) error {
	finder, err := addNewRepoFinder(repoOverrides)
	if err != nil {
		return fmt.Errorf("load repository config: %w", err)
	}

	_, err = finder.Find(name, version)
	if err != nil {
		return fmt.Errorf("package %q not found in repository: %w", name, err)
	}
	return nil
}

func resetAddFlagState(cmd *cobra.Command) {
	resetAddFlag := func(name string) {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			return
		}
		if replacer, ok := flag.Value.(interface{ Replace([]string) error }); ok {
			_ = replacer.Replace(nil)
		} else {
			_ = flag.Value.Set(flag.DefValue)
		}
		flag.Changed = false
	}
	resetAddFlag("source")
	resetAddFlag("build-type")
	resetAddFlag("git-url")
	resetAddFlag("tag")
	resetAddFlag("cmake-define")
	resetAddFlag("cxx-flags")
	resetAddFlag("link-flags")
}

func init() {
	addCmd.Flags().String("source", "registry", "dependency source (registry|local|git)")
	addCmd.Flags().String("build-type", "", "dependency build type (static|shared|header-only)")
	addCmd.Flags().String("git-url", "", "git repository URL (required when --source is git)")
	addCmd.Flags().String("tag", "", "git tag or branch to checkout (default: main)")
	addCmd.Flags().StringArray("cmake-define", nil, "cmake define KEY=VAL (repeatable)")
	addCmd.Flags().String("cxx-flags", "", "additional C++ compiler flags")
	addCmd.Flags().String("link-flags", "", "additional linker flags")
	rootCmd.AddCommand(addCmd)
}
