package artifactdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreUpsertAndList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "gcc13-cxx17-linux-x86_64",
		BuildType:  "shared",
		InstallDir: "/tmp/cache/fmt/10.2.1/gcc13-cxx17-linux-x86_64/shared",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fmt", rows[0].Name)
	assert.Equal(t, "shared", rows[0].BuildType)
	assert.Equal(t, "registry", rows[0].Origin)
	assert.Equal(t, "", rows[0].HashID)
	assert.Empty(t, rows[0].BuildTags)
}

func TestStoreUpsertPreservesKnownOriginWhenUnknownSeenLater(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	firstSeen := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi",
		BuildType:  "shared",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi/shared",
		Origin:     "registry",
		CreatedAt:  firstSeen,
		UpdatedAt:  firstSeen,
		LastSeenAt: firstSeen,
	}))

	secondSeen := firstSeen.Add(time.Hour)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi",
		BuildType:  "shared",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi/shared",
		Origin:     "unknown",
		CreatedAt:  secondSeen,
		UpdatedAt:  secondSeen,
		LastSeenAt: secondSeen,
	}))

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "registry", rows[0].Origin)
	assert.Equal(t, secondSeen.UTC(), rows[0].LastSeenAt.UTC())
}

func TestStoreFindByHashIDSupportsExactAndUniquePrefix(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "gcc13-cxx17-linux-x86_64",
		BuildType:  "shared",
		HashID:     "aabbccddeeff0011",
		BuildTags:  []string{"lto", "asan"},
		InstallDir: "/tmp/cache/fmt",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	exact, err := store.FindByHashID("aabbccddeeff0011")
	require.NoError(t, err)
	assert.Equal(t, "fmt", exact.Name)
	assert.Equal(t, []string{"lto", "asan"}, exact.BuildTags)

	prefix, err := store.FindByHashID("aabbccdd")
	require.NoError(t, err)
	assert.Equal(t, "aabbccddeeff0011", prefix.HashID)
}

func TestStoreFindByHashIDReturnsAmbiguityWithCandidates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi-a",
		BuildType:  "shared",
		HashID:     "abc111",
		InstallDir: "/tmp/cache/fmt/a",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, store.Upsert(Record{
		Name:       "spdlog",
		Version:    "1.14.0",
		ABITag:     "abi-b",
		BuildType:  "static",
		HashID:     "abc222",
		InstallDir: "/tmp/cache/spdlog/b",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	_, err = store.FindByHashID("abc")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "abc111"))
	assert.True(t, strings.Contains(err.Error(), "abc222"))
}

func TestStoreUpsertRoundTripsHashMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "gcc13-cxx17-linux-x86_64",
		BuildType:  "shared",
		HashID:     "f00dbabe",
		BuildTags:  []string{"lto", "sanitizer=address"},
		InstallDir: "/tmp/cache/fmt/10.2.1/gcc13-cxx17-linux-x86_64/shared",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "f00dbabe", rows[0].HashID)
	assert.Equal(t, []string{"lto", "sanitizer=address"}, rows[0].BuildTags)
}

func TestOpenMigratesLegacyDBWithoutHashColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	_, err = db.Exec(`
PRAGMA user_version = 1;
CREATE TABLE artifacts (
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    abi_tag TEXT NOT NULL,
    build_type TEXT NOT NULL DEFAULT '',
    install_dir TEXT NOT NULL,
    origin TEXT NOT NULL DEFAULT 'unknown',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (name, version, abi_tag, build_type)
);
CREATE INDEX idx_artifacts_name ON artifacts (name);
CREATE INDEX idx_artifacts_updated_at ON artifacts (updated_at DESC);
`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
}

func TestStoreUpsertPreservesExistingHashMetadataWhenOmitted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	firstSeen := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi",
		BuildType:  "shared",
		HashID:     "hash-12345",
		BuildTags:  []string{"lto", "asan"},
		InstallDir: "/tmp/cache/fmt/10.2.1/abi/shared",
		Origin:     "registry",
		CreatedAt:  firstSeen,
		UpdatedAt:  firstSeen,
		LastSeenAt: firstSeen,
	}))

	secondSeen := firstSeen.Add(time.Hour)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi",
		BuildType:  "shared",
		HashID:     "",
		BuildTags:  nil,
		InstallDir: "/tmp/cache/fmt/10.2.1/abi/shared",
		Origin:     "registry",
		CreatedAt:  secondSeen,
		UpdatedAt:  secondSeen,
		LastSeenAt: secondSeen,
	}))

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "hash-12345", rows[0].HashID)
	assert.Equal(t, []string{"lto", "asan"}, rows[0].BuildTags)
	assert.Equal(t, secondSeen.UTC(), rows[0].LastSeenAt.UTC())
}

func TestOpenDefaultUsesResolvedArtifactDBPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".cstow", "config.toml"), []byte(`
[cache]
dir = "~/configured-cache"
`), 0o644))

	store, err := OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = os.Stat(filepath.Join(home, "cstow.db"))
	assert.NoError(t, err)
}
