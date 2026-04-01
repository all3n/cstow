package pack

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAndExtractTarZst(t *testing.T) {
	// Create a source directory with files
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("nested content"), 0o644))

	// Create archive
	data, err := CreateTarZst(srcDir)
	require.NoError(t, err)
	assert.True(t, len(data) > 0)

	// Extract
	dstDir := t.TempDir()
	require.NoError(t, ExtractTarZst(data, dstDir))

	// Verify
	content, err := os.ReadFile(filepath.Join(dstDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))

	content2, err := os.ReadFile(filepath.Join(dstDir, "sub", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(content2))
}

func TestExtractTarZstPathTraversal(t *testing.T) {
	// Build a malicious tar.zst with path traversal
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	require.NoError(t, err)

	tw := tar.NewWriter(enc)

	// Try to write ../../etc/passwd
	hdr := &tar.Header{
		Name: "../../tmp/evil.txt",
		Mode: 0o644,
		Size: int64(len("evil")),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err = tw.Write([]byte("evil"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, enc.Close())

	dstDir := t.TempDir()
	err = ExtractTarZst(buf.Bytes(), dstDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestCreateTarZstManyFiles(t *testing.T) {
	// Create enough files to verify FDs are released properly
	srcDir := t.TempDir()
	for i := 0; i < 50; i++ {
		name := filepath.Join(srcDir, filepath.Join("file", string(rune('a'+i%26))))
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(t, os.WriteFile(name, []byte("content"), 0o644))
	}

	data, err := CreateTarZst(srcDir)
	require.NoError(t, err)
	assert.True(t, len(data) > 0)
}
