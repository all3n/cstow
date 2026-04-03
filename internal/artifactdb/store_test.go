package artifactdb

import (
	"path/filepath"
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
