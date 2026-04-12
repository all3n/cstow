package cmd

import (
	"fmt"
	"os"
	"slices"

	"github.com/all3n/cstow/internal/cmakegen"
	"github.com/all3n/cstow/internal/config"
	"github.com/spf13/cobra"
)

var genCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate CMakeLists.txt and CMakePresets.json from cstow.toml",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found in current directory")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		global, err := config.LoadGlobal()
		if err != nil {
			return fmt.Errorf("load global config: %w", err)
		}

		// Discover dependencies from cstow_deps/ if it exists.
		var deps []cmakegen.DepTarget
		entries, err := os.ReadDir("cstow_deps")
		if err == nil && len(entries) > 0 {
			deps, err = cmakegen.DiscoverDeps("cstow_deps")
			if err != nil {
				return fmt.Errorf("discover deps: %w", err)
			}
		}

		opts, err := generateOptionsFromConfig(cfg, global, deps)
		if err != nil {
			return err
		}

		genCMakeLists, _ := cmd.Flags().GetBool("cmakelists")
		genPresets, _ := cmd.Flags().GetBool("presets")
		force, _ := cmd.Flags().GetBool("force")

		if genCMakeLists {
			content := cmakegen.GenerateCMakeLists(opts)
			if err := writeFile("CMakeLists.txt", content, force); err != nil {
				return err
			}
			fmt.Println(">> generated CMakeLists.txt")
		}

		if genPresets {
			content, err := cmakegen.GeneratePresets(opts)
			if err != nil {
				return fmt.Errorf("generate presets: %w", err)
			}
			if err := writeFile("CMakePresets.json", content, force); err != nil {
				return err
			}
			fmt.Println(">> generated CMakePresets.json")
		}

		return nil
	},
}

func generateOptionsFromConfig(cfg *config.Config, global *config.Global, deps []cmakegen.DepTarget) (cmakegen.GenerateOptions, error) {
	if cfg == nil {
		return cmakegen.GenerateOptions{}, fmt.Errorf("config is required")
	}
	if cfg.Package.Name == "" {
		return cmakegen.GenerateOptions{}, fmt.Errorf("package.name is required in cstow.toml")
	}

	std := cfg.Package.Std
	if std == "" && global != nil && global.Defaults.Std != "" {
		std = global.Defaults.Std
	}
	if std == "" {
		std = "c++17"
	}

	buildType := cfg.Build.Type
	if buildType == "" {
		buildType = "executable"
	}

	toolchainCfg := cfg.Toolchain
	if toolchainCfg.Compiler == "" && global != nil && global.Toolchain.Prefer != "" {
		toolchainCfg.Compiler = global.Toolchain.Prefer
	}

	globalFlags := config.GlobalBuildFlags{}
	if global != nil {
		globalFlags = global.Build.Flags
	}

	defines := slices.Clone(globalFlags.Defines)
	defines = append(defines, cfg.Build.Defines...)
	defines = append(defines, cfg.Build.Flags.Defines...)

	cxxFlags := slices.Clone(globalFlags.CXXFlags)
	cxxFlags = append(cxxFlags, cfg.Build.Flags.CXXFlags...)

	linkFlags := slices.Clone(globalFlags.LinkFlags)
	linkFlags = append(linkFlags, cfg.Build.Flags.LinkFlags...)

	return cmakegen.GenerateOptions{
		Name:      cfg.Package.Name,
		Type:      buildType,
		Std:       std,
		Sources:   cfg.Build.Sources,
		Include:   cfg.Build.Include,
		Defines:   defines,
		CXXFlags:  cxxFlags,
		LinkFlags: linkFlags,
		Deps:      deps,
		Profiles:  cfg.Profiles,
		Toolchain: toolchainCfg,
	}, nil
}

func writeFile(path, content string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists — use --force to overwrite", path)
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func init() {
	genCmd.Flags().Bool("cmakelists", true, "generate CMakeLists.txt")
	genCmd.Flags().Bool("presets", true, "generate CMakePresets.json")
	genCmd.Flags().Bool("force", false, "overwrite existing files")
	rootCmd.AddCommand(genCmd)
}
