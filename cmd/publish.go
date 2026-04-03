package cmd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/spf13/cobra"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish the current package to the registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := "cstow.toml"
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("cstow.toml not found")
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		globalCfg, err := config.LoadGlobal()
		if err != nil {
			return fmt.Errorf("load global config: %w", err)
		}

		reg, err := config.ResolvePrimaryRegistry(cfg.Registries, globalCfg)
		if err != nil {
			return fmt.Errorf("resolve registry: %w", err)
		}
		s3client, err := registry.NewS3Client(context.Background(), reg)
		if err != nil {
			return fmt.Errorf("create S3 client: %w", err)
		}

		abiTag, _ := cmd.Flags().GetString("abi-tag")
		if abiTag == "" {
			abiTag = "default"
		}
		buildType, _ := cmd.Flags().GetString("build-type")
		if err := validateBuildType(buildType); err != nil {
			return err
		}

		buildDir := "build/release"
		if _, err := os.Stat(buildDir); err != nil {
			buildDir = "build/debug"
		}

		fmt.Printf(">> packaging %s@%s (abi: %s, type: %s)\n", cfg.Package.Name, cfg.Package.Version, abiTag, buildType)

		data, err := pack.CreateTarZst(buildDir)
		if err != nil {
			return fmt.Errorf("package build dir: %w", err)
		}

		hash := sha256.Sum256(data)
		shaStr := fmt.Sprintf("%x", hash)

		fmt.Printf(">> uploading (%d bytes, sha256: %s...)\n", len(data), shaStr[:12])

		if err := s3client.Upload(context.Background(), cfg.Package.Name, cfg.Package.Version, abiTag, buildType, data); err != nil {
			return fmt.Errorf("upload artifact: %w", err)
		}

		manifest := &registry.Manifest{
			Name:    cfg.Package.Name,
			Version: cfg.Package.Version,
			Std:     cfg.Package.Std,
			Artifacts: []registry.Artifact{
				{
					ABITag:    abiTag,
					BuildType: buildType,
					SHA256:    shaStr,
					Size:      int64(len(data)),
				},
			},
		}

		if err := s3client.UploadManifest(context.Background(), cfg.Package.Name, cfg.Package.Version, manifest); err != nil {
			return fmt.Errorf("upload manifest: %w", err)
		}

		fmt.Printf(">> published %s@%s\n", cfg.Package.Name, cfg.Package.Version)
		return nil
	},
}

func init() {
	publishCmd.Flags().String("abi-tag", "", "ABI tag for this artifact")
	publishCmd.Flags().String("build-type", "static", "artifact build type (static|shared|header-only)")
	rootCmd.AddCommand(publishCmd)
}
