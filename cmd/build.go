package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/all3n/cstow/internal/abi"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/hooks"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		defer resetBuildFlagState(cmd)
		profile, _ := cmd.Flags().GetString("profile")
		toolchainName, _ := cmd.Flags().GetString("toolchain")
		autoFetch, _ := cmd.Flags().GetBool("fetch")

		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found in current directory")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Merge --toolchain flag into config
		tcCfg := cfg.Toolchain
		if toolchainName != "" && toolchainName != "auto" {
			tcCfg.Compiler = toolchainName
		}

		tc, err := toolchain.Detect(&tcCfg)
		if err != nil {
			return fmt.Errorf("detect toolchain: %w", err)
		}

		// Detect ABI tag
		abiTag := abi.DetectFromToolchain(tc.Kind, tc.Version, cfg.Package.Std)

		fmt.Printf(">> toolchain: %s %d.%d.%d (%s)\n", tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2], tc.Target)
		fmt.Printf(">> abi: %s\n", abiTag.String())

		// Check that all dependencies from cstow.lock are present
		if err := checkDependenciesReady(); err != nil {
			if autoFetch {
				fmt.Println(">> missing dependencies, fetching...")
				if err := runFetch(cfg, profile, toolchainName, true, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
					return fmt.Errorf("auto-fetch failed: %w", err)
				}
				// Re-check after fetch
				if err := checkDependenciesReady(); err != nil {
					return err
				}
			} else {
				return err
			}
		}

		// Run pre-build hook
		dir, _ := os.Getwd()
		hr := hooks.New(cfg.Hooks, dir)
		if err := hr.Run("pre-build"); err != nil {
			return err
		}

		buildDir := filepath.Join("build", profile)
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			return fmt.Errorf("create build dir: %w", err)
		}

		cmakeType := "Debug"
		if profile == "release" {
			cmakeType = "Release"
		}

		// Determine source directory
		sourceDir := "."
		if _, err := os.Stat("CMakeLists.txt"); err != nil {
			if cfg.Legacy != nil && cfg.Legacy.Root != "" {
				sourceDir = cfg.Legacy.Root
			} else if len(cfg.Build.Sources) > 0 && !strings.Contains(cfg.Build.Sources[0], "*") {
				// Use Build.Sources[0] only if it's a plain path (no globs)
				sourceDir = cfg.Build.Sources[0]
			}
		}

		cmakeArgs := []string{
			"-S", sourceDir,
			"-B", buildDir,
			fmt.Sprintf("-DCMAKE_BUILD_TYPE=%s", cmakeType),
		}
		cmakeArgs = append(cmakeArgs, tc.CMakeFlags()...)
		cmakeArgs = appendCMakeConfigArgs(cmakeArgs, cfg, profile)

		// Inject dependency paths from cstow_deps
		depsDir := filepath.Join(".", "cstow_deps")
		if entries, err := os.ReadDir(depsDir); err == nil && len(entries) > 0 {
			var paths []string
			for _, e := range entries {
				if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
					paths = append(paths, filepath.Join(depsDir, e.Name()))
				}
			}
			if len(paths) > 0 {
				cmakeArgs = append(cmakeArgs, fmt.Sprintf("-DCMAKE_PREFIX_PATH=%s", strings.Join(paths, ";")))
			}
		}

		// Add legacy extra args
		if cfg.Legacy != nil && len(cfg.Legacy.ExtraArgs) > 0 {
			cmakeArgs = append(cmakeArgs, cfg.Legacy.ExtraArgs...)
		}

		fmt.Printf(">> cmake configure (%s)\n", profile)
		cmakeCmd := exec.Command("cmake", cmakeArgs...)
		cmakeCmd.Stdout = os.Stdout
		cmakeCmd.Stderr = os.Stderr
		if err := cmakeCmd.Run(); err != nil {
			return fmt.Errorf("cmake configure failed: %w", err)
		}

		fmt.Println(">> cmake build")
		buildArgs := []string{
			"--build", buildDir,
			"--", fmt.Sprintf("-j%d", guessJobs()),
		}

		buildExe := exec.Command("cmake", buildArgs...)
		buildExe.Stdout = os.Stdout
		buildExe.Stderr = os.Stderr
		if err := buildExe.Run(); err != nil {
			return fmt.Errorf("cmake build failed: %w", err)
		}

		// Run post-build hook
		if err := hr.Run("post-build"); err != nil {
			return err
		}

		fmt.Printf(">> build complete (%s/%s)\n", buildDir, cfg.Package.Name)
		return nil
	},
}

