package cmd

import (
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/project"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new C++ project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if _, err := os.Stat(name); err == nil {
			return fmt.Errorf("directory %s already exists", name)
		}

		std, _ := cmd.Flags().GetString("std")
		buildType, _ := cmd.Flags().GetString("type")

		opts := project.ScaffoldOptions{
			Name: name,
			Std:  std,
			Type: buildType,
		}

		if err := project.Scaffold(name, opts); err != nil {
			return err
		}

		fmt.Printf("Created project %s\n", name)
		fmt.Printf("  cd %s && cstow build\n", name)
		return nil
	},
}

func init() {
	initCmd.Flags().String("std", "c++17", "C++ standard (c++14|c++17|c++20|c++23)")
	initCmd.Flags().String("type", "executable", "project type (executable|library|header-only)")
	rootCmd.AddCommand(initCmd)
}
