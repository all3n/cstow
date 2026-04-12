package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/aws/smithy-go"
	"github.com/spf13/cobra"
)

type doctorArtifactStore interface {
	List() ([]artifactdb.Record, error)
	Close() error
}

var doctorOpenStore = func() (doctorArtifactStore, error) {
	return artifactdb.OpenDefault()
}

var doctorNewRegistryClient = func(ctx context.Context, reg config.Registry) (*registry.S3Client, error) {
	return registry.NewS3Client(ctx, reg)
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system environment and configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, ">> cstow doctor: checking system environment...")

		// 1. Check CMake
		checkCMake(out)

		// 2. Check Toolchain
		checkToolchain(out)

		// 3. Check Cache & DB
		checkStorage(out)

		// 4. Check Registry
		checkRegistry(out)

		fmt.Fprintln(out, "\n>> doctor check complete.")
		return nil
	},
}

func checkCMake(w io.Writer) {
	fmt.Fprint(w, "[ ] CMake: ")
	path, err := exec.LookPath("cmake")
	if err != nil {
		fmt.Fprintln(w, "❌ NOT FOUND. Please install CMake.")
		return
	}
	out, err := exec.Command("cmake", "--version").Output()
	if err != nil {
		fmt.Fprintf(w, "❌ ERROR: %v\n", err)
		return
	}
	version := "unknown"
	lines := splitLines(string(out))
	if len(lines) > 0 {
		version = lines[0]
	}
	fmt.Fprintf(w, "✅ %s (%s)\n", version, path)
}

func checkToolchain(w io.Writer) {
	fmt.Fprint(w, "[ ] Compiler: ")
	global, _ := config.LoadGlobal()
	tcCfg := config.Toolchain{}
	if global != nil {
		tcCfg.Compiler = global.Toolchain.Prefer
	}
	tc, err := toolchain.Detect(&tcCfg)
	if err != nil {
		fmt.Fprintf(w, "❌ FAILED: %v\n", err)
		return
	}
	fmt.Fprintf(w, "✅ %s %d.%d.%d (%s)\n", tc.Kind, tc.Version[0], tc.Version[1], tc.Version[2], tc.Target)
}

func checkStorage(w io.Writer) {
	global, _ := config.LoadGlobal()
	cacheDir, err := resolveDoctorCacheDir(global)
	if err != nil {
		cacheDir = ""
	}

	// Cache Dir
	fmt.Fprint(w, "[ ] Cache Dir: ")
	if fi, err := os.Stat(cacheDir); err == nil && fi.IsDir() {
		fmt.Fprintf(w, "✅ %s\n", cacheDir)
	} else {
		fmt.Fprintf(w, "⚠️  MISSING: %s (will be created on first fetch)\n", cacheDir)
	}

	// DB Integrity
	fmt.Fprint(w, "[ ] Artifact DB: ")
	store, err := doctorOpenStore()
	if err != nil {
		fmt.Fprintf(w, "❌ ERROR: %v\n", err)
	} else {
		defer store.Close()
		list, err := store.List()
		if err != nil {
			fmt.Fprintf(w, "❌ CORRUPTED: %v\n", err)
		} else {
			fmt.Fprintf(w, "✅ OK (%d records index)\n", len(list))
		}
	}
}

func checkRegistry(w io.Writer) {
	fmt.Fprint(w, "[ ] Registry: ")
	global, _ := config.LoadGlobal()
	project, err := config.Load("cstow.toml")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(w, "⚠️  project config unreadable: %v\n", err)
		return
	}

	var projectRegs []config.Registry
	if project != nil {
		projectRegs = project.Registries
	}

	reg, err := config.ResolvePrimaryRegistry(projectRegs, global)
	if err != nil {
		fmt.Fprintln(w, "⚪ NOT CONFIGURED (skipped)")
		return
	}

	fmt.Fprintf(w, "Testing %s ... ", reg.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := doctorNewRegistryClient(ctx, reg)
	if err != nil {
		fmt.Fprintf(w, "❌ CONNECT FAILED: %v\n", err)
		return
	}

	// Try a simple operation
	_, err = client.ListVersions(ctx, "nonexistent-pkg-for-test")
	if err != nil && !isRegistryNotFoundError(err) {
		fmt.Fprintf(w, "❌ API FAILED: %v\n", err)
		return
	}
	fmt.Fprintln(w, "✅ CONNECTED")
}

func isRegistryNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch strings.ToLower(apiErr.ErrorCode()) {
		case "nosuchkey", "notfound", "404":
			return true
		case "nosuchbucket", "accessdenied", "invalidaccesskeyid", "signaturedoesnotmatch":
			return false
		}
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "404") || strings.Contains(lower, "not found")
}

func resolveDoctorCacheDir(global *config.Global) (string, error) {
	return config.ResolveCacheDir(global)
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
