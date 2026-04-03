package artifactdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	Name       string
	Version    string
	ABITag     string
	BuildType  string
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
PRAGMA user_version = 1;
CREATE TABLE IF NOT EXISTS artifacts (
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
CREATE INDEX IF NOT EXISTS idx_artifacts_name ON artifacts (name);
CREATE INDEX IF NOT EXISTS idx_artifacts_updated_at ON artifacts (updated_at DESC);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func (s *Store) List() ([]Record, error) {
	rows, err := s.db.Query(`
SELECT name, version, abi_tag, build_type, install_dir, origin, created_at, updated_at, last_seen_at
FROM artifacts
ORDER BY name, version, abi_tag, build_type`)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var rec Record
		var createdAt, updatedAt, lastSeenAt string
		if err := rows.Scan(&rec.Name, &rec.Version, &rec.ABITag, &rec.BuildType, &rec.InstallDir, &rec.Origin, &createdAt, &updatedAt, &lastSeenAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
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

	var existingOrigin, existingInstallDir string
	err := s.db.QueryRow(`
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
INSERT INTO artifacts (name, version, abi_tag, build_type, install_dir, origin, created_at, updated_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name, version, abi_tag, build_type) DO UPDATE SET
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
		rec.Name, rec.Version, rec.ABITag, rec.BuildType, rec.InstallDir, rec.Origin,
		rec.CreatedAt.UTC().Format(time.RFC3339),
		rec.UpdatedAt.UTC().Format(time.RFC3339),
		rec.LastSeenAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert artifact: %w", err)
	}
	return nil
}
