package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/all3n/cstow/internal/legacy"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate an existing CMake/Make project to cstow",
	RunE: func(cmd *cobra.Command, args []string) error {
		from, _ := cmd.Flags().GetString("from")
		if from == "" {
			from = "cmake"
		}

		name, _ := cmd.Flags().GetString("name")
		version, _ := cmd.Flags().GetString("version")
		if version == "" {
			version = "0.1.0"
		}

		switch from {
		case "cmake":
			return migrateCMake(name, version)
		default:
			return fmt.Errorf("unsupported --from value: %s (supported: cmake)", from)
		}
	},
}

func migrateCMake(providedName, version string) error {
	cmakePath := "CMakeLists.txt"
	if _, err := os.Stat(cmakePath); err != nil {
		return fmt.Errorf("CMakeLists.txt not found in current directory")
	}

	scanner := &legacy.CMakeScanner{}
	result, err := scanner.Scan(cmakePath)
	if err != nil {
		return fmt.Errorf("scan CMakeLists.txt: %w", err)
	}

	name := providedName
	if name == "" {
		if result.Name != "" {
			name = result.Name
		} else {
			dir, _ := os.Getwd()
			name = filepath.Base(dir)
		}
	}

	fmt.Printf(">> migrating project: %s (version: %s)\n", name, version)
	fmt.Printf(">> found %d dependencies (std: %s)\n", len(result.Dependencies), result.Std)
	for _, dep := range result.Dependencies {
		fmt.Printf("   - %s@%s (source: %s)\n", dep.Name, dep.Version, dep.Source)
	}

	extraArgs := []string{}
	cfg := legacy.GenerateCStowToml(name, version, result.Std, ".", extraArgs, result.Dependencies)

	// Save cstow.toml
	if _, err := os.Stat("cstow.toml"); err == nil {
		fmt.Println(">> cstow.toml already exists, skipping")
	} else {
		if err := cfg.Save("cstow.toml"); err != nil {
			return fmt.Errorf("save cstow.toml: %w", err)
		}
		fmt.Println(">> generated cstow.toml")
	}

	// Generate a build wrapper
	fmt.Println("\nYou can now use:")
	fmt.Printf("  cstow fetch    # download dependencies\n")
	fmt.Printf("  cstow build    # build with cmake wrapper\n")

	return nil
}

func init() {
	migrateCmd.Flags().String("from", "cmake", "source build system (cmake)")
	migrateCmd.Flags().String("name", "", "project name (defaults to directory name)")
	migrateCmd.Flags().String("version", "0.1.0", "initial version")
	rootCmd.AddCommand(migrateCmd)
}
