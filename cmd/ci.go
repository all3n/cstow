package cmd

import (
	"fmt"
	"os"
	"text/template"

	"github.com/all3n/cstow/internal/workspace"
	"github.com/spf13/cobra"
)

var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "Generate CI configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		emit, _ := cmd.Flags().GetString("emit")
		if emit == "" {
			emit = "github"
		}

		switch emit {
		case "github":
			return emitGitHubActions()
		default:
			return fmt.Errorf("unsupported --emit value: %s (supported: github)", emit)
		}
	},
}

const githubActionsTemplate = `name: cstow build

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build:
    strategy:
      matrix:
        toolchain: [gcc, clang]
        profile: [debug, release]
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Install cmake
        run: sudo apt-get update && sudo apt-get install -y cmake

      - name: Install Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.24"

      - name: Build cstow
        run: go build -o cstow .

      - name: Cache cstow packages
        uses: actions/cache@v4
        with:
          path: ~/.cstow/cache
          key: cstow-${{ matrix.toolchain }}-${{ hashFiles('cstow.lock') }}

      - name: Fetch dependencies
        run: {{ .FetchCmd }}
        env:
          CSTOW_REGISTRY_KEY: ${{ secrets.CSTOW_REGISTRY_KEY }}
          CSTOW_REGISTRY_SECRET: ${{ secrets.CSTOW_REGISTRY_SECRET }}

      - name: Build
        run: {{ .BuildCmd }}
        env:
          CSTOW_CI: 1
`

func emitGitHubActions() error {
	dir, _ := os.Getwd()
	isWorkspace := false
	if ws, err := workspace.Load(dir); err == nil && ws.Root == dir {
		isWorkspace = true
	}

	data := struct {
		FetchCmd string
		BuildCmd string
	}{
		FetchCmd: "./cstow fetch",
		BuildCmd: "./cstow build --profile ${{ matrix.profile }} --toolchain ${{ matrix.toolchain }}",
	}

	if isWorkspace {
		data.FetchCmd = "./cstow workspace fetch"
		data.BuildCmd = "./cstow workspace build --profile ${{ matrix.profile }} --toolchain ${{ matrix.toolchain }} --jobs 4"
	}

	tmpl, err := template.New("github").Parse(githubActionsTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	// Create .github/workflows directory
	if err := os.MkdirAll(".github/workflows", 0o755); err != nil {
		return fmt.Errorf("create workflows dir: %w", err)
	}

	f, err := os.Create(".github/workflows/cstow.yml")
	if err != nil {
		return fmt.Errorf("create workflow file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("write workflow: %w", err)
	}

	fmt.Printf(">> generated .github/workflows/cstow.yml (workspace: %v)\n", isWorkspace)
	fmt.Println("   Remember to set CSTOW_REGISTRY_KEY and CSTOW_REGISTRY_SECRET secrets")
	return nil
}

func init() {
	ciCmd.Flags().String("emit", "github", "CI platform to generate (github)")
	rootCmd.AddCommand(ciCmd)
}
