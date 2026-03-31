package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"

	"github.com/all3n/cstow/internal/config"
)

// LockFile represents cstow.lock
type LockFile struct {
	Version  int         `toml:"version"`
	Packages []LockEntry `toml:"package"`
}

// LockEntry is one resolved dependency in the lock file
type LockEntry struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Source  string `toml:"source"`
	SHA256  string `toml:"sha256"`
	ABITag  string `toml:"abi_tag,omitempty"`
	Deps    []string `toml:"deps,omitempty"`
	Path    string `toml:"path,omitempty"`
}

// RegistryClient fetches available versions for a package.
type RegistryClient interface {
	ListVersions(pkg string) ([]string, error)
}

// LocalCache is the local package cache
type LocalCache interface {
	Has(name, version, abiTag string) bool
	Path(name, version, abiTag string) string
}

// Resolver resolves dependencies using semver constraints
type Resolver struct {
	cache    LocalCache
	registry RegistryClient
}

// New creates a new resolver
func New(cache LocalCache, registry RegistryClient) *Resolver {
	return &Resolver{cache: cache, registry: registry}
}

// Resolve takes root dependencies and produces a lock file
func (r *Resolver) Resolve(deps []config.Dependency) (*LockFile, error) {
	locked := make(map[string]LockEntry)
	if err := r.resolveRecursive(deps, locked); err != nil {
		return nil, err
	}

	var entries []LockEntry
	for _, e := range locked {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return &LockFile{Version: 1, Packages: entries}, nil
}

func (r *Resolver) resolveRecursive(deps []config.Dependency, locked map[string]LockEntry) error {
	for _, dep := range deps {
		constraint, err := semver.NewConstraint(dep.Version)
		if err != nil {
			return fmt.Errorf("invalid version constraint for %s: %w", dep.Name, err)
		}

		// If already locked, check compatibility
		if existing, ok := locked[dep.Name]; ok {
			if existing.Version == dep.Version {
				continue // exact same constraint, already satisfied
			}
			existingVer, _ := semver.NewVersion(existing.Version)
			if existingVer != nil && constraint.Check(existingVer) {
				continue // already satisfied
			}
			return fmt.Errorf("conflicting versions for %s: need %s, already locked %s",
				dep.Name, dep.Version, existing.Version)
		}

		var chosenVer string
		var source string

		switch dep.Source {
		case "local":
			chosenVer = dep.Version
			if dep.Version == "*" || dep.Version == "" {
				chosenVer = "0.0.0"
			}
			source = "local:" + dep.Path
		default:
			// registry or git — try to resolve from registry
			if r.registry != nil {
				versions, err := r.registry.ListVersions(dep.Name)
				if err != nil {
					return fmt.Errorf("list versions for %s: %w", dep.Name, err)
				}
				chosenVer, err = pickBest(versions, constraint)
				if err != nil {
					return fmt.Errorf("resolve %s@%s: %w", dep.Name, dep.Version, err)
				}
				source = "registry:default"
			} else {
				// No registry — treat version as exact
				chosenVer = dep.Version
				if dep.Version == "*" {
					chosenVer = "0.0.0"
				}
				source = "local"
			}
		}

		locked[dep.Name] = LockEntry{
			Name:    dep.Name,
			Version: chosenVer,
			Source:  source,
			Path:    dep.Path,
		}
	}
	return nil
}

// pickBest selects the highest version matching the constraint
func pickBest(versions []string, constraint *semver.Constraints) (string, error) {
	var matched []*semver.Version
	for _, v := range versions {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if constraint.Check(sv) {
			matched = append(matched, sv)
		}
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("no matching version found")
	}
	sort.Sort(sort.Reverse(semver.Collection(matched)))
	return matched[0].Original(), nil
}

// LoadLock reads a lock file from disk
func LoadLock(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lock file: %w", err)
	}
	var lf LockFile
	if err := toml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse lock file: %w", err)
	}
	return &lf, nil
}

// SaveLock writes a lock file to disk
func SaveLock(path string, lf *LockFile) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create lock file: %w", err)
	}
	defer f.Close()

	header := "# cstow.lock — auto-generated, do not edit\n# version = " + fmt.Sprintf("%d\n\n", lf.Version)
	if _, err := f.WriteString(header); err != nil {
		return err
	}

	enc := toml.NewEncoder(f)
	return enc.Encode(map[string]interface{}{
		"version": lf.Version,
		"package": lf.Packages,
	})
}

// AddDependency adds a dependency to cstow.toml and regenerates the lock
func AddDependency(cfg *config.Config, name, version, source string) {
	for _, d := range cfg.Dependencies {
		if d.Name == name {
			return // already present
		}
	}
	cfg.Dependencies = append(cfg.Dependencies, config.Dependency{
		Name:    name,
		Version: version,
		Source:  source,
	})
}

// FSChunk implements LocalCache using filesystem
type FSCache struct {
	Root string // ~/.cstow/cache
}

func NewFSCache() *FSCache {
	root := os.Getenv("CSTOW_CACHE_DIR")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".cstow", "cache")
	}
	return &FSCache{Root: root}
}

func (c *FSCache) Has(name, version, abiTag string) bool {
	p := c.Path(name, version, abiTag)
	_, err := os.Stat(p)
	return err == nil
}

func (c *FSCache) Path(name, version, abiTag string) string {
	return filepath.Join(c.Root, name, version, abiTag)
}
