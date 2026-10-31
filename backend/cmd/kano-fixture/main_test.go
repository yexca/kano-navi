package main

import (
	"path/filepath"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// The fixture schema is hand-written, so this keeps it in step with the
// read-only snapshot check before the Docker smoke has to find out.
func TestFixtureOpensAsReadOnlySnapshot(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite")
	if _, err := createFixture(databasePath); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.OpenReadOnly(databasePath)
	if err != nil {
		t.Fatalf("fixture does not satisfy the read-only schema: %v", err)
	}
	store.Close()
}
