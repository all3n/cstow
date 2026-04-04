package cmd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

type publishRegistryClient interface {
	Upload(ctx context.Context, pkg, version, abiTag, buildType, hashID string, data []byte) error
	UploadManifest(ctx context.Context, pkg, version string, manifest *registry.Manifest) error
	GetManifest(ctx context.Context, pkg, version string) (*registry.Manifest, error)
}

var publishNewRegistryClient = func(ctx context.Context, reg config.Registry) (publishRegistryClient, error) {
	return registry.NewS3Client(ctx, reg)
}

var publishCmd = &cobra.Command{
	Use:   "publish [name]",
	Short: "Publish the current package to the registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		defer resetPublishFlagState(cmd)

		if len(args) > 1 {
			return fmt.Errorf("publish accepts at most one package name")
		}

		projectCfg, err := loadProjectConfigIfPresent()
		if err != nil {
			return err
		}
		globalCfg, err := config.LoadGlobal()
		if err != nil {
			return fmt.Errorf("load global config: %w", err)
		}

		var projectRegistries []config.Registry
		if projectCfg != nil {
			projectRegistries = projectCfg.Registries
		}
		reg, err := config.ResolvePrimaryRegistry(projectRegistries, globalCfg)
		if err != nil {
			return fmt.Errorf("resolve registry: %w", err)
		}
		s3client, err := publishNewRegistryClient(context.Background(), reg)
		if err != nil {
			return fmt.Errorf("create S3 client: %w", err)
		}

		abiTag, _ := cmd.Flags().GetString("abi-tag")
		buildType, _ := cmd.Flags().GetString("build-type")
		version, _ := cmd.Flags().GetString("version")
		buildTagsRaw, _ := cmd.Flags().GetStringArray("build-tag")
		buildTags, err := parseBuildTags(buildTagsRaw)
		if err != nil {
			return err
		}

		var (
			name       string
			pkgStd     string
			publishDir string
			localMode  bool
		)

		switch len(args) {
		case 0:
			if projectCfg == nil {
				return fmt.Errorf("cstow.toml not found")
			}
			if abiTag == "" {
				abiTag = "default"
			}
			if buildType == "" {
				buildType = "static"
			}
			if err := validateBuildType(buildType); err != nil {
				return err
			}
			name = projectCfg.Package.Name
			version = projectCfg.Package.Version
			pkgStd = projectCfg.Package.Std
			publishDir = "build/release"
			if _, err := os.Stat(publishDir); err != nil {
				publishDir = "build/debug"
			}
		case 1:
			localMode = true
			name = args[0]
			if version == "" {
				return fmt.Errorf("--version is required when publishing a local artifact")
			}
			if abiTag == "" {
				return fmt.Errorf("--abi-tag is required when publishing a local artifact")
			}
			if buildType == "" {
				return fmt.Errorf("--build-type must be explicitly provided when publishing a local artifact")
			}
			if err := validateBuildType(buildType); err != nil {
				return err
			}
			publishDir, err = resolveLocalArtifactPrefix(name, version, abiTag, buildType)
			if err != nil {
				return err
			}
		}

		fmt.Printf(">> packaging %s@%s (abi: %s, type: %s)\n", name, version, abiTag, buildType)

		data, err := pack.CreateTarZst(publishDir)
		if err != nil {
			return fmt.Errorf("package artifact dir: %w", err)
		}

		hash := sha256.Sum256(data)
		hashID := fmt.Sprintf("%x", hash)

		fmt.Printf(">> uploading (%d bytes, sha256: %s...)\n", len(data), hashID[:12])

		if err := s3client.Upload(context.Background(), name, version, abiTag, buildType, hashID, data); err != nil {
			return fmt.Errorf("upload artifact: %w", err)
		}

		manifest, err := s3client.GetManifest(context.Background(), name, version)
		if err != nil {
			if !isManifestNotFoundError(err) {
				return fmt.Errorf("get manifest: %w", err)
			}
			manifest = &registry.Manifest{}
		}
		if manifest == nil {
			manifest = &registry.Manifest{}
		}
		manifest.Name = name
		manifest.Version = version
		if pkgStd != "" {
			manifest.Std = pkgStd
		}

		manifest.Artifacts = mergeManifestArtifacts(manifest.Artifacts, registry.Artifact{
			ABITag:    abiTag,
			BuildType: buildType,
			HashID:    hashID,
			BuildTags: buildTags,
			SHA256:    hashID,
			Size:      int64(len(data)),
		})

		if err := s3client.UploadManifest(context.Background(), name, version, manifest); err != nil {
			return fmt.Errorf("upload manifest: %w", err)
		}

		if localMode {
			cache := resolver.NewFSCache()
			if err := indexSuccessfulArtifact(cache, indexedArtifact{
				Name:       name,
				Version:    version,
				ABITag:     abiTag,
				BuildType:  buildType,
				HashID:     hashID,
				BuildTags:  buildTags,
				InstallDir: publishDir,
				Origin:     "registry",
			}); err != nil {
				return err
			}
		}

		fmt.Printf(">> published %s@%s\n", name, version)
		return nil
	},
}

