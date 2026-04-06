package repository

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExpandTagTemplate replaces "{version}" in a tag template.
// "v{version}" + "1.14.0" → "v1.14.0".
// If template is empty, returns the version as-is.
func ExpandTagTemplate(template, version string) string {
	if template == "" {
		return version
	}
	return strings.ReplaceAll(template, "{version}", version)
}

// FetchSource fetches source code for a package to destDir.
// Returns the path to the source root.
// Supports git (clone + checkout) and archive sources.
func FetchSource(srcDef SourceDef, ver *VersionOverride, version, expectedSHA256, destDir string) (string, error) {
	switch srcDef.Type {
	case "git":
		url := srcDef.URL
		if ver != nil && ver.Source != nil && ver.Source.URL != "" {
			url = ver.Source.URL
		}
		tagTemplate := srcDef.TagTemplate
		if ver != nil && ver.Source != nil && ver.Source.URLTemplate != "" {
			tagTemplate = ver.Source.URLTemplate
		}
		tag := ExpandTagTemplate(tagTemplate, version)
		if err := FetchGit(url, tag, destDir); err != nil {
			return "", fmt.Errorf("git fetch %s@%s: %w", url, tag, err)
		}
		return destDir, nil
	case "archive":
		urlTemplate := srcDef.URLTemplate
		if ver != nil && ver.Source != nil && ver.Source.URLTemplate != "" {
			urlTemplate = ver.Source.URLTemplate
		}
		url := ExpandTagTemplate(urlTemplate, version)
		if ver != nil && ver.Source != nil && ver.Source.URL != "" {
			url = ver.Source.URL
		}
		if err := FetchArchive(url, expectedSHA256, destDir); err != nil {
			return "", fmt.Errorf("archive fetch %s: %w", url, err)
		}
		return destDir, nil
	default:
		return "", fmt.Errorf("unsupported source type %q", srcDef.Type)
	}
}

// FetchGit clones a repo and checks out the specified tag using a shallow clone.
func FetchGit(url, tag, destDir string) error {
	cmd := exec.Command("git", "clone", "--branch", tag, "--depth", "1", url, destDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %s: %w", string(out), err)
	}
	return nil
}

// FetchArchive downloads and extracts an archive to destDir.
func FetchArchive(url, expectedSHA256, destDir string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if expectedSHA256 != "" {
		sum := sha256.Sum256(data)
		actual := fmt.Sprintf("%x", sum)
		if !strings.EqualFold(actual, expectedSHA256) {
			return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA256, actual)
		}
	}

	tmpExtract, err := os.MkdirTemp("", "cstow-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpExtract)

	lowerURL := strings.ToLower(url)
	if strings.HasSuffix(lowerURL, ".zip") {
		if err := extractZip(data, tmpExtract); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	} else if strings.HasSuffix(lowerURL, ".tar.gz") || strings.HasSuffix(lowerURL, ".tgz") {
		if err := extractTarGz(data, tmpExtract); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	} else {
		// Fallback to calling system tar for unknown extensions (e.g. .tar.xz)
		if err := extractSystemTar(data, tmpExtract); err != nil {
			return fmt.Errorf("extract via system tar: %w", err)
		}
	}

	return stripComponentsAndMove(tmpExtract, destDir)
}

func extractZip(data []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(data []byte, destDir string) error {
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractSystemTar(data []byte, destDir string) error {
	cmd := exec.Command("tar", "-x", "-C", destDir)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func stripComponentsAndMove(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	moveSrc := src
	if len(entries) == 1 && entries[0].IsDir() {
		moveSrc = filepath.Join(src, entries[0].Name())
	}

	// Move contents of moveSrc to dst
	subEntries, err := os.ReadDir(moveSrc)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for _, e := range subEntries {
		oldPath := filepath.Join(moveSrc, e.Name())
		newPath := filepath.Join(dst, e.Name())
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
	}
	return nil
}
