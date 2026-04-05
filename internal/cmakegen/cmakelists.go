package cmakegen

import (
	"fmt"
	"strings"
)

// GenerateCMakeLists produces a CMakeLists.txt content string from the given
// options. It follows conventional CMake patterns: cmake_minimum_required,
// project declaration, C++ standard, target definition, include directories,
// compile definitions, and dependency linking.
func GenerateCMakeLists(opts GenerateOptions) string {
	var b strings.Builder

	// 1. CMake minimum version and project declaration.
	b.WriteString("cmake_minimum_required(VERSION 3.16)\n")
	b.WriteString(fmt.Sprintf("project(%s LANGUAGES CXX)\n", opts.Name))

	// 2. C++ standard.
	stdNum := strings.TrimPrefix(opts.Std, "c++")
	b.WriteString(fmt.Sprintf("set(CMAKE_CXX_STANDARD %s)\n", stdNum))
	b.WriteString("set(CMAKE_CXX_STANDARD_REQUIRED ON)\n")

	// 3. Target definition based on type.
	visibility := "PUBLIC"
	switch opts.Type {
	case "header-only":
		visibility = "INTERFACE"
		b.WriteString(fmt.Sprintf("add_library(%s INTERFACE)\n", opts.Name))
	default:
		// executable or library
		b.WriteString("file(GLOB_RECURSE SOURCES src/*.cpp)\n")
		if opts.Type == "executable" {
			b.WriteString(fmt.Sprintf("add_executable(%s ${SOURCES})\n", opts.Name))
		} else {
			b.WriteString(fmt.Sprintf("add_library(%s ${SOURCES})\n", opts.Name))
		}
	}

	// 4. Include directories from user-svided paths.
	if len(opts.Include) > 0 {
		b.WriteString(fmt.Sprintf("target_include_directories(%s %s %s)\n",
			opts.Name, visibility, strings.Join(opts.Include, " ")))
	}

	// 5. Compile definitions.
	if len(opts.Defines) > 0 {
		b.WriteString(fmt.Sprintf("target_compile_definitions(%s PRIVATE %s)\n",
			opts.Name, strings.Join(opts.Defines, " ")))
	}

	// 6. Dependencies: split into cmake-config deps and fallback deps.
	var cmakeDeps []DepTarget
	var fallbackDeps []DepTarget
	for _, dep := range opts.Deps {
		if dep.TargetName != "" {
			cmakeDeps = append(cmakeDeps, dep)
		} else {
			fallbackDeps = append(fallbackDeps, dep)
		}
	}

	// CMake-config deps: CMAKE_PREFIX_PATH + find_package + target_link_libraries.
	if len(cmakeDeps) > 0 {
		for _, dep := range cmakeDeps {
			b.WriteString(fmt.Sprintf(
				"list(APPEND CMAKE_PREFIX_PATH \"${CMAKE_SOURCE_DIR}/cstow_deps/%s\")\n",
				dep.Name))
		}
		for _, dep := range cmakeDeps {
			b.WriteString(fmt.Sprintf("find_package(%s REQUIRED)\n", dep.Name))
		}
		var targets []string
		for _, dep := range cmakeDeps {
			targets = append(targets, dep.TargetName)
		}
		b.WriteString(fmt.Sprintf("target_link_libraries(%s PRIVATE %s)\n",
			opts.Name, strings.Join(targets, " ")))
	}

	// Fallback header-only deps: target_include_directories.
	if len(fallbackDeps) > 0 {
		var incPaths []string
		for _, dep := range fallbackDeps {
			incPaths = append(incPaths,
				fmt.Sprintf("${CMAKE_SOURCE_DIR}/cstow_deps/%s/include", dep.Name))
		}
		b.WriteString(fmt.Sprintf("target_include_directories(%s PRIVATE %s)\n",
			opts.Name, strings.Join(incPaths, " ")))
	}

	return b.String()
}
