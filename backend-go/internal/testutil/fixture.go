package testutil

import (
	"context"
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
		CREATE TABLE profiles (
			id TEXT PRIMARY KEY, display_name TEXT NOT NULL, romanized_name TEXT,
			bio TEXT, avatar_url TEXT, banner_url TEXT, x_url TEXT, youtube_url TEXT, updated_at TEXT, raw_json TEXT
		);
		CREATE TABLE posts (
			id TEXT PRIMARY KEY, source TEXT NOT NULL, account_handle TEXT, type TEXT NOT NULL DEFAULT 'daily', label TEXT,
			text TEXT NOT NULL, published_at TEXT NOT NULL, url TEXT NOT NULL, likes INTEGER DEFAULT 0, reposts INTEGER DEFAULT 0,
			replies INTEGER DEFAULT 0, media_url TEXT, media_alt TEXT, raw_json TEXT, quote_json TEXT, quote_hidden_at TEXT, refresh_requested_at TEXT
		);
		CREATE TABLE post_llm_states (
			post_id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'never', route TEXT,
			last_attempt_at TEXT, last_processed_at TEXT, last_error TEXT,
			last_extraction_id INTEGER, reprocess_requested INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT '', failure_count INTEGER NOT NULL DEFAULT 0,
			failure_fingerprint TEXT, next_attempt_at TEXT
		);
		CREATE TABLE events (
			id TEXT PRIMARY KEY, source TEXT NOT NULL, source_item_id TEXT, source_key TEXT, title TEXT NOT NULL, detail TEXT,
			starts_on TEXT NOT NULL, starts_at TEXT, ends_at TEXT, timezone TEXT NOT NULL DEFAULT 'Asia/Tokyo',
			time_precision TEXT NOT NULL DEFAULT 'exact', status TEXT, event_type TEXT DEFAULT 'event', url TEXT,
			is_upcoming INTEGER DEFAULT 0, provenance TEXT NOT NULL DEFAULT 'automatic', manual_locked INTEGER NOT NULL DEFAULT 0,
			confidence REAL, extraction_id INTEGER, cancellation_status TEXT NOT NULL DEFAULT 'none', cancellation_source TEXT,
			cancellation_reason TEXT, cancellation_evidence TEXT, cancellation_source_item_id TEXT,
			cancellation_confidence REAL, cancellation_at TEXT, deleted_at TEXT,
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '', raw_json TEXT,
			merged_into TEXT, merge_source TEXT, merge_reason TEXT, merged_at TEXT
		);
		CREATE TABLE event_sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT NOT NULL, source TEXT NOT NULL, source_item_id TEXT NOT NULL,
			source_key TEXT, url TEXT, raw_json TEXT, deleted_at TEXT, created_at TEXT, updated_at TEXT
		);
		CREATE UNIQUE INDEX event_sources_identity_idx ON event_sources (source, source_item_id, source_key);
		CREATE TABLE videos (
			id TEXT PRIMARY KEY, source TEXT NOT NULL, title TEXT NOT NULL, published_at TEXT, scheduled_at TEXT,
			url TEXT NOT NULL, thumbnail_url TEXT, kind TEXT, is_upcoming INTEGER DEFAULT 0, raw_json TEXT,
			live_status TEXT, live_checked_at TEXT, duration_seconds INTEGER, manual_title TEXT, manual_kind TEXT,
			manual_thumbnail_media_id TEXT, manual_thumbnail_sha256 TEXT, manual_updated_at TEXT, refresh_requested_at TEXT,
			merged_into TEXT, merge_source TEXT, merge_reason TEXT, merged_at TEXT, is_short INTEGER, manual_is_short INTEGER,
			is_premiere INTEGER
		);
		CREATE TABLE video_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, video_id TEXT NOT NULL, origin TEXT NOT NULL, title TEXT, thumbnail_url TEXT,
			thumbnail_media_id TEXT, thumbnail_sha256 TEXT, kind TEXT, replaced_at TEXT NOT NULL
		);
		CREATE TABLE focus (
			id INTEGER PRIMARY KEY, video_id TEXT, date_label TEXT, title TEXT NOT NULL, description TEXT, image_url TEXT,
			url TEXT, source_url TEXT, updated_at TEXT, raw_json TEXT
		);
		CREATE TABLE timeline (id INTEGER PRIMARY KEY, year TEXT NOT NULL, title TEXT NOT NULL, detail TEXT, sort_order INTEGER DEFAULT 0, raw_json TEXT);
		CREATE TABLE resources (id INTEGER PRIMARY KEY, title TEXT NOT NULL, detail TEXT, icon TEXT, tone TEXT, url TEXT NOT NULL, sort_order INTEGER DEFAULT 0, raw_json TEXT);
		CREATE TABLE assets (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, url TEXT NOT NULL, source_url TEXT, alt TEXT, week_start TEXT, source_account TEXT,
			updated_at TEXT, raw_json TEXT, period_start TEXT, period_end TEXT, period_basis TEXT, manual_period_start TEXT, manual_period_end TEXT, asset_version INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE schedule_asset_reviews (
			asset_id TEXT PRIMARY KEY, llm_status TEXT NOT NULL DEFAULT 'pending', manual_status TEXT NOT NULL DEFAULT 'unreviewed', updated_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE profile_media (
			id TEXT PRIMARY KEY, slot TEXT NOT NULL, source TEXT NOT NULL, source_url TEXT, active_cache_path TEXT, active_sha256 TEXT,
			active_mime_type TEXT, status TEXT NOT NULL DEFAULT 'pending', is_active INTEGER NOT NULL DEFAULT 0, raw_json TEXT
		);
		CREATE TABLE media_assets (
			id TEXT PRIMARY KEY, source TEXT NOT NULL, source_url TEXT NOT NULL, cache_path TEXT, mime_type TEXT, byte_size INTEGER,
			sha256 TEXT, width INTEGER, height INTEGER, status TEXT NOT NULL DEFAULT 'pending', fetched_at TEXT, last_checked_at TEXT,
			last_seen_at TEXT, etag TEXT, last_modified TEXT, last_error TEXT, retry_attempts INTEGER NOT NULL DEFAULT 0, next_retry_at TEXT, created_at TEXT, updated_at TEXT, raw_json TEXT
		);
		CREATE TABLE media_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT, media_id TEXT NOT NULL, owner_type TEXT NOT NULL, owner_id TEXT NOT NULL,
			role TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0, alt TEXT, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE media_versions (
			media_id TEXT NOT NULL, sha256 TEXT NOT NULL, cache_path TEXT NOT NULL, mime_type TEXT NOT NULL, byte_size INTEGER,
			fetched_at TEXT, replaced_at TEXT NOT NULL, PRIMARY KEY (media_id, sha256)
		);
		CREATE TABLE translation_states (
			owner_type TEXT NOT NULL, owner_id TEXT NOT NULL, field TEXT NOT NULL, source_hash TEXT, source_language TEXT,
			status TEXT NOT NULL DEFAULT 'never', last_attempt_at TEXT, last_error TEXT, requested INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL, PRIMARY KEY (owner_type, owner_id, field)
		);
		CREATE TABLE translations (
			owner_type TEXT NOT NULL, owner_id TEXT NOT NULL, field TEXT NOT NULL, locale TEXT NOT NULL, origin TEXT NOT NULL,
			platform TEXT NOT NULL DEFAULT '', text TEXT NOT NULL, source_hash TEXT NOT NULL, provider_id TEXT, model_id TEXT,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY (owner_type, owner_id, field, locale, origin, platform)
		);
		CREATE TABLE sync_state (
			source TEXT NOT NULL, account_id TEXT NOT NULL, cursor_id TEXT, cursor_time TEXT,
			last_success_at TEXT, metadata_json TEXT, PRIMARY KEY (source, account_id)
		);
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

// Exec writes to a snapshot that a running handler may be reading. The busy
// timeout lets the write wait out a reader's shared lock; it is set on the
// connection rather than in a file: URI because t.TempDir keeps '#' and '%'
// from subtest names, which a URI would misread.
func Exec(t testing.TB, path, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture for write: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open fixture for write: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("set fixture busy timeout: %v", err)
	}
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
