package repository

import "github.com/all3n/cstow/internal/config"

type PackageDef struct {
	Package   PackageMeta         `toml:"package"`
	Versions  []string            `toml:"versions"`
	Source    SourceDef           `toml:"source"`
	Build     BuildDef            `toml:"build"`
	Artifacts ArtifactsDef        `toml:"artifacts"`
	Deps      []config.Dependency `toml:"dependencies"`
}

type PackageMeta struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Homepage    string `toml:"homepage"`
	License     string `toml:"license"`
}

type SourceDef struct {
	Type        string            `toml:"type"`            // git | archive
	URL         string            `toml:"url"`
	TagTemplate string            `toml:"tag_template"`    // "v{version}"
	URLTemplate string            `toml:"url_template"`
	SHA256      map[string]string `toml:"sha256_versions"` // version -> sha256
}

type BuildDef struct {
	System   string                      `toml:"system"`   // cmake|make|header-only
	Type     string                      `toml:"type"`     // static|shared|header-only
	CMake    CMakeBuildDef               `toml:"cmake"`
	Profiles map[string]ProfileOverride  `toml:"profile"`
	Compiler map[string]CompilerOverride `toml:"compiler"` // gcc|clang|msvc
	Platform map[string]PlatformOverride `toml:"platform"` // linux|macos|windows
}

type CMakeBuildDef struct {
	Defines        []string `toml:"defines"`
	CXXFlags       []string `toml:"cxx_flags"`
	LinkFlags      []string `toml:"link_flags"`
	InstallTargets []string `toml:"install_targets"`
}

type ProfileOverride struct {
	Defines   []string `toml:"defines"`
	CXXFlags  []string `toml:"cxx_flags"`
	LinkFlags []string `toml:"link_flags"`
}

type CompilerOverride struct {
	Defines   []string `toml:"defines"`
	CXXFlags  []string `toml:"cxx_flags"`
	LinkFlags []string `toml:"link_flags"`
}

type PlatformOverride struct {
	Defines   []string `toml:"defines"`
	CXXFlags  []string `toml:"cxx_flags"`
	LinkFlags []string `toml:"link_flags"`
}

type ArtifactsDef struct {
	IncludeDirs []string `toml:"include_dirs"`
	Libs        []string `toml:"libs"`
}

// VersionOverride: only fields that differ from package.toml
type VersionOverride struct {
	Source *SourceOverride `toml:"source"`
	Build  *BuildOverride  `toml:"build"`
	Patch  string          `toml:"patch"`
}

type SourceOverride struct {
	SHA256 string `toml:"sha256"`
}

type BuildOverride struct {
	CMake    *CMakeBuildDef              `toml:"cmake"`
	Compiler map[string]CompilerOverride `toml:"compiler"`
}
