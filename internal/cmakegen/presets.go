package cmakegen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/all3n/cstow/internal/config"
)

// presetsFile represents the top-level CMakePresets.json structure.
type presetsFile struct {
	Version          int                       `json:"version"`
	ConfigurePresets []presetEntry             `json:"configurePresets"`
	BuildPresets     []presetEntry             `json:"buildPresets"`
}

// presetEntry is a generic map used for both configure and build presets.
type presetEntry map[string]interface{}

// GeneratePresets produces a CMakePresets.json content string from the given
// options. Each profile becomes a configurePreset and a matching buildPreset.
// If Profiles is empty, default debug and release presets are generated.
func GeneratePresets(opts GenerateOptions) (string, error) {
	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = map[string]config.Profile{
			"debug":   {Debug: true},
			"release": {LTO: true},
		}
	}

	// Sort profile names for deterministic output.
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	stdNum := strings.TrimPrefix(opts.Std, "c++")

	var configurePresets []presetEntry
	var buildPresets []presetEntry

	for _, name := range names {
		p := profiles[name]
		cacheVars := presetEntry{
			"CMAKE_BUILD_TYPE":            buildType(p),
			"CMAKE_CXX_STANDARD":          stdNum,
			"CMAKE_EXPORT_COMPILE_COMMANDS": "ON",
		}
		if p.LTO {
			cacheVars["CMAKE_INTERPROCEDURAL_OPTIMIZATION"] = "ON"
		}
		if opts.Toolchain.Compiler != "" && opts.Toolchain.Compiler != "auto" {
			cacheVars["CMAKE_CXX_COMPILER"] = compilerName(opts.Toolchain.Compiler)
		}

		configurePresets = append(configurePresets, presetEntry{
			"name":          name,
			"binaryDir":     fmt.Sprintf("${sourceDir}/build/%s", name),
			"cacheVariables": cacheVars,
		})

		buildPresets = append(buildPresets, presetEntry{
			"name":             name,
			"configurePreset":  name,
		})
	}

	pf := presetsFile{
		Version:          6,
		ConfigurePresets: configurePresets,
		BuildPresets:     buildPresets,
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal presets: %w", err)
	}
	return string(data) + "\n", nil
}

// buildType returns the CMAKE_BUILD_TYPE value for a profile.
func buildType(p config.Profile) string {
	if p.Debug {
		return "Debug"
	}
	switch p.Optimize {
	case "2", "3", "s", "z":
		return "Release"
	default:
		return "Debug"
	}
}

// compilerName returns the C++ compiler command for a compiler identifier.
func compilerName(compiler string) string {
	switch compiler {
	case "clang":
		return "clang++"
	case "gcc":
		return "g++"
	default:
		return compiler + "++"
	}
}
