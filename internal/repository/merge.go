package repository

import "slices"

// MergedBuildConfig is the fully resolved build configuration for one dependency.
type MergedBuildConfig struct {
	System         string
	CMakeDefines   []string
	AutomakeArgs   []string
	CXXFlags       []string
	LinkFlags      []string
	IncludeDirs    []string
	Libs           []string
	InstallTargets []string
	Patch          string
	BuildType      string // static, shared, header-only
}

// Merge applies configuration layers in priority order (lowest → highest):
//  1. package-level cmake defines + cxx_flags + link_flags
//  2. profile override (appends)
//  3. compiler override (appends)
//  4. platform override (appends)
//  5. version-specific cmake override (replaces defines/link_flags if non-empty; cxx_flags append; compiler appends)
func Merge(pkg *PackageDef, ver *VersionOverride, toolchainKind, profile, goos string) *MergedBuildConfig {
	out := &MergedBuildConfig{
		System:         pkg.Build.System,
		IncludeDirs:    slices.Clone(pkg.Artifacts.IncludeDirs),
		Libs:           slices.Clone(pkg.Artifacts.Libs),
		InstallTargets: slices.Clone(pkg.Build.CMake.InstallTargets),
		BuildType:      pkg.Build.Type,
	}

	// Version override can change build type
	if ver != nil && ver.Build != nil && ver.Build.Type != "" {
		out.BuildType = ver.Build.Type
	}

	// Layer 1: package base
	out.CMakeDefines = slices.Clone(pkg.Build.CMake.Defines)
	out.AutomakeArgs = slices.Clone(pkg.Build.Automake.Args)
	out.CXXFlags = slices.Clone(pkg.Build.CMake.CXXFlags)
	out.CXXFlags = append(out.CXXFlags, pkg.Build.Automake.CXXFlags...)
	out.LinkFlags = slices.Clone(pkg.Build.CMake.LinkFlags)
	out.LinkFlags = append(out.LinkFlags, pkg.Build.Automake.LinkFlags...)

	// Layer 2: profile
	if po, ok := pkg.Build.Profiles[profile]; ok {
		out.CMakeDefines = append(out.CMakeDefines, po.Defines...)
		out.CXXFlags = append(out.CXXFlags, po.CXXFlags...)
		out.LinkFlags = append(out.LinkFlags, po.LinkFlags...)
	}

	// Layer 3: compiler
	if co, ok := pkg.Build.Compiler[toolchainKind]; ok {
		out.CMakeDefines = append(out.CMakeDefines, co.Defines...)
		out.CXXFlags = append(out.CXXFlags, co.CXXFlags...)
		out.LinkFlags = append(out.LinkFlags, co.LinkFlags...)
	}

	// Layer 4: platform
	if po, ok := pkg.Build.Platform[goos]; ok {
		out.CMakeDefines = append(out.CMakeDefines, po.Defines...)
		out.CXXFlags = append(out.CXXFlags, po.CXXFlags...)
		out.LinkFlags = append(out.LinkFlags, po.LinkFlags...)
	}

	// Layer 5: version-specific override
	if ver != nil {
		if ver.Build != nil {
			if ver.Build.CMake != nil {
				if len(ver.Build.CMake.Defines) > 0 {
					out.CMakeDefines = slices.Clone(ver.Build.CMake.Defines)
				}
				out.CXXFlags = append(out.CXXFlags, ver.Build.CMake.CXXFlags...)
				if len(ver.Build.CMake.LinkFlags) > 0 {
					out.LinkFlags = slices.Clone(ver.Build.CMake.LinkFlags)
				}
				if len(ver.Build.CMake.InstallTargets) > 0 {
					out.InstallTargets = slices.Clone(ver.Build.CMake.InstallTargets)
				}
			}
			if ver.Build.Automake != nil {
				if len(ver.Build.Automake.Args) > 0 {
					out.AutomakeArgs = slices.Clone(ver.Build.Automake.Args)
				}
				out.CXXFlags = append(out.CXXFlags, ver.Build.Automake.CXXFlags...)
				if len(ver.Build.Automake.LinkFlags) > 0 {
					out.LinkFlags = slices.Clone(ver.Build.Automake.LinkFlags)
				}
			}
			if co, ok := ver.Build.Compiler[toolchainKind]; ok {
				out.CMakeDefines = append(out.CMakeDefines, co.Defines...)
				out.CXXFlags = append(out.CXXFlags, co.CXXFlags...)
				out.LinkFlags = append(out.LinkFlags, co.LinkFlags...)
			}
		}
		out.Patch = ver.Patch
	}

	return out
}