func guessJobs() int {
	return 4
}

func resetBuildFlagState(cmd *cobra.Command) {
	resetBuildFlag := func(name string) {
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
	resetBuildFlag("profile")
	resetBuildFlag("toolchain")
	resetBuildFlag("fetch")
}

func init() {
	buildCmd.Flags().StringP("profile", "p", "debug", "build profile (debug|release)")
	buildCmd.Flags().String("toolchain", "auto", "compiler to use (auto|gcc|clang|msvc)")
	buildCmd.Flags().Bool("fetch", false, "automatically fetch missing dependencies before building")
	rootCmd.AddCommand(buildCmd)
}


func checkDependenciesReady() error {
	lockPath := "cstow.lock"
	_, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cstow.lock not found — run `cstow add` to declare dependencies first")
		}
		return fmt.Errorf("read cstow.lock: %w", err)
	}

	lf, err := resolver.LoadLock(lockPath)
	if err != nil {
		return fmt.Errorf("parse cstow.lock: %w", err)
	}

	if len(lf.Packages) == 0 {
		return nil
	}

	var missing []string
	for _, pkg := range lf.Packages {
		pkgDir := filepath.Join("cstow_deps", pkg.Name)
		if _, err := os.Stat(pkgDir); err != nil {
			missing = append(missing, fmt.Sprintf("%s@%s", pkg.Name, pkg.Version))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing dependencies: %s\nRun `cstow fetch` to download them.", strings.Join(missing, ", "))
	}
	return nil
}

// appendCMakeConfigArgs adds build.defines, build.include, and profile flags to cmake args.
func appendCMakeConfigArgs(args []string, cfg *config.Config, profile string) []string {
	// Inject build.defines as cmake -D flags
	for _, d := range cfg.Build.Defines {
		args = append(args, "-D"+d)
	}
	// Inject build.flags.defines
	for _, d := range cfg.Build.Flags.Defines {
		args = append(args, "-D"+d)
	}

	// Inject build.include paths
	if len(cfg.Build.Include) > 0 {
		args = append(args, fmt.Sprintf("-DCMAKE_INCLUDE_PATH=%s", strings.Join(cfg.Build.Include, ";")))
	}

	// Inject build.flags
	if len(cfg.Build.Flags.CXXFlags) > 0 {
		args = append(args, fmt.Sprintf("-DCMAKE_CXX_FLAGS=%s", strings.Join(cfg.Build.Flags.CXXFlags, " ")))
	}
	if len(cfg.Build.Flags.LinkFlags) > 0 {
		joined := strings.Join(cfg.Build.Flags.LinkFlags, " ")
		args = append(args,
			fmt.Sprintf("-DCMAKE_EXE_LINKER_FLAGS=%s", joined),
			fmt.Sprintf("-DCMAKE_SHARED_LINKER_FLAGS=%s", joined),
			fmt.Sprintf("-DCMAKE_MODULE_LINKER_FLAGS=%s", joined),
		)
	}

	// Apply profile-specific settings
	if p, ok := cfg.Profiles[profile]; ok {
		if p.LTO {
			args = append(args, "-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON")
		}
		// Also apply profile flags if present (not currently in Config struct but in PLAN.md spirit)
	}

	return args
}
