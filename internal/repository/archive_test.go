package repository

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTarGz(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := map[string]string{
		"root/file1.txt":     "content1",
		"root/subdir/f2.txt": "content2",
	}

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(content)),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	dest := t.TempDir()
	require.NoError(t, extractTarGz(buf.Bytes(), dest))

	for name, content := range files {
		data, err := os.ReadFile(filepath.Join(dest, name))
		assert.NoError(t, err)
		assert.Equal(t, content, string(data))
	}
}

func TestExtractZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := map[string]string{
		"root/file1.txt":     "content1",
		"root/subdir/f2.txt": "content2",
	}

	for name, content := range files {
		f, err := zw.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())

	dest := t.TempDir()
	require.NoError(t, extractZip(buf.Bytes(), dest))

	for name, content := range files {
		data, err := os.ReadFile(filepath.Join(dest, name))
		assert.NoError(t, err)
		assert.Equal(t, content, string(data))
	}
}

func TestStripComponentsAndMove(t *testing.T) {
	tmp := t.TempDir()
	// Create a single root dir structure
	// tmp/myapp-1.0/include/myapp.h
	// tmp/myapp-1.0/src/main.c
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "myapp-1.0", "include"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "myapp-1.0", "src"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "myapp-1.0", "include", "myapp.h"), []byte("h"), 0644))

	dest := t.TempDir()
	require.NoError(t, stripComponentsAndMove(tmp, dest))

	// Should be moved to dest/include/myapp.h
	assert.FileExists(t, filepath.Join(dest, "include", "myapp.h"))
	assert.DirExists(t, filepath.Join(dest, "src"))
}

func TestFetchArchive(t *testing.T) {
	// Mock server to serve a tar.gz
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: "pkg/hello.txt", Mode: 0644, Size: 5}
	require.NoError(t, tw.WriteHeader(hdr))
	tw.Write([]byte("hello"))
	tw.Close()
	gw.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer ts.Close()

	dest := t.TempDir()
	// Pass a URL that ends in .tar.gz by adding a dummy path segment
	require.NoError(t, FetchArchive(ts.URL+"/file.tar.gz", "", dest))
	assert.FileExists(t, filepath.Join(dest, "hello.txt")) // stripped
}

func TestFetchArchiveHonorsRetries(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: "pkg/hello.txt", Mode: 0644, Size: 5}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "try again", http.StatusBadGateway)
			return
		}
		_, _ = w.Write(buf.Bytes())
	}))
	defer ts.Close()

	dest := t.TempDir()
	err = FetchArchiveWithOptions(ts.URL+"/file.tar.gz", "", dest, FetchOptions{
		Network: &config.GlobalNetwork{Retries: 3, Timeout: 2},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(3), attempts.Load())
	assert.FileExists(t, filepath.Join(dest, "hello.txt"))
}

func TestFetchArchiveHonorsTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	}))
	defer ts.Close()

	dest := t.TempDir()
	err := FetchArchiveWithOptions(ts.URL+"/file.tar.gz", "", dest, FetchOptions{
		Network: &config.GlobalNetwork{Timeout: 1, Retries: 1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}
