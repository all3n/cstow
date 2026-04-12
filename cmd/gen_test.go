package cmd

import (
	"testing"

	"github.com/all3n/cstow/internal/cmakegen"
	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateOptionsFromConfigUsesGlobalDefaultsAndFlags(t *testing.T) {
	cfg := &config.Config{
		Package: config.Package{
			Name: "demo",
		},
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
		Defaults: config.GlobalDefaults{
			Std: "c++20",
		},
		Toolchain: config.GlobalToolchain{
			Prefer: "clang",
		},
		Build: config.GlobalBuild{
			Flags: config.GlobalBuildFlags{
				Defines:   []string{"GLOBAL_DEFINE=1"},
				CXXFlags:  []string{"-fstack-protector-strong"},
				LinkFlags: []string{"-ldl"},
			},
		},
	}

	opts, err := generateOptionsFromConfig(cfg, global, []cmakegen.DepTarget{{Name: "fmt"}})
	require.NoError(t, err)

	assert.Equal(t, "demo", opts.Name)
	assert.Equal(t, "executable", opts.Type)
	assert.Equal(t, "c++20", opts.Std)
	assert.Equal(t, "clang", opts.Toolchain.Compiler)
	assert.Equal(t, []string{"GLOBAL_DEFINE=1", "PROJECT_DEFINE=1", "PROJECT_FLAG_DEFINE=1"}, opts.Defines)
	assert.Equal(t, []string{"-fstack-protector-strong", "-Wall"}, opts.CXXFlags)
	assert.Equal(t, []string{"-ldl", "-lpthread"}, opts.LinkFlags)
	require.Len(t, opts.Deps, 1)
}
