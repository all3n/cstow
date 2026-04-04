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
		rel = filepath.ToSlash(rel)

		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, linkTarget)
			if err != nil {
				return err
			}
			hdr.Name = rel
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
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
	cleanDir := filepath.Clean(dir)
	cleanDirWithSep := cleanDir + string(os.PathSeparator)

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
		if cleanTarget != cleanDir && !strings.HasPrefix(cleanTarget, cleanDirWithSep) {
			return fmt.Errorf("path traversal detected: %s", hdr.Name)
		}

		parent := filepath.Dir(target)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}

		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return err
		}
		resolvedParent = filepath.Clean(resolvedParent)
		if resolvedParent != cleanDir && !strings.HasPrefix(resolvedParent, cleanDirWithSep) {
			return fmt.Errorf("path traversal detected via symlink: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeSymlink:
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if existing, err := os.Lstat(target); err == nil && existing.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to write through symlink: %s", hdr.Name)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		default:
			return fmt.Errorf("unsupported tar entry type: %v", hdr.Typeflag)
		}
	}

	return nil
}
