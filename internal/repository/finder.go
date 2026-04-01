package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
)

// Finder searches ordered repository directories for package definitions.
type Finder struct {
	searchPaths []string
}

// NewFinder returns a Finder using ~/.cstow/repository/ as the single search path.
func NewFinder() *Finder {
	home, _ := os.UserHomeDir()
	return &Finder{searchPaths: []string{filepath.Join(home, ".cstow", "repository")}}
}

// NewFinderWithPaths returns a Finder with explicit search paths (used in tests).
func NewFinderWithPaths(paths []string) *Finder {
	return &Finder{searchPaths: paths}
}

// ResolvedPkg is the result of a successful Find call.
type ResolvedPkg struct {
	Def      *PackageDef
	Version  string           // resolved concrete version satisfying the constraint
	Override *VersionOverride // nil if no version-specific override file exists
	RepoPath string           // which repository root matched
}

// Find searches all repository paths for name matching versionConstraint.
// Returns a descriptive error when not found — callers must fail hard.
func (f *Finder) Find(name, versionConstraint string) (*ResolvedPkg, error) {
	letter := indexLetter(name)

	for _, root := range f.searchPaths {
		pkgFile := filepath.Join(root, letter, name, "package.toml")
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

		override := loadVersionOverride(filepath.Join(root, letter, name, "versions"), matched)

		return &ResolvedPkg{
			Def:      def,
			Version:  matched,
			Override: override,
			RepoPath: root,
		}, nil
	}

	return nil, fmt.Errorf("package %q not found in any repository (constraint: %s)", name, versionConstraint)
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
