package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/all3n/cstow/internal/config"
)

type ScaffoldOptions struct {
	Name string
	Std  string
	Type string
}

func Scaffold(dir string, opts ScaffoldOptions) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return fmt.Errorf("create src dir: %w", err)
	}

	includeDir := filepath.Join(dir, "include")
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		return fmt.Errorf("create include dir: %w", err)
	}

	cfg := &config.Config{
		Package: config.Package{
			Name:    opts.Name,
			Version: "0.1.0",
			Std:     opts.Std,
			Authors: []string{},
		},
		Build: config.Build{
			Type:    opts.Type,
			Sources: []string{"src/**/*.cpp"},
			Include: []string{"include"},
		},
		Profiles: map[string]config.Profile{
			"debug":   {Optimize: "0", Debug: true},
			"release": {Optimize: "3", LTO: true},
		},
		Toolchain: config.Toolchain{
			Compiler: "auto",
		},
	}

	tomlPath := filepath.Join(dir, "cstow.toml")
	if err := cfg.Save(tomlPath); err != nil {
		return fmt.Errorf("write cstow.toml: %w", err)
	}

	if opts.Type == "executable" {
		if err := writeMainCpp(srcDir, opts.Name); err != nil {
			return err
		}
	}

	if err := writeCMakeLists(dir, opts.Name, opts.Type, opts.Std); err != nil {
		return err
	}

	return nil
}

func writeMainCpp(srcDir, name string) error {
	content := `#include <iostream>

int main() {
    std::cout << "` + name + ` works!" << std::endl;
    return 0;
}
`
	return os.WriteFile(filepath.Join(srcDir, "main.cpp"), []byte(content), 0o644)
}

func stdToNumber(std string) string {
	switch std {
	case "c++20", "c++2a":
		return "20"
	case "c++23", "c++2b":
		return "23"
	case "c++14":
		return "14"
	case "c++11":
		return "11"
	default:
		return "17"
	}
}

func writeCMakeLists(dir, name, buildType, std string) error {
	cxxStd := stdToNumber(std)
	content := fmt.Sprintf(`cmake_minimum_required(VERSION 3.16)
project(%s LANGUAGES CXX)

set(CMAKE_CXX_STANDARD %s)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
`, name, cxxStd)

	if buildType == "executable" {
		content += fmt.Sprintf("add_executable(%s ${SOURCES})\n", name)
	} else {
		content += fmt.Sprintf("add_library(%s ${SOURCES})\n", name)
	}

	content += "target_include_directories(${PROJECT_NAME} PUBLIC include)\n"

	return os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(content), 0o644)
}
