package legacy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanFindPackage(t *testing.T) {
	dir := t.TempDir()
	cmake := `cmake_minimum_required(VERSION 3.16)
project(myapp)

find_package(fmt VERSION 10.2.1 REQUIRED)
find_package(spdlog REQUIRED)
find_package(ZLIB 1.3 REQUIRED)

add_executable(myapp main.cpp)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(cmake), 0o644))

	scanner := &CMakeScanner{}
	result, err := scanner.Scan(filepath.Join(dir, "CMakeLists.txt"))
	require.NoError(t, err)

	assert.Equal(t, 3, len(result.Dependencies))
	assert.Equal(t, "fmt", result.Dependencies[0].Name)
	assert.Equal(t, "^10.2.1", result.Dependencies[0].Version)
	assert.Equal(t, "spdlog", result.Dependencies[1].Name)
	assert.Equal(t, "zlib", result.Dependencies[2].Name)
}

func TestScanFetchContent(t *testing.T) {
	dir := t.TempDir()
	cmake := `cmake_minimum_required(VERSION 3.16)
project(myapp)

include(FetchContent)
FetchContent_Declare(
  googletest
  GIT_REPOSITORY https://github.com/google/googletest.git
  GIT_TAG v1.14.0
)
FetchContent_MakeAvailable(googletest)

add_executable(myapp main.cpp)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(cmake), 0o644))

	scanner := &CMakeScanner{}
	result, err := scanner.Scan(filepath.Join(dir, "CMakeLists.txt"))
	require.NoError(t, err)

	assert.Equal(t, 1, len(result.Dependencies))
	assert.Equal(t, "googletest", result.Dependencies[0].Name)
	assert.Equal(t, "https://github.com/google/googletest.git", result.Dependencies[0].Git)
	assert.Equal(t, "v1.14.0", result.Dependencies[0].Rev)
	assert.Equal(t, "^1.14.0", result.Dependencies[0].Version)
}

func TestScanEmpty(t *testing.T) {
	dir := t.TempDir()
	cmake := `cmake_minimum_required(VERSION 3.16)
project(myapp)
add_executable(myapp main.cpp)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(cmake), 0o644))

	scanner := &CMakeScanner{}
	result, err := scanner.Scan(filepath.Join(dir, "CMakeLists.txt"))
	require.NoError(t, err)
	assert.Equal(t, 0, len(result.Dependencies))
}

func TestScanMixed(t *testing.T) {
	dir := t.TempDir()
	cmake := `cmake_minimum_required(VERSION 3.16)
project(legacy-app LANGUAGES CXX)

find_package(fmt VERSION 10.2.1 REQUIRED)
find_package(ZLIB 1.3 REQUIRED)

include(FetchContent)
FetchContent_Declare(
  googletest
  GIT_REPOSITORY https://github.com/google/googletest.git
  GIT_TAG v1.14.0
)

add_executable(legacy-app main.cpp)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(cmake), 0o644))

	scanner := &CMakeScanner{}
	result, err := scanner.Scan(filepath.Join(dir, "CMakeLists.txt"))
	require.NoError(t, err)

	assert.Equal(t, 3, len(result.Dependencies), "should find 2 find_package + 1 FetchContent")
	assert.Equal(t, "fmt", result.Dependencies[0].Name)
	assert.Equal(t, "zlib", result.Dependencies[1].Name)
	assert.Equal(t, "googletest", result.Dependencies[2].Name)
	assert.Equal(t, "git", result.Dependencies[2].Source)
}

func TestGenerateCStowToml(t *testing.T) {
	cfg := GenerateCStowToml("mylib", "1.0.0", "c++17", ".", []string{"-DBUILD_SHARED=OFF"}, nil)
	assert.Equal(t, "mylib", cfg.Package.Name)
	assert.Equal(t, "c++17", cfg.Package.Std)
	assert.NotNil(t, cfg.Legacy)
	assert.Equal(t, "cmake", cfg.Legacy.Type)
}
