package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestMissingSnapshotFailsWithoutCreatingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	if _, err := OpenReadOnly(path); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing database was created: %v", err)
	}
}

func TestMissingRequiredTableFailsAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete.sqlite")
	testutil.Exec(t, path, `CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	if _, err := OpenReadOnly(path); !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want ErrSchema", err)
	}
}

func TestMissingRequiredColumnFailsAtStartup(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `ALTER TABLE profiles DROP COLUMN updated_at`)
	if _, err := OpenReadOnly(path); !errors.Is(err, ErrSchema) {
		t.Fatalf("error = %v, want ErrSchema", err)
	}
}

func TestCorruptSnapshotFailsAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.sqlite")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadOnly(path); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestReadOnlyConnectionRejectsWrites(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('blocked', '1', '2026-10-11T00:00:00.000Z')`); err == nil {
		t.Fatal("read-only store accepted a write")
	}
}
