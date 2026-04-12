package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDependenciesReady_NoLockFile(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	err = checkDependenciesReady(".")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cstow.lock not found")
	assert.Contains(t, err.Error(), "cstow add")
}

func TestCheckDependenciesReady_AllPresent(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "fmt"
version = "10.2.1"
source = "registry"
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join("cstow_deps", "fmt"), 0o755))

	err = checkDependenciesReady(".")
	assert.NoError(t, err)
}

func TestCheckDependenciesReady_MissingDeps(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1

[[package]]
name = "fmt"
version = "10.2.1"
source = "registry"

[[package]]
name = "spdlog"
version = "1.13.0"
source = "registry"
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join("cstow_deps", "fmt"), 0o755))
	// spdlog dir intentionally NOT created

	err = checkDependenciesReady(".")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependencies")
	assert.Contains(t, err.Error(), "spdlog@1.13.0")
	assert.Contains(t, err.Error(), "cstow fetch")
	assert.NotContains(t, err.Error(), "fmt@10.2.1")
}

func TestCheckDependenciesReady_EmptyLock(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	require.NoError(t, os.WriteFile("cstow.lock", []byte(`
version = 1
`), 0o644))

	err = checkDependenciesReady(".")
	assert.NoError(t, err)
}

func TestRunBuildAutoFetchFailureIncludesFetchContext(t *testing.T) {
	setupFetchGitTest(t)

	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0", Std: "c++17"},
		Dependencies: []config.Dependency{
			{
				Name:      "brokenlib",
				Version:   "1.0.0",
				Source:    "git",
				BuildType: "header-only",
				Git:       "https://example.com/brokenlib.git",
				Rev:       "v1.0.0",
			},
		},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:      "brokenlib",
				Version:   "1.0.0",
				Source:    "git:https://example.com/brokenlib.git",
				BuildType: "header-only",
				Git:       "https://example.com/brokenlib.git",
				Rev:       "v1.0.0",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	prevGitClone := fetchGitCloneFunc
	fetchGitCloneFunc = func(url, tag, destDir string) error {
		return assert.AnError
	}
	t.Cleanup(func() { fetchGitCloneFunc = prevGitClone })

	err := runBuild(".", "debug", "auto", true, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto-fetch failed")
	assert.Contains(t, err.Error(), "git source build for brokenlib@1.0.0")
	assert.Contains(t, err.Error(), "git dependency brokenlib@1.0.0[header-only]: clone source")
}

func TestRunBuildAutoFetchRegistryFailureIncludesFetchContext(t *testing.T) {
	setupFetchGitTest(t)

	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0", Std: "c++17"},
		Dependencies: []config.Dependency{
			{
				Name:      "fmt",
				Version:   "10.2.1",
				Source:    "registry",
				BuildType: "shared",
			},
		},
		Registries: []config.Registry{{
			Name: "default",
			URL:  "s3://example/cstow",
		}},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:      "fmt",
				Version:   "10.2.1",
				Source:    "registry:default",
				ABITag:    "abi-registry",
				BuildType: "shared",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	prevNewClient := fetchNewRegistryClient
	fetchNewRegistryClient = func(ctx context.Context, reg config.Registry) (fetchRegistryClient, error) {
		return &fakeFetchRegistryClient{
			manifest: &registry.Manifest{
				Name:    "fmt",
				Version: "10.2.1",
				Artifacts: []registry.Artifact{{
					ABITag:    "abi-registry",
					BuildType: "shared",
					HashID:    "deadbeef",
				}},
			},
			archive: []byte("bad-archive"),
		}, nil
	}
	t.Cleanup(func() { fetchNewRegistryClient = prevNewClient })

	err := runBuild(".", "debug", "auto", true, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto-fetch failed")
	assert.Contains(t, err.Error(), "registry artifact fmt@10.2.1[shared]: verify artifact hash")
}

func TestRunBuildAutoFetchSourceFallbackFailureIncludesPrebuiltCause(t *testing.T) {
	setupFetchGitTest(t)
	requireTool(t, "git")

	repoRoot := filepath.Join(t.TempDir(), "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	writeRepositoryPackage(t, repoRoot, "cycle-a", sourceRepo, packageOptions{
		buildType: "header-only",
		dependencies: []config.Dependency{
			{Name: "cycle-b", Version: "^1", BuildType: "header-only"},
		},
	})
	writeRepositoryPackage(t, repoRoot, "cycle-b", sourceRepo, packageOptions{
		buildType: "header-only",
		dependencies: []config.Dependency{
			{Name: "cycle-a", Version: "^1", BuildType: "header-only"},
		},
	})

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(fmt.Sprintf(`
[defaults]
std = "c++17"

[toolchain]
prefer = "gcc"

[[repositories]]
name = "local"
path = %q
priority = 10
`, repoRoot)), 0o644))

	cfg := &config.Config{
		Package: config.Package{Name: "demo", Version: "0.1.0", Std: "c++17"},
		Dependencies: []config.Dependency{
			{
				Name:      "cycle-a",
				Version:   "1.0.0",
				Source:    "registry",
				BuildType: "header-only",
			},
		},
	}
	require.NoError(t, cfg.Save("cstow.toml"))

	lf := &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{
			{
				Name:      "cycle-a",
				Version:   "1.0.0",
				Source:    "registry:default",
				BuildType: "header-only",
			},
		},
	}
	require.NoError(t, resolver.SaveLock("cstow.lock", lf))

	err = runBuild(".", "debug", "auto", true, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto-fetch failed")
	assert.Contains(t, err.Error(), "no registry is configured for cycle-a@1.0.0")
	assert.Contains(t, err.Error(), "source fallback for cycle-a@1.0.0")
	assert.Contains(t, err.Error(), "repository dependency cycle detected")
}

