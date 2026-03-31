package pack

import (
	"os"
	"path/filepath"
	"testing"

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
