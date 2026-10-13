package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestGoInitializationStartsEmptyAndPreservesSnapshotsOnReseed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite")
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
	for _, table := range []string{"posts", "events", "videos", "focus", "assets", "llm_providers", "llm_models", "llm_route_targets"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("fresh %s count=%d err=%v", table, count, err)
		}
	}
	var profile, timeline, resources int
	_ = db.QueryRow(`SELECT COUNT(*) FROM profiles`).Scan(&profile)
	_ = db.QueryRow(`SELECT COUNT(*) FROM timeline`).Scan(&timeline)
	_ = db.QueryRow(`SELECT COUNT(*) FROM resources`).Scan(&resources)
	if profile != 1 || timeline != 6 || resources != 10 {
		t.Fatalf("static seed=%d %d %d", profile, timeline, resources)
	}
	if _, err = db.Exec(`UPDATE profiles SET display_name='operator'; INSERT INTO app_settings(key,value,updated_at) VALUES('operator_setting','retained','2026-01-01T00:00:00Z'); INSERT INTO posts(id,source,text,published_at,url) VALUES('fixture','x','retained','2026-01-01T00:00:00Z','https://example.invalid/post')`); err != nil {
		t.Fatal(err)
	}
	s, err = OpenInitialized(p, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var name, setting string
	var posts int
	_ = db.QueryRow(`SELECT display_name FROM profiles`).Scan(&name)
	_ = db.QueryRow(`SELECT value FROM app_settings WHERE key='operator_setting'`).Scan(&setting)
	_ = db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&posts)
	if name != "operator" || setting != "retained" || posts != 1 {
		t.Fatal("reseed replaced operator state")
	}
	if _, err = s.CreateManualEvent(context.Background(), EventInput{ID: "disabled", Title: "disabled", StartsOn: "2026-01-01"}); err != ErrReadOnly {
		t.Fatalf("initialization silently enabled admin writes: %v", err)
	}
}
func TestInitializationRejectsUnknownSchemaWithoutChangingRows(t *testing.T) {
	p := filepath.Join(t.TempDir(), "snapshot.sqlite")
	db, _ := sql.Open("sqlite", p)
	_, err := db.Exec(`CREATE TABLE retained(id TEXT PRIMARY KEY,value TEXT); INSERT INTO retained VALUES('fixture','retained')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := OpenInitialized(p, true, true); err == nil {
		s.Close()
		t.Fatal("accepted unknown schema")
	}
	db, _ = sql.Open("sqlite", p)
	defer db.Close()
	var value string
	var tables int
	_ = db.QueryRow(`SELECT value FROM retained`).Scan(&value)
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables)
	if value != "retained" || tables != 1 {
		t.Fatal("failed initialization changed unknown schema")
	}
}
