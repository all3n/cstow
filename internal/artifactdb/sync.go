package artifactdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var validBuildTypes = map[string]struct{}{
	"static":      {},
	"shared":      {},
	"header-only": {},
}

func (s *Store) SyncFromCache(cacheRoot string) (SyncStats, error) {
	now := time.Now().UTC()
	records, err := scanCache(cacheRoot, now)
	if err != nil {
		return SyncStats{}, err
	}

	var stats SyncStats
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		key := strings.Join([]string{rec.Name, rec.Version, rec.ABITag, rec.BuildType}, "\x00")
		seen[key] = struct{}{}
		existed, changed, err := s.upsertAndReport(rec)
		if err != nil {
			return SyncStats{}, err
		}
		switch {
		case !existed:
			stats.Inserted++
		case changed:
			stats.Updated++
		}
	}

	rows, err := s.List()
	if err != nil {
		return SyncStats{}, err
	}
	for _, rec := range rows {
		key := strings.Join([]string{rec.Name, rec.Version, rec.ABITag, rec.BuildType}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		if _, err := s.db.Exec(`
DELETE FROM artifacts
WHERE name = ? AND version = ? AND abi_tag = ? AND build_type = ?`,
			rec.Name, rec.Version, rec.ABITag, rec.BuildType,
		); err != nil {
			return SyncStats{}, fmt.Errorf("delete stale artifact: %w", err)
		}
		stats.Deleted++
	}

	return stats, nil
}

func scanCache(cacheRoot string, now time.Time) ([]Record, error) {
	if _, err := os.Stat(cacheRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat cache root: %w", err)
	}

	var records []Record
	err := filepath.WalkDir(cacheRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == cacheRoot {
			return nil
		}

		rel, err := filepath.Rel(cacheRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		switch len(parts) {
		case 3:
			if hasFiles(path) && !hasTypedChildren(path) {
				records = append(records, Record{
					Name:       parts[0],
					Version:    parts[1],
					ABITag:     parts[2],
					BuildType:  "",
					InstallDir: path,
					Origin:     "unknown",
					CreatedAt:  now,
					UpdatedAt:  now,
					LastSeenAt: now,
				})
				return filepath.SkipDir
			}
		case 4:
			if _, ok := validBuildTypes[parts[3]]; ok && hasFiles(path) {
				records = append(records, Record{
					Name:       parts[0],
					Version:    parts[1],
					ABITag:     parts[2],
					BuildType:  parts[3],
					InstallDir: path,
					Origin:     "unknown",
					CreatedAt:  now,
					UpdatedAt:  now,
					LastSeenAt: now,
				})
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan cache: %w", err)
	}
	return records, nil
}

func hasFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func hasTypedChildren(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := validBuildTypes[entry.Name()]; ok {
			return true
		}
	}
	return false
}

func (s *Store) upsertAndReport(rec Record) (bool, bool, error) {
	var oldInstallDir, oldOrigin string
	err := s.db.QueryRow(`
SELECT install_dir, origin
FROM artifacts
WHERE name = ? AND version = ? AND abi_tag = ? AND build_type = ?`,
		rec.Name, rec.Version, rec.ABITag, rec.BuildType,
	).Scan(&oldInstallDir, &oldOrigin)
	if err != nil && err != sql.ErrNoRows {
		return false, false, fmt.Errorf("query artifact before sync upsert: %w", err)
	}
	existed := err == nil
	changed := !existed || oldInstallDir != rec.InstallDir || (rec.Origin != "unknown" && oldOrigin != rec.Origin)
	if err := s.Upsert(rec); err != nil {
		return false, false, err
	}
	return existed, changed, nil
}
