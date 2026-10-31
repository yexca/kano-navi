package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

func TestRunPrunerRecordsEachPass(t *testing.T) {
	store, err := sqlite.OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	pruner := &RunPruner{Store: store, Now: func() time.Time { return now }}
	if status := pruner.Status(); status["lastPrunedAt"] != nil || status["running"] != false {
		t.Fatalf("before=%v", status)
	}
	if _, err = pruner.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := pruner.Status()
	if status["lastPrunedAt"] != "2026-06-01T00:00:00.000Z" || status["lastDeleted"] != 0 || status["lastFailed"] != false || status["intervalHours"] != 24 {
		t.Fatalf("after=%v", status)
	}
	pruner.Start()
	pruner.Start()
	pruner.Stop()
	pruner.Stop()
}
