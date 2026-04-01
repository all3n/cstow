package builder

import (
	"testing"

	"github.com/all3n/cstow/internal/repository"
	"github.com/stretchr/testify/assert"
)

func TestCmakeBuildType(t *testing.T) {
	assert.Equal(t, "Debug", CmakeBuildType("debug"))
	assert.Equal(t, "Release", CmakeBuildType("release"))
	assert.Equal(t, "Debug", CmakeBuildType("other"))
}

func TestGuessJobs(t *testing.T) {
	jobs := GuessJobs()
	assert.Greater(t, jobs, 0)
}

func TestCmakeArgsFor_Debug(t *testing.T) {
	cfg := &repository.MergedBuildConfig{
		System:       "cmake",
		CMakeDefines: []string{"FOO=1", "BAR=2"},
	}
	opts := Options{
		SourcePath: "/src/mylib",
		InstallDir: "/cache/mylib/1.0.0/gcc13-cxx17-linux-x86_64",
		Config:     cfg,
		Profile:    "debug",
	}

	args := CmakeArgsFor(opts)

	assert.Contains(t, args, "-S")
	assert.Contains(t, args, "/src/mylib")
	assert.Contains(t, args, "-DCMAKE_BUILD_TYPE=Debug")
	assert.Contains(t, args, "-DCMAKE_INSTALL_PREFIX=/cache/mylib/1.0.0/gcc13-cxx17-linux-x86_64")
	assert.Contains(t, args, "-DFOO=1")
	assert.Contains(t, args, "-DBAR=2")
}

func TestCmakeArgsFor_Release(t *testing.T) {
	cfg := &repository.MergedBuildConfig{System: "cmake"}
	opts := Options{
		SourcePath: "/src",
		InstallDir: "/dst",
		Config:     cfg,
		Profile:    "release",
	}

	args := CmakeArgsFor(opts)
	assert.Contains(t, args, "-DCMAKE_BUILD_TYPE=Release")
}

func TestCmakeArgsFor_NoDefines(t *testing.T) {
	cfg := &repository.MergedBuildConfig{System: "cmake"}
	opts := Options{
		SourcePath: "/src",
		InstallDir: "/dst",
		Config:     cfg,
		Profile:    "debug",
	}

	args := CmakeArgsFor(opts)
	// Expected: -S /src -B /src/.cstow-build -DCMAKE_BUILD_TYPE=Debug -DCMAKE_INSTALL_PREFIX=/dst
	expected := []string{"-S", "/src", "-B", "/src/.cstow-build",
		"-DCMAKE_BUILD_TYPE=Debug", "-DCMAKE_INSTALL_PREFIX=/dst"}
	assert.Equal(t, expected, args)
}

func TestInstallDir(t *testing.T) {
	dir := InstallDir("/cache", "fmt", "10.2.1", "gcc13-cxx17-linux-x86_64")
	assert.Equal(t, "/cache/fmt/10.2.1/gcc13-cxx17-linux-x86_64", dir)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
