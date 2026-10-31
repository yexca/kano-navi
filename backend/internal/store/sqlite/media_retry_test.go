package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaRetryDelayIsBoundedAndRejectsPermanentFailures(t *testing.T) {
	for _, tc := range []struct {
		code     string
		attempts int
		want     time.Duration
	}{
		{"media_timeout", 1, 30 * time.Minute},
		{"media_timeout", 2, time.Hour},
		{"media_rate_limited", 3, 2 * time.Hour},
		{"media_unavailable", 12, 24 * time.Hour},
		{"media_body_unavailable", 100, 24 * time.Hour},
		{"media_missing", 1, 24 * time.Hour},
		{"media_http_error", 1, 24 * time.Hour},
		{"media_invalid_image", 1, 0},
		{"media_destination_rejected", 1, 0},
		{"media_response_oversized", 1, 0},
	} {
		if got := mediaRetryDelay(tc.code, tc.attempts); got != tc.want {
			t.Errorf("%s attempt %d = %s, want %s", tc.code, tc.attempts, got, tc.want)
		}
	}
}

func TestMediaRetryUpgradeSurvivesRestartRediscoveryAndRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "database", "snapshot.sqlite")
	s, err := OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	u := "https://pbs.twimg.com/synthetic-retry.png"
	id, _, _ := mediaIdentity(u)
	if _, err = s.db.Exec(`INSERT INTO media_assets(id,source,source_url,status,last_error,created_at,updated_at) VALUES(?,'x',?,'failed','media_timeout','','')`, id, u); err != nil {
		t.Fatal(err)
	}
	// Reproduce a snapshot before retry scheduling was persisted.
	for _, column := range []string{"retry_attempts", "next_retry_at"} {
		if _, err = s.db.Exec(`ALTER TABLE media_assets DROP COLUMN ` + column); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	eligible, err := s.PendingMedia(ctx, 1)
	if err != nil || len(eligible) != 1 || eligible[0].ID != id {
		t.Fatalf("legacy failure lost: %#v %v", eligible, err)
	}
	asset := eligible[0]
	if err = s.SaveMediaDownload(ctx, asset, nil, "", "", "", "failed", "media_timeout"); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = s.db.QueryRow(`SELECT next_retry_at FROM media_assets WHERE id=?`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = replaceMediaLink(ctx, tx, "x", u, "post", "synthetic", "post-image", 0, ISOTime(time.Now())); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var after, status string
	var attempts int
	if err = s.db.QueryRow(`SELECT next_retry_at,status,retry_attempts FROM media_assets WHERE id=?`, id).Scan(&after, &status, &attempts); err != nil || after != before || status != "failed" || attempts != 1 {
		t.Fatalf("rediscovery reset retry: %s %s %d %v", after, status, attempts, err)
	}
	eligible, err = s.PendingMedia(ctx, 1)
	if err != nil || len(eligible) != 0 {
		t.Fatalf("restart bypassed backoff: %#v %v", eligible, err)
	}
	if _, err = s.db.Exec(`UPDATE media_assets SET next_retry_at='2000-01-01T00:00:00Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	eligible, err = s.PendingMedia(ctx, 1)
	if err != nil || len(eligible) != 1 {
		t.Fatalf("due retry unavailable: %#v %v", eligible, err)
	}
	if err = s.SaveMediaDownload(ctx, asset, []byte("\x89PNG\r\n\x1a\nsynthetic"), "image/png", "", "", "ready", ""); err != nil {
		t.Fatal(err)
	}
	var retry sql.NullString
	if err = s.db.QueryRow(`SELECT next_retry_at,status,retry_attempts FROM media_assets WHERE id=?`, id).Scan(&retry, &status, &attempts); err != nil || retry.Valid || status != "ready" || attempts != 0 {
		t.Fatalf("successful download kept retry: %v %s %d %v", retry, status, attempts, err)
	}
}

func TestReadyMediaRetryRetainsBytesAndCannotBypassBackoff(t *testing.T) {
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "snapshot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u := "https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg"
	id, _, _ := mediaIdentity(u)
	if _, err = s.db.Exec(`INSERT INTO videos(id,source,title,url,is_upcoming,live_checked_at) VALUES('AAAAAAAAAAA','youtube','synthetic','https://example.invalid/video',1,?)`, ISOTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO media_assets(id,source,source_url,status,created_at,updated_at) VALUES(?,'youtube',?,'pending','','')`, id, u); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO media_links(media_id,owner_type,owner_id,role,created_at,updated_at) VALUES(?,'video','AAAAAAAAAAA','thumbnail','','')`, id); err != nil {
		t.Fatal(err)
	}
	asset := PendingMedia{ID: id, Source: "youtube", SourceURL: u}
	content := []byte("\x89PNG\r\n\x1a\nsynthetic")
	if err = s.SaveMediaDownload(ctx, asset, content, "image/png", `"synthetic"`, "", "ready", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveMediaDownload(ctx, asset, nil, "", "", "", "failed", "media_rate_limited"); err != nil {
		t.Fatal(err)
	}
	file, err := s.ReadMedia(ctx, id, "")
	if err != nil || string(file.Content) != string(content) {
		t.Fatalf("failed revalidation lost bytes: %v", err)
	}
	if pending, err := s.PendingMedia(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("new watch check bypassed backoff: %#v %v", pending, err)
	}
	if _, err = s.db.Exec(`UPDATE media_assets SET next_retry_at='2000-01-01T00:00:00Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].ETag != `"synthetic"` {
		t.Fatalf("ready retry lost validator: %#v %v", pending, err)
	}
	if err = s.MarkMediaUnchanged(ctx, pending[0]); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var retry, failure sql.NullString
	if err = s.db.QueryRow(`SELECT retry_attempts,next_retry_at,last_error FROM media_assets WHERE id=?`, id).Scan(&attempts, &retry, &failure); err != nil || attempts != 0 || retry.Valid || failure.Valid {
		t.Fatalf("304 retained failure state: %d %v %v %v", attempts, retry, failure, err)
	}
	if err = s.SaveMediaDownload(ctx, asset, nil, "", "", "", "failed", "media_invalid_image"); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingMedia(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("permanent failure was retried: %#v %v", pending, err)
	}
}
