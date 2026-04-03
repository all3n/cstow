package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch dependencies into local cache, falling back to source builds when needed",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		lockPath := "cstow.lock"
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
			fmt.Println("No dependencies to fetch")
			return nil
		}

		profile, _ := cmd.Flags().GetString("profile")
		toolchainName, _ := cmd.Flags().GetString("toolchain")
		sourceFallback, _ := cmd.Flags().GetBool("source-fallback")

		cache := resolver.NewFSCache()

		var s3client *registry.S3Client
		globalCfg, err := config.LoadGlobal()
		if err != nil {
			return fmt.Errorf("load global config: %w", err)
		}
		if reg, regErr := config.ResolvePrimaryRegistry(cfg.Registries, globalCfg); regErr == nil {
			s3client, err = registry.NewS3Client(context.Background(), reg)
			if err != nil {
				return fmt.Errorf("create S3 client: %w", err)
			}
		}

		var installCtx *repositoryInstallContext
		var installCtxErr error
		ensureInstallCtx := func() (*repositoryInstallContext, error) {
			if installCtx != nil || installCtxErr != nil {
				return installCtx, installCtxErr
			}
			installCtx, installCtxErr = newRepositoryInstallContext(cfg, profile, toolchainName)
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
				fmt.Printf("  [cached] %s@%s (%s, %s)\n", pkg.Name, pkg.Version, resolvedABITag, displayBuildType(buildType))
				continue
			}

			if s3client != nil && !strings.HasPrefix(pkg.Source, "local") {
				fmt.Printf("  [fetch] %s@%s ...\n", pkg.Name, pkg.Version)

				var data []byte
				var fetchedABITag string
				var fetchedBuildType string
				var downloadErr error

				manifest, manifestErr := s3client.GetManifest(context.Background(), pkg.Name, pkg.Version)
				if manifestErr == nil {
					artifact, selectErr := registry.SelectArtifact(manifest, abiTags, buildType)
					if selectErr != nil {
						downloadErr = selectErr
					} else {
						data, downloadErr = s3client.Download(context.Background(), pkg.Name, pkg.Version, artifact.ABITag, artifact.BuildType)
						if downloadErr == nil {
							fetchedABITag = artifact.ABITag
							fetchedBuildType = artifact.BuildType
						}
					}
				} else {
					for _, abiTag := range abiTags {
						data, downloadErr = s3client.Download(context.Background(), pkg.Name, pkg.Version, abiTag, buildType)
						if downloadErr == nil {
							fetchedABITag = abiTag
							fetchedBuildType = buildType
							break
						}
					}
				}

				if downloadErr == nil {
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
					fmt.Printf("  [done]  %s@%s (%s) -> %s\n", pkg.Name, pkg.Version, displayBuildType(fetchedBuildType), destDir)
					continue
				}

				fmt.Printf("  [warn] prebuilt artifact unavailable for %s@%s, trying source fallback\n", pkg.Name, pkg.Version)
			}

			if strings.HasPrefix(pkg.Source, "local") && pkg.Path != "" {
				depPaths[pkg.Name] = pkg.Path
				fmt.Printf("  [local] %s@%s -> %s\n", pkg.Name, pkg.Version, pkg.Path)
				continue
			}

			if !sourceFallback {
				fmt.Printf("  [skip] %s@%s (source fallback disabled)\n", pkg.Name, pkg.Version)
				continue
			}

			ctx, err := ensureInstallCtx()
			if err != nil {
				return fmt.Errorf("prepare source fallback for %s@%s: %w", pkg.Name, pkg.Version, err)
			}

			result, err := installFromRepository(pkg.Name, pkg.Version, repositoryInstallOptions{
				Context:   ctx,
				BuildType: buildType,
				Stdout:    os.Stdout,
				Stderr:    os.Stderr,
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
				fmt.Printf("  [cached-source] %s@%s (%s, %s)\n", pkg.Name, result.Version, result.ABITag, result.BuildType)
			} else {
				fmt.Printf("  [built] %s@%s (%s) -> %s\n", pkg.Name, result.Version, result.BuildType, result.InstallDir)
			}
		}

		if lockDirty {
			if err := resolver.SaveLock(lockPath, lf); err != nil {
				return fmt.Errorf("save updated lock file: %w", err)
			}
		}

		depsDir := filepath.Join(".", "cstow_deps")
		if err := os.MkdirAll(depsDir, 0o755); err != nil {
			return err
		}

		var prefixPaths []string
		for _, pkg := range lf.Packages {
			src, ok := depPaths[pkg.Name]
			if !ok {
				fmt.Printf("  [warn] no dependency path available for %s@%s\n", pkg.Name, pkg.Version)
				continue
			}
			if _, err := os.Stat(src); err != nil {
				fmt.Printf("  [warn] skip %s@%s: %v\n", pkg.Name, pkg.Version, err)
				continue
			}

			dst := filepath.Join(depsDir, pkg.Name)
			_ = os.Remove(dst)
			if err := os.Symlink(src, dst); err != nil {
				fmt.Printf("  [warn] symlink %s: %v\n", pkg.Name, err)
				continue
			}
			prefixPaths = append(prefixPaths, dst)
		}

		if len(prefixPaths) > 0 {
			fmt.Printf("\n  CMAKE_PREFIX_PATH=%s\n", strings.Join(prefixPaths, string(filepath.ListSeparator)))
		}

		return nil
	},
}

func init() {
	fetchCmd.Flags().String("toolchain", "auto", "compiler to use for ABI detection and source fallback (auto|gcc|clang|msvc)")
	fetchCmd.Flags().Bool("source-fallback", true, "build from repository source when prebuilt artifacts are unavailable")
	rootCmd.AddCommand(fetchCmd)
}

func displayBuildType(buildType string) string {
	if buildType == "" {
		return "default"
	}
	return buildType
}
