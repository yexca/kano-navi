package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// olderSnapshot builds a current database and then removes what an earlier
// release did not have yet, leaving operator data in the old shape.
func olderSnapshot(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "database", "kano.sqlite")
	s, err := OpenInitialized(p, true, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`DROP INDEX events_instant_idx`, `DROP INDEX events_source_item_idx`, `DROP INDEX events_stream_identity_idx`,
		`DROP INDEX posts_instant_idx`, `DROP INDEX posts_url_published_at_idx`, `DROP INDEX videos_instant_idx`, `DROP INDEX videos_schedule_instant_idx`,
		`ALTER TABLE assets DROP COLUMN asset_version`, `ALTER TABLE assets DROP COLUMN period_start`, `ALTER TABLE assets DROP COLUMN period_end`,
		`ALTER TABLE assets DROP COLUMN period_basis`, `ALTER TABLE assets DROP COLUMN manual_period_start`, `ALTER TABLE assets DROP COLUMN manual_period_end`,
		`ALTER TABLE assets DROP COLUMN manual_period_reason`,
		`ALTER TABLE profile_media DROP COLUMN active_sha256`, `ALTER TABLE profile_media DROP COLUMN active_mime_type`,
		`ALTER TABLE profile_media DROP COLUMN download_status`, `ALTER TABLE profile_media DROP COLUMN last_download_at`,
		`DROP INDEX schedule_asset_reviews_status_idx`, `ALTER TABLE schedule_asset_reviews DROP COLUMN llm_input_fingerprint`,
		`DROP INDEX events_merged_into_idx`, `ALTER TABLE events DROP COLUMN merged_into`, `ALTER TABLE events DROP COLUMN merge_source`,
		`ALTER TABLE events DROP COLUMN merge_reason`, `ALTER TABLE events DROP COLUMN merged_at`,
		`INSERT INTO events(id,source,title,starts_on,created_at,updated_at) VALUES('synthetic-event','x','retained','2026-03-02','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`,
		`DROP TABLE llm_route_targets`, `DROP TABLE workflow_step_attempts`, `DROP TABLE workflow_leases`, `DROP TABLE workflow_run_steps`,
		`DROP TABLE workflow_runs`, `DROP TABLE workflow_migrations`,
		`DELETE FROM app_settings WHERE key='llm_route_targets_migrated'`,
		`UPDATE resources SET title='旧 official site',detail='更新截至 2022 · 资料存档' WHERE id=9`,
		`INSERT INTO assets(id,kind,url,week_start) VALUES('synthetic-board','schedule','https://media.example.test/board.png','2026-03-02')`,
		`INSERT INTO posts(id,source,text,published_at,url) VALUES('synthetic-post','x','retained','2026-03-01T00:00:00.000Z','https://x.com/synthetic_handle/status/1')`,
		`INSERT INTO llm_providers(id,name,base_url,model,vision_capable,max_retries,created_at,updated_at) VALUES('synthetic','Synthetic','https://provider.example.test/v1','synthetic-model',0,0,'2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`,
		`INSERT INTO llm_route_providers(route,provider_id,priority,created_at,updated_at) VALUES('schedule_vision','synthetic',0,'2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`,
		`INSERT INTO profile_media(id,slot,source,active_cache_path,status,is_active,created_at,updated_at) VALUES('synthetic-avatar','avatar','upload','avatar/avatar.png','ready',1,'2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`,
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	return p, root
}

