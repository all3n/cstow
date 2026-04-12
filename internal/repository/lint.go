package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
)

type LintResult struct {
	Name       string
	PackageDir string
	Errors     []string
	Warnings   []string
}

func (r *LintResult) OK() bool {
	return r != nil && len(r.Errors) == 0
}

func LintPackageDir(pkgDir string) (*LintResult, error) {
	pkgFile := filepath.Join(pkgDir, "package.toml")
	def, meta, err := decodePackageDef(pkgFile)
	if err != nil {
		return nil, fmt.Errorf("load package %s: %w", pkgFile, err)
	}

	result := &LintResult{Name: filepath.Base(pkgDir), PackageDir: pkgDir}
	addError := func(msg string) {
		result.Errors = append(result.Errors, msg)
	}
	addWarning := func(msg string) {
		result.Warnings = append(result.Warnings, msg)
	}

	if undecoded := lintUndecoded(meta.Undecoded()); len(undecoded) > 0 {
		addError("package.toml has unknown keys: " + strings.Join(undecoded, ", "))
	}

	expectedName := filepath.Base(pkgDir)
	if strings.TrimSpace(def.Package.Name) == "" {
		addError("package.name must not be empty")
	} else if def.Package.Name != expectedName {
		addError(fmt.Sprintf("package.name %q must match directory name %q", def.Package.Name, expectedName))
	}
	if strings.TrimSpace(def.Package.Description) == "" {
		addWarning("package.description is empty")
	}
	if strings.TrimSpace(def.Package.Homepage) == "" {
		addWarning("package.homepage is empty")
	}
	if strings.TrimSpace(def.Package.License) == "" {
		addWarning("package.license is empty")
	}

	if len(def.Versions) == 0 {
		addError("versions must not be empty")
	}
	versionSet := make(map[string]struct{}, len(def.Versions))
	for _, version := range def.Versions {
		versionSet[version] = struct{}{}
		if _, err := semver.NewVersion(version); err != nil {
			addError(fmt.Sprintf("version %q is not valid semver", version))
		}
	}

	switch def.Source.Type {
	case "git":
		if strings.TrimSpace(def.Source.URL) == "" {
			addError("source.url is required when source.type = \"git\"")
		}
	case "archive":
		if strings.TrimSpace(def.Source.URL) == "" && strings.TrimSpace(def.Source.URLTemplate) == "" {
			addError("source.url or source.url_template is required when source.type = \"archive\"")
		}
	default:
		addError(fmt.Sprintf("source.type %q is not supported (supported: git, archive)", def.Source.Type))
	}

	switch def.Build.System {
	case "cmake", "autotools", "header-only":
	default:
		addError(fmt.Sprintf("build.system %q is not supported (supported: cmake, autotools, header-only)", def.Build.System))
	}

	switch def.Build.Type {
	case "static", "shared", "header-only":
	default:
		addError(fmt.Sprintf("build.type %q is not supported (supported: static, shared, header-only)", def.Build.Type))
	}

	if def.Build.System == "header-only" && def.Build.Type != "header-only" {
		addError("build.type must be \"header-only\" when build.system = \"header-only\"")
	}

	for version := range def.Source.SHA256 {
		if _, ok := versionSet[version]; !ok {
			addError(fmt.Sprintf("source.sha256_versions contains unknown version %q", version))
		}
	}

	overrideDir := filepath.Join(pkgDir, "versions")
	if entries, err := os.ReadDir(overrideDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
				continue
			}
			version := strings.TrimSuffix(entry.Name(), ".toml")
			if _, ok := versionSet[version]; !ok {
				addError(fmt.Sprintf("versions/%s is not listed in package versions", entry.Name()))
			}

			overridePath := filepath.Join(overrideDir, entry.Name())
			override, meta, err := decodeVersionOverride(overridePath)
			if err != nil {
				return nil, fmt.Errorf("load override %s: %w", overridePath, err)
			}
			if undecoded := lintUndecoded(meta.Undecoded()); len(undecoded) > 0 {
				addError(fmt.Sprintf("%s has unknown keys: %s", filepath.ToSlash(filepath.Join("versions", entry.Name())), strings.Join(undecoded, ", ")))
			}
			if override.Patch != "" {
				patchPath := filepath.Join(pkgDir, "patches", override.Patch)
				if _, err := os.Stat(patchPath); err != nil {
					addError(fmt.Sprintf("patch file %q does not exist", filepath.ToSlash(filepath.Join("patches", override.Patch))))
				}
			}
			if override.Build != nil && override.Build.Type != "" {
				switch override.Build.Type {
				case "static", "shared", "header-only":
				default:
					addError(fmt.Sprintf("%s has unsupported build.type %q", filepath.ToSlash(filepath.Join("versions", entry.Name())), override.Build.Type))
				}
			}
		}
	}

	return result, nil
}

func LintRepositoryDir(repoDir string) ([]*LintResult, error) {
	var results []*LintResult
	err := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "package.toml" {
			return nil
		}
		result, err := LintPackageDir(filepath.Dir(path))
		if err != nil {
			return err
		}
		results = append(results, result)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(results, func(a, b *LintResult) int {
		return strings.Compare(a.Name, b.Name)
	})
	return results, nil
}

func decodePackageDef(path string) (*PackageDef, toml.MetaData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, toml.MetaData{}, err
	}
	var def PackageDef
	meta, err := toml.Decode(string(data), &def)
	if err != nil {
		return nil, toml.MetaData{}, err
	}
	return &def, meta, nil
}

func decodeVersionOverride(path string) (*VersionOverride, toml.MetaData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, toml.MetaData{}, err
	}
	var override VersionOverride
	meta, err := toml.Decode(string(data), &override)
	if err != nil {
		return nil, toml.MetaData{}, err
	}
	return &override, meta, nil
}

func lintUndecoded(keys []toml.Key) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key.String())
	}
	slices.Sort(out)
	return out
}
