// Command kano-fixture creates the disposable snapshot used by the isolated
// Go Docker smoke check. It is a validation helper, never a production seed.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type fixtureInfo struct {
	MediaID   string `json:"mediaId"`
	MediaHash string `json:"mediaHash"`
}

func main() {
	databasePath := flag.String("database", "", "fixture SQLite path")
	flag.Parse()
	if strings.TrimSpace(*databasePath) == "" {
		fmt.Fprintln(os.Stderr, "database path is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Dir(*databasePath), 0o755); err != nil {
		panic(err)
	}
	db, err := sql.Open("sqlite", *databasePath)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		panic(err)
	}
	body := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("go-docker-fixture")...)
	digest := sha256.Sum256(body)
	mediaHash := hex.EncodeToString(digest[:])
	mediaID := strings.Repeat("a", 64)
	relative := filepath.ToSlash(filepath.Join("x", "sha256", mediaHash[:2], mediaHash+".png"))
	dataRoot := filepath.Dir(filepath.Dir(*databasePath))
	mediaPath := filepath.Join(dataRoot, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(mediaPath, body, 0o644); err != nil {
		panic(err)
	}
	profileRelative := filepath.ToSlash(filepath.Join("avatar", "avatar.png"))
	profilePath := filepath.Join(dataRoot, filepath.FromSlash(profileRelative))
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(profilePath, body, 0o644); err != nil {
		panic(err)
	}
	if _, err := db.Exec(`INSERT INTO profiles (id,display_name,updated_at) VALUES ('profile','Synthetic','')`); err != nil {
		panic(err)
	}
	if _, err := db.Exec(`
		INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at)
		VALUES ('fixture-event','x','Synthetic event','2026-10-11','2026-10-11T00:00:00Z','Asia/Tokyo','exact','scheduled','event','automatic',0,'none','','')`); err != nil {
		panic(err)
	}
	if _, err := db.Exec(`
		INSERT INTO media_assets (id,source,source_url,cache_path,mime_type,byte_size,sha256,status,fetched_at,updated_at)
		VALUES (?, 'x','https://cdn.example.invalid/go-fixture.png',?,?,?,?, 'ready','2026-10-11T00:00:00Z','')`, mediaID, relative, "image/png", len(body), mediaHash); err != nil {
		panic(err)
	}
	if _, err := db.Exec(`
		INSERT INTO profile_media (id,slot,source,source_url,active_cache_path,active_sha256,active_mime_type,status,is_active)
		VALUES ('fixture-profile','avatar','upload','https://example.invalid/profile.png',?,?,?,'ready',1)`, profileRelative, mediaHash, "image/png"); err != nil {
		panic(err)
	}
	if _, err := db.Exec(`INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','1','')`); err != nil {
		panic(err)
	}
	encoded, _ := json.Marshal(fixtureInfo{MediaID: mediaID, MediaHash: mediaHash})
	fmt.Println(string(encoded))
}

