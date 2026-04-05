package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/all3n/cstow/internal/abi"
	"github.com/all3n/cstow/internal/cmakegen"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/hooks"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/all3n/cstow/internal/workspace"
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

		return runBuild(".", profile, toolchainName, autoFetch, cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

func runBuild(workDir, profile, toolchainName string, autoFetch bool, stdout, stderr io.Writer) error {
	if workDir == "" {
		workDir = "."
	}
	cfgPath := filepath.Join(workDir, "cstow.toml")
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("cstow.toml not found in %s", workDir)
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

	fmt.Fprintf(stdout, ">> toolchain: %s %d.%d.%d (%s)\n", tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2], tc.Target)
	fmt.Fprintf(stdout, ">> abi: %s\n", abiTag.String())

	// Check that all dependencies are present
	if err := checkDependenciesReady(workDir); err != nil {
		if autoFetch {
			fmt.Fprintln(stdout, ">> missing dependencies, fetching...")
			fetchOpts := fetchOptions{
				WorkDir:        workDir,
				Profile:        profile,
				Toolchain:      toolchainName,
				SourceFallback: true,
				Stdout:         stdout,
				Stderr:         stderr,
			}
			// If in workspace, use workspace-wide fetch context
			if ws, err := workspace.Load(workDir); err == nil {
				fetchOpts.LockPath = ws.RootLockPath()
				fetchOpts.DepsDir = ws.RootDepsDir()
			}
			if err := runFetch(cfg, fetchOpts); err != nil {
				return fmt.Errorf("auto-fetch failed: %w", err)
			}
			// Re-check after fetch
			if err := checkDependenciesReady(workDir); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Run pre-build hook
	hr := hooks.New(cfg.Hooks, workDir)
	if err := hr.Run("pre-build"); err != nil {
		return err
	}

	buildDir := filepath.Join(workDir, "build", profile)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}

	cmakeType := "Debug"
	if profile == "release" {
		cmakeType = "Release"
	}

	// Determine source directory
	sourceDir := workDir
	hasCMakeLists := true
	if _, err := os.Stat(filepath.Join(workDir, "CMakeLists.txt")); err != nil {
		hasCMakeLists = false
		if cfg.Legacy != nil && cfg.Legacy.Root != "" {
			sourceDir = filepath.Join(workDir, cfg.Legacy.Root)
		} else if len(cfg.Build.Sources) > 0 && !strings.Contains(cfg.Build.Sources[0], "*") {
			sourceDir = filepath.Join(workDir, cfg.Build.Sources[0])
		}
	}

	cmakeArgs := []string{
		"-S", sourceDir,
		"-B", buildDir,
		fmt.Sprintf("-DCMAKE_BUILD_TYPE=%s", cmakeType),
	}
	cmakeArgs = append(cmakeArgs, tc.CMakeFlags()...)
	cmakeArgs = appendCMakeConfigArgs(cmakeArgs, cfg, profile)

	// Inject dependency paths from cstow_deps (local and workspace root)
	depsDirs := []string{filepath.Join(workDir, "cstow_deps")}
	if ws, err := workspace.Load(workDir); err == nil {
		rootDeps := ws.RootDepsDir()
		if rootDeps != depsDirs[0] {
			depsDirs = append(depsDirs, rootDeps)
		}
	}

	var prefixPaths []string
	for _, ddir := range depsDirs {
		if entries, err := os.ReadDir(ddir); err == nil && len(entries) > 0 {
			for _, e := range entries {
				if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
					prefixPaths = append(prefixPaths, filepath.Join(ddir, e.Name()))
				}
			}
		}
	}
	if len(prefixPaths) > 0 {
		cmakeArgs = append(cmakeArgs, fmt.Sprintf("-DCMAKE_PREFIX_PATH=%s", strings.Join(prefixPaths, ";")))
	}

	// Add legacy extra args
	if cfg.Legacy != nil && len(cfg.Legacy.ExtraArgs) > 0 {
		cmakeArgs = append(cmakeArgs, cfg.Legacy.ExtraArgs...)
	}

	// Auto-generate CMakeLists.txt if missing
	if !hasCMakeLists && sourceDir == workDir {
		fmt.Fprintln(stdout, ">> no CMakeLists.txt found, auto-generating from cstow.toml...")
		var deps []cmakegen.DepTarget
		for _, ddir := range depsDirs {
			if entries, derr := os.ReadDir(ddir); derr == nil && len(entries) > 0 {
				found, _ := cmakegen.DiscoverDeps(ddir)
				deps = append(deps, found...)
			}
		}
		genOpts := cmakegen.GenerateOptions{
			Name:      cfg.Package.Name,
			Type:      cfg.Build.Type,
			Std:       cfg.Package.Std,
			Sources:   cfg.Build.Sources,
			Include:   cfg.Build.Include,
			Defines:   cfg.Build.Defines,
			Deps:      deps,
			Profiles:  cfg.Profiles,
			Toolchain: cfg.Toolchain,
		}
		content := cmakegen.GenerateCMakeLists(genOpts)
		if err := os.WriteFile(filepath.Join(workDir, "CMakeLists.txt"), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write CMakeLists.txt: %w", err)
		}
	}

	fmt.Fprintf(stdout, ">> cmake configure (%s)\n", profile)
	cmakeCmd := exec.Command("cmake", cmakeArgs...)
	
	// Capture output for diagnosis on failure
	var configOut bytes.Buffer
	cmakeCmd.Stdout = io.MultiWriter(stdout, &configOut)
	cmakeCmd.Stderr = io.MultiWriter(stderr, &configOut)

	if err := cmakeCmd.Run(); err != nil {
		fmt.Fprintf(stderr, "\n!! cmake configure failed for %s\n", cfg.Package.Name)
		fmt.Fprintf(stderr, "!! last configure output:\n%s\n", lastLines(configOut.String(), 20))
		return fmt.Errorf("cmake configure failed: %w", err)
	}

	fmt.Fprintf(stdout, ">> cmake build\n")
	buildArgs := []string{
		"--build", buildDir,
		"--", fmt.Sprintf("-j%d", guessJobs()),
	}

	buildExe := exec.Command("cmake", buildArgs...)
	
	var buildOut bytes.Buffer
	buildExe.Stdout = io.MultiWriter(stdout, &buildOut)
	buildExe.Stderr = io.MultiWriter(stderr, &buildOut)

	if err := buildExe.Run(); err != nil {
		fmt.Fprintf(stderr, "\n!! cmake build failed for %s\n", cfg.Package.Name)
		fmt.Fprintf(stderr, "!! build directory: %s\n", buildDir)
		fmt.Fprintf(stderr, "!! last build output:\n%s\n", lastLines(buildOut.String(), 20))

		// Environment snapshot
		fmt.Fprintf(stderr, "!! environment snapshot (CSTOW/CC/CXX):\n")
		for _, env := range os.Environ() {
			if strings.HasPrefix(env, "CSTOW_") || strings.HasPrefix(env, "CC=") || strings.HasPrefix(env, "CXX=") {
				fmt.Fprintf(stderr, "   %s\n", env)
			}
		}
		return fmt.Errorf("cmake build failed: %w", err)
	}

	// Run post-build hook
	if err := hr.Run("post-build"); err != nil {
		return err
	}

	fmt.Fprintf(stdout, ">> build complete (%s/%s)\n", buildDir, cfg.Package.Name)
	return nil
}

