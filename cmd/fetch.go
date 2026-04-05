package cmd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
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

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch dependencies into local cache, falling back to source builds when needed",
	RunE: func(cmd *cobra.Command, args []string) error {
		defer resetFetchFlagState(cmd)

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

		return runFetch(cfg, profile, toolchainName, sourceFallback, cmd.OutOrStdout(), cmd.ErrOrStderr())
	},
}

func runFetch(cfg *config.Config, profile, toolchainName string, sourceFallback bool, stdout, stderr io.Writer) error {
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
		fmt.Fprintln(stdout, "No dependencies to fetch")
		return nil
	}

	cache := resolver.NewFSCache()

	var s3client fetchRegistryClient
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}
	if reg, regErr := config.ResolvePrimaryRegistry(cfg.Registries, globalCfg); regErr == nil {
		s3client, err = fetchNewRegistryClient(context.Background(), reg)
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
			fmt.Fprintf(stdout, "  [cached] %s@%s (%s, %s)\n", pkg.Name, pkg.Version, resolvedABITag, displayBuildType(buildType))
			continue
		}

		if s3client != nil && !strings.HasPrefix(pkg.Source, "local") {
			fmt.Fprintf(stdout, "  [fetch] %s@%s ...\n", pkg.Name, pkg.Version)

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
				fmt.Fprintf(stdout, "  [done]  %s@%s (%s) -> %s\n", pkg.Name, pkg.Version, displayBuildType(fetchedBuildType), destDir)
				continue
			}

			fmt.Fprintf(stdout, "  [warn] prebuilt artifact unavailable for %s@%s, trying source fallback\n", pkg.Name, pkg.Version)
		}

		if strings.HasPrefix(pkg.Source, "local") && pkg.Path != "" {
			depPaths[pkg.Name] = pkg.Path
			fmt.Fprintf(stdout, "  [local] %s@%s -> %s\n", pkg.Name, pkg.Version, pkg.Path)
			continue
		}

		if !sourceFallback {
			fmt.Fprintf(stdout, "  [skip] %s@%s (source fallback disabled)\n", pkg.Name, pkg.Version)
			continue
		}

		ctx, err := ensureInstallCtx()
		if err != nil {
			return fmt.Errorf("prepare source fallback for %s@%s: %w", pkg.Name, pkg.Version, err)
		}

		result, err := installFromRepository(pkg.Name, pkg.Version, repositoryInstallOptions{
			Context:   ctx,
			BuildType: buildType,
			Stdout:    stdout,
			Stderr:    stderr,
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
			fmt.Fprintf(stdout, "  [cached-source] %s@%s (%s, %s)\n", pkg.Name, result.Version, result.ABITag, result.BuildType)
		} else {
			fmt.Fprintf(stdout, "  [built] %s@%s (%s) -> %s\n", pkg.Name, result.Version, result.BuildType, result.InstallDir)
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
			fmt.Fprintf(stdout, "  [warn] no dependency path available for %s@%s\n", pkg.Name, pkg.Version)
			continue
		}
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(stdout, "  [warn] skip %s@%s: %v\n", pkg.Name, pkg.Version, err)
			continue
		}

		dst := filepath.Join(depsDir, pkg.Name)
		_ = os.Remove(dst)
		if err := os.Symlink(src, dst); err != nil {
			fmt.Fprintf(stdout, "  [warn] symlink %s: %v\n", pkg.Name, err)
			continue
		}
		prefixPaths = append(prefixPaths, dst)
	}

	if len(prefixPaths) > 0 {
		fmt.Fprintf(stdout, "\n  CMAKE_PREFIX_PATH=%s\n", strings.Join(prefixPaths, string(filepath.ListSeparator)))
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

func fetchByHashID(cmd *cobra.Command, hashID string) error {
	hashID = strings.TrimSpace(hashID)
	if hashID == "" {
		return fmt.Errorf("hash_id must not be empty")
	}

	cache := resolver.NewFSCache()
	store, err := artifactdb.OpenDefault()
	if err != nil {
		return fmt.Errorf("open artifact db: %w", err)
	}
	defer store.Close()

	var staleCandidate *artifactdb.Record
	if rec, err := store.FindByHashID(hashID); err == nil {
		if pathExists(rec.InstallDir) {
			if err := indexSuccessfulArtifact(cache, indexedArtifact{
				Name:       rec.Name,
				Version:    rec.Version,
				ABITag:     rec.ABITag,
				BuildType:  rec.BuildType,
				HashID:     rec.HashID,
				BuildTags:  rec.BuildTags,
				InstallDir: rec.InstallDir,
				Origin:     rec.Origin,
			}); err != nil {
				return err
			}
			if err := linkFetchedArtifact(rec.Name, rec.InstallDir); err != nil {
				return err
			}
			fmt.Printf("  [cached] %s@%s (%s, %s)\n", rec.Name, rec.Version, rec.ABITag, displayBuildType(rec.BuildType))
			return nil
		}
		recCopy := rec
		staleCandidate = &recCopy
	} else if !isArtifactNotFoundError(err) {
		return err
	}

	cfg, err := loadProjectConfigIfPresent()
	if err != nil {
		return err
	}
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}

	var projectRegistries []config.Registry
	if cfg != nil {
		projectRegistries = cfg.Registries
	}
	reg, err := config.ResolvePrimaryRegistry(projectRegistries, globalCfg)
	if err != nil {
		return fmt.Errorf("resolve registry: %w", err)
	}
	s3client, err := fetchNewRegistryClient(context.Background(), reg)
	if err != nil {
		return fmt.Errorf("create S3 client: %w", err)
	}

	candidates, err := fetchManifestCandidates(cfg)
	if err != nil {
		return err
	}
	if staleCandidate != nil && staleCandidate.Name != "" && staleCandidate.Version != "" {
		candidates = prependUniqueCandidate(candidates, fetchManifestCandidate{
			Name:    staleCandidate.Name,
			Version: staleCandidate.Version,
		})
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no candidate package versions available to resolve hash_id %q; run fetch with dependencies first", hashID)
	}

	type matchedManifestArtifact struct {
		name     string
		version  string
		artifact registry.Artifact
	}

	var (
		matches        []matchedManifestArtifact
		manifestErrors []error
	)

	for _, candidate := range candidates {
		manifest, manifestErr := s3client.GetManifest(context.Background(), candidate.Name, candidate.Version)
		if manifestErr != nil {
			if isManifestNotFoundError(manifestErr) {
				continue
			}
			manifestErrors = append(manifestErrors, fmt.Errorf("load manifest %s@%s: %w", candidate.Name, candidate.Version, manifestErr))
			continue
		}
		artifact, findErr := registry.FindArtifactByHashID(manifest, hashID)
		if findErr != nil {
			if isArtifactNotFoundError(findErr) {
				continue
			}
			return findErr
		}
		matches = append(matches, matchedManifestArtifact{
			name:     candidate.Name,
			version:  candidate.Version,
			artifact: *artifact,
		})
		if len(matches) > 1 {
			return fmt.Errorf("hash_id prefix %q is ambiguous across manifests: %s@%s (%s), %s@%s (%s)",
				hashID,
				matches[0].name, matches[0].version, matches[0].artifact.HashID,
				matches[1].name, matches[1].version, matches[1].artifact.HashID,
			)
		}
	}

	if len(manifestErrors) > 0 && len(matches) > 0 {
		return fmt.Errorf("hash_id resolution incomplete for %q due to manifest load failure(s): %w", hashID, manifestErrors[0])
	}

	if len(matches) == 0 {
		if len(manifestErrors) > 0 {
			return fmt.Errorf("failed to load manifests while resolving hash_id %q: %w", hashID, manifestErrors[0])
		}
		return fmt.Errorf("artifact with hash_id prefix %q not found in available manifests", hashID)
	}
	match := matches[0]

	data, err := downloadFromManifestArtifact(context.Background(), s3client, match.name, match.version, match.artifact)
	if err != nil {
		return fmt.Errorf("download artifact %s@%s (%s, %s, %s): %w", match.name, match.version, match.artifact.ABITag, match.artifact.BuildType, match.artifact.HashID, err)
	}
	if err := verifyArtifactHash(data, match.artifact.HashID); err != nil {
		return fmt.Errorf("integrity check failed for %s@%s: %w", match.name, match.version, err)
	}

	destDir := cache.Path(match.name, match.version, match.artifact.ABITag, match.artifact.BuildType)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	if err := pack.ExtractTarZst(data, destDir); err != nil {
		return fmt.Errorf("extract %s@%s: %w", match.name, match.version, err)
	}

	if err := store.Upsert(artifactdb.Record{
		Name:       match.name,
		Version:    match.version,
		ABITag:     match.artifact.ABITag,
		BuildType:  match.artifact.BuildType,
		HashID:     match.artifact.HashID,
		BuildTags:  match.artifact.BuildTags,
		InstallDir: destDir,
		Origin:     "registry",
	}); err != nil {
		return fmt.Errorf("index artifact: %w", err)
	}

	if err := linkFetchedArtifact(match.name, destDir); err != nil {
		return err
	}

	fmt.Printf("  [downloaded] %s@%s (%s, %s)\n", match.name, match.version, match.artifact.ABITag, displayBuildType(match.artifact.BuildType))
	return nil
}

