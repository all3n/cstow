package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenSSL_VersionResolution(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	repoRoot := filepath.Join(home, "workspaces", "cstow-repository")

	// Skip if the external repository doesn't exist (e.g. in some CI environments)
	if _, err := os.Stat(repoRoot); err != nil {
		t.Skipf("External repository not found at %s, skipping", repoRoot)
	}

	finder := NewFinderWithPaths([]string{repoRoot})
	
	tests := []struct {
		version string
		wantURL string
	}{
		{"3.0.19", "https://www.openssl.org/source/openssl-3.0.19.tar.gz"},
		{"1.1.1w", "https://www.openssl.org/source/openssl-1.1.1w.tar.gz"},
		{"1.0.2u", "https://www.openssl.org/source/openssl-1.0.2u.tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			pkg, err := finder.Find("openssl", tt.version)
			require.NoError(t, err)
			assert.Equal(t, tt.version, pkg.Version)
			
			url := ExpandTagTemplate(pkg.Def.Source.URLTemplate, pkg.Version)
			assert.Equal(t, tt.wantURL, url)
		})
	}
}
