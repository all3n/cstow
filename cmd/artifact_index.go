package cmd

import (
	"fmt"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/resolver"
)

type indexedArtifact struct {
	Name       string
	Version    string
	ABITag     string
	BuildType  string
	HashID     string
	BuildTags  []string
	InstallDir string
	Origin     string
}

func indexSuccessfulArtifact(cache resolver.LocalCache, item indexedArtifact) error {
	store, err := artifactdb.OpenDefault()
	if err != nil {
		return fmt.Errorf("open artifact db: %w", err)
	}
	defer store.Close()

	buildType := item.BuildType
	if legacyPath := cache.LegacyPath(item.Name, item.Version, item.ABITag); item.InstallDir == legacyPath {
		buildType = ""
	}

	return store.Upsert(artifactdb.Record{
		Name:       item.Name,
		Version:    item.Version,
		ABITag:     item.ABITag,
		BuildType:  buildType,
		HashID:     item.HashID,
		BuildTags:  item.BuildTags,
		InstallDir: item.InstallDir,
		Origin:     item.Origin,
	})
}