func loadProjectConfigIfPresent() (*config.Config, error) {
	cfgPath := "cstow.toml"
	if _, err := os.Stat(cfgPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat cstow.toml: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func parseBuildTags(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid --build-tag %q (expected key=value)", item)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate --build-tag key %q", key)
		}
		seen[key] = struct{}{}
		out = append(out, key+"="+value)
	}
	return out, nil
}

func resolveLocalArtifactPrefix(name, version, abiTag, buildType string) (string, error) {
	store, err := artifactdb.OpenDefault()
	if err == nil {
		defer store.Close()
		rows, listErr := store.List()
		if listErr != nil {
			return "", fmt.Errorf("list artifact index: %w", listErr)
		}
		for _, row := range rows {
			if row.Name == name && row.Version == version && row.ABITag == abiTag && row.BuildType == buildType && pathExists(row.InstallDir) {
				return row.InstallDir, nil
			}
		}
	}

	cache := resolver.NewFSCache()
	typedPath := cache.Path(name, version, abiTag, buildType)
	if pathExists(typedPath) {
		return typedPath, nil
	}
	if buildType == "" {
		legacyPath := cache.LegacyPath(name, version, abiTag)
		if pathExists(legacyPath) {
			return legacyPath, nil
		}
	}
	return "", fmt.Errorf("local artifact not found for %s@%s (abi: %s, type: %s)", name, version, abiTag, buildType)
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mergeManifestArtifacts(existing []registry.Artifact, next registry.Artifact) []registry.Artifact {
	merged := append([]registry.Artifact(nil), existing...)
	for i := range merged {
		if sameManifestArtifact(merged[i], next) {
			merged[i] = next
			return merged
		}
	}
	return append(merged, next)
}

func sameManifestArtifact(a, b registry.Artifact) bool {
	return a.ABITag == b.ABITag && a.BuildType == b.BuildType
}

func isManifestNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "nosuchkey") ||
		strings.Contains(lower, "manifest not found") ||
		strings.Contains(lower, "not found")
}

func resetPublishFlagState(cmd *cobra.Command) {
	resetPublishFlag := func(name string) {
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
	resetPublishFlag("abi-tag")
	resetPublishFlag("build-type")
	resetPublishFlag("version")
	resetPublishFlag("build-tag")
}

func init() {
	publishCmd.Flags().String("abi-tag", "", "ABI tag for this artifact")
	publishCmd.Flags().String("build-type", "", "artifact build type (static|shared|header-only)")
	publishCmd.Flags().String("version", "", "version to publish (required with local-artifact mode)")
	publishCmd.Flags().StringArray("build-tag", nil, "build tag metadata for this artifact (repeatable key=value)")
	rootCmd.AddCommand(publishCmd)
}