func TestStandaloneUpgradesOlderSnapshotAdditively(t *testing.T) {
	p, root := olderSnapshot(t)
	png := []byte("\x89PNG\r\n\x1a\nsynthetic")
	if err := os.MkdirAll(filepath.Join(root, "avatar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "avatar", "avatar.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenInitialized(p, true, true); err == nil {
		s.Close()
		t.Fatal("the development initializer must keep refusing an older schema")
	}
	s, err := OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	query := func(statement string, args ...any) string {
		t.Helper()
		var value sql.NullString
		if err := s.db.QueryRow(statement, args...).Scan(&value); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
		return value.String
	}
	sum := sha256.Sum256(png)
	for statement, want := range map[string]string{
		`SELECT period_start||'/'||period_end||'/'||period_basis||'/'||asset_version FROM assets WHERE id='synthetic-board'`:             "2026-03-02/2026-03-08/legacy/1",
		`SELECT account_handle FROM posts WHERE id='synthetic-post'`:                                                                     "synthetic_handle",
		`SELECT text FROM posts WHERE id='synthetic-post'`:                                                                               "retained",
		`SELECT title||'/'||COALESCE(merged_into,merge_source,merge_reason,merged_at,'unmerged') FROM events WHERE id='synthetic-event'`: "retained/unmerged",
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='events_merged_into_idx'`:                                        "1",
		`SELECT max_retries FROM llm_providers WHERE id='synthetic'`:                                                                     "2",
		`SELECT group_concat(route,',') FROM (SELECT route FROM llm_route_targets WHERE provider_id='synthetic' ORDER BY route)`:         "schedule",
		`SELECT model_id||'/'||origin FROM llm_models WHERE provider_id='synthetic'`:                                                     "synthetic-model/legacy",
		`SELECT value FROM app_settings WHERE key='llm_route_targets_migrated'`:                                                          "1",
		`SELECT title FROM resources WHERE id=9`:                                                                                         "official site",
		`SELECT active_sha256||'/'||active_mime_type FROM profile_media WHERE id='synthetic-avatar'`:                                     hex.EncodeToString(sum[:]) + "/image/png",
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN('events_instant_idx','events_stream_identity_idx','posts_instant_idx','posts_url_published_at_idx','videos_instant_idx','videos_schedule_instant_idx','events_source_item_idx','schedule_asset_reviews_status_idx')`: "8",
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN('workflow_runs','workflow_run_steps','workflow_step_attempts','workflow_leases','workflow_migrations')`:                                                                                                              "5",
	} {
		if got := query(statement); got != want {
			t.Fatalf("%s = %q, want %q", statement, got, want)
		}
	}
	if revision := query(`SELECT value FROM app_settings WHERE key='dashboard_revision'`); revision == "0" {
		t.Fatal("repairing the selected profile must tell browsers to reload")
	}
	// A second start changes nothing and stays quiet.
	revision := query(`SELECT value FROM app_settings WHERE key='dashboard_revision'`)
	s.Close()
	s, err = OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	if again := query(`SELECT value FROM app_settings WHERE key='dashboard_revision'`); again != revision {
		t.Fatalf("idempotent start changed the revision: %s -> %s", revision, again)
	}
	if count := query(`SELECT COUNT(*) FROM llm_route_targets`); count != "1" {
		t.Fatalf("idempotent start duplicated routes: %s", count)
	}
}

func TestStandaloneMergesStageRoutesIntoOneQueue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "snapshot.sqlite")
	s, err := OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	const stamp = "2026-03-01T00:00:00.000Z"
	for _, statement := range []string{
		`INSERT INTO llm_providers(id,name,base_url,model,created_at,updated_at) VALUES('alpha','Alpha','https://alpha.example.test/v1','a-1','` + stamp + `','` + stamp + `')`,
		`INSERT INTO llm_providers(id,name,base_url,model,created_at,updated_at) VALUES('beta','Beta','https://beta.example.test/v1','b-1','` + stamp + `','` + stamp + `')`,
		`INSERT INTO llm_providers(id,name,base_url,model,created_at,updated_at) VALUES('gamma','Gamma','https://gamma.example.test/v1','','` + stamp + `','` + stamp + `')`,
		`INSERT INTO llm_route_targets(route,provider_id,model_id,priority,created_at,updated_at) VALUES
			('schedule_vision','alpha','',0,'` + stamp + `','` + stamp + `'),
			('schedule_board','beta','b-vision',0,'` + stamp + `','` + stamp + `'),
			('schedule_board','alpha','',1,'` + stamp + `','` + stamp + `'),
			('schedule_board','beta','b-1',2,'` + stamp + `','` + stamp + `'),
			('schedule_message','beta','',0,'` + stamp + `','` + stamp + `'),
			('schedule_message','gamma','',1,'` + stamp + `','` + stamp + `')`,
	} {
		if _, err = s.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	s.Close()
	queue := func() string {
		t.Helper()
		s, err := OpenStandalone(p)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		var got sql.NullString
		if err = s.db.QueryRow(`SELECT group_concat(route||':'||provider_id||'/'||model_id||':'||priority,',') FROM
			(SELECT * FROM llm_route_targets ORDER BY route,priority)`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got.String
	}
	// Message, then board, then vision; a repeated provider/model pair keeps
	// its first position. Targets that followed the retired provider default
	// name that model, so beta/b-1 keeps the earlier slot, and gamma, which
	// never had a default, is dropped.
	want := "schedule:beta/b-1:0,schedule:beta/b-vision:2,schedule:alpha/a-1:3"
	if got := queue(); got != want {
		t.Fatalf("merged queue = %q, want %q", got, want)
	}
	if got := queue(); got != want {
		t.Fatalf("second start changed the queue: %q", got)
	}
}

func TestStandaloneRefusesUnrecognizedSchemaUnchanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "snapshot.sqlite")
	db, _ := sql.Open("sqlite", p)
	if _, err := db.Exec(`CREATE TABLE retained(id TEXT PRIMARY KEY,value TEXT); INSERT INTO retained VALUES('fixture','retained')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := OpenStandalone(p)
	if err == nil {
		s.Close()
		t.Fatal("accepted an unrecognized schema")
	}
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("error=%v", err)
	}
	db, _ = sql.Open("sqlite", p)
	defer db.Close()
	var value string
	var tables int
	_ = db.QueryRow(`SELECT value FROM retained`).Scan(&value)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables)
	if value != "retained" || tables != 1 {
		t.Fatal("a refused upgrade changed the snapshot")
	}
}
