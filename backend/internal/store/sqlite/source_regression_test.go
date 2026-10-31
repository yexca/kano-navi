package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestExpiredSourceLeaseCannotCommitPostsOrRevision(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	def := &WorkflowDefinition{ID: AdhocFetchWorkflow, Steps: []string{"x"}}
	r, err := s.CreateWorkflowRun(ctx, *def, "operator", "", map[string]bool{"x": false}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.ClaimWorkflowRun(ctx, r.ID, "stale-worker", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, p, `UPDATE workflow_leases SET expires_at='2026-01-01T00:00:00Z'`)
	result := xsource.Result{Posts: []xsource.Post{{ID: "111111111111", AccountHandle: "fixture", Type: "daily", Text: "must not write", PublishedAt: "2026-01-01T00:00:00Z", URL: "https://example.invalid/post"}}}
	if err = s.WriteXRecent(WithSourceLease(ctx, l), "fixture", result, true); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("source fence=%v", err)
	}
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&count)
	rev, _ := s.GetDashboardRevision(ctx)
	if count != 0 || rev != 0 {
		t.Fatalf("stale write posts=%d rev=%d", count, rev)
	}
}

func TestLegacyMediaIdentityRepairKeepsBytesLinksAndUniqueSource(t *testing.T) {
	p := testutil.NewSnapshot(t)
	testutil.Exec(t, p, `DROP TABLE media_links`)
	testutil.Exec(t, p, `CREATE TABLE media_links(id INTEGER PRIMARY KEY,media_id TEXT NOT NULL REFERENCES media_assets(id),owner_type TEXT NOT NULL,owner_id TEXT NOT NULL,role TEXT NOT NULL,position INTEGER NOT NULL DEFAULT 0,alt TEXT,created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`)
	testutil.Exec(t, p, `CREATE UNIQUE INDEX media_assets_source_url_idx ON media_assets(source_url)`)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u := "https://pbs.twimg.com/fixture.png"
	h := sha256.Sum256([]byte(u))
	canonical := hex.EncodeToString(h[:])
	legacy := "x-" + canonical[:32]
	testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status,sha256,cache_path,created_at,updated_at) VALUES(?,'x',?,'ready',?,'x/sha256/fixture.png','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, legacy, u, canonical)
	testutil.Exec(t, p, `INSERT INTO media_links(media_id,owner_type,owner_id,role,created_at,updated_at) VALUES(?,'post','fixture','post-image','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, legacy)
	if err = s.RepairLegacyXMedia(context.Background()); err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	var id, status, hash, cache, link string
	if err = db.QueryRow(`SELECT id,status,sha256,cache_path FROM media_assets`).Scan(&id, &status, &hash, &cache); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT media_id FROM media_links`).Scan(&link)
	if id != canonical || link != canonical || status != "ready" || hash != canonical || cache != "x/sha256/fixture.png" {
		t.Fatalf("identity repair changed data %s %s %s %s %s", id, link, status, hash, cache)
	}
	revision, _ := s.GetDashboardRevision(context.Background())
	if err = s.RepairLegacyXMedia(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetDashboardRevision(context.Background())
	if after != revision {
		t.Fatal("repeat repair changed revision")
	}
}
func TestWorkflowV1UpgradeRetainsRunsAttemptsAndFrozenInputs(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// A stored run of a saved workflow keeps its ID.
	def := &WorkflowDefinition{ID: "x-recent", Steps: []string{"x"}}
	r, err := s.CreateWorkflowRun(ctx, *def, "operator", "fixture-key", map[string]bool{"x": true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.ClaimWorkflowRun(ctx, r.ID, "fixture", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := s.StartWorkflowStep(ctx, l, r.Steps[0].ID, time.Now())
	_ = s.FinishWorkflowStep(ctx, l, r.Steps[0].ID, n, WorkflowOutcome{Status: "succeeded"}, false, time.Now())
	_ = s.FinishWorkflowRun(ctx, l, "succeeded", "", time.Now())
	s.Close()
	// Reconstruct a synthetic v1 snapshot to exercise the additive upgrade.
	testutil.Exec(t, p, `ALTER TABLE workflow_runs DROP COLUMN source_lookback_json`)
	testutil.Exec(t, p, `ALTER TABLE workflow_run_steps DROP COLUMN adapter_capability`)
	testutil.Exec(t, p, `ALTER TABLE workflow_runs DROP COLUMN source_sync_json`)
	testutil.Exec(t, p, `ALTER TABLE workflows DROP COLUMN source_sync_json`)
	testutil.Exec(t, p, `UPDATE workflow_migrations SET version=1`)
	old, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := old.GetWorkflowRun(ctx, r.ID)
	old.Close()
	if err != nil || got.Status != "succeeded" || len(got.Steps[0].Attempts) != 1 {
		t.Fatalf("v1 read=%#v %v", got, err)
	}
	// Sync settings come from the source schedules, which read as their
	// defaults without a table.
	if old, err = OpenReadOnly(p); err != nil {
		t.Fatal(err)
	}
	settings, err := old.SourceScheduleSync(ctx)
	old.Close()
	if err != nil || settings["x"] != (SourceSync{Mode: SyncIncremental, Max: DefaultSyncMax}) {
		t.Fatalf("legacy sync=%#v %v", settings, err)
	}
	s, err = OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.GetWorkflowRun(ctx, r.ID)
	if err != nil || got.Status != "succeeded" || got.WorkflowID != "x-recent" || len(got.Steps[0].Attempts) != 1 {
		t.Fatalf("upgraded=%#v %v", got, err)
	}
	// The unread workflows table stays in the schema, with its columns.
	var columns int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('workflows') WHERE name IN ('steps_json','schedule_enabled','source_sync_json')`).Scan(&columns); err != nil || columns != 3 {
		t.Fatalf("workflows columns=%d %v", columns, err)
	}
	def.SourceSync = map[string]SourceSync{"x": {Mode: SyncCount, Count: 3}}
	def.SourceLookbackDays = map[string]int{"x": 3}
	newRun, err := s.CreateWorkflowRun(ctx, *def, "operator", "fresh", map[string]bool{"x": false}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.GetWorkflowRun(ctx, newRun.ID)
	if err != nil || read.SourceSync["x"] != (SourceSync{Mode: SyncCount, Count: 3}) || read.SourceLookbackDays["x"] != 3 || read.Steps[0].Capability != "x_recent" {
		t.Fatalf("frozen inputs=%#v %v", read, err)
	}
}
