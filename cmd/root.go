package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var profile string

var rootCmd = &cobra.Command{
	Use:   "cstow",
	Short: "C++ package manager and build system",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&profile, "profile", "p", "debug", "build profile (debug|release)")
	rootCmd.PersistentFlags().StringSlice("repository", nil, "additional repository paths to search")
	rootCmd.PersistentFlags().String("registry", "", "override primary registry URL")
}
