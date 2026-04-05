package cmakegen

import "github.com/all3n/cstow/internal/config"

// DepTarget describes a discovered dependency's CMake information.
type DepTarget struct {
	Name       string // dependency directory name
	ConfigFile string // path to found *Config.cmake file (empty if fallback)
	TargetName string // inferred CMake target (e.g. "fmt::fmt")
	Prefix     string // dependency install prefix path
}

// GenerateOptions holds all data needed to generate CMake files.
type GenerateOptions struct {
	Name      string
	Type      string // executable | library | header-only
	Std       string // c++17 etc
	Sources   []string
	Include   []string
	Defines   []string
	Deps      []DepTarget
	Profiles  map[string]config.Profile
	Toolchain config.Toolchain
}
