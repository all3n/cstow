package registry

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/all3n/cstow/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactObjectKeyIncludesBuildType(t *testing.T) {
	assert.Equal(t,
		"prefix/fmt/10.2.1/gcc13-cxx17-linux-x86_64/shared/hash-abc123.tar.zst",
		artifactObjectKey("prefix", "fmt", "10.2.1", "gcc13-cxx17-linux-x86_64", "shared", "hash-abc123"),
	)
}

func TestLegacyArtifactObjectKey(t *testing.T) {
	assert.Equal(t,
		"prefix/fmt/10.2.1/gcc13-cxx17-linux-x86_64.tar.zst",
		legacyArtifactObjectKey("prefix", "fmt", "10.2.1", "gcc13-cxx17-linux-x86_64"),
	)
}

func TestManifestRoundTripIncludesBuildType(t *testing.T) {
	manifest := &Manifest{
		Name:    "fmt",
		Version: "10.2.1",
		Artifacts: []Artifact{
			{
				ABITag:    "gcc13-cxx17-linux-x86_64",
				BuildType: "shared",
				SHA256:    "abc123",
				Size:      42,
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, toml.NewEncoder(&buf).Encode(manifest))

	var decoded Manifest
	require.NoError(t, toml.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Artifacts, 1)
	assert.Equal(t, "shared", decoded.Artifacts[0].BuildType)
}

func TestManifestRoundTripIncludesHashMetadata(t *testing.T) {
	manifest := &Manifest{
		Name:    "fmt",
		Version: "10.2.1",
		Artifacts: []Artifact{
			{
				ABITag:    "gcc13-cxx17-linux-x86_64",
				BuildType: "shared",
				HashID:    "aabbccddeeff00112233445566778899",
				BuildTags: []string{"lto", "asan"},
				SHA256:    "abc123",
				Size:      42,
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, toml.NewEncoder(&buf).Encode(manifest))

	var decoded Manifest
	require.NoError(t, toml.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Artifacts, 1)
	assert.Equal(t, "aabbccddeeff00112233445566778899", decoded.Artifacts[0].HashID)
	assert.Equal(t, []string{"lto", "asan"}, decoded.Artifacts[0].BuildTags)
}

func TestSelectArtifactPrefersExactBuildType(t *testing.T) {
	manifest := &Manifest{
		Artifacts: []Artifact{
			{ABITag: "gcc13", BuildType: "static"},
			{ABITag: "gcc13", BuildType: "shared"},
		},
	}

	artifact, err := SelectArtifact(manifest, []string{"gcc13"}, "shared")
	require.NoError(t, err)
	require.NotNil(t, artifact)
	assert.Equal(t, "shared", artifact.BuildType)
}

func TestSelectArtifactFallsBackToLegacyArtifactForExplicitBuildType(t *testing.T) {
	manifest := &Manifest{
		Artifacts: []Artifact{
			{ABITag: "gcc13"},
		},
	}

	artifact, err := SelectArtifact(manifest, []string{"gcc13"}, "shared")
	require.NoError(t, err)
	require.NotNil(t, artifact)
	assert.Equal(t, "", artifact.BuildType)
}

func TestSelectArtifactDoesNotGuessTypedArtifactWhenBuildTypeIsUnspecified(t *testing.T) {
	manifest := &Manifest{
		Artifacts: []Artifact{
			{ABITag: "gcc13", BuildType: "static"},
			{ABITag: "gcc13", BuildType: "shared"},
		},
	}

	artifact, err := SelectArtifact(manifest, []string{"gcc13"}, "")
	require.Error(t, err)
	assert.Nil(t, artifact)
}

func TestResolveRegistryRuntimeConfigPrefersExplicitRegistryValues(t *testing.T) {
	t.Setenv("CSTOW_REGISTRY_URL", "https://env.example.com")
	t.Setenv("CSTOW_REGISTRY_KEY", "env-key")
	t.Setenv("CSTOW_REGISTRY_SECRET", "env-secret")

	cfg, err := resolveRegistryRuntimeConfig(context.Background(), config.Registry{
		Name:        "default",
		URL:         "s3://bucket/prefix",
		EndpointURL: "https://config.example.com",
		AccessKey:   "cfg-key",
		SecretKey:   "cfg-secret",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://env.example.com", cfg.EndpointURL)
	assert.Equal(t, "env-key", cfg.AccessKey)
	assert.Equal(t, "env-secret", cfg.SecretKey)
	assert.True(t, cfg.UsePathStyle)
}

func TestResolveRegistryRuntimeConfigUsesSharedConfigEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("CSTOW_REGISTRY_URL", "")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".aws", "config"), []byte(`
[profile cstow]
region = us-east-1
s3 =
    endpoint_url = https://profile.example.com
`), 0o644))

	cfg, err := resolveRegistryRuntimeConfig(context.Background(), config.Registry{
		Name:    "default",
		URL:     "s3://bucket/prefix",
		Profile: "cstow",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://profile.example.com", cfg.EndpointURL)
	assert.True(t, cfg.UsePathStyle)
}

func TestResolveRegistryRuntimeConfigPrefersRegistryCredentialsOverSharedCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("CSTOW_REGISTRY_KEY", "")
	t.Setenv("CSTOW_REGISTRY_SECRET", "")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".aws", "credentials"), []byte(`
[cstow]
aws_access_key_id = shared-key
aws_secret_access_key = shared-secret
`), 0o644))

	cfg, err := resolveRegistryRuntimeConfig(context.Background(), config.Registry{
		Name:      "default",
		URL:       "s3://bucket/prefix",
		Profile:   "cstow",
		AccessKey: "cfg-key",
		SecretKey: "cfg-secret",
	})
	require.NoError(t, err)
	assert.Equal(t, "cfg-key", cfg.AccessKey)
	assert.Equal(t, "cfg-secret", cfg.SecretKey)
}
