package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/all3n/cstow/internal/abi"
	"github.com/all3n/cstow/internal/builder"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/all3n/cstow/internal/toolchain"
)

type repositoryInstallContext struct {
	Global     *config.Global
	Toolchain  *toolchain.Toolchain
	Std        string
	Profile    string
	ExtraRepos []string
}

type repositoryInstallOptions struct {
	Context     *repositoryInstallContext
	BuildType   string
	Force       bool
	ForceShared bool // parent is shared, force all transitive deps to shared
	Stdout      io.Writer
	Stderr      io.Writer
	trace       *repositoryInstallTrace
}

type repositoryInstallResult struct {
	InstallDir string
	Version    string
	ABITag     string
	BuildType  string
	RepoPath   string
	Cached     bool
}

type repositoryInstallTrace struct {
	active map[string]int
	stack  []string
}

func newRepositoryInstallTrace() *repositoryInstallTrace {
	return &repositoryInstallTrace{
		active: make(map[string]int),
	}
}

func (t *repositoryInstallTrace) push(node string) error {
	if idx, ok := t.active[node]; ok {
		cycle := append(append([]string{}, t.stack[idx:]...), node)
		return fmt.Errorf("repository dependency cycle detected: %s", strings.Join(cycle, " -> "))
	}
	t.active[node] = len(t.stack)
	t.stack = append(t.stack, node)
	return nil
}

func (t *repositoryInstallTrace) pop(node string) {
	if t == nil || len(t.stack) == 0 {
		return
	}
	delete(t.active, node)
	t.stack = t.stack[:len(t.stack)-1]
}

func repositoryInstallNode(name, version, buildType string) string {
	return fmt.Sprintf("%s@%s[%s]", name, version, displayBuildType(buildType))
}

func wrapRepositoryInstallStage(node, stage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("repository package %s: %s: %w", node, stage, err)
}

func wrapGitInstallStage(node, stage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("git dependency %s: %s: %w", node, stage, err)
}

type gitSourceOptions struct {
	Context   *repositoryInstallContext
	BuildType string
	CMake     config.GitCMake
	Stdout    io.Writer
	Stderr    io.Writer
}

type gitSourceResult struct {
	InstallDir string
	Version    string
	ABITag     string
	BuildType  string
	Cached     bool
}

// findProjectRoot walks up from cwd to find the directory containing
// cstow.toml or .git, returning "" if none found.
func findProjectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "cstow.toml")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func newRepositoryInstallContext(projectCfg *config.Config, profile, toolchainName string, extraRepos []string) (*repositoryInstallContext, error) {
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
		Global:     g,
		Toolchain:  tc,
		Std:        std,
		Profile:    resolvedProfile,
		ExtraRepos: extraRepos,
	}, nil
}

func (c *repositoryInstallContext) detectedABITag() string {
	if c == nil || c.Toolchain == nil {
		return ""
	}
	return abi.DetectFromToolchain(c.Toolchain.Kind, c.Toolchain.Version, c.Std).String()
}

