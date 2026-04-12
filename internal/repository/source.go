package repository

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/all3n/cstow/internal/config"
)

type FetchOptions struct {
	Network *config.GlobalNetwork
}

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
	return FetchSourceWithOptions(srcDef, ver, version, expectedSHA256, destDir, FetchOptions{})
}

func FetchSourceWithOptions(srcDef SourceDef, ver *VersionOverride, version, expectedSHA256, destDir string, opts FetchOptions) (string, error) {
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
		if err := FetchGitWithOptions(url, tag, destDir, opts); err != nil {
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
		if err := FetchArchiveWithOptions(url, expectedSHA256, destDir, opts); err != nil {
			return "", fmt.Errorf("archive fetch %s: %w", url, err)
		}
		return destDir, nil
	default:
		return "", fmt.Errorf("unsupported source type %q", srcDef.Type)
	}
}

// FetchGit clones a repo and checks out the specified tag using a shallow clone.
func FetchGit(url, tag, destDir string) error {
	return FetchGitWithOptions(url, tag, destDir, FetchOptions{})
}

func FetchGitWithOptions(urlStr, tag, destDir string, opts FetchOptions) error {
	tag = defaultGitRef(tag)
	attempts := 1
	timeout := 0
	var env []string
	if opts.Network != nil {
		if opts.Network.Retries > 1 {
			attempts = opts.Network.Retries
		}
		timeout = opts.Network.Timeout
		env = gitNetworkEnv(opts.Network)
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		_ = os.RemoveAll(destDir)

		ctx := context.Background()
		var cancel context.CancelFunc
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		}

		cmd := exec.CommandContext(ctx, "git", "clone", "--branch", tag, "--depth", "1", urlStr, destDir)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() != nil {
				lastErr = fmt.Errorf("git clone timeout: %w", ctx.Err())
			} else {
				lastErr = fmt.Errorf("git clone failed: %s: %w", string(out), err)
			}
			if cancel != nil {
				cancel()
			}
			continue
		}
		if cancel != nil {
			cancel()
		}
		return nil
	}
	return lastErr
}

func SyncGitRepositoryWithOptions(urlStr, branch, destDir string, update bool, opts FetchOptions) error {
	if _, err := os.Stat(filepath.Join(destDir, ".git")); err == nil {
		if !update {
			return nil
		}
		return UpdateGitRepositoryWithOptions(branch, destDir, opts)
	}
	_ = os.RemoveAll(destDir)
	return FetchGitWithOptions(urlStr, branch, destDir, opts)
}

func SyncArchiveRepositoryWithOptions(urlStr, destDir string, update bool, opts FetchOptions) error {
	if _, err := os.Stat(destDir); err == nil && !update {
		return nil
	}
	_ = os.RemoveAll(destDir)
	return FetchArchiveWithOptions(urlStr, "", destDir, opts)
}

func UpdateGitRepositoryWithOptions(branch, destDir string, opts FetchOptions) error {
	branch = defaultGitRef(branch)
	if err := runGitCommandWithOptions([]string{"-C", destDir, "fetch", "--depth", "1", "origin", branch}, opts); err != nil {
		return fmt.Errorf("git fetch %s: %w", branch, err)
	}
	if err := runGitCommandWithOptions([]string{"-C", destDir, "reset", "--hard", "FETCH_HEAD"}, opts); err != nil {
		return fmt.Errorf("git reset %s: %w", branch, err)
	}
	return nil
}

func runGitCommandWithOptions(args []string, opts FetchOptions) error {
	timeout := 0
	var env []string
	if opts.Network != nil {
		timeout = opts.Network.Timeout
		env = gitNetworkEnv(opts.Network)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	}
	if cancel != nil {
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("git timeout: %w", ctx.Err())
		}
		return fmt.Errorf("%s: %w", string(out), err)
	}
	return nil
}

// FetchArchive downloads and extracts an archive to destDir.
func FetchArchive(url, expectedSHA256, destDir string) error {
	return FetchArchiveWithOptions(url, expectedSHA256, destDir, FetchOptions{})
}

func FetchArchiveWithOptions(urlStr, expectedSHA256, destDir string, opts FetchOptions) error {
	data, err := downloadArchive(urlStr, opts.Network)
	if err != nil {
		return err
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

	lowerURL := strings.ToLower(urlStr)
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

func downloadArchive(urlStr string, network *config.GlobalNetwork) ([]byte, error) {
	client, err := archiveHTTPClient(network)
	if err != nil {
		return nil, err
	}

	attempts := 1
	if network != nil && network.Retries > 1 {
		attempts = network.Retries
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := client.Get(urlStr)
		if err != nil {
			lastErr = fmt.Errorf("http get: %w", err)
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read body: %w", readErr)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("http status: %s", resp.Status)
			continue
		}
		return data, nil
	}

	return nil, lastErr
}

func archiveHTTPClient(network *config.GlobalNetwork) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if network != nil && network.Proxy != "" {
		proxyURL, err := url.Parse(network.Proxy)
		if err != nil {
			return nil, fmt.Errorf("parse proxy: %w", err)
		}
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			if shouldBypassProxy(req.URL.Hostname(), network.NoProxy) {
				return nil, nil
			}
			return proxyURL, nil
		}
	}

	client := &http.Client{Transport: transport}
	if network != nil && network.Timeout > 0 {
		client.Timeout = time.Duration(network.Timeout) * time.Second
	}
	return client, nil
}

func shouldBypassProxy(host string, noProxy []string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return false
	}
	for _, pattern := range noProxy {
		pattern = strings.TrimSpace(strings.ToLower(pattern))
		if pattern == "" {
			continue
		}
		if host == pattern || strings.HasSuffix(host, "."+pattern) {
			return true
		}
	}
	return false
}

func gitNetworkEnv(network *config.GlobalNetwork) []string {
	if network == nil || network.Proxy == "" {
		if network == nil || len(network.NoProxy) == 0 {
			return nil
		}
		return []string{
			"NO_PROXY=" + strings.Join(network.NoProxy, ","),
			"no_proxy=" + strings.Join(network.NoProxy, ","),
		}
	}

	env := []string{
		"HTTP_PROXY=" + network.Proxy,
		"HTTPS_PROXY=" + network.Proxy,
		"http_proxy=" + network.Proxy,
		"https_proxy=" + network.Proxy,
	}
	if len(network.NoProxy) > 0 {
		joined := strings.Join(network.NoProxy, ",")
		env = append(env,
			"NO_PROXY="+joined,
			"no_proxy="+joined,
		)
	}
	return env
}

func defaultGitRef(ref string) string {
	if strings.TrimSpace(ref) == "" {
		return "main"
	}
	return ref
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
