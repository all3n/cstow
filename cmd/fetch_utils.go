package cmd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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

	overrideRegistry, _ := rootCmd.PersistentFlags().GetString("registry")

	var projectRegistries []config.Registry
	if cfg != nil {
		projectRegistries = cfg.Registries
	}
	reg, err := config.ResolvePrimaryRegistry(projectRegistries, globalCfg)
	if err != nil {
		if overrideRegistry == "" {
			return fmt.Errorf("resolve registry: %w", err)
		}
		reg = config.Registry{URL: overrideRegistry}
	} else if overrideRegistry != "" {
		reg.URL = overrideRegistry
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

func linkFetchedArtifactAt(name, src, depsDir string) error {
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		return fmt.Errorf("create deps dir %s: %w", depsDir, err)
	}
	dst := filepath.Join(depsDir, name)
	_ = os.Remove(dst)
	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("symlink %s: %w", name, err)
	}
	return nil
}

func linkFetchedArtifact(name, src string) error {
	return linkFetchedArtifactAt(name, src, "cstow_deps")
}

func isArtifactNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no artifact matches") ||
		(strings.Contains(msg, "artifact with hash_id prefix") && strings.Contains(msg, "not found"))
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
