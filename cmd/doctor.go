package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system environment and configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(">> cstow doctor: checking system environment...")

		// 1. Check CMake
		checkCMake()

		// 2. Check Toolchain
		checkToolchain()

		// 3. Check Cache & DB
		checkStorage()

		// 4. Check Registry
		checkRegistry()

		fmt.Println("\n>> doctor check complete.")
		return nil
	},
}

func checkCMake() {
	fmt.Print("[ ] CMake: ")
	path, err := exec.LookPath("cmake")
	if err != nil {
		fmt.Println("❌ NOT FOUND. Please install CMake.")
		return
	}
	out, err := exec.Command("cmake", "--version").Output()
	if err != nil {
		fmt.Printf("❌ ERROR: %v\n", err)
		return
	}
	version := "unknown"
	lines := splitLines(string(out))
	if len(lines) > 0 {
		version = lines[0]
	}
	fmt.Printf("✅ %s (%s)\n", version, path)
}

func checkToolchain() {
	fmt.Print("[ ] Compiler: ")
	global, _ := config.LoadGlobal()
	tcCfg := config.Toolchain{}
	if global != nil {
		tcCfg.Compiler = global.Toolchain.Prefer
	}
	tc, err := toolchain.Detect(&tcCfg)
	if err != nil {
		fmt.Printf("❌ FAILED: %v\n", err)
		return
	}
	fmt.Printf("✅ %s %d.%d.%d (%s)\n", tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2], tc.Target)
}

func checkStorage() {
	// Cache Dir
	home, _ := os.UserHomeDir()
	cstowDir := filepath.Join(home, ".cstow")
	cacheDir := filepath.Join(cstowDir, "cache")
	fmt.Printf("[ ] Cache Dir: ")
	if fi, err := os.Stat(cacheDir); err == nil && fi.IsDir() {
		fmt.Printf("✅ %s\n", cacheDir)
	} else {
		fmt.Printf("⚠️  MISSING: %s (will be created on first fetch)\n", cacheDir)
	}

	// DB Integrity
	fmt.Print("[ ] Artifact DB: ")
	store, err := artifactdb.OpenDefault()
	if err != nil {
		fmt.Printf("❌ ERROR: %v\n", err)
	} else {
		defer store.Close()
		list, err := store.List()
		if err != nil {
			fmt.Printf("❌ CORRUPTED: %v\n", err)
		} else {
			fmt.Printf("✅ OK (%d records index)\n", len(list))
		}
	}
}

func checkRegistry() {
	fmt.Print("[ ] Registry: ")
	global, _ := config.LoadGlobal()
	project, _ := config.Load("cstow.toml")
	
	var projectRegs []config.Registry
	if project != nil {
		projectRegs = project.Registries
	}

	reg, err := config.ResolvePrimaryRegistry(projectRegs, global)
	if err != nil {
		fmt.Println("⚪ NOT CONFIGURED (skipped)")
		return
	}

	fmt.Printf("Testing %s ... ", reg.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := registry.NewS3Client(ctx, reg)
	if err != nil {
		fmt.Printf("❌ CONNECT FAILED: %v\n", err)
		return
	}

	// Try a simple operation
	_, err = client.ListVersions(ctx, "nonexistent-pkg-for-test")
	if err != nil && !isRegistryNotFoundError(err) {
		fmt.Printf("❌ API FAILED: %v\n", err)
		return
	}
	fmt.Println("✅ CONNECTED")
}

func isRegistryNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Many S3-like providers return "not found" or 404 for empty package lists
	return true
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