func (c *repositoryInstallContext) repositoryPaths(projectRoot string) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	basePaths, err := configuredRepositoryPaths(c.Global, projectRoot)
	if err != nil {
		return nil, err
	}
	return mergeRepositoryPaths(basePaths, c.ExtraRepos), nil
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
	if opts.trace == nil {
		opts.trace = newRepositoryInstallTrace()
	}

	repoPaths, err := opts.Context.repositoryPaths(findProjectRoot())
	if err != nil {
		return nil, fmt.Errorf("resolve repository paths: %w", err)
	}
	finder := repository.NewFinderWithPaths(repoPaths)
	pkg, err := finder.Find(name, versionConstraint)
	if err != nil {
		return nil, err
	}

	merged := repository.Merge(pkg.Def, pkg.Override, opts.Context.Toolchain.Kind, opts.Context.Profile, runtime.GOOS)
	merged = applyGlobalBuildFlagsToMergedConfig(merged, opts.Context.Global)
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

	node := repositoryInstallNode(name, pkg.Version, buildType)
	if err := opts.trace.push(node); err != nil {
		return nil, err
	}
	defer opts.trace.pop(node)

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
		return nil, fmt.Errorf("repository package %s: build prerequisites: cmake not found on PATH", node)
	}

	tmpDir, err := os.MkdirTemp("", "cstow-build-*")
	if err != nil {
		return nil, wrapRepositoryInstallStage(node, "create temp dir", err)
	}
	defer os.RemoveAll(tmpDir)

	sourcePath, err := repository.FetchSourceWithOptions(pkg.Def.Source, pkg.Override, pkg.Version, expectedSHA256(pkg), tmpDir, repository.FetchOptions{
		Network: globalNetworkConfig(opts.Context.Global),
	})
	if err != nil {
		return nil, wrapRepositoryInstallStage(node, "fetch source", err)
	}

	if merged.Patch != "" {
		if !builder.IsPatchInstalled() {
			return nil, fmt.Errorf("repository package %s: apply patch %s: patch command not found on PATH", node, merged.Patch)
		}
		patchPath := filepath.Join(pkg.PackageDir, "patches", merged.Patch)
		fmt.Fprintf(opts.Stdout, ">> applying patch %s\n", merged.Patch)
		if err := builder.ApplyPatch(patchPath, sourcePath); err != nil {
			return nil, wrapRepositoryInstallStage(node, "apply patch "+merged.Patch, err)
		}
	}

	var prefixPaths []string
	if len(pkg.Def.Deps) > 0 {
		fmt.Fprintf(opts.Stdout, ">> resolving %d dependencies for %s\n", len(pkg.Def.Deps), name)
		for _, dep := range pkg.Def.Deps {
			depBuildType := normalizeBuildType(dep.BuildType)
			if depBuildType == "" {
				depBuildType = "static"
			}
			// When the parent is shared, force dependencies to shared too
			// so they are built with -fPIC and can be linked into a shared library.
			// This propagates transitively through the ForceShared field.
			if merged.BuildType == "shared" || opts.ForceShared {
				depBuildType = "shared"
			}
			depOpts := opts
			depOpts.BuildType = depBuildType
			depOpts.ForceShared = merged.BuildType == "shared"
			depOpts.Force = false // default cache-first for dependencies
			depResult, err := installFromRepository(dep.Name, dep.Version, depOpts)
			if err != nil {
				return nil, wrapRepositoryInstallStage(node, fmt.Sprintf("dependency %s@%s", dep.Name, dep.Version), err)
			}
			prefixPaths = append(prefixPaths, depResult.InstallDir)
		}
	}

	result, err := builder.Build(builder.Options{
		SourcePath:  sourcePath,
		InstallDir:  installDir,
		Config:      merged,
		Toolchain:   opts.Context.Toolchain,
		Profile:     opts.Context.Profile,
		Jobs:        builder.GuessJobs(),
		PrefixPaths: prefixPaths,
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
	})
	if err != nil {
		return nil, wrapRepositoryInstallStage(node, "build", err)
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

func installFromGitSource(name, version, gitURL, rev string, opts gitSourceOptions) (*gitSourceResult, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	buildType := normalizeBuildType(opts.BuildType)
	if buildType == "" {
		buildType = "static"
	}
	if err := validateBuildType(buildType); err != nil {
		return nil, err
	}
	node := repositoryInstallNode(name, version, buildType)

	abiTag := opts.Context.detectedABITag()
	cache := resolver.NewFSCache()
	installDir := cache.Path(name, version, abiTag, buildType)

	if resolvedPath, resolvedABITag, ok := findCachedPackage(cache, name, version, []string{abiTag}, buildType); ok {
		if err := indexSuccessfulArtifact(cache, indexedArtifact{
			Name:       name,
			Version:    version,
			ABITag:     resolvedABITag,
			BuildType:  buildType,
			InstallDir: resolvedPath,
			Origin:     "git",
		}); err != nil {
			return nil, err
		}
		return &gitSourceResult{
			InstallDir: resolvedPath,
			Version:    version,
			ABITag:     resolvedABITag,
			BuildType:  buildType,
			Cached:     true,
		}, nil
	}

	tmpDir, err := os.MkdirTemp("", "cstow-git-*")
	if err != nil {
		return nil, wrapGitInstallStage(node, "create temp dir", err)
	}
	defer os.RemoveAll(tmpDir)

	if rev == "" {
		rev = "main"
	}
	if err := fetchGitCloneFunc(gitURL, rev, tmpDir); err != nil {
		return nil, wrapGitInstallStage(node, fmt.Sprintf("clone source from %s@%s", gitURL, rev), err)
	}

	merged := &repository.MergedBuildConfig{
		System:       "cmake",
		BuildType:    buildType,
		CMakeDefines: opts.CMake.Defines,
		CXXFlags:     opts.CMake.CXXFlags,
		LinkFlags:    opts.CMake.LinkFlags,
	}
	merged = applyGlobalBuildFlagsToMergedConfig(merged, opts.Context.Global)

	result, err := builder.Build(builder.Options{
		SourcePath: tmpDir,
		InstallDir: installDir,
		Config:     merged,
		Toolchain:  opts.Context.Toolchain,
		Profile:    opts.Context.Profile,
		Jobs:       builder.GuessJobs(),
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
	})
	if err != nil {
		return nil, wrapGitInstallStage(node, "build", err)
	}

	if err := indexSuccessfulArtifact(cache, indexedArtifact{
		Name:       name,
		Version:    version,
		ABITag:     abiTag,
		BuildType:  buildType,
		InstallDir: result.InstallDir,
		Origin:     "git",
	}); err != nil {
		return nil, err
	}

	return &gitSourceResult{
		InstallDir: result.InstallDir,
		Version:    version,
		ABITag:     abiTag,
		BuildType:  buildType,
	}, nil
}

func gitCMakeFromLock(cfg *config.Config, name string) config.GitCMake {
	if cfg == nil {
		return config.GitCMake{}
	}
	for _, dep := range cfg.Dependencies {
		if dep.Name == name {
			return dep.CMake
		}
	}
	return config.GitCMake{}
}

func globalNetworkConfig(global *config.Global) *config.GlobalNetwork {
	if global == nil {
		return nil
	}
	return &global.Network
}

func applyGlobalBuildFlagsToMergedConfig(merged *repository.MergedBuildConfig, global *config.Global) *repository.MergedBuildConfig {
	if merged == nil {
		return nil
	}
	if global == nil {
		return merged
	}

	flags := global.Build.Flags
	if len(flags.Defines) == 0 && len(flags.CXXFlags) == 0 && len(flags.LinkFlags) == 0 {
		return merged
	}

	out := &repository.MergedBuildConfig{
		System:          merged.System,
		CMakeDefines:    append(slices.Clone(flags.Defines), merged.CMakeDefines...),
		AutotoolsArgs:   slices.Clone(merged.AutotoolsArgs),
		AutotoolsScript: merged.AutotoolsScript,
		CFlags:          slices.Clone(merged.CFlags),
		CXXFlags:        append(slices.Clone(flags.CXXFlags), merged.CXXFlags...),
		LinkFlags:       append(slices.Clone(flags.LinkFlags), merged.LinkFlags...),
		IncludeDirs:     slices.Clone(merged.IncludeDirs),
		Libs:            slices.Clone(merged.Libs),
		InstallTargets:  slices.Clone(merged.InstallTargets),
		Patch:           merged.Patch,
		BuildType:       merged.BuildType,
	}
	return out
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
		// When buildType is specified, only match exact build-type paths.
		// Do NOT fall back to legacy (buildType-less) paths.
		if buildType != "" {
			p := cache.Path(name, version, abiTag, buildType)
			if _, err := os.Stat(p); err == nil {
				return p, abiTag, true
			}
			continue
		}
		// No buildType specified: try legacy path first, then typed path.
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

func expectedSHA256(pkg *repository.ResolvedPkg) string {
	if pkg.Override != nil && pkg.Override.Source != nil && pkg.Override.Source.SHA256 != "" {
		return pkg.Override.Source.SHA256
	}
	if pkg.Def.Source.SHA256 != nil {
		return pkg.Def.Source.SHA256[pkg.Version]
	}
	return ""
}
