package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/all3n/cstow/internal/abi"
	"github.com/all3n/cstow/internal/builder"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/all3n/cstow/internal/toolchain"
)

type repositoryInstallContext struct {
	Global    *config.Global
	Toolchain *toolchain.Toolchain
	Std       string
	Profile   string
}

type repositoryInstallOptions struct {
	Context   *repositoryInstallContext
	BuildType string
	Force     bool
	Stdout    io.Writer
	Stderr    io.Writer
}

type repositoryInstallResult struct {
	InstallDir string
	Version    string
	ABITag     string
	BuildType  string
	RepoPath   string
	Cached     bool
}

func newRepositoryInstallContext(projectCfg *config.Config, profile, toolchainName string) (*repositoryInstallContext, error) {
	g, err := config.LoadGlobal()
	if err != nil {
		return nil, fmt.Errorf("load global config: %w", err)
	}

	resolvedProfile := profile
	if resolvedProfile == "" {
		resolvedProfile = g.Defaults.Profile
	}
	if resolvedProfile == "" {
		resolvedProfile = "debug"
	}

	std := g.Defaults.Std
	if std == "" {
		std = "c++17"
	}
	if projectCfg != nil && projectCfg.Package.Std != "" {
		std = projectCfg.Package.Std
	}

	tcCfg := config.Toolchain{
		Compiler: g.Toolchain.Prefer,
	}
	if projectCfg != nil && projectCfg.Toolchain.Compiler != "" {
		tcCfg.Compiler = projectCfg.Toolchain.Compiler
	}
	if toolchainName != "" && toolchainName != "auto" {
		tcCfg.Compiler = toolchainName
	}

	tc, err := toolchain.Detect(&tcCfg)
	if err != nil {
		return nil, fmt.Errorf("detect toolchain: %w", err)
	}

	return &repositoryInstallContext{
		Global:    g,
		Toolchain: tc,
		Std:       std,
		Profile:   resolvedProfile,
	}, nil
}

func (c *repositoryInstallContext) detectedABITag() string {
	if c == nil || c.Toolchain == nil {
		return ""
	}
	return abi.DetectFromToolchain(c.Toolchain.Kind, c.Toolchain.Version, c.Std).String()
}

