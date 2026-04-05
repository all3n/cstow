package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
	"github.com/all3n/cstow/internal/config"
)

// Finder searches ordered repository directories for package definitions.
type Finder struct {
	searchPaths []string
}

// NewFinder loads ~/.cstow/config.toml and builds search paths from it.
// Falls back to ~/.cstow/repository/ if config is missing.
// If projectRoot is non-empty, includes <projectRoot>/.cstow/repository/
// with highest priority.
func NewFinder(projectRoot ...string) (*Finder, error) {
	g, err := config.LoadGlobal()
	if err != nil {
		return nil, fmt.Errorf("load global config: %w", err)
	}
	return &Finder{searchPaths: g.RepositoryPaths(projectRoot...)}, nil
}

// NewFinderWithPaths returns a Finder with explicit search paths (used in tests).
func NewFinderWithPaths(paths []string) *Finder {
	return &Finder{searchPaths: paths}
}

// ResolvedPkg is the result of a successful Find call.
type ResolvedPkg struct {
	Def        *PackageDef
	Version    string           // resolved concrete version satisfying the constraint
	Override   *VersionOverride // nil if no version-specific override file exists
	RepoPath   string           // which repository root matched
	PackageDir string           // path to the package directory within the repository
}

// Find searches all repository paths for name matching versionConstraint.
// Returns a descriptive error when not found — callers must fail hard.
func (f *Finder) Find(name, versionConstraint string) (*ResolvedPkg, error) {
	letter := indexLetter(name)

	for _, root := range f.searchPaths {
		pkgDir := filepath.Join(root, letter, name)
		pkgFile := filepath.Join(pkgDir, "package.toml")
		if _, err := os.Stat(pkgFile); err != nil {
			continue
		}

		def, err := loadPackageDef(pkgFile)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", pkgFile, err)
		}

		matched, err := pickBestVersion(def.Versions, versionConstraint)
		if err != nil {
			continue // no matching version in this root, try next
		}

		override := loadVersionOverride(filepath.Join(pkgDir, "versions"), matched)

		return &ResolvedPkg{
			Def:        def,
			Version:    matched,
			Override:   override,
			RepoPath:   root,
			PackageDir: pkgDir,
		}, nil
	}

	return nil, fmt.Errorf("package %q not found in any repository (constraint: %s)", name, versionConstraint)
}

// SearchResult is one matched package from a Search call.
type SearchResult struct {
	Name        string
	Description string
	Version     string // latest version
	RepoPath    string // which repository root matched
}

// Search scans all repository paths for packages whose name contains query.
// Pass "" to list all packages. Results are deduplicated by name (first match wins).
func (f *Finder) Search(query string) ([]SearchResult, error) {
	seen := map[string]bool{}
	var results []SearchResult

	for _, root := range f.searchPaths {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, letter := range entries {
			if !letter.IsDir() {
				continue
			}
			pkgs, err := os.ReadDir(filepath.Join(root, letter.Name()))
			if err != nil {
				continue
			}
			for _, pkg := range pkgs {
				if !pkg.IsDir() {
					continue
				}
				name := pkg.Name()
				if query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
					continue
				}
				if seen[name] {
					continue
				}
				seen[name] = true

				pkgFile := filepath.Join(root, letter.Name(), name, "package.toml")
				def, err := loadPackageDef(pkgFile)
				if err != nil {
					continue
				}

				latest := ""
				if len(def.Versions) > 0 {
					latest, _ = pickBestVersion(def.Versions, "*")
				}
				results = append(results, SearchResult{
					Name:        name,
					Description: def.Package.Description,
					Version:     latest,
					RepoPath:    root,
				})
			}
		}
	}

	return results, nil
}

// indexLetter returns the first-letter directory name for a package.
func indexLetter(name string) string {
	if len(name) == 0 {
		return "_"
	}
	r := []rune(name)[0]
	if !unicode.IsLetter(r) {
		return "_"
	}
	return string(unicode.ToLower(r))
}

// loadPackageDef reads and parses a package.toml file.
func loadPackageDef(path string) (*PackageDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def PackageDef
	if err := toml.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// loadVersionOverride reads versions/<version>.toml if it exists; returns nil otherwise.
func loadVersionOverride(versionsDir, version string) *VersionOverride {
	path := filepath.Join(versionsDir, version+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var vo VersionOverride
	if err := toml.Unmarshal(data, &vo); err != nil {
		return nil
	}
	return &vo
}

// pickBestVersion selects the highest version from candidates satisfying constraint.
func pickBestVersion(candidates []string, constraint string) (string, error) {
	// "*" or "" means any version — pick the latest
	if constraint == "*" || constraint == "" {
		if len(candidates) == 0 {
			return "", fmt.Errorf("no versions available")
		}
		var versions []*semver.Version
		for _, c := range candidates {
			if sv, err := semver.NewVersion(c); err == nil {
				versions = append(versions, sv)
			}
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("no valid semver versions")
		}
		sort.Sort(sort.Reverse(semver.Collection(versions)))
		return versions[0].Original(), nil
	}

	c, err := semver.NewConstraint(constraint)
	if err != nil {
		// treat as exact version
		for _, v := range candidates {
			if v == constraint {
				return v, nil
			}
		}
		return "", fmt.Errorf("version %q not in list", constraint)
	}

	var matched []*semver.Version
	for _, v := range candidates {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if c.Check(sv) {
			matched = append(matched, sv)
		}
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("no version matching %q", constraint)
	}
	sort.Sort(sort.Reverse(semver.Collection(matched)))
	return matched[0].Original(), nil
}
