package toolchain

import (
	"os"
	"os/exec"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectAutoGCC(t *testing.T) {
	// Most CI/dev machines have gcc
	tc, err := Detect(&config.Toolchain{Compiler: "auto"})
	if err != nil {
		t.Skipf("no compiler found: %v", err)
	}

	assert.NotEmpty(t, tc.CXX)
	assert.NotEmpty(t, tc.Kind)
	assert.Contains(t, []string{"gcc", "clang", "appleclang"}, tc.Kind)
	t.Logf("detected: kind=%s version=%v cxx=%s target=%s", tc.Kind, tc.Version, tc.CXX, tc.Target)
}

func TestDetectExplicitGCC(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ not found")
	}

	tc, err := Detect(&config.Toolchain{Compiler: "gcc"})
	require.NoError(t, err)
	assert.Equal(t, "gcc", tc.Kind)
	assert.True(t, tc.Version[0] > 0)
}

func TestDetectExplicitClang(t *testing.T) {
	if _, err := exec.LookPath("clang++"); err != nil {
		t.Skip("clang++ not found")
	}

	tc, err := Detect(&config.Toolchain{Compiler: "clang"})
	require.NoError(t, err)
	assert.Equal(t, "clang", tc.Kind)
	assert.True(t, tc.Version[0] > 0)
}

func TestDetectExplicitUnknown(t *testing.T) {
	_, err := Detect(&config.Toolchain{Compiler: "unknown-cc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown compiler")
}

func TestDetectEnvOverride(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ not found")
	}

	os.Setenv("CSTOW_CXX", gxx)
	defer os.Unsetenv("CSTOW_CXX")

	tc, err := Detect(&config.Toolchain{Compiler: "auto"})
	require.NoError(t, err)
	assert.Equal(t, gxx, tc.CXX)
}

func TestCMakeFlags(t *testing.T) {
	tc := &Toolchain{
		Kind:    "gcc",
		Version: [3]int{11, 4, 0},
		CXX:     "/usr/bin/g++",
		CC:      "/usr/bin/gcc",
		Target:  "x86_64-linux-gnu",
	}

	flags := tc.CMakeFlags()
	assert.Contains(t, flags, "-DCMAKE_CXX_COMPILER=/usr/bin/g++")
	assert.Contains(t, flags, "-DCMAKE_C_COMPILER=/usr/bin/gcc")
}

func TestCMakeFlagsWithSysroot(t *testing.T) {
	tc := &Toolchain{
		Kind:    "gcc",
		CXX:     "/usr/bin/g++",
		CC:      "/usr/bin/gcc",
		Sysroot: "/opt/sysroot",
	}

	flags := tc.CMakeFlags()
	assert.Contains(t, flags, "-DCMAKE_SYSROOT=/opt/sysroot")
}