func installFromRepository(name, versionConstraint string, opts repositoryInstallOptions) (*repositoryInstallResult, error) {
	if opts.Context == nil {
		return nil, fmt.Errorf("repository install context is required")
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	finder := repository.NewFinderWithPaths(opts.Context.Global.RepositoryPaths())
	pkg, err := finder.Find(name, versionConstraint)
	if err != nil {
		return nil, err
	}

	merged := repository.Merge(pkg.Def, pkg.Override, opts.Context.Toolchain.Kind, opts.Context.Profile, runtime.GOOS)
	if opts.BuildType != "" {
		merged.BuildType = opts.BuildType
	}
	buildType := normalizeBuildType(merged.BuildType)
	if buildType == "" {
		buildType = "static"
	}
	if err := validateBuildType(buildType); err != nil {
		return nil, err
	}
	merged.BuildType = buildType

	abiTag := opts.Context.detectedABITag()
	cache := resolver.NewFSCache()
	installDir := cache.Path(name, pkg.Version, abiTag, buildType)
	if !opts.Force {
		if resolvedPath, resolvedABITag, ok := findCachedPackage(cache, name, pkg.Version, []string{abiTag}, buildType); ok {
			if err := indexSuccessfulArtifact(cache, indexedArtifact{
				Name:       name,
				Version:    pkg.Version,
				ABITag:     resolvedABITag,
				BuildType:  buildType,
				InstallDir: resolvedPath,
				Origin:     "repository",
			}); err != nil {
				return nil, err
			}
			return &repositoryInstallResult{
				InstallDir: resolvedPath,
				Version:    pkg.Version,
				ABITag:     resolvedABITag,
				BuildType:  buildType,
				RepoPath:   pkg.RepoPath,
				Cached:     true,
			}, nil
		}
	}

	if merged.BuildType != "header-only" && !builder.IsCmakeInstalled() {
		return nil, fmt.Errorf("cmake not found on PATH")
	}

	tmpDir, err := os.MkdirTemp("", "cstow-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	sourcePath, err := repository.FetchSource(pkg.Def.Source, pkg.Version, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("fetch source: %w", err)
	}

	result, err := builder.Build(builder.Options{
		SourcePath: sourcePath,
		InstallDir: installDir,
		Config:     merged,
		Toolchain:  opts.Context.Toolchain,
		Profile:    opts.Context.Profile,
		Jobs:       builder.GuessJobs(),
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("build %s %s: %w", name, pkg.Version, err)
	}

	if err := indexSuccessfulArtifact(cache, indexedArtifact{
		Name:       name,
		Version:    pkg.Version,
		ABITag:     abiTag,
		BuildType:  buildType,
		InstallDir: result.InstallDir,
		Origin:     "repository",
	}); err != nil {
		return nil, err
	}

	return &repositoryInstallResult{
		InstallDir: result.InstallDir,
		Version:    pkg.Version,
		ABITag:     abiTag,
		BuildType:  buildType,
		RepoPath:   pkg.RepoPath,
	}, nil
}

func candidateABITags(lockTag, detectedTag string) []string {
	var tags []string
	appendUnique := func(tag string) {
		if tag == "" {
			return
		}
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}

	appendUnique(lockTag)
	appendUnique(detectedTag)
	appendUnique("default")
	return tags
}

func findCachedPackage(cache resolver.LocalCache, name, version string, abiTags []string, buildType string) (string, string, bool) {
	for _, abiTag := range abiTags {
		if !cache.Has(name, version, abiTag, buildType) {
			continue
		}
		newPath := cache.Path(name, version, abiTag, buildType)
		if _, err := os.Stat(newPath); err == nil {
			return newPath, abiTag, true
		}
		legacyPath := cache.LegacyPath(name, version, abiTag)
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath, abiTag, true
		}
	}
	return "", "", false
}

func desiredBuildType(name string, pkg resolver.LockEntry, cfg *config.Config) string {
	if buildType := configuredBuildType(name, pkg, cfg); buildType != "" {
		return buildType
	}
	return "static"
}

func configuredBuildType(name string, pkg resolver.LockEntry, cfg *config.Config) string {
	if buildType := normalizeBuildType(pkg.BuildType); buildType != "" {
		return buildType
	}
	if cfg != nil {
		for _, dep := range cfg.Dependencies {
			if dep.Name == name {
				if buildType := normalizeBuildType(dep.BuildType); buildType != "" {
					return buildType
				}
				break
			}
		}
	}
	return ""
}

func fetchBuildType(name string, pkg resolver.LockEntry, cfg *config.Config) string {
	return configuredBuildType(name, pkg, cfg)
}

func lockEntryByName(lf *resolver.LockFile, name string) (resolver.LockEntry, bool) {
	if lf == nil {
		return resolver.LockEntry{}, false
	}
	for _, pkg := range lf.Packages {
		if pkg.Name == name {
			return pkg, true
		}
	}
	return resolver.LockEntry{}, false
}

func normalizeBuildType(buildType string) string {
	switch buildType {
	case "static", "shared", "header-only":
		return buildType
	default:
		return ""
	}
}

func validateBuildType(buildType string) error {
	if normalizeBuildType(buildType) == "" {
		return fmt.Errorf("unsupported build type %q (supported: static, shared, header-only)", buildType)
	}
	return nil
}

func dependencyLinkTarget(pkg resolver.LockEntry, cachePath string) string {
	if strings.HasPrefix(pkg.Source, "local") && pkg.Path != "" {
		return pkg.Path
	}
	return cachePath
}
