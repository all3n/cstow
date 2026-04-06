package repository

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMerge_BaseOnly(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake: CMakeBuildDef{
				Defines:  []string{"BUILD_SHARED_LIBS=OFF"},
				CXXFlags: []string{"-Wall"},
			},
		},
		Artifacts: ArtifactsDef{
			IncludeDirs: []string{"include"},
			Libs:        []string{"libfoo.a"},
		},
	}
	got := Merge(pkg, nil, "gcc", "debug", "linux")
	assert.Equal(t, "cmake", got.System)
	assert.Equal(t, []string{"BUILD_SHARED_LIBS=OFF"}, got.CMakeDefines)
	assert.Equal(t, []string{"-Wall"}, got.CXXFlags)
	assert.Equal(t, []string{"include"}, got.IncludeDirs)
	assert.Equal(t, []string{"libfoo.a"}, got.Libs)
}

func TestMerge_ProfileAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Profiles: map[string]ProfileOverride{
				"release": {Defines: []string{"CMAKE_BUILD_TYPE=Release"}, CXXFlags: []string{"-O3"}},
			},
		},
	}
	got := Merge(pkg, nil, "gcc", "release", "linux")
	assert.Equal(t, []string{"BASE=1", "CMAKE_BUILD_TYPE=Release"}, got.CMakeDefines)
	assert.Equal(t, []string{"-O3"}, got.CXXFlags)
}

func TestMerge_CompilerAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Compiler: map[string]CompilerOverride{
				"msvc": {Defines: []string{"_CRT_SECURE_NO_WARNINGS=1"}, CXXFlags: []string{"/EHsc"}},
			},
		},
	}
	got := Merge(pkg, nil, "msvc", "debug", "windows")
	assert.Equal(t, []string{"BASE=1", "_CRT_SECURE_NO_WARNINGS=1"}, got.CMakeDefines)
	assert.Equal(t, []string{"/EHsc"}, got.CXXFlags)
}

func TestMerge_PlatformAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Platform: map[string]PlatformOverride{
				"linux": {Defines: []string{"LINUX=1"}},
			},
		},
	}
	got := Merge(pkg, nil, "gcc", "debug", "linux")
	assert.Equal(t, []string{"BASE=1", "LINUX=1"}, got.CMakeDefines)
}

func TestMerge_VersionOverrideReplaces(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"OLD=1"}},
		},
	}
	ver := &VersionOverride{
		Build: &BuildOverride{
			CMake: &CMakeBuildDef{Defines: []string{"NEW=1", "NEW=2"}},
		},
		Patch: "fix.patch",
	}
	got := Merge(pkg, ver, "gcc", "debug", "linux")
	// version override replaces, not appends
	assert.Equal(t, []string{"NEW=1", "NEW=2"}, got.CMakeDefines)
	assert.Equal(t, "fix.patch", got.Patch)
}

func TestMerge_VersionOverrideCompilerAppends(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			Compiler: map[string]CompilerOverride{
				"clang": {CXXFlags: []string{"-Wno-old"}},
			},
		},
	}
	ver := &VersionOverride{
		Build: &BuildOverride{
			Compiler: map[string]CompilerOverride{
				"clang": {CXXFlags: []string{"-Wno-new"}},
			},
		},
	}
	got := Merge(pkg, ver, "clang", "debug", "linux")
	// version compiler override appends to package compiler override
	assert.Equal(t, []string{"-Wno-old", "-Wno-new"}, got.CXXFlags)
}

