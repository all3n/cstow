package cmd

import (
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install <package>[@<version>]",
	Short: "Build a package from source and install to local cache",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profile, _ := cmd.Flags().GetString("profile")
		force, _ := cmd.Flags().GetBool("force")
		buildType, _ := cmd.Flags().GetString("type")
		toolchainName, _ := cmd.Flags().GetString("toolchain")

		name, versionConstraint := parsePackageSpec(args[0])
		var projectCfg *config.Config
		if _, err := os.Stat("cstow.toml"); err == nil {
			var loadErr error
			projectCfg, loadErr = config.Load("cstow.toml")
			if loadErr != nil {
				return loadErr
			}
		}

		var lockFile *resolver.LockFile
		if _, err := os.Stat("cstow.lock"); err == nil {
			lockFile, _ = resolver.LoadLock("cstow.lock")
		}

		// Check if this package has a git source in cstow.toml
		var gitURL, gitRev string
		var gitCMake config.GitCMake
		if projectCfg != nil {
			for _, dep := range projectCfg.Dependencies {
				if dep.Name == name && dep.Source == "git" {
					gitURL = dep.Git
					gitRev = dep.Rev
					gitCMake = dep.CMake
					break
				}
			}
		}

		if buildType == "" {
			lockEntry, _ := lockEntryByName(lockFile, name)
			buildType = configuredBuildType(name, lockEntry, projectCfg)
		}
		if buildType != "" {
			if err := validateBuildType(buildType); err != nil {
				return err
			}
		}

		ctx, err := newRepositoryInstallContext(projectCfg, profile, toolchainName)
		if err != nil {
			return err
		}

		fmt.Printf(">> toolchain: %s %d.%d.%d | abi: %s\n",
			ctx.Toolchain.Kind, ctx.Toolchain.Version[0], ctx.Toolchain.Version[1], ctx.Toolchain.Version[2], ctx.detectedABITag())

		if gitURL != "" {
			effectiveVersion := versionConstraint
			if effectiveVersion == "*" || effectiveVersion == "" {
				effectiveVersion = gitRev
			}
			result, err := installFromGitSource(name, effectiveVersion, gitURL, gitRev, gitSourceOptions{
				Context:   ctx,
				BuildType: buildType,
				CMake:     gitCMake,
			})
			if err != nil {
				return err
			}
			if result.Cached {
				fmt.Printf(">> already installed: %s\n", result.InstallDir)
				return nil
			}
			fmt.Printf(">> installed %s %s → %s\n", name, result.Version, result.InstallDir)
			return nil
		}

		result, err := installFromRepository(name, versionConstraint, repositoryInstallOptions{
			Context:   ctx,
			BuildType: buildType,
			Force:     force,
		})
		if err != nil {
			return err
		}
		if result.Cached {
			fmt.Printf(">> already installed: %s\n", result.InstallDir)
			return nil
		}

		fmt.Printf(">> installed %s %s → %s\n", name, result.Version, result.InstallDir)
		return nil
	},
}

func init() {
	installCmd.Flags().StringP("profile", "p", "debug", "build profile (debug|release)")
	installCmd.Flags().Bool("force", false, "rebuild even if already cached")
	installCmd.Flags().String("type", "", "build type: static|shared|header-only (overrides package default)")
	installCmd.Flags().String("toolchain", "auto", "compiler to use when building from source (auto|gcc|clang|msvc)")
	rootCmd.AddCommand(installCmd)
}
