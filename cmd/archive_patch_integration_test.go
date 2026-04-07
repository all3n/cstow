package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallFromRepositoryWithArchiveAndPatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	requireTool(t, "cmake")
	requireTool(t, "patch")
	requireTool(t, "g++")

	// 1. Create a mock source archive (.tar.gz)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := map[string]string{
		"mini-1.0.0/CMakeLists.txt": `cmake_minimum_required(VERSION 3.15)
project(mini-patch LANGUAGES CXX)
add_executable(mini main.cpp)
install(TARGETS mini RUNTIME DESTINATION bin)
`,
		"mini-1.0.0/main.cpp": `#include <iostream>
int main() {
    std::cout << "original" << std::endl;
    return 0;
}
`,
	}

	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}
		require.NoError(t, tw.WriteHeader(hdr))
		tw.Write([]byte(content))
	}
	tw.Close()
	gw.Close()

	archiveData := buf.Bytes()
	sum := sha256.Sum256(archiveData)
	archiveSHA := fmt.Sprintf("%x", sum)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveData)
	}))
	defer ts.Close()

	// 2. Setup fake home and repository
	fakeHome := t.TempDir()
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	repoRoot := filepath.Join(fakeHome, "repository")
	pkgDir := filepath.Join(repoRoot, "m", "mini-patch")
	require.NoError(t, os.MkdirAll(filepath.Join(pkgDir, "versions"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(pkgDir, "patches"), 0755))

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)

	// 3. Write package.toml and version override with patch
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "package.toml"), []byte(fmt.Sprintf(`
versions = ["1.0.0"]
[package]
name = "mini-patch"
[source]
type = "archive"
url_template = %q
[build]
system = "cmake"
type = "static"
`, ts.URL+"/mini-1.0.0.tar.gz")), 0644))

	// The patch changes "original" to "patched"
	patchContent := `--- a/main.cpp
+++ b/main.cpp
@@ -1,5 +1,5 @@
 #include <iostream>
 int main() {
-    std::cout << "original" << std::endl;
+    std::cout << "patched" << std::endl;
     return 0;
 }
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "patches", "fix.patch"), []byte(patchContent), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "versions", "1.0.0.toml"), []byte(fmt.Sprintf(`
patch = "fix.patch"
[source]
sha256 = %q
`, archiveSHA)), 0644))

	// 4. Setup global config
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeHome, ".cstow", "config.toml"), []byte(fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10
[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)), 0644))

	// 5. Run install
	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc", nil)
	require.NoError(t, err)

	result, err := installFromRepository("mini-patch", "1.0.0", repositoryInstallOptions{
		Context: ctx,
		Force:   true,
	})
	require.NoError(t, err)

	// 6. Verify installation and patch
	exePath := filepath.Join(result.InstallDir, "bin", "mini")
	if runtime.GOOS == "windows" {
		exePath += ".exe"
	}
	assert.FileExists(t, exePath)

	// Run the executable and check output
	out, err := exec.Command(exePath).Output()
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(out), "patched"), "Expected output to contain 'patched', got %q", string(out))
}
