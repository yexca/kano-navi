package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/testutil"
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

func TestReadContainedFileHonorsPublicMediaBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversize.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxCachedMediaBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readContainedFile(path, maxCachedMediaBytes); err == nil {
		t.Fatal("oversized media file was accepted")
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

func TestMissingDashboardRawJSONColumnFailsAtStartup(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `ALTER TABLE posts DROP COLUMN raw_json`)
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

func TestReadOnlyDashboardLeavesSnapshotAndDirectoryUnchanged(t *testing.T) {
	path := testutil.NewSnapshot(t)
	beforeHash := fileHash(t, path)
	beforeEntries := directoryEntries(t, filepath.Dir(path))
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDashboard(context.Background(), 3, time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if got := fileHash(t, path); got != beforeHash {
		t.Fatalf("read-only dashboard changed snapshot hash: before=%s after=%s", beforeHash, got)
	}
	afterEntries := directoryEntries(t, filepath.Dir(path))
	if len(afterEntries) != len(beforeEntries) {
		t.Fatalf("read-only dashboard changed directory entries: before=%v after=%v", beforeEntries, afterEntries)
	}
	for name := range beforeEntries {
		if !afterEntries[name] {
			t.Fatalf("read-only dashboard removed or changed directory entry %q", name)
		}
	}
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only dashboard created %s: %v", suffix, err)
		}
	}
}

func TestReadOnlyDashboardReadsCommittedWALData(t *testing.T) {
	path := testutil.NewSnapshot(t)
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if _, err := writer.Exec(`INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','41','2026-10-11T00:00:00Z')`); err != nil {
		t.Fatalf("write WAL fixture: %v", err)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("open WAL snapshot read-only: %v", err)
	}
	defer store.Close()
	revision, err := store.GetDashboardRevision(context.Background())
	if err != nil {
		t.Fatalf("read committed WAL data: %v", err)
	}
	if revision != 41 {
		t.Fatalf("revision=%d, want 41 from committed WAL data", revision)
	}
}

func fileHash(t testing.TB, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func directoryEntries(t testing.TB, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read directory %s: %v", dir, err)
	}
	out := make(map[string]bool, len(entries))
	for _, entry := range entries {
		out[entry.Name()] = true
	}
	return out
}