func guessJobs() int {
	return 4
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return s
	}
	return "... (truncated)\n" + strings.Join(lines[len(lines)-n:], "\n")
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

func checkDependenciesReady(workDir string) error {
	lockPath := filepath.Join(workDir, "cstow.lock")
	if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
		// Fallback to workspace root lock
		if ws, err := workspace.Load(workDir); err == nil {
			lockPath = ws.RootLockPath()
		}
	}

	lf, err := resolver.LoadLock(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cstow.lock not found — run `cstow add` to declare dependencies first")
		}
		return fmt.Errorf("read/parse lock file %s: %w", lockPath, err)
	}

	if len(lf.Packages) == 0 {
		return nil
	}

	// Possible deps locations: local and workspace root
	depsDirs := []string{filepath.Join(workDir, "cstow_deps")}
	if ws, err := workspace.Load(workDir); err == nil {
		rootDeps := ws.RootDepsDir()
		if rootDeps != depsDirs[0] {
			depsDirs = append(depsDirs, rootDeps)
		}
	}

	var missing []string
	for _, pkg := range lf.Packages {
		found := false
		for _, ddir := range depsDirs {
			pkgDir := filepath.Join(ddir, pkg.Name)
			if _, err := os.Stat(pkgDir); err == nil {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s@%s", pkg.Name, pkg.Version))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing dependencies: %s (run `cstow fetch` or use --fetch)", strings.Join(missing, ", "))
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
	}

	return args
}
