package legacy

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/all3n/cstow/internal/config"
)

// CMakeScanner scans CMakeLists.txt for find_package and FetchContent calls
type CMakeScanner struct{}
// ScanResult holds discovered dependencies
type ScanResult struct {
	Dependencies []config.Dependency
	Warnings     []string
	Std          string
}

var (
	findPackageRe  = regexp.MustCompile(`(?i)find_package\s*\(\s*(\w+)\s*(?:VERSION\s+(\S+))?\s*(REQUIRED)?`)
	fetchDeclareRe = regexp.MustCompile(`(?i)FetchContent_Declare\s*\(`)
	fetchNameRe    = regexp.MustCompile(`\(\s*(\w+)\s`)
	fetchGitRepoRe = regexp.MustCompile(`(?i)GIT_REPOSITORY\s+(\S+)`)
	fetchGitTagRe  = regexp.MustCompile(`(?i)GIT_TAG\s+(\S+)`)
	fetchUrlRe     = regexp.MustCompile(`(?i)URL\s+(\S+)`)
	cxxStdRe       = regexp.MustCompile(`(?i)set\s*\(\s*CMAKE_CXX_STANDARD\s+(\d+)\s*\)`)
)

// Scan parses a CMakeLists.txt file and extracts dependency info
func (s *CMakeScanner) Scan(cmakePath string) (*ScanResult, error) {
	f, err := os.Open(cmakePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cmakePath, err)
	}
	defer f.Close()

	result := &ScanResult{
		Std: "c++17", // default
	}
	scanner := bufio.NewScanner(f)
	inFetchBlock := false
	var fetchBlock strings.Builder
	depth := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		// set(CMAKE_CXX_STANDARD 20)
		if m := cxxStdRe.FindStringSubmatch(line); m != nil {
			result.Std = "c++" + m[1]
		}

		// find_package(Foo VERSION x.y.z REQUIRED)
		if m := findPackageRe.FindStringSubmatch(line); m != nil {
			dep := config.Dependency{
				Name:   strings.ToLower(m[1]),
				Source: "registry",
			}
			if m[2] != "" {
				dep.Version = "^" + m[2]
			} else {
				dep.Version = "*"
			}
			result.Dependencies = append(result.Dependencies, dep)
		}

		// FetchContent_Declare(
		if !inFetchBlock && fetchDeclareRe.MatchString(line) {
			inFetchBlock = true
			fetchBlock.Reset()
			fetchBlock.WriteString(line)
			fetchBlock.WriteString(" ")
			depth = strings.Count(line, "(") - strings.Count(line, ")")
			if depth <= 0 {
				// Single-line FetchContent_Declare
				inFetchBlock = false
				dep := parseFetchBlock(fetchBlock.String())
				if dep != nil {
					result.Dependencies = append(result.Dependencies, *dep)
				}
			}
			continue
		}

		if inFetchBlock {
			fetchBlock.WriteString(line)
			fetchBlock.WriteString(" ")
			depth += strings.Count(line, "(") - strings.Count(line, ")")
			if depth <= 0 {
				inFetchBlock = false
				dep := parseFetchBlock(fetchBlock.String())
				if dep != nil {
					result.Dependencies = append(result.Dependencies, *dep)
				}
			}
		}
	}

	return result, scanner.Err()
}

func parseFetchBlock(text string) *config.Dependency {
	dep := &config.Dependency{
		Name:    "",
		Source:  "git",
		Version: "*",
	}

	// Extract name
	if m := fetchNameRe.FindStringSubmatch(text); m != nil {
		dep.Name = strings.ToLower(m[1])
	} else {
		return nil
	}

	// Extract GIT_REPOSITORY
	if m := fetchGitRepoRe.FindStringSubmatch(text); m != nil {
		dep.Git = strings.Trim(m[1], "\"")
	}

	// Extract GIT_TAG
	if m := fetchGitTagRe.FindStringSubmatch(text); m != nil {
		tag := strings.Trim(m[1], "\"")
		dep.Rev = tag
		if strings.HasPrefix(tag, "v") {
			dep.Version = "^" + strings.TrimPrefix(tag, "v")
		}
	}

	// Extract URL (fallback if GIT_REPOSITORY not present)
	if dep.Git == "" {
		if m := fetchUrlRe.FindStringSubmatch(text); m != nil {
			dep.Source = "registry" // Treat as registry for now, could be better handled
			dep.Path = strings.Trim(m[1], "\"")
		}
	}

	return dep
}

// GenerateCStowToml generates a cstow.toml for a legacy CMake project
func GenerateCStowToml(name, version, std, cmakeRoot string, extraArgs []string, deps []config.Dependency) *config.Config {
	cfg := &config.Config{
		Package: config.Package{
			Name:    name,
			Version: version,
			Std:     std,
		},
		Build: config.Build{
			Type: "library",
		},
		Dependencies: deps,
		Legacy: &config.Legacy{
			Type:      "cmake",
			Root:      cmakeRoot,
			ExtraArgs: extraArgs,
		},
		Toolchain: config.Toolchain{
			Compiler: "auto",
		},
	}
	return cfg
}