func TestMerge_BuildTypePropagation(t *testing.T) {
	t.Run("from package base", func(t *testing.T) {
		pkg := &PackageDef{
			Build: BuildDef{
				System: "cmake",
				Type:   "shared",
				CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			},
		}
		got := Merge(pkg, nil, "gcc", "debug", "linux")
		assert.Equal(t, "shared", got.BuildType)
	})

	t.Run("version override replaces", func(t *testing.T) {
		pkg := &PackageDef{
			Build: BuildDef{
				System: "cmake",
				Type:   "static",
				CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			},
		}
		ver := &VersionOverride{
			Build: &BuildOverride{
				Type: "shared",
			},
		}
		got := Merge(pkg, ver, "gcc", "debug", "linux")
		assert.Equal(t, "shared", got.BuildType)
	})

	t.Run("header-only from package", func(t *testing.T) {
		pkg := &PackageDef{
			Build: BuildDef{
				System: "cmake",
				Type:   "header-only",
			},
		}
		got := Merge(pkg, nil, "gcc", "debug", "linux")
		assert.Equal(t, "header-only", got.BuildType)
	})

	t.Run("empty defaults empty", func(t *testing.T) {
		pkg := &PackageDef{
			Build: BuildDef{
				System: "cmake",
				CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}},
			},
		}
		got := Merge(pkg, nil, "gcc", "debug", "linux")
		assert.Equal(t, "", got.BuildType)
	})
}

func TestMerge_AllLayers(t *testing.T) {
	goos := "linux"
	if runtime.GOOS == "windows" {
		goos = "windows"
	}
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake:  CMakeBuildDef{Defines: []string{"BASE=1"}, CXXFlags: []string{"-Wall"}},
			Profiles: map[string]ProfileOverride{
				"release": {Defines: []string{"NDEBUG=1"}},
			},
			Compiler: map[string]CompilerOverride{
				"gcc": {CXXFlags: []string{"-fstack-protector"}},
			},
			Platform: map[string]PlatformOverride{
				goos: {Defines: []string{"OS_DEFINE=1"}},
			},
		},
		Artifacts: ArtifactsDef{IncludeDirs: []string{"include"}, Libs: []string{"libfoo.a"}},
	}
	ver := &VersionOverride{
		Build: &BuildOverride{
			CMake: &CMakeBuildDef{Defines: []string{"OVERRIDE=1"}},
		},
	}
	got := Merge(pkg, ver, "gcc", "release", goos)
	// version override replaces cmake.defines entirely
	assert.Equal(t, []string{"OVERRIDE=1"}, got.CMakeDefines)
	// cxx_flags: base + release(none) + gcc + ver compiler(none)
	assert.Equal(t, []string{"-Wall", "-fstack-protector"}, got.CXXFlags)
}

func TestMerge_LinkFlagsBaseOnly(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "cmake",
			CMake: CMakeBuildDef{
				LinkFlags: []string{"-lpthread"},
			},
		},
	}
	got := Merge(pkg, nil, "gcc", "debug", "linux")
	assert.Equal(t, []string{"-lpthread"}, got.LinkFlags)
}

func TestMerge_AutotoolsEnhanced(t *testing.T) {
	pkg := &PackageDef{
		Build: BuildDef{
			System: "autotools",
			Autotools: AutotoolsBuildDef{
				Script: "config",
				Args:   []string{"no-shared"},
				CFlags: []string{"-O2"},
			},
		},
	}
	got := Merge(pkg, nil, "gcc", "debug", "linux")
	assert.Equal(t, "autotools", got.System)
	assert.Equal(t, "config", got.AutotoolsScript)
	assert.Equal(t, []string{"no-shared"}, got.AutotoolsArgs)
	assert.Equal(t, []string{"-O2"}, got.CFlags)

	ver := &VersionOverride{
		Build: &BuildOverride{
			Autotools: &AutotoolsBuildDef{
				Script: "Configure",
				Args:   []string{"shared"},
				CFlags: []string{"-O3"},
			},
		},
	}
	got2 := Merge(pkg, ver, "gcc", "debug", "linux")
	assert.Equal(t, "Configure", got2.AutotoolsScript)
	assert.Equal(t, []string{"shared"}, got2.AutotoolsArgs)
	assert.Equal(t, []string{"-O2", "-O3"}, got2.CFlags) // CFlags appends? Wait, let's check Merge implementation
}
