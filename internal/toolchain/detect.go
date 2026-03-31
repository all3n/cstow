package toolchain

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/all3n/cstow/internal/config"
)

type Toolchain struct {
	Kind    string // gcc | clang | msvc | appleclang
	Version [3]int // major.minor.patch
	Path    string
	CXX     string
	CC      string
	Sysroot string
	Target  string // e.g. x86_64-linux-gnu
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Detect discovers the compiler toolchain.
// Priority: cstow.toml toolchain.compiler > CSTOW_CXX env > CC/CXX env > PATH scan.
func Detect(cfg *config.Toolchain) (*Toolchain, error) {
	tc := &Toolchain{
		Sysroot: cfg.Sysroot,
	}

	// 1. Explicit config override
	if cfg.Compiler != "" && cfg.Compiler != "auto" {
		if err := tc.resolveExplicit(cfg.Compiler); err != nil {
			return nil, err
		}
		return tc, nil
	}

	// 2. CSTOW_CXX / CSTOW_CC env
	if cxx := os.Getenv("CSTOW_CXX"); cxx != "" {
		if path, err := exec.LookPath(cxx); err == nil {
			tc.CXX = path
			tc.CC = os.Getenv("CSTOW_CC")
			if tc.CC == "" {
				tc.CC = strings.Replace(path, "++", "", 1)
			}
			if err := tc.probe(); err != nil {
				return nil, err
			}
			return tc, nil
		}
	}

	// 3. CC / CXX env
	if cxx := os.Getenv("CXX"); cxx != "" {
		if path, err := exec.LookPath(cxx); err == nil {
			tc.CXX = path
			tc.CC = os.Getenv("CC")
			if tc.CC == "" {
				tc.CC = strings.Replace(path, "++", "", 1)
			}
			if err := tc.probe(); err != nil {
				return nil, err
			}
			return tc, nil
		}
	}

	// 4. PATH scan: prefer clang > gcc
	candidates := []string{"clang++", "g++"}
	if runtime.GOOS == "windows" {
		candidates = []string{"cl.exe", "clang++", "g++"}
	}
	if runtime.GOOS == "darwin" {
		candidates = []string{"clang++", "g++"}
	}

	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			tc.CXX = path
			tc.CC = strings.Replace(path, "++", "", 1)
			if err := tc.probe(); err != nil {
				return nil, err
			}
			return tc, nil
		}
	}

	return nil, fmt.Errorf("no C++ compiler found in PATH")
}

func (tc *Toolchain) resolveExplicit(compiler string) error {
	names := map[string][]string{
		"gcc":   {"g++", "gcc"},
		"clang": {"clang++", "clang"},
		"msvc":  {"cl.exe"},
	}

	suffixes, ok := names[compiler]
	if !ok {
		return fmt.Errorf("unknown compiler: %s (supported: gcc, clang, msvc)", compiler)
	}

	for _, s := range suffixes {
		if path, err := exec.LookPath(s); err == nil {
			tc.CXX = path
			tc.CC = strings.Replace(path, "++", "", 1)
			break
		}
	}

	if tc.CXX == "" {
		return fmt.Errorf("compiler %s not found in PATH", compiler)
	}

	return tc.probe() // probe modifies tc in place, returns only error
}

// probe runs the compiler to determine kind, version, target.
func (tc *Toolchain) probe() error {
	out, err := exec.Command(tc.CXX, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("probe %s: %w", tc.CXX, err)
	}

	text := string(out)

	switch {
	case strings.Contains(text, "clang"):
		if strings.Contains(strings.ToLower(text), "apple") {
			tc.Kind = "appleclang"
		} else {
			tc.Kind = "clang"
		}
	case strings.Contains(text, "gcc") || strings.Contains(text, "GCC") || strings.Contains(text, "g++"):
		tc.Kind = "gcc"
	case strings.Contains(text, "MSVC") || strings.Contains(text, "Microsoft"):
		tc.Kind = "msvc"
	default:
		tc.Kind = "unknown"
	}

	// Parse version
	m := versionRe.FindStringSubmatch(text)
	if len(m) == 4 {
		tc.Version[0] = atoi(m[1])
		tc.Version[1] = atoi(m[2])
		tc.Version[2] = atoi(m[3])
	}

	// Get target triple
	if tc.Kind != "msvc" {
		targetOut, err := exec.Command(tc.CXX, "-dumpmachine").Output()
		if err == nil {
			tc.Target = strings.TrimSpace(string(targetOut))
		}
	}

	return nil
}

// CMakeFlags returns the CMake flags for this toolchain.
func (tc *Toolchain) CMakeFlags() []string {
	flags := []string{
		fmt.Sprintf("-DCMAKE_CXX_COMPILER=%s", tc.CXX),
		fmt.Sprintf("-DCMAKE_C_COMPILER=%s", tc.CC),
	}
	if tc.Sysroot != "" {
		flags = append(flags,
			fmt.Sprintf("-DCMAKE_SYSROOT=%s", tc.Sysroot),
		)
	}
	return flags
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
