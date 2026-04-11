package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/flock"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/repository"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

type artifactDownloader interface {
	Download(ctx context.Context, pkg, version, abiTag, buildType, hashID string) ([]byte, error)
}

type fetchRegistryClient interface {
	artifactDownloader
	GetManifest(ctx context.Context, pkg, version string) (*registry.Manifest, error)
}

var fetchNewRegistryClient = func(ctx context.Context, reg config.Registry) (fetchRegistryClient, error) {
	return registry.NewS3Client(ctx, reg)
}

// fetchGitCloneFunc allows tests to mock git clone operations.
var fetchGitCloneFunc = repository.FetchGit

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch dependencies into local cache, falling back to source builds when needed",
	RunE: func(cmd *cobra.Command, args []string) error {
		defer resetFetchFlagState(cmd)
		defer resetRootFlagState(cmd)

		artifactHashID, _ := cmd.Flags().GetString("artifact")
		if strings.TrimSpace(artifactHashID) != "" {
			return fetchByHashID(cmd, artifactHashID)
		}

		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		profile, _ := cmd.Flags().GetString("profile")
		toolchainName, _ := cmd.Flags().GetString("toolchain")
		sourceFallback, _ := cmd.Flags().GetBool("source-fallback")

		return runFetch(cfg, fetchOptions{
			Profile:        profile,
			Toolchain:      toolchainName,
			SourceFallback: sourceFallback,
			Stdout:         cmd.OutOrStdout(),
			Stderr:         cmd.ErrOrStderr(),
		})
	},
}

type fetchOptions struct {
	WorkDir          string
	LockPath         string
	DepsDir          string
	ExtraRepos       []string
	OverrideRegistry string
	Profile          string
	Toolchain        string
	SourceFallback   bool
	Stdout           io.Writer
	Stderr           io.Writer
}

