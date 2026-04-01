package builder

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/toolchain"
)

// Options holds all inputs for building a package from source.
type Options struct {
	SourcePath string                       // path to fetched source root
	InstallDir string                       // CMAKE_INSTALL_PREFIX target
	Config     *repository.MergedBuildConfig // merged cmake defines, flags, etc.
	Toolchain  *toolchain.Toolchain         // detected compiler info
	Profile    string                       // "debug" or "release"
	Jobs       int                          // parallel build jobs, 0 = auto
	Stdout     io.Writer
	Stderr     io.Writer
}

// Result holds the outcome of a successful build.
type Result struct {
	InstallDir string
}

// Build runs cmake configure → build → install.
func Build(opts Options) (*Result, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Jobs <= 0 {
		opts.Jobs = runtime.NumCPU()
	}

	buildDir := filepath.Join(opts.SourcePath, ".cstow-build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return nil, fmt.Errorf("create build dir: %w", err)
	}

	cmakeType := "Debug"
	if opts.Profile == "release" {
		cmakeType = "Release"
	}

	// ── Configure ──────────────────────────────────────────────
	cmakeArgs := []string{
		"-S", opts.SourcePath,
		"-B", buildDir,
		fmt.Sprintf("-DCMAKE_BUILD_TYPE=%s", cmakeType),
		fmt.Sprintf("-DCMAKE_INSTALL_PREFIX=%s", opts.InstallDir),
	}
	if opts.Toolchain != nil {
		cmakeArgs = append(cmakeArgs, opts.Toolchain.CMakeFlags()...)
	}
	for _, d := range opts.Config.CMakeDefines {
		cmakeArgs = append(cmakeArgs, "-D"+d)
	}

	if err := runCmake(cmakeArgs, opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("cmake configure: %w", err)
	}

	// ── Build ──────────────────────────────────────────────────
	buildArgs := []string{
		"--build", buildDir,
		"--", fmt.Sprintf("-j%d", opts.Jobs),
	}
	if err := runCmake(buildArgs, opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("cmake build: %w", err)
	}

	// ── Install ────────────────────────────────────────────────
	installArgs := []string{"--install", buildDir}
	if err := runCmake(installArgs, opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("cmake install: %w", err)
	}

	return &Result{InstallDir: opts.InstallDir}, nil
}

func runCmake(args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command("cmake", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// InstallDir returns the cache install path for a package.
func InstallDir(cacheRoot, name, version, abiTag string) string {
	return filepath.Join(cacheRoot, name, version, abiTag)
}

// CmakeArgsFor returns the cmake configure arguments that would be used.
// Exported for testing.
func CmakeArgsFor(opts Options) []string {
	cmakeType := "Debug"
	if opts.Profile == "release" {
		cmakeType = "Release"
	}

	args := []string{
		"-S", opts.SourcePath,
		"-B", filepath.Join(opts.SourcePath, ".cstow-build"),
		fmt.Sprintf("-DCMAKE_BUILD_TYPE=%s", cmakeType),
		fmt.Sprintf("-DCMAKE_INSTALL_PREFIX=%s", opts.InstallDir),
	}
	if opts.Toolchain != nil {
		args = append(args, opts.Toolchain.CMakeFlags()...)
	}
	for _, d := range opts.Config.CMakeDefines {
		args = append(args, "-D"+d)
	}
	return args
}

// CmakeBuildType converts profile name to cmake build type string.
func CmakeBuildType(profile string) string {
	if profile == "release" {
		return "Release"
	}
	return "Debug"
}

// IsCmakeInstalled checks if cmake is available on PATH.
func IsCmakeInstalled() bool {
	_, err := exec.LookPath("cmake")
	return err == nil
}

// GuessJobs returns a reasonable parallelism level.
func GuessJobs() int {
	return runtime.NumCPU()
}

// SplitCmakeOutput splits combined output into lines for assertion.
func SplitCmakeOutput(output []byte) []string {
	s := strings.TrimSpace(string(output))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