const schema = `
CREATE TABLE profiles (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, romanized_name TEXT, bio TEXT, avatar_url TEXT, banner_url TEXT, x_url TEXT, youtube_url TEXT, updated_at TEXT, raw_json TEXT);
CREATE TABLE posts (id TEXT PRIMARY KEY, source TEXT NOT NULL, account_handle TEXT, type TEXT NOT NULL DEFAULT 'daily', label TEXT, text TEXT NOT NULL, published_at TEXT NOT NULL, url TEXT NOT NULL, likes INTEGER DEFAULT 0, reposts INTEGER DEFAULT 0, replies INTEGER DEFAULT 0, media_url TEXT, media_alt TEXT, raw_json TEXT);
CREATE TABLE post_llm_states (post_id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'never', route TEXT, last_attempt_at TEXT, last_processed_at TEXT, last_error TEXT, last_extraction_id INTEGER, reprocess_requested INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT '');
CREATE TABLE events (id TEXT PRIMARY KEY, source TEXT NOT NULL, source_item_id TEXT, source_key TEXT, title TEXT NOT NULL, detail TEXT, starts_on TEXT NOT NULL, starts_at TEXT, ends_at TEXT, timezone TEXT NOT NULL DEFAULT 'Asia/Tokyo', time_precision TEXT NOT NULL DEFAULT 'exact', status TEXT, event_type TEXT DEFAULT 'event', url TEXT, is_upcoming INTEGER DEFAULT 0, provenance TEXT NOT NULL DEFAULT 'automatic', manual_locked INTEGER NOT NULL DEFAULT 0, confidence REAL, extraction_id INTEGER, cancellation_status TEXT NOT NULL DEFAULT 'none', cancellation_source TEXT, cancellation_reason TEXT, cancellation_evidence TEXT, cancellation_source_item_id TEXT, cancellation_confidence REAL, cancellation_at TEXT, deleted_at TEXT, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '', raw_json TEXT, merged_into TEXT, merge_source TEXT, merge_reason TEXT, merged_at TEXT);
CREATE TABLE event_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT, source TEXT, source_item_id TEXT, source_key TEXT, url TEXT, raw_json TEXT, deleted_at TEXT, created_at TEXT, updated_at TEXT);
CREATE TABLE videos (id TEXT PRIMARY KEY, source TEXT NOT NULL, title TEXT NOT NULL, published_at TEXT, scheduled_at TEXT, url TEXT NOT NULL, thumbnail_url TEXT, kind TEXT, is_upcoming INTEGER DEFAULT 0, raw_json TEXT, live_status TEXT, live_checked_at TEXT);
CREATE TABLE focus (id INTEGER PRIMARY KEY, video_id TEXT, date_label TEXT, title TEXT NOT NULL, description TEXT, image_url TEXT, url TEXT, source_url TEXT, updated_at TEXT, raw_json TEXT);
CREATE TABLE timeline (id INTEGER PRIMARY KEY, year TEXT NOT NULL, title TEXT NOT NULL, detail TEXT, sort_order INTEGER DEFAULT 0, raw_json TEXT);
CREATE TABLE resources (id INTEGER PRIMARY KEY, title TEXT NOT NULL, detail TEXT, icon TEXT, tone TEXT, url TEXT NOT NULL, sort_order INTEGER DEFAULT 0, raw_json TEXT);
CREATE TABLE assets (id TEXT PRIMARY KEY, kind TEXT NOT NULL, url TEXT NOT NULL, source_url TEXT, alt TEXT, week_start TEXT, source_account TEXT, updated_at TEXT, period_start TEXT, period_end TEXT, period_basis TEXT, manual_period_start TEXT, manual_period_end TEXT);
CREATE TABLE schedule_asset_reviews (asset_id TEXT, llm_status TEXT, manual_status TEXT, updated_at TEXT);
CREATE TABLE profile_media (id TEXT PRIMARY KEY, slot TEXT NOT NULL, source TEXT NOT NULL, source_ref TEXT, source_url TEXT, cache_path TEXT, active_cache_path TEXT, active_sha256 TEXT, active_mime_type TEXT, download_status TEXT, last_download_at TEXT, mime_type TEXT, extension TEXT, byte_size INTEGER, sha256 TEXT, width INTEGER, height INTEGER, status TEXT NOT NULL DEFAULT 'pending', is_active INTEGER NOT NULL DEFAULT 0, last_error TEXT, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '', raw_json TEXT);
CREATE TABLE media_assets (id TEXT PRIMARY KEY, source TEXT NOT NULL, source_url TEXT NOT NULL, cache_path TEXT, mime_type TEXT, extension TEXT, byte_size INTEGER, sha256 TEXT, width INTEGER, height INTEGER, status TEXT NOT NULL DEFAULT 'pending', fetched_at TEXT, last_checked_at TEXT, last_seen_at TEXT, etag TEXT, last_modified TEXT, last_error TEXT, created_at TEXT, updated_at TEXT, raw_json TEXT);
CREATE TABLE media_links (id INTEGER PRIMARY KEY AUTOINCREMENT, media_id TEXT, owner_type TEXT, owner_id TEXT, role TEXT, position INTEGER, alt TEXT, created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '');
CREATE TABLE sync_runs (id INTEGER PRIMARY KEY AUTOINCREMENT, source TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, status TEXT NOT NULL, message TEXT, counts_json TEXT, triggered_by TEXT, job_id TEXT);
CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);
`
