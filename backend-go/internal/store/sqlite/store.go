package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrUnavailable = errors.New("database unavailable")
	ErrSchema      = errors.New("database schema unavailable")
)

type SyncRun struct {
	ID         int64
	Source     string
	StartedAt  string
	FinishedAt *string
	Status     string
}

type Store struct {
	db               *sql.DB
	dataRoot         string
	writable         bool
	workflowWritable bool
	workflowVersion  int
	// sourceSync reports the source_sync_json columns, which a read-only
	// snapshot that has not been upgraded lacks.
	sourceSync bool
	// scheduleOffset draws a source schedule's jitter; nil draws at random.
	scheduleOffset func(spread int64) int64
}

// OpenReadOnly opens an existing snapshot without creating directories,
// running migrations, enabling WAL, or writing any SQLite state.
func OpenReadOnly(filename string) (*Store, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, fmt.Errorf("%w: empty path", ErrUnavailable)
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid path", ErrUnavailable)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: snapshot is unavailable", ErrUnavailable)
	}

	// modernc.org/sqlite is a pure-Go driver. mode=ro is part of SQLite's URI
	// contract and prevents this process from creating or modifying the file.
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("%w: snapshot could not be opened", ErrUnavailable)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: snapshot could not be read", ErrUnavailable)
	}
	if err := verifySchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	var workflowTable int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='workflow_migrations'`).Scan(&workflowTable); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: workflow schema could not be inspected", ErrUnavailable)
	}
	if workflowTable == 1 {
		if err := verifyWorkflowSchema(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%w: workflow schema unavailable", ErrSchema)
		}
	}
	var version int
	sync := false
	if workflowTable == 1 {
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM workflow_migrations`).Scan(&version)
		var columns int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('workflow_runs') WHERE name='source_sync_json'`).Scan(&columns)
		sync = columns == 1
	}
	return &Store{db: db, dataRoot: filepath.Dir(filepath.Dir(abs)), workflowVersion: version, sourceSync: sync}, nil
}

// OpenAdminWrites opens an existing fixture in SQLite rw mode. It never creates
// a file or seeds data. Its additive workflow migration is restricted to this
// explicit development-only opener.
func OpenAdminWrites(filename string) (*Store, error) {
	abs, err := filepath.Abs(strings.TrimSpace(filename))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid path", ErrUnavailable)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: snapshot is unavailable", ErrUnavailable)
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=rw&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("%w: snapshot could not be opened", ErrUnavailable)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: snapshot could not be read", ErrUnavailable)
	}
	if err := verifySchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db, dataRoot: filepath.Dir(filepath.Dir(abs)), writable: true}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrateWorkflowSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: workflow schema unavailable", ErrSchema)
	}
	store.workflowWritable = true
	store.workflowVersion = WorkflowSchemaVersion
	store.sourceSync = true
	return store, nil
}

type schemaQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func verifySchema(ctx context.Context, db schemaQuerier) error {
	required := map[string][]string{
		"profiles":               {"id", "display_name", "romanized_name", "bio", "avatar_url", "banner_url", "x_url", "youtube_url", "updated_at"},
		"posts":                  {"id", "source", "account_handle", "type", "label", "text", "published_at", "url", "likes", "reposts", "replies", "media_url", "media_alt", "raw_json", "quote_json", "quote_hidden_at", "refresh_requested_at"},
		"post_llm_states":        {"post_id", "status", "route", "last_attempt_at", "last_processed_at", "last_error", "last_extraction_id", "reprocess_requested", "failure_count", "failure_fingerprint", "next_attempt_at"},
		"events":                 {"id", "source", "source_item_id", "source_key", "title", "detail", "starts_on", "starts_at", "ends_at", "timezone", "time_precision", "status", "event_type", "url", "provenance", "manual_locked", "cancellation_status", "cancellation_source", "cancellation_reason", "cancellation_evidence", "cancellation_source_item_id", "cancellation_confidence", "cancellation_at", "deleted_at", "created_at", "updated_at", "merged_into", "merge_source", "merge_reason", "merged_at"},
		"event_sources":          {"event_id", "source", "source_item_id", "deleted_at"},
		"videos":                 {"id", "source", "title", "published_at", "scheduled_at", "url", "thumbnail_url", "kind", "is_upcoming", "live_status", "live_checked_at", "duration_seconds", "manual_title", "manual_kind", "manual_thumbnail_media_id", "manual_thumbnail_sha256", "refresh_requested_at", "merged_into", "merge_source", "merge_reason", "merged_at", "is_short", "manual_is_short"},
		"video_revisions":        {"id", "video_id", "origin", "title", "thumbnail_url", "thumbnail_media_id", "thumbnail_sha256", "replaced_at"},
		"media_versions":         {"media_id", "sha256", "cache_path", "mime_type", "replaced_at"},
		"focus":                  {"id", "video_id", "date_label", "title", "description", "image_url", "url", "source_url", "updated_at"},
		"timeline":               {"id", "year", "title", "detail", "sort_order"},
		"resources":              {"id", "title", "detail", "icon", "tone", "url", "sort_order"},
		"assets":                 {"id", "kind", "url", "source_url", "alt", "week_start", "source_account", "updated_at", "period_start", "period_end", "period_basis", "manual_period_start", "manual_period_end"},
		"schedule_asset_reviews": {"asset_id", "llm_status", "manual_status", "updated_at"},
		"profile_media":          {"id", "slot", "source", "source_url", "active_cache_path", "active_sha256", "active_mime_type", "status", "is_active"},
		"media_assets":           {"id", "source", "source_url", "cache_path", "mime_type", "byte_size", "sha256", "width", "height", "status", "fetched_at", "retry_attempts", "next_retry_at"},
		"media_links":            {"media_id", "owner_type", "owner_id", "role", "position", "alt"},
		"sync_runs":              {"id", "source", "started_at", "finished_at", "status"},
		"app_settings":           {"key", "value"},
		"translations":           {"owner_type", "owner_id", "field", "locale", "origin", "platform", "text", "source_hash"},
		"translation_states":     {"owner_type", "owner_id", "field", "source_hash", "source_language", "status", "requested"},
	}
	for table, columns := range required {
		rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
		if err != nil {
			return fmt.Errorf("%w: schema could not be inspected", ErrUnavailable)
		}
		seen := make(map[string]bool, len(columns))
		for rows.Next() {
			var cid int
			var name, columnType string
			var notNull, primaryKey int
			var defaultValue any
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				_ = rows.Close()
				return fmt.Errorf("%w: schema could not be read", ErrUnavailable)
			}
			seen[name] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("%w: schema could not be read", ErrUnavailable)
		}
		_ = rows.Close()
		for _, column := range columns {
			if !seen[column] {
				return fmt.Errorf("%w: required table or column is missing", ErrSchema)
			}
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) GetLatestSync(ctx context.Context) (*SyncRun, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, source, started_at, finished_at, status
		FROM sync_runs
		WHERE status != 'running'
		ORDER BY COALESCE(finished_at, started_at) DESC
		LIMIT 1`)
	var run SyncRun
	var finishedAt sql.NullString
	if err := row.Scan(&run.ID, &run.Source, &run.StartedAt, &finishedAt, &run.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: latest sync could not be read", ErrUnavailable)
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.String
	}
	return &run, nil
}

func (s *Store) GetDashboardRevision(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key = 'dashboard_revision'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: revision could not be read", ErrUnavailable)
	}
	return parseRevision(value), nil
}

func parseRevision(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	switch value[0] {
	case '+':
		value = value[1:]
	case '-':
		return 0
	}
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.ParseInt(value[:end], 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
