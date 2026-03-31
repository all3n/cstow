package cmd

import (
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/abi"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/spf13/cobra"
)

var checkAbiCmd = &cobra.Command{
	Use:   "check-abi",
	Short: "Report the current environment ABI tag",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := "cstow.toml"
		hasConfig := true
		if _, err := os.Stat(cfgPath); err != nil {
			hasConfig = false
		}

		var cfg *config.Config
		if hasConfig {
			var err error
			cfg, err = config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
		}

		tcCfg := &config.Toolchain{Compiler: "auto"}
		toolchainName, _ := cmd.Flags().GetString("toolchain")
		if toolchainName != "" && toolchainName != "auto" {
			tcCfg.Compiler = toolchainName
		}

		tc, err := toolchain.Detect(tcCfg)
		if err != nil {
			return fmt.Errorf("detect toolchain: %w", err)
		}

		cxxStd := "c++17"
		if cfg != nil && cfg.Package.Std != "" {
			cxxStd = cfg.Package.Std
		}

		tag := abi.DetectFromToolchain(tc.Kind, tc.Version, cxxStd)

		fmt.Printf("ABI Tag:    %s\n", tag.String())
		fmt.Printf("Compiler:   %s %d.%d.%d\n", tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2])
		fmt.Printf("Target:     %s\n", tc.Target)
		fmt.Printf("C++ Std:    c++%d\n", tag.CxxStd)
		fmt.Printf("Stdlib:     %s\n", tag.Stdlib)
		fmt.Printf("OS/Arch:    %s/%s\n", tag.OS, tag.Arch)
		return nil
	},
}

func init() {
	checkAbiCmd.Flags().String("toolchain", "auto", "compiler to check (auto|gcc|clang|msvc)")
	rootCmd.AddCommand(checkAbiCmd)
}
