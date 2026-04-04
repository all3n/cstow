package artifactdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	Name       string
	Version    string
	ABITag     string
	BuildType  string
	HashID     string
	BuildTags  []string
	InstallDir string
	Origin     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastSeenAt time.Time
}

type Store struct {
	db *sql.DB
}

type SyncStats struct {
	Inserted int
	Updated  int
	Deleted  int
}

func OpenDefault() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".cstow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return Open(filepath.Join(dir, "cstow.db"))
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	const schema = `
CREATE TABLE IF NOT EXISTS artifacts (
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    abi_tag TEXT NOT NULL,
    build_type TEXT NOT NULL DEFAULT '',
    hash_id TEXT NOT NULL DEFAULT '',
    build_tags TEXT NOT NULL DEFAULT '[]',
    install_dir TEXT NOT NULL,
    origin TEXT NOT NULL DEFAULT 'unknown',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (name, version, abi_tag, build_type)
);
CREATE INDEX IF NOT EXISTS idx_artifacts_name ON artifacts (name);
CREATE INDEX IF NOT EXISTS idx_artifacts_updated_at ON artifacts (updated_at DESC);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	if err := s.ensureColumn("artifacts", "hash_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("artifacts", "build_tags", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_artifacts_hash_id_non_empty ON artifacts (hash_id) WHERE hash_id <> ''`); err != nil {
		return fmt.Errorf("create hash_id index: %w", err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 2`); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return nil
}

func (s *Store) List() ([]Record, error) {
	rows, err := s.db.Query(`
SELECT name, version, abi_tag, build_type, hash_id, build_tags, install_dir, origin, created_at, updated_at, last_seen_at
FROM artifacts
ORDER BY name, version, abi_tag, build_type`)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var rec Record
		var createdAt, updatedAt, lastSeenAt, buildTagsRaw string
		if err := rows.Scan(&rec.Name, &rec.Version, &rec.ABITag, &rec.BuildType, &rec.HashID, &buildTagsRaw, &rec.InstallDir, &rec.Origin, &createdAt, &updatedAt, &lastSeenAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		if rec.BuildTags, err = decodeBuildTags(buildTagsRaw); err != nil {
			return nil, fmt.Errorf("decode build tags for %s@%s: %w", rec.Name, rec.Version, err)
		}
		rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		rec.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) Upsert(rec Record) error {
	now := time.Now().UTC()
	if rec.Origin == "" {
		rec.Origin = "unknown"
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	if rec.LastSeenAt.IsZero() {
		rec.LastSeenAt = now
	}
	buildTagsJSON, err := encodeBuildTags(rec.BuildTags)
	if err != nil {
		return fmt.Errorf("encode build tags: %w", err)
	}

	var existingOrigin, existingInstallDir string
	err = s.db.QueryRow(`
SELECT origin, install_dir
FROM artifacts
WHERE name = ? AND version = ? AND abi_tag = ? AND build_type = ?`,
		rec.Name, rec.Version, rec.ABITag, rec.BuildType,
	).Scan(&existingOrigin, &existingInstallDir)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("query existing artifact: %w", err)
	}

	if err == nil {
		if rec.Origin == "unknown" && existingOrigin != "" && existingOrigin != "unknown" {
			rec.Origin = existingOrigin
		}
		if existingInstallDir == rec.InstallDir && existingOrigin == rec.Origin {
			rec.UpdatedAt = now
		}
	}

	_, err = s.db.Exec(`
INSERT INTO artifacts (name, version, abi_tag, build_type, hash_id, build_tags, install_dir, origin, created_at, updated_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name, version, abi_tag, build_type) DO UPDATE SET
    hash_id = CASE
        WHEN excluded.hash_id = '' THEN artifacts.hash_id
        ELSE excluded.hash_id
    END,
    build_tags = CASE
        WHEN excluded.build_tags = '[]' THEN artifacts.build_tags
        ELSE excluded.build_tags
    END,
    install_dir = excluded.install_dir,
    origin = CASE
        WHEN excluded.origin = 'unknown' AND artifacts.origin <> 'unknown' THEN artifacts.origin
        ELSE excluded.origin
    END,
    updated_at = CASE
        WHEN artifacts.install_dir = excluded.install_dir
         AND (artifacts.origin = excluded.origin OR excluded.origin = 'unknown')
        THEN artifacts.updated_at
        ELSE excluded.updated_at
    END,
    last_seen_at = excluded.last_seen_at`,
		rec.Name, rec.Version, rec.ABITag, rec.BuildType, rec.HashID, buildTagsJSON, rec.InstallDir, rec.Origin,
		rec.CreatedAt.UTC().Format(time.RFC3339),
		rec.UpdatedAt.UTC().Format(time.RFC3339),
		rec.LastSeenAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert artifact: %w", err)
	}
	return nil
}

func (s *Store) FindByHashID(prefix string) (Record, error) {
	var rec Record
	var createdAt, updatedAt, lastSeenAt, buildTagsRaw string
	err := s.db.QueryRow(`
SELECT name, version, abi_tag, build_type, hash_id, build_tags, install_dir, origin, created_at, updated_at, last_seen_at
FROM artifacts
WHERE hash_id = ?`,
		prefix,
	).Scan(&rec.Name, &rec.Version, &rec.ABITag, &rec.BuildType, &rec.HashID, &buildTagsRaw, &rec.InstallDir, &rec.Origin, &createdAt, &updatedAt, &lastSeenAt)
	if err == nil {
		rec.BuildTags, err = decodeBuildTags(buildTagsRaw)
		if err != nil {
			return Record{}, fmt.Errorf("decode build tags for hash_id %q: %w", rec.HashID, err)
		}
		rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		rec.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
		return rec, nil
	}
	if err != sql.ErrNoRows {
		return Record{}, fmt.Errorf("find by hash_id %q: %w", prefix, err)
	}

	rows, err := s.db.Query(`
SELECT name, version, abi_tag, build_type, hash_id, build_tags, install_dir, origin, created_at, updated_at, last_seen_at
FROM artifacts
WHERE hash_id LIKE ? AND hash_id <> ''
ORDER BY hash_id`,
		prefix+"%",
	)
	if err != nil {
		return Record{}, fmt.Errorf("find by hash_id prefix %q: %w", prefix, err)
	}
	defer rows.Close()

	var matches []Record
	for rows.Next() {
		var match Record
		var matchCreatedAt, matchUpdatedAt, matchLastSeenAt, matchBuildTagsRaw string
		if err := rows.Scan(&match.Name, &match.Version, &match.ABITag, &match.BuildType, &match.HashID, &matchBuildTagsRaw, &match.InstallDir, &match.Origin, &matchCreatedAt, &matchUpdatedAt, &matchLastSeenAt); err != nil {
			return Record{}, fmt.Errorf("scan hash_id match: %w", err)
		}
		match.BuildTags, err = decodeBuildTags(matchBuildTagsRaw)
		if err != nil {
			return Record{}, fmt.Errorf("decode build tags for hash_id %q: %w", match.HashID, err)
		}
		match.CreatedAt, _ = time.Parse(time.RFC3339, matchCreatedAt)
		match.UpdatedAt, _ = time.Parse(time.RFC3339, matchUpdatedAt)
		match.LastSeenAt, _ = time.Parse(time.RFC3339, matchLastSeenAt)
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return Record{}, fmt.Errorf("iterate hash_id matches: %w", err)
	}
	if len(matches) == 0 {
		return Record{}, fmt.Errorf("find by hash_id %q: %w", prefix, sql.ErrNoRows)
	}
	if len(matches) > 1 {
		candidates := make([]string, 0, len(matches))
		for _, match := range matches {
			candidates = append(candidates, match.HashID)
		}
		return Record{}, fmt.Errorf("hash_id prefix %q is ambiguous: %s", prefix, strings.Join(candidates, ", "))
	}
	return matches[0], nil
}

func (s *Store) ensureColumn(table, column, definition string) error {
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)
	if _, err := s.db.Exec(stmt); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func encodeBuildTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeBuildTags(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil, err
	}
	return tags, nil
}