func TestAppendCMakeConfigArgs_Defines(t *testing.T) {
	cfg := &config.Config{
		Build: config.Build{
			Defines: []string{"FOO=BAR", "ENABLE_TESTS=ON"},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, nil, "debug")
	assert.Contains(t, args, "-DFOO=BAR")
	assert.Contains(t, args, "-DENABLE_TESTS=ON")
}

func TestAppendCMakeConfigArgs_IncludePaths(t *testing.T) {
	cfg := &config.Config{
		Build: config.Build{
			Include: []string{"vendor/include", "third_party/include"},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, nil, "debug")
	found := false
	for _, a := range args {
		if len(a) > len("-DCMAKE_INCLUDE_PATH=") && a[:len("-DCMAKE_INCLUDE_PATH=")] == "-DCMAKE_INCLUDE_PATH=" {
			assert.Contains(t, a, "vendor/include")
			assert.Contains(t, a, "third_party/include")
			found = true
		}
	}
	assert.True(t, found, "expected CMAKE_INCLUDE_PATH in args")
}

func TestAppendCMakeConfigArgs_ProfileLTO(t *testing.T) {
	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"release": {LTO: true},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, nil, "release")
	assert.Contains(t, args, "-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON")
}

func TestAppendCMakeConfigArgs_ProfileNoLTO(t *testing.T) {
	cfg := &config.Config{
		Profiles: map[string]config.Profile{
			"debug": {LTO: false},
		},
	}
	args := appendCMakeConfigArgs(nil, cfg, nil, "debug")
	assert.NotContains(t, args, "-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON")
}

func TestAppendCMakeConfigArgs_NoExtras(t *testing.T) {
	cfg := &config.Config{}
	args := appendCMakeConfigArgs(nil, cfg, nil, "debug")
	assert.Empty(t, args)
}

func TestAppendCMakeConfigArgs_IncludesGlobalBuildFlags(t *testing.T) {
	cfg := &config.Config{
		Build: config.Build{
			Defines: []string{"PROJECT_DEFINE=1"},
			Flags: config.BuildFlags{
				CXXFlags:  []string{"-Wall"},
				LinkFlags: []string{"-lpthread"},
				Defines:   []string{"PROJECT_FLAG_DEFINE=1"},
			},
		},
	}
	global := &config.Global{
		Build: config.GlobalBuild{
			Flags: config.GlobalBuildFlags{
				CXXFlags:  []string{"-fstack-protector-strong"},
				LinkFlags: []string{"-ldl"},
				Defines:   []string{"GLOBAL_DEFINE=1"},
			},
		},
	}

	args := appendCMakeConfigArgs(nil, cfg, global, "debug")
	assert.Contains(t, args, "-DGLOBAL_DEFINE=1")
	assert.Contains(t, args, "-DPROJECT_DEFINE=1")
	assert.Contains(t, args, "-DPROJECT_FLAG_DEFINE=1")
	assert.Contains(t, args, "-DCMAKE_CXX_FLAGS=-fstack-protector-strong -Wall")
	assert.Contains(t, args, "-DCMAKE_EXE_LINKER_FLAGS=-ldl -lpthread")
}
