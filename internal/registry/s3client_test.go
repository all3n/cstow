package registry

import (
	"bytes"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactObjectKeyIncludesBuildType(t *testing.T) {
	assert.Equal(t,
		"prefix/fmt/10.2.1/gcc13-cxx17-linux-x86_64/shared.tar.zst",
		artifactObjectKey("prefix", "fmt", "10.2.1", "gcc13-cxx17-linux-x86_64", "shared"),
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