type fetchManifestCandidate struct {
	Name    string
	Version string
}

func fetchManifestCandidates(cfg *config.Config) ([]fetchManifestCandidate, error) {
	seen := make(map[string]struct{})
	out := make([]fetchManifestCandidate, 0)
	appendCandidate := func(name, version string) {
		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		if name == "" || version == "" {
			return
		}
		key := name + "@" + version
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, fetchManifestCandidate{Name: name, Version: version})
	}

	lf, err := resolver.LoadLock("cstow.lock")
	if err == nil {
		for _, pkg := range lf.Packages {
			appendCandidate(pkg.Name, pkg.Version)
		}
	} else if !errors.Is(err, os.ErrNotExist) && !strings.Contains(strings.ToLower(err.Error()), "no such file or directory") {
		return nil, fmt.Errorf("load lock file: %w", err)
	}

	if cfg != nil {
		for _, dep := range cfg.Dependencies {
			appendCandidate(dep.Name, dep.Version)
		}
	}
	return out, nil
}

func prependUniqueCandidate(candidates []fetchManifestCandidate, preferred fetchManifestCandidate) []fetchManifestCandidate {
	preferred.Name = strings.TrimSpace(preferred.Name)
	preferred.Version = strings.TrimSpace(preferred.Version)
	if preferred.Name == "" || preferred.Version == "" {
		return candidates
	}
	filtered := make([]fetchManifestCandidate, 0, len(candidates)+1)
	filtered = append(filtered, preferred)
	for _, c := range candidates {
		if c.Name == preferred.Name && c.Version == preferred.Version {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

func linkFetchedArtifact(name, src string) error {
	depsDir := filepath.Join(".", "cstow_deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		return err
	}

	dst := filepath.Join(depsDir, name)
	_ = os.Remove(dst)
	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("symlink %s: %w", name, err)
	}
	return nil
}

func isArtifactNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no artifact matches") ||
		(strings.Contains(msg, "artifact with hash_id prefix") && strings.Contains(msg, "not found"))
}

func resetFetchFlagState(cmd *cobra.Command) {
	resetFetchFlag := func(name string) {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			return
		}
		if replacer, ok := flag.Value.(interface{ Replace([]string) error }); ok {
			_ = replacer.Replace(nil)
		} else {
			_ = flag.Value.Set(flag.DefValue)
		}
		flag.Changed = false
	}
	resetFetchFlag("artifact")
	resetFetchFlag("toolchain")
	resetFetchFlag("source-fallback")
}

func verifyArtifactHash(data []byte, expectedHex string) error {
	if expectedHex == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	actual := fmt.Sprintf("%x", sum)
	if !strings.EqualFold(actual, expectedHex) {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedHex, actual)
	}
	return nil
}
