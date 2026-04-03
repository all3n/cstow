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
	SourcePath string
	InstallDir string
	Config     *repository.MergedBuildConfig
	Toolchain  *toolchain.Toolchain
	Profile    string
	Jobs       int
	Stdout     io.Writer
	Stderr     io.Writer
}

// Result holds the outcome of a successful build.
type Result struct {
	InstallDir string
}

// Build runs cmake configure -> build -> install.
// For header-only libraries, skips cmake entirely and copies headers.
func Build(opts Options) (*Result, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("build config is required")
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Jobs <= 0 {
		opts.Jobs = runtime.NumCPU()
	}

	if opts.Config.BuildType == "header-only" {
		return installHeaderOnly(opts)
	}

	buildDir := filepath.Join(opts.SourcePath, ".cstow-build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return nil, fmt.Errorf("create build dir: %w", err)
	}

	if err := runCmake(configureArgs(opts, buildDir), opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("cmake configure: %w", err)
	}

	buildArgs := []string{
		"--build", buildDir,
		"--", fmt.Sprintf("-j%d", opts.Jobs),
	}
	if err := runCmake(buildArgs, opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("cmake build: %w", err)
	}

	installArgs := []string{"--install", buildDir}
	if err := runCmake(installArgs, opts.Stdout, opts.Stderr); err != nil {
		return nil, fmt.Errorf("cmake install: %w", err)
	}

	return &Result{InstallDir: opts.InstallDir}, nil
}

func configureArgs(opts Options, buildDir string) []string {
	cmakeType := "Debug"
	if opts.Profile == "release" {
		cmakeType = "Release"
	}

	args := []string{
		"-S", opts.SourcePath,
		"-B", buildDir,
		fmt.Sprintf("-DCMAKE_BUILD_TYPE=%s", cmakeType),
		fmt.Sprintf("-DCMAKE_INSTALL_PREFIX=%s", opts.InstallDir),
	}
	if opts.Toolchain != nil {
		args = append(args, opts.Toolchain.CMakeFlags()...)
	}

	buildSharedDefine := ""
	for _, d := range opts.Config.CMakeDefines {
		if strings.HasPrefix(d, "BUILD_SHARED_LIBS=") {
			buildSharedDefine = d
			continue
		}
		args = append(args, "-D"+d)
	}
	switch opts.Config.BuildType {
	case "shared":
		buildSharedDefine = "BUILD_SHARED_LIBS=ON"
	case "static":
		buildSharedDefine = "BUILD_SHARED_LIBS=OFF"
	}
	if buildSharedDefine != "" {
		args = append(args, "-D"+buildSharedDefine)
	}

	if len(opts.Config.CXXFlags) > 0 {
		args = append(args, fmt.Sprintf("-DCMAKE_CXX_FLAGS=%s", strings.Join(opts.Config.CXXFlags, " ")))
	}
	if len(opts.Config.LinkFlags) > 0 {
		joined := strings.Join(opts.Config.LinkFlags, " ")
		args = append(args,
			fmt.Sprintf("-DCMAKE_EXE_LINKER_FLAGS=%s", joined),
			fmt.Sprintf("-DCMAKE_SHARED_LINKER_FLAGS=%s", joined),
			fmt.Sprintf("-DCMAKE_MODULE_LINKER_FLAGS=%s", joined),
		)
	}

	return args
}

// installHeaderOnly copies include directories from source to install dir.
func installHeaderOnly(opts Options) (*Result, error) {
	if err := os.MkdirAll(opts.InstallDir, 0o755); err != nil {
		return nil, fmt.Errorf("create install dir: %w", err)
	}
	for _, incDir := range opts.Config.IncludeDirs {
		src := filepath.Join(opts.SourcePath, incDir)
		dst := filepath.Join(opts.InstallDir, "include", filepath.Base(incDir))
		if err := copyDir(src, dst); err != nil {
			fmt.Fprintf(opts.Stderr, "warning: skip include dir %s: %v\n", incDir, err)
		}
	}
	return &Result{InstallDir: opts.InstallDir}, nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
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

// IsCmakeInstalled checks if cmake is available on PATH.
func IsCmakeInstalled() bool {
	_, err := exec.LookPath("cmake")
	return err == nil
}

// GuessJobs returns a reasonable parallelism level.
func GuessJobs() int {
	return runtime.NumCPU()
}
