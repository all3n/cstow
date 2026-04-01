package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/all3n/cstow/internal/abi"
	"github.com/all3n/cstow/internal/builder"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/all3n/cstow/internal/toolchain"
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

		if !builder.IsCmakeInstalled() {
			return fmt.Errorf("cmake not found on PATH — install cmake first")
		}

		name, versionConstraint := parsePackageSpec(args[0])

		// 1. Load global config and find package definition
		g, err := config.LoadGlobal()
		if err != nil {
			return fmt.Errorf("load global config: %w", err)
		}

		finder := repository.NewFinderWithPaths(g.RepositoryPaths())
		pkg, err := finder.Find(name, versionConstraint)
		if err != nil {
			return err
		}

		fmt.Printf(">> found %s %s in %s\n", name, pkg.Version, pkg.RepoPath)

		// 2. Detect toolchain + ABI
		tc, err := toolchain.Detect(&config.Toolchain{
			Compiler: g.Toolchain.Prefer,
		})
		if err != nil {
			return fmt.Errorf("detect toolchain: %w", err)
		}

		abiTag := abi.DetectFromToolchain(tc.Kind, tc.Version, g.Defaults.Std)
		fmt.Printf(">> toolchain: %s %d.%d.%d | abi: %s\n",
			tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2], abiTag.String())

		// 3. Compute cache path and check if already installed
		cache := resolver.NewFSCache()
		installDir := cache.Path(name, pkg.Version, abiTag.String())

		if !force {
			if cache.Has(name, pkg.Version, abiTag.String()) {
				fmt.Printf(">> already installed: %s\n", installDir)
				return nil
			}
		}

		// 4. Merge build config
		merged := repository.Merge(pkg.Def, pkg.Override, tc.Kind, profile, runtime.GOOS)

		// CLI --type overrides the package-defined build type
		if buildType != "" {
			merged.BuildType = buildType
		}
		if merged.BuildType == "" {
			merged.BuildType = "static" // default
		}

		// 5. Fetch source
		fmt.Printf(">> fetching source: %s\n", pkg.Def.Source.URL)
		tmpDir, err := os.MkdirTemp("", "cstow-build-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		sourcePath, err := repository.FetchSource(pkg.Def.Source, pkg.Version, tmpDir)
		if err != nil {
			return fmt.Errorf("fetch source: %w", err)
		}

		// 6. Build
		fmt.Printf(">> building %s %s (%s)\n", name, pkg.Version, profile)
		result, err := builder.Build(builder.Options{
			SourcePath: sourcePath,
			InstallDir: installDir,
			Config:     merged,
			Toolchain:  tc,
			Profile:    profile,
			Jobs:       builder.GuessJobs(),
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		})
		if err != nil {
			return fmt.Errorf("build failed: %w", err)
		}

		fmt.Printf(">> installed %s %s → %s\n", name, pkg.Version, result.InstallDir)
		return nil
	},
}

func init() {
	installCmd.Flags().StringP("profile", "p", "debug", "build profile (debug|release)")
	installCmd.Flags().Bool("force", false, "rebuild even if already cached")
	installCmd.Flags().String("type", "", "build type: static|shared|header-only (overrides package default)")
	rootCmd.AddCommand(installCmd)
}
