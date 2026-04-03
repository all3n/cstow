package builder

import (
	"testing"

	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/toolchain"
	"github.com/stretchr/testify/assert"
)

func TestInstallDir(t *testing.T) {
	dir := InstallDir("/cache", "fmt", "10.2.1", "gcc13-cxx17-linux-x86_64")
	assert.Equal(t, "/cache/fmt/10.2.1/gcc13-cxx17-linux-x86_64", dir)
}

func TestGuessJobs(t *testing.T) {
	jobs := GuessJobs()
	assert.Greater(t, jobs, 0)
}

func TestIsCmakeInstalled(t *testing.T) {
	_ = IsCmakeInstalled()
}

func TestConfigureArgsIncludesMergedFlags(t *testing.T) {
	args := configureArgs(Options{
		SourcePath: "/src",
		InstallDir: "/install",
		Profile:    "release",
		Toolchain: &toolchain.Toolchain{
			CXX: "/usr/bin/clang++",
			CC:  "/usr/bin/clang",
		},
		Config: &repository.MergedBuildConfig{
			BuildType:    "shared",
			CMakeDefines: []string{"FMT_INSTALL=ON"},
			CXXFlags:     []string{"-Wall", "-Wextra"},
			LinkFlags:    []string{"-lpthread", "-ldl"},
		},
	}, "/tmp/build")

	assert.Contains(t, args, "-S")
	assert.Contains(t, args, "/src")
	assert.Contains(t, args, "-B")
	assert.Contains(t, args, "/tmp/build")
	assert.Contains(t, args, "-DCMAKE_BUILD_TYPE=Release")
	assert.Contains(t, args, "-DCMAKE_INSTALL_PREFIX=/install")
	assert.Contains(t, args, "-DCMAKE_CXX_COMPILER=/usr/bin/clang++")
	assert.Contains(t, args, "-DCMAKE_C_COMPILER=/usr/bin/clang")
	assert.Contains(t, args, "-DBUILD_SHARED_LIBS=ON")
	assert.Contains(t, args, "-DFMT_INSTALL=ON")
	assert.Contains(t, args, "-DCMAKE_CXX_FLAGS=-Wall -Wextra")
	assert.Contains(t, args, "-DCMAKE_EXE_LINKER_FLAGS=-lpthread -ldl")
	assert.Contains(t, args, "-DCMAKE_SHARED_LINKER_FLAGS=-lpthread -ldl")
	assert.Contains(t, args, "-DCMAKE_MODULE_LINKER_FLAGS=-lpthread -ldl")
}

func TestConfigureArgsBuildTypeOverridesConflictingBuildSharedDefine(t *testing.T) {
	args := configureArgs(Options{
		SourcePath: "/src",
		InstallDir: "/install",
		Config: &repository.MergedBuildConfig{
			BuildType:    "shared",
			CMakeDefines: []string{"BUILD_SHARED_LIBS=OFF", "FMT_INSTALL=ON"},
		},
	}, "/tmp/build")

	assert.Contains(t, args, "-DBUILD_SHARED_LIBS=ON")
	assert.NotContains(t, args, "-DBUILD_SHARED_LIBS=OFF")
	assert.Contains(t, args, "-DFMT_INSTALL=ON")
}
