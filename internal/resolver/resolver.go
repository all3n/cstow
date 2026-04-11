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
	Name      string   `toml:"name"`
	Version   string   `toml:"version"`
	Source    string   `toml:"source"`
	SHA256    string   `toml:"sha256"`
	ABITag    string   `toml:"abi_tag,omitempty"`
	BuildType string   `toml:"build_type,omitempty"`
	Deps      []string `toml:"deps,omitempty"`
	Path      string   `toml:"path,omitempty"`
	Git       string   `toml:"git,omitempty"`
	Rev       string   `toml:"rev,omitempty"`
}

// RegistryClient fetches available versions for a package.
type RegistryClient interface {
	ListVersions(pkg string) ([]string, error)
}

// LocalCache is the local package cache
type LocalCache interface {
	Has(name, version, abiTag, buildType string) bool
	Path(name, version, abiTag, buildType string) string
	LegacyPath(name, version, abiTag string) string
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
		// Git sources with empty or wildcard versions skip semver constraint parsing
		var constraint *semver.Constraints
		if !(dep.Source == "git" && (dep.Version == "" || dep.Version == "*")) {
			var err error
			constraint, err = semver.NewConstraint(dep.Version)
			if err != nil {
				return fmt.Errorf("invalid version constraint for %s: %w", dep.Name, err)
			}
		}

		// If already locked, check compatibility
		if existing, ok := locked[dep.Name]; ok {
			if existing.Version == dep.Version {
				continue // exact same constraint, already satisfied
			}
			if constraint != nil {
				existingVer, _ := semver.NewVersion(existing.Version)
				if existingVer != nil && constraint.Check(existingVer) {
					continue // already satisfied
				}
			}
			return fmt.Errorf("conflicting versions for %s: need %s, already locked %s",
				dep.Name, dep.Version, existing.Version)
		}

		var chosenVer string
		var source string

		switch dep.Source {
		case "git":
			chosenVer = dep.Version
			if chosenVer == "*" || chosenVer == "" {
				if dep.Rev != "" {
					chosenVer = dep.Rev
				} else {
					chosenVer = "0.0.0"
				}
			}
			source = "git:" + dep.Git
		case "local":
			// For local source, if version looks like a constraint (not a plain semver),
			// try to parse it; otherwise use as-is
			if sv, err := semver.NewVersion(dep.Version); err == nil {
				chosenVer = sv.Original()
			} else if dep.Version == "*" || dep.Version == "" {
				chosenVer = "0.0.0"
			} else {
				chosenVer = dep.Version
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
			Name:      dep.Name,
			Version:   chosenVer,
			Source:    source,
			BuildType: dep.BuildType,
			Path:      dep.Path,
			Git:       dep.Git,
			Rev:       dep.Rev,
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

	header := "# cstow.lock — auto-generated, do not edit\n\n"
	if _, err := f.WriteString(header); err != nil {
		return err
	}

	enc := toml.NewEncoder(f)
	return enc.Encode(lf)
}

// AddDependency adds or updates a dependency in cstow.toml.
// Returns true if an existing dependency was updated, false if a new one was added.
func AddDependency(cfg *config.Config, dep config.Dependency) bool {
	for i, d := range cfg.Dependencies {
		if d.Name == dep.Name {
			cfg.Dependencies[i] = dep
			return true // updated
		}
	}
	cfg.Dependencies = append(cfg.Dependencies, dep)
	return false // added
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

func (c *FSCache) Has(name, version, abiTag, buildType string) bool {
	paths := []string{c.Path(name, version, abiTag, buildType)}
	if buildType != "" {
		paths = append(paths, c.LegacyPath(name, version, abiTag))
	}
	for _, p := range paths {
		_, err := os.Stat(p)
		if err == nil {
			return true
		}
	}
	return false
}

func (c *FSCache) Path(name, version, abiTag, buildType string) string {
	if buildType == "" {
		return c.LegacyPath(name, version, abiTag)
	}
	return filepath.Join(c.Root, name, version, abiTag, buildType)
}

func (c *FSCache) LegacyPath(name, version, abiTag string) string {
	return filepath.Join(c.Root, name, version, abiTag)
}