func runFetch(cfg *config.Config, opts fetchOptions) error {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "."
	}
	lockPath := opts.LockPath
	if lockPath == "" {
		lockPath = filepath.Join(workDir, "cstow.lock")
	}
	depsDir := opts.DepsDir
	if depsDir == "" {
		depsDir = filepath.Join(workDir, "cstow_deps")
	}

	// Persistent flags (from rootCmd)
	extraRepos := opts.ExtraRepos
	if len(extraRepos) == 0 {
		extraRepos, _ = rootCmd.PersistentFlags().GetStringSlice("repository")
	}
	overrideRegistry := opts.OverrideRegistry
	if overrideRegistry == "" {
		overrideRegistry, _ = rootCmd.PersistentFlags().GetString("registry")
	}

	// Acquire file lock for the project's dependency operations.
	// We use a hidden .cstow.lock.lock file to manage access to cstow.lock and cstow_deps.
	projectLock := flock.New(filepath.Join(filepath.Dir(lockPath), ".cstow.lock.lock"))
	if err := projectLock.Lock(5 * time.Minute); err != nil {
		return fmt.Errorf("acquire project lock: %w", err)
	}
	defer projectLock.Unlock()

	lf, err := resolver.LoadLock(lockPath)
	if err != nil {
		r := resolver.New(resolver.NewFSCache(), nil)
		lf, err = r.Resolve(cfg.Dependencies)
		if err != nil {
			return fmt.Errorf("resolve dependencies: %w", err)
		}
		if err := resolver.SaveLock(lockPath, lf); err != nil {
			return fmt.Errorf("save lock file: %w", err)
		}
	}

	if len(lf.Packages) == 0 {
		fmt.Fprintln(opts.Stdout, "No dependencies to fetch")
		return nil
	}

	cache := resolver.NewFSCache()

	var s3client fetchRegistryClient
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}

	reg, regErr := config.ResolvePrimaryRegistry(cfg.Registries, globalCfg)
	if regErr == nil {
		if overrideRegistry != "" {
			reg.URL = overrideRegistry
		}
		s3client, err = fetchNewRegistryClient(context.Background(), reg)
		if err != nil {
			return fmt.Errorf("create S3 client: %w", err)
		}
	} else if overrideRegistry != "" {
		s3client, err = fetchNewRegistryClient(context.Background(), config.Registry{URL: overrideRegistry})
		if err != nil {
			return fmt.Errorf("create S3 client from override: %w", err)
		}
	}

	var installCtx *repositoryInstallContext
	var installCtxErr error
	ensureInstallCtx := func() (*repositoryInstallContext, error) {
		if installCtx != nil || installCtxErr != nil {
			return installCtx, installCtxErr
		}
		// Pass repo overrides to repository context
		installCtx, installCtxErr = newRepositoryInstallContext(cfg, opts.Profile, opts.Toolchain, extraRepos)
		return installCtx, installCtxErr
	}

	detectedABITag := ""
	if ctx, err := ensureInstallCtx(); err == nil {
		detectedABITag = ctx.detectedABITag()
	}

	depPaths := make(map[string]string, len(lf.Packages))
	lockDirty := false

	for i := range lf.Packages {
		pkg := &lf.Packages[i]
		abiTags := candidateABITags(pkg.ABITag, detectedABITag)
		buildType := fetchBuildType(pkg.Name, *pkg, cfg)
		if pkg.BuildType != buildType {
			pkg.BuildType = buildType
			lockDirty = true
		}

		if path, resolvedABITag, ok := findCachedPackage(cache, pkg.Name, pkg.Version, abiTags, buildType); ok {
			if err := indexSuccessfulArtifact(cache, indexedArtifact{
				Name:       pkg.Name,
				Version:    pkg.Version,
				ABITag:     resolvedABITag,
				BuildType:  buildType,
				InstallDir: path,
				Origin:     "unknown",
			}); err != nil {
				return err
			}
			depPaths[pkg.Name] = dependencyLinkTarget(*pkg, path)
			if pkg.ABITag != resolvedABITag {
				pkg.ABITag = resolvedABITag
				lockDirty = true
			}
			fmt.Fprintf(opts.Stdout, "  [cached] %s@%s (%s, %s)\n", pkg.Name, pkg.Version, resolvedABITag, displayBuildType(buildType))
			continue
		}

		if strings.HasPrefix(pkg.Source, "git:") && pkg.Git != "" {
			ctx, err := ensureInstallCtx()
			if err != nil {
				return fmt.Errorf("prepare git build for %s@%s: %w", pkg.Name, pkg.Version, err)
			}

			result, err := installFromGitSource(pkg.Name, pkg.Version, pkg.Git, pkg.Rev, gitSourceOptions{
				Context:   ctx,
				BuildType: buildType,
				CMake:     gitCMakeFromLock(cfg, pkg.Name),
				Stdout:    opts.Stdout,
				Stderr:    opts.Stderr,
			})
			if err != nil {
				return fmt.Errorf("git source build for %s@%s: %w", pkg.Name, pkg.Version, err)
			}

			depPaths[pkg.Name] = result.InstallDir
			if pkg.ABITag != result.ABITag {
				pkg.ABITag = result.ABITag
				lockDirty = true
			}
			if pkg.BuildType != result.BuildType {
				pkg.BuildType = result.BuildType
				lockDirty = true
			}
			if result.Cached {
				fmt.Fprintf(opts.Stdout, "  [cached-git] %s@%s (%s, %s)\n", pkg.Name, result.Version, result.ABITag, result.BuildType)
			} else {
				fmt.Fprintf(opts.Stdout, "  [built-git] %s@%s (%s) -> %s\n", pkg.Name, result.Version, result.BuildType, result.InstallDir)
			}
			continue
		}

		if s3client != nil && !strings.HasPrefix(pkg.Source, "local") {
			fmt.Fprintf(opts.Stdout, "  [fetch] %s@%s ...\n", pkg.Name, pkg.Version)

			var data []byte
			var fetchedABITag string
			var fetchedBuildType string
			var fetchedHashID string
			var fetchedBuildTags []string
			var downloadErr error

			manifest, manifestErr := s3client.GetManifest(context.Background(), pkg.Name, pkg.Version)
			if manifestErr == nil {
				artifact, selectErr := registry.SelectArtifact(manifest, abiTags, buildType)
				if selectErr != nil {
					downloadErr = selectErr
				} else {
					data, downloadErr = downloadFromManifestArtifact(context.Background(), s3client, pkg.Name, pkg.Version, *artifact)
					if downloadErr == nil {
						fetchedABITag = artifact.ABITag
						fetchedBuildType = artifact.BuildType
						fetchedHashID = artifact.HashID
						fetchedBuildTags = artifact.BuildTags
					}
				}
			} else {
				for _, abiTag := range abiTags {
					data, downloadErr = s3client.Download(context.Background(), pkg.Name, pkg.Version, abiTag, buildType, "")
					if downloadErr == nil {
						fetchedABITag = abiTag
						fetchedBuildType = buildType
						break
					}
				}
			}

			if downloadErr == nil {
				if err := verifyArtifactHash(data, fetchedHashID); err != nil {
					return fmt.Errorf("integrity check failed for %s@%s: %w", pkg.Name, pkg.Version, err)
				}
				destDir := cache.Path(pkg.Name, pkg.Version, fetchedABITag, fetchedBuildType)
				if err := os.MkdirAll(destDir, 0o755); err != nil {
					return fmt.Errorf("create cache dir: %w", err)
				}
				if err := pack.ExtractTarZst(data, destDir); err != nil {
					return fmt.Errorf("extract %s@%s: %w", pkg.Name, pkg.Version, err)
				}

				if err := indexSuccessfulArtifact(cache, indexedArtifact{
					Name:       pkg.Name,
					Version:    pkg.Version,
					ABITag:     fetchedABITag,
					BuildType:  fetchedBuildType,
					HashID:     fetchedHashID,
					BuildTags:  fetchedBuildTags,
					InstallDir: destDir,
					Origin:     "registry",
				}); err != nil {
					return err
				}
				depPaths[pkg.Name] = destDir
				if pkg.ABITag != fetchedABITag {
					pkg.ABITag = fetchedABITag
					lockDirty = true
				}
				fmt.Fprintf(opts.Stdout, "  [done]  %s@%s (%s) -> %s\n", pkg.Name, pkg.Version, displayBuildType(fetchedBuildType), destDir)
				continue
			}

			fmt.Fprintf(opts.Stdout, "  [warn] prebuilt artifact unavailable for %s@%s, trying source fallback\n", pkg.Name, pkg.Version)
		}

		if strings.HasPrefix(pkg.Source, "local") && pkg.Path != "" {
			depPaths[pkg.Name] = pkg.Path
			fmt.Fprintf(opts.Stdout, "  [local] %s@%s -> %s\n", pkg.Name, pkg.Version, pkg.Path)
			continue
		}

		if !opts.SourceFallback {
			fmt.Fprintf(opts.Stdout, "  [skip] %s@%s (source fallback disabled)\n", pkg.Name, pkg.Version)
			continue
		}

		ctx, err := ensureInstallCtx()
		if err != nil {
			return fmt.Errorf("prepare source fallback for %s@%s: %w", pkg.Name, pkg.Version, err)
		}

		result, err := installFromRepository(pkg.Name, pkg.Version, repositoryInstallOptions{
			Context:   ctx,
			BuildType: buildType,
			Stdout:    opts.Stdout,
			Stderr:    opts.Stderr,
		})
		if err != nil {
			return fmt.Errorf("source fallback for %s@%s: %w", pkg.Name, pkg.Version, err)
		}

		depPaths[pkg.Name] = result.InstallDir
		if pkg.ABITag != result.ABITag {
			pkg.ABITag = result.ABITag
			lockDirty = true
		}
		if pkg.BuildType != result.BuildType {
			pkg.BuildType = result.BuildType
			lockDirty = true
		}
		if result.Cached {
			fmt.Fprintf(opts.Stdout, "  [cached-source] %s@%s (%s, %s)\n", pkg.Name, result.Version, result.ABITag, result.BuildType)
		} else {
			fmt.Fprintf(opts.Stdout, "  [built] %s@%s (%s) -> %s\n", pkg.Name, result.Version, result.BuildType, result.InstallDir)
		}
	}

	if lockDirty {
		if err := resolver.SaveLock(lockPath, lf); err != nil {
			return fmt.Errorf("save updated lock file: %w", err)
		}
	}

	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		return fmt.Errorf("create deps dir: %w", err)
	}

	var prefixPaths []string
	for _, pkg := range lf.Packages {
		src, ok := depPaths[pkg.Name]
		if !ok {
			fmt.Fprintf(opts.Stdout, "  [warn] no dependency path available for %s@%s\n", pkg.Name, pkg.Version)
			continue
		}
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(opts.Stdout, "  [warn] skip %s@%s: %v\n", pkg.Name, pkg.Version, err)
			continue
		}

		dst := filepath.Join(depsDir, pkg.Name)
		_ = os.Remove(dst)
		// Compute symlink target relative to depsDir so the link resolves correctly
		linkTarget := src
		if rel, err := filepath.Rel(depsDir, src); err == nil {
			linkTarget = rel
		}
		if err := os.Symlink(linkTarget, dst); err != nil {
			fmt.Fprintf(opts.Stdout, "  [warn] symlink %s: %v\n", pkg.Name, err)
			continue
		}
		prefixPaths = append(prefixPaths, dst)
	}

	if len(prefixPaths) > 0 {
		fmt.Fprintf(opts.Stdout, "\n  CMAKE_PREFIX_PATH=%s\n", strings.Join(prefixPaths, string(filepath.ListSeparator)))
	}

	// 6. Automatic cache pruning (background-like, only if limits are set)
	if globalCfg.Cache.MaxSizeGB > 0 || globalCfg.Cache.RetentionDays > 0 {
		if store, err := artifactdb.OpenDefault(); err == nil {
			defer store.Close()
			_, _ = store.Prune(globalCfg.Cache.RetentionDays, globalCfg.Cache.MaxSizeGB, false)
		}
	}

	return nil
}

func init() {
	fetchCmd.Flags().String("toolchain", "auto", "compiler to use for ABI detection and source fallback (auto|gcc|clang|msvc)")
	fetchCmd.Flags().Bool("source-fallback", true, "build from repository source when prebuilt artifacts are unavailable")
	fetchCmd.Flags().String("artifact", "", "fetch an artifact by hash_id (or unique prefix)")
	rootCmd.AddCommand(fetchCmd)
}

func displayBuildType(buildType string) string {
	if buildType == "" {
		return "default"
	}
	return buildType
}

func downloadFromManifestArtifact(ctx context.Context, downloader artifactDownloader, name, version string, artifact registry.Artifact) ([]byte, error) {
	return downloader.Download(ctx, name, version, artifact.ABITag, artifact.BuildType, artifact.HashID)
}

func resetFetchFlagState(cmd *cobra.Command) {
	resetFlagState(cmd, "artifact")
	resetFlagState(cmd, "toolchain")
	resetFlagState(cmd, "source-fallback")
}
