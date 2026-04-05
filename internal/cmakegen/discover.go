package cmakegen

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverDeps scans depsDir for dependency directories and resolves their
// CMake package information. If depsDir does not exist it returns nil with no
// error. Each subdirectory (or symlink to a directory) is inspected for
// *Config.cmake or *-config.cmake files. When found the package name and
// CMake target are extracted; otherwise a fallback entry with empty
// ConfigFile/TargetName is returned (useful for header-only libraries).
func DiscoverDeps(depsDir string) ([]DepTarget, error) {
	entries, err := os.ReadDir(depsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var deps []DepTarget
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Follow symlinks to directories.
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			realPath := filepath.Join(depsDir, e.Name())
			resolved, err := filepath.EvalSymlinks(realPath)
			if err != nil {
				continue
			}
			fi, err := os.Stat(resolved)
			if err != nil || !fi.IsDir() {
				continue
			}
		}

		depDir := filepath.Join(depsDir, e.Name())
		dt := discoverDep(e.Name(), depDir)
		deps = append(deps, dt)
	}

	return deps, nil
}

// discoverDep walks depDir looking for a CMake package config file and returns
// a DepTarget with the resolved information.
func discoverDep(name, depDir string) DepTarget {
	dt := DepTarget{
		Name:   name,
		Prefix: depDir,
	}

	var configFile string
	filepath.WalkDir(depDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors, keep walking
		}
		if d.IsDir() {
			return nil
		}

		fn := d.Name()
		if strings.HasSuffix(fn, "Config.cmake") {
			configFile = path
			return filepath.SkipAll // found one, stop searching
		}
		if strings.HasSuffix(fn, "-config.cmake") {
			configFile = path
			return filepath.SkipAll
		}
		return nil
	})

	if configFile != "" {
		dt.ConfigFile = configFile
		dt.TargetName = pkgName(configFile) + "::" + pkgName(configFile)
	}

	return dt
}

// pkgName extracts the CMake package name from a config filename by stripping
// the "Config.cmake" or "-config.cmake" suffix.
func pkgName(configPath string) string {
	base := filepath.Base(configPath)
	base = strings.TrimSuffix(base, "Config.cmake")
	base = strings.TrimSuffix(base, "-config.cmake")
	return base
}
