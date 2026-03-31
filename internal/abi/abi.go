package abi

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// ABITag uniquely identifies the binary interface of a compiled artifact.
// Format: <compiler><major>-cxx<std_year>-<stdlib>-<os>-<arch>[-<extra>]
//
// Examples:
//
//	gcc13-cxx17-libstdc-linux-x86_64
//	clang17-cxx20-libcxx-macos-arm64
//	msvc193-cxx17-msvcrt-windows-x64
type ABITag struct {
	Compiler string // gcc | clang | msvc | appleclang
	CompVer  int    // major compiler version
	CxxStd   int    // 14 | 17 | 20 | 23
	Stdlib   string // libstdc | libcxx | msvcrt
	OS       string // linux | macos | windows | android | ios
	Arch     string // x86_64 | aarch64 | arm | wasm32 | x64
	Extra    string // android api level, etc.
}

// String serializes the ABI tag
func (t ABITag) String() string {
	s := fmt.Sprintf("%s%d-cxx%d-%s-%s-%s",
		t.Compiler, t.CompVer, t.CxxStd, t.Stdlib, t.OS, t.Arch)
	if t.Extra != "" {
		s += "-" + t.Extra
	}
	return s
}

// Parse deserializes an ABI tag string
func Parse(s string) (ABITag, error) {
	parts := strings.Split(s, "-")
	if len(parts) < 6 {
		return ABITag{}, fmt.Errorf("invalid ABI tag: %s (need at least 6 components)", s)
	}

	// Parse compiler+version (e.g. "gcc13")
	compiler, compVer := parseCompilerPart(parts[0])

	// Parse cxx standard (e.g. "cxx17")
	cxxStd := parseCxxPart(parts[1])

	// Parts: compiler-ver, cxxStd, stdlib, os, arch[, extra...]
	tag := ABITag{
		Compiler: compiler,
		CompVer:  compVer,
		CxxStd:   cxxStd,
		Stdlib:   parts[2],
		OS:       parts[3],
		Arch:     parts[4],
	}
	if len(parts) > 5 {
		tag.Extra = strings.Join(parts[5:], "-")
	}
	return tag, nil
}

// Compatible checks if the "have" ABI can satisfy the "need" ABI.
// Rules:
// 1. OS and Arch must match exactly
// 2. Stdlib must match (libstdc++ and libc++ are binary-incompatible)
// 3. C++ std is upward-compatible (have >= need)
// 4. Compiler family must match; version is upward-compatible within same family
// 5. MSVC: only same major version compatible
func Compatible(have, need ABITag) bool {
	// 1. OS and Arch must match
	if have.OS != need.OS || have.Arch != need.Arch {
		return false
	}

	// 2. Stdlib must match
	if have.Stdlib != need.Stdlib {
		return false
	}

	// 3. C++ std upward compatible
	if have.CxxStd < need.CxxStd {
		return false
	}

	// 4. Compiler family must match
	if !sameCompilerFamily(have.Compiler, need.Compiler) {
		return false
	}

	// 5. MSVC: same major version only
	if have.Compiler == "msvc" || need.Compiler == "msvc" {
		return have.CompVer == need.CompVer
	}

	// 6. Compiler version upward compatible
	if have.CompVer < need.CompVer {
		return false
	}

	return true
}

// DetectFromToolchain builds an ABI tag from the detected toolchain info
func DetectFromToolchain(compilerKind string, compVer [3]int, cxxStd string) ABITag {
	stdMap := map[string]int{
		"c++14": 14, "c++17": 17, "c++20": 20, "c++23": 23,
		"14": 14, "17": 17, "20": 20, "23": 23,
	}
	std := stdMap[cxxStd]
	if std == 0 {
		std = 17 // default
	}

	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "macos"
	case "windows":
		osName = "windows"
	default:
		osName = "linux"
	}

	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	case "arm":
		arch = "arm"
	}

	stdlib := "libstdc"
	if compilerKind == "clang" || compilerKind == "appleclang" {
		if osName == "macos" {
			stdlib = "libcxx"
		}
	}
	if compilerKind == "msvc" {
		stdlib = "msvcrt"
	}

	return ABITag{
		Compiler: compilerKind,
		CompVer:  compVer[0],
		CxxStd:   std,
		Stdlib:   stdlib,
		OS:       osName,
		Arch:     arch,
	}
}

func parseCompilerPart(s string) (string, int) {
	// "gcc13" -> "gcc", 13
	// "clang17" -> "clang", 17
	// "msvc193" -> "msvc", 193
	// "appleclang15" -> "appleclang", 15
	for _, prefix := range []string{"appleclang", "clang", "msvc", "gcc"} {
		if strings.HasPrefix(s, prefix) {
			verStr := strings.TrimPrefix(s, prefix)
			ver, _ := strconv.Atoi(verStr)
			return prefix, ver
		}
	}
	return s, 0
}

func parseCxxPart(s string) int {
	// "cxx17" -> 17
	return atoi(strings.TrimPrefix(s, "cxx"))
}

func sameCompilerFamily(a, b string) bool {
	if a == b {
		return true
	}
	// clang and appleclang are considered same family
	if (a == "clang" || a == "appleclang") && (b == "clang" || b == "appleclang") {
		return true
	}
	return false
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
