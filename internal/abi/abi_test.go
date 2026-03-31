package abi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestString(t *testing.T) {
	tag := ABITag{
		Compiler: "gcc",
		CompVer:  13,
		CxxStd:   17,
		Stdlib:   "libstdc",
		OS:       "linux",
		Arch:     "x86_64",
	}
	assert.Equal(t, "gcc13-cxx17-libstdc-linux-x86_64", tag.String())

	tag2 := ABITag{
		Compiler: "clang",
		CompVer:  16,
		CxxStd:   17,
		Stdlib:   "libcxx",
		OS:       "linux",
		Arch:     "aarch64",
		Extra:    "android29",
	}
	assert.Equal(t, "clang16-cxx17-libcxx-linux-aarch64-android29", tag2.String())
}

func TestParse(t *testing.T) {
	tag, err := Parse("gcc13-cxx17-libstdc-linux-x86_64")
	require.NoError(t, err)
	assert.Equal(t, "gcc", tag.Compiler)
	assert.Equal(t, 13, tag.CompVer)
	assert.Equal(t, 17, tag.CxxStd)
	assert.Equal(t, "libstdc", tag.Stdlib)
	assert.Equal(t, "linux", tag.OS)
	assert.Equal(t, "x86_64", tag.Arch)

	tag2, err := Parse("msvc193-cxx17-msvcrt-windows-x64")
	require.NoError(t, err)
	assert.Equal(t, "msvc", tag2.Compiler)
	assert.Equal(t, 193, tag2.CompVer)

	tag3, err := Parse("clang16-cxx17-libcxx-linux-aarch64-android29")
	require.NoError(t, err)
	assert.Equal(t, "clang", tag3.Compiler)
	assert.Equal(t, "android29", tag3.Extra)
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse("short")
	assert.Error(t, err)
}

func TestCompatible(t *testing.T) {
	tests := []struct {
		name string
		have ABITag
		need ABITag
		want bool
	}{
		{
			"exact match",
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			true,
		},
		{
			"higher gcc version",
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			ABITag{Compiler: "gcc", CompVer: 11, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			true,
		},
		{
			"higher cxx std",
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 20, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			true,
		},
		{
			"lower cxx std",
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 14, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			false,
		},
		{
			"different stdlib",
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			ABITag{Compiler: "clang", CompVer: 17, CxxStd: 17, Stdlib: "libcxx", OS: "linux", Arch: "x86_64"},
			false,
		},
		{
			"different OS",
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "linux", Arch: "x86_64"},
			ABITag{Compiler: "gcc", CompVer: 13, CxxStd: 17, Stdlib: "libstdc", OS: "macos", Arch: "x86_64"},
			false,
		},
		{
			"msvc same major",
			ABITag{Compiler: "msvc", CompVer: 193, CxxStd: 17, Stdlib: "msvcrt", OS: "windows", Arch: "x64"},
			ABITag{Compiler: "msvc", CompVer: 193, CxxStd: 17, Stdlib: "msvcrt", OS: "windows", Arch: "x64"},
			true,
		},
		{
			"msvc different major",
			ABITag{Compiler: "msvc", CompVer: 193, CxxStd: 17, Stdlib: "msvcrt", OS: "windows", Arch: "x64"},
			ABITag{Compiler: "msvc", CompVer: 192, CxxStd: 17, Stdlib: "msvcrt", OS: "windows", Arch: "x64"},
			false,
		},
		{
			"clang family compatible",
			ABITag{Compiler: "appleclang", CompVer: 15, CxxStd: 17, Stdlib: "libcxx", OS: "macos", Arch: "aarch64"},
			ABITag{Compiler: "clang", CompVer: 15, CxxStd: 17, Stdlib: "libcxx", OS: "macos", Arch: "aarch64"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compatible(tt.have, tt.need)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	original := ABITag{
		Compiler: "gcc",
		CompVer:  13,
		CxxStd:   17,
		Stdlib:   "libstdc",
		OS:       "linux",
		Arch:     "x86_64",
	}
	s := original.String()
	parsed, err := Parse(s)
	require.NoError(t, err)
	assert.Equal(t, original, parsed)
}

func TestDetectFromToolchain(t *testing.T) {
	tag := DetectFromToolchain("gcc", [3]int{11, 4, 0}, "c++17")
	assert.Equal(t, "gcc", tag.Compiler)
	assert.Equal(t, 11, tag.CompVer)
	assert.Equal(t, 17, tag.CxxStd)
	assert.NotEmpty(t, tag.OS)
	assert.NotEmpty(t, tag.Arch)
}
