package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		profile, _ := cmd.Flags().GetString("profile")
		toolchainName, _ := cmd.Flags().GetString("toolchain")

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

		fmt.Printf(">> toolchain: %s %d.%d.%d (%s)\n", tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2], tc.Target)

		buildDir := filepath.Join("build", profile)
		if err := os.MkdirAll(buildDir, 0o755); err != nil {
			return fmt.Errorf("create build dir: %w", err)
		}

		cmakeType := "Debug"
		if profile == "release" {
			cmakeType = "Release"
		}

		cmakeArgs := []string{
			"-S", ".",
			"-B", buildDir,
			fmt.Sprintf("-DCMAKE_BUILD_TYPE=%s", cmakeType),
		}
		cmakeArgs = append(cmakeArgs, tc.CMakeFlags()...)

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

		fmt.Printf(">> build complete (%s/%s)\n", buildDir, cfg.Package.Name)
		return nil
	},
}

func guessJobs() int {
	return 4
}

func init() {
	buildCmd.Flags().StringP("profile", "p", "debug", "build profile (debug|release)")
	buildCmd.Flags().String("toolchain", "auto", "compiler to use (auto|gcc|clang|msvc)")
	rootCmd.AddCommand(buildCmd)
}
