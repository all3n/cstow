package pack

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// CreateTarZst creates a .tar.zst archive from a directory
func CreateTarZst(dir string) ([]byte, error) {
	var buf bytes.Buffer

	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, err
	}

	tw := tar.NewWriter(enc)

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		hdr := &tar.Header{
			Name: rel,
			Mode: int64(info.Mode()),
			Size: info.Size(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ExtractTarZst extracts a .tar.zst archive to a directory
func ExtractTarZst(data []byte, dir string) error {
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer dec.Close()

	tr := tar.NewReader(dec)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dir, hdr.Name)
		cleanTarget := filepath.Clean(target)
		cleanDir := filepath.Clean(dir) + string(os.PathSeparator)
		if !strings.HasPrefix(cleanTarget, cleanDir) {
			return fmt.Errorf("path traversal detected: %s", hdr.Name)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
		if err != nil {
			return err
		}

		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}

	return nil
}
