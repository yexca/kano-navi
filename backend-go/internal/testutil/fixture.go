package testutil

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func NewSnapshot(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE sync_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			status TEXT NOT NULL,
			message TEXT,
			counts_json TEXT,
			triggered_by TEXT,
			job_id TEXT
		);
		CREATE TABLE app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	return path
}

func Exec(t testing.TB, path, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture for write: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
