package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Download dependencies from the registry into local cache",
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
			// No lock file — resolve first
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

		cache := resolver.NewFSCache()

		// If registry is configured, create S3 client for fetching
		var s3client *registry.S3Client
		if len(cfg.Registries) > 0 {
			s3client, err = registry.NewS3Client(context.Background(), cfg.Registries[0])
			if err != nil {
				return fmt.Errorf("create S3 client: %w", err)
			}
		}

		for _, pkg := range lf.Packages {
			abiTag := pkg.ABITag
			if abiTag == "" {
				abiTag = "default"
			}

			if cache.Has(pkg.Name, pkg.Version, abiTag) {
				fmt.Printf("  [cached] %s@%s\n", pkg.Name, pkg.Version)
				continue
			}

			if s3client == nil {
				fmt.Printf("  [skip] %s@%s (no registry configured)\n", pkg.Name, pkg.Version)
				continue
			}

			fmt.Printf("  [fetch] %s@%s ...\n", pkg.Name, pkg.Version)

			data, err := s3client.Download(context.Background(), pkg.Name, pkg.Version, abiTag)
			if err != nil {
				return fmt.Errorf("download %s@%s: %w", pkg.Name, pkg.Version, err)
			}

			destDir := cache.Path(pkg.Name, pkg.Version, abiTag)
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				return fmt.Errorf("create cache dir: %w", err)
			}

			if err := pack.ExtractTarZst(data, destDir); err != nil {
				return fmt.Errorf("extract %s@%s: %w", pkg.Name, pkg.Version, err)
			}

			fmt.Printf("  [done]  %s@%s -> %s\n", pkg.Name, pkg.Version, destDir)
		}

		// Write CMAKE_PREFIX_PATH hints
		depsDir := filepath.Join(".", "cstow_deps")
		if err := os.MkdirAll(depsDir, 0o755); err != nil {
			return err
		}

		// Create symlinks or copy from cache to deps
		prefixPath := ""
		for _, pkg := range lf.Packages {
			abiTag := pkg.ABITag
			if abiTag == "" {
				abiTag = "default"
			}
			src := cache.Path(pkg.Name, pkg.Version, abiTag)
			dst := filepath.Join(depsDir, pkg.Name)
			os.Remove(dst)
			if err := os.Symlink(src, dst); err != nil {
				fmt.Printf("  [warn] symlink %s: %v\n", pkg.Name, err)
			}
			prefixPath += dst + string(filepath.ListSeparator)
		}

		if prefixPath != "" {
			fmt.Printf("\n  CMAKE_PREFIX_PATH=%s\n", prefixPath)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(fetchCmd)
}
