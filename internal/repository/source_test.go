package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTagTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		version  string
		want     string
	}{
		{"standard v prefix", "v{version}", "1.14.0", "v1.14.0"},
		{"no template", "", "1.14.0", "1.14.0"},
		{"plain version tag", "{version}", "1.14.0", "1.14.0"},
		{"release prefix", "release-{version}", "2.0.0", "release-2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExpandTagTemplate(tt.template, tt.version))
		})
	}
}

func TestFetchSource_UnsupportedType(t *testing.T) {
	_, err := FetchSource(SourceDef{Type: "ftp"}, nil, "1.0.0", "", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported source type")
}

func TestFetchSource_ArchiveNotImplemented(t *testing.T) {
	// Archive is now implemented but will fail with invalid URL
	_, err := FetchSource(SourceDef{Type: "archive", URLTemplate: "http://invalid-url.local"}, nil, "1.0.0", "", t.TempDir())
	assert.Error(t, err)
}

func TestFetchGitWithOptionsSetsProxyEnv(t *testing.T) {
	tmpBin := t.TempDir()
	envLog := filepath.Join(t.TempDir(), "env.txt")
	require.NoError(t, os.WriteFile(filepath.Join(tmpBin, "git"), []byte(`#!/usr/bin/env bash
set -euo pipefail
printf 'HTTP_PROXY=%s\nHTTPS_PROXY=%s\nNO_PROXY=%s\n' "${HTTP_PROXY:-}" "${HTTPS_PROXY:-}" "${NO_PROXY:-}" > "$ECC_GIT_ENV_LOG"
mkdir -p "$6"
`), 0o755))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpBin+string(os.PathListSeparator)+origPath)
	t.Setenv("ECC_GIT_ENV_LOG", envLog)

	err := FetchGitWithOptions("https://example.com/repo.git", "v1.0.0", t.TempDir(), FetchOptions{
		Network: &config.GlobalNetwork{
			Proxy:   "http://proxy.internal:8080",
			NoProxy: []string{"localhost", "internal.example.com"},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(envLog)
	require.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, "HTTP_PROXY=http://proxy.internal:8080")
	assert.Contains(t, out, "HTTPS_PROXY=http://proxy.internal:8080")
	assert.Contains(t, out, "NO_PROXY=localhost,internal.example.com")
}

func TestFetchGitWithOptionsHonorsTimeout(t *testing.T) {
	tmpBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpBin, "git"), []byte(`#!/usr/bin/env bash
sleep 2
`), 0o755))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpBin+string(os.PathListSeparator)+origPath)

	err := FetchGitWithOptions("https://example.com/repo.git", "v1.0.0", t.TempDir(), FetchOptions{
		Network: &config.GlobalNetwork{Timeout: 1, Retries: 1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git clone timeout")
	assert.Contains(t, err.Error(), "deadline exceeded")
}

func TestFetchGitWithOptionsHonorsRetries(t *testing.T) {
	tmpBin := t.TempDir()
	attemptFile := filepath.Join(t.TempDir(), "attempts")
	require.NoError(t, os.WriteFile(filepath.Join(tmpBin, "git"), []byte(`#!/usr/bin/env bash
set -euo pipefail
count=0
if [[ -f "$ECC_GIT_ATTEMPTS" ]]; then
  count=$(cat "$ECC_GIT_ATTEMPTS")
fi
count=$((count + 1))
printf '%s' "$count" > "$ECC_GIT_ATTEMPTS"
if [[ "$count" -lt 3 ]]; then
  exit 1
fi
mkdir -p "$6"
`), 0o755))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpBin+string(os.PathListSeparator)+origPath)
	t.Setenv("ECC_GIT_ATTEMPTS", attemptFile)

	err := FetchGitWithOptions("https://example.com/repo.git", "v1.0.0", t.TempDir(), FetchOptions{
		Network: &config.GlobalNetwork{Retries: 3},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(attemptFile)
	require.NoError(t, err)
	assert.Equal(t, "3", string(data))
}
