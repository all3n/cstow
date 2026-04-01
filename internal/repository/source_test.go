package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
	_, err := FetchSource(SourceDef{Type: "ftp"}, "1.0.0", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported source type")
}

func TestFetchSource_ArchiveNotImplemented(t *testing.T) {
	_, err := FetchSource(SourceDef{Type: "archive"}, "1.0.0", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}
