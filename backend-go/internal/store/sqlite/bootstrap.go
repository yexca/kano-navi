package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Canonical fresh schema and public static seeds are compiled into the binary.
// bootstrap.sql and static-seed.json are the source of truth for a new
// database; schema_upgrade.go brings older snapshots to the same shape.
//
//go:embed bootstrap.sql
var bootstrapSchema string

//go:embed static-seed.json
var publicStaticSeed []byte

// OpenInitialized creates a fresh current-schema database or seeds missing
// static records in an existing current-schema snapshot. It never reconstructs
// unknown/legacy tables, clears snapshots, or changes provider configuration.
func OpenInitialized(filename string, adminWrites, workflowWrites bool) (*Store, error) {
	return openInitialized(filename, adminWrites, workflowWrites, false)
}

func openInitialized(filename string, adminWrites, workflowWrites, upgrade bool) (*Store, error) {
	if strings.TrimSpace(filename) == "" {
		return nil, ErrUnavailable
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, ErrUnavailable
	}
	if info, e := os.Stat(abs); e == nil && !info.Mode().IsRegular() {
		return nil, ErrUnavailable
	} else if e != nil && !os.IsNotExist(e) {
		return nil, ErrUnavailable
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return nil, ErrUnavailable
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=rwc&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, ErrUnavailable
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	failed := true
	defer func() {
		if failed {
			db.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()
	var tables int
	if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables) != nil {
		return nil, ErrSchema
	}
	if tables == 0 {
		if _, err = tx.ExecContext(ctx, bootstrapSchema); err != nil {
			return nil, fmt.Errorf("%w: fresh schema could not be created", ErrSchema)
		}
	} else {
		// The site owner applies additive upgrades first; every other opener
		// validates before making any seed write and leaves old schemas alone.
		if upgrade {
			if err = upgradeSchema(ctx, tx); err != nil {
				return nil, err
			}
		}
		if err = verifySchema(ctx, dbInTransaction{tx}); err != nil {
			return nil, err
		}
	}
	if err = seedStatic(ctx, tx, false); err != nil {
		return nil, ErrSchema
	}
	if tx.Commit() != nil {
		return nil, ErrUnavailable
	}
	if err = migrateWorkflowSchema(ctx, db); err != nil {
		return nil, err
	}
	if err = verifySchema(ctx, db); err != nil {
		return nil, err
	}
	failed = false
	return &Store{db: db, dataRoot: filepath.Dir(filepath.Dir(abs)), writable: adminWrites, workflowWritable: workflowWrites, workflowVersion: WorkflowSchemaVersion}, nil
}

// verifySchema only requires QueryContext; both fresh creation and inspection
// use the same transaction so validation cannot race a seed write.
type dbInTransaction struct{ *sql.Tx }

// seedStatic inserts the public static records. Existing rows are kept unless
// overwrite restores them to the shipped wording.
func seedStatic(ctx context.Context, tx *sql.Tx, overwrite bool) error {
	var data struct {
		Profile   map[string]any   `json:"profile"`
		Timeline  []map[string]any `json:"timeline"`
		Resources []map[string]any `json:"resources"`
	}
	if json.Unmarshal(publicStaticSeed, &data) != nil {
		return ErrSchema
	}
	p := data.Profile
	profileSQL, timelineSQL, resourceSQL := `INSERT OR IGNORE INTO profiles(id,display_name,romanized_name,bio,avatar_url,banner_url,x_url,youtube_url,updated_at,raw_json) VALUES(?,?,?,?,?,?,?,?,?,NULL)`,
		`INSERT OR IGNORE INTO timeline(id,year,title,detail,sort_order,raw_json) VALUES(?,?,?,?,?,NULL)`,
		`INSERT OR IGNORE INTO resources(id,title,detail,icon,tone,url,sort_order,raw_json) VALUES(?,?,?,?,?,?,?,NULL)`
	if overwrite {
		profileSQL = `INSERT INTO profiles(id,display_name,romanized_name,bio,avatar_url,banner_url,x_url,youtube_url,updated_at,raw_json) VALUES(?,?,?,?,?,?,?,?,?,NULL) ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,romanized_name=excluded.romanized_name,bio=excluded.bio,avatar_url=excluded.avatar_url,banner_url=excluded.banner_url,x_url=excluded.x_url,youtube_url=excluded.youtube_url,updated_at=excluded.updated_at`
		timelineSQL = `INSERT INTO timeline(id,year,title,detail,sort_order,raw_json) VALUES(?,?,?,?,?,NULL) ON CONFLICT(id) DO UPDATE SET year=excluded.year,title=excluded.title,detail=excluded.detail,sort_order=excluded.sort_order`
		resourceSQL = `INSERT INTO resources(id,title,detail,icon,tone,url,sort_order,raw_json) VALUES(?,?,?,?,?,?,?,NULL) ON CONFLICT(id) DO UPDATE SET title=excluded.title,detail=excluded.detail,icon=excluded.icon,tone=excluded.tone,url=excluded.url,sort_order=excluded.sort_order`
	}
	if _, err := tx.ExecContext(ctx, profileSQL, p["id"], p["display_name"], p["romanized_name"], p["bio"], p["avatar_url"], p["banner_url"], p["x_url"], p["youtube_url"], p["updated_at"]); err != nil {
		return err
	}
	for _, v := range data.Timeline {
		if _, err := tx.ExecContext(ctx, timelineSQL, v["id"], v["year"], v["title"], v["detail"], v["sort_order"]); err != nil {
			return err
		}
	}
	for _, v := range data.Resources {
		if _, err := tx.ExecContext(ctx, resourceSQL, v["id"], v["title"], v["detail"], v["icon"], v["tone"], v["url"], v["sort_order"]); err != nil {
			return err
		}
	}
	now := workflowTime(time.Now())
	settings := map[string]string{"dashboard_revision": "0", "x_accounts": `["kano_2525","_Kanotic"]`, "llm_route_targets_migrated": "1"}
	for key, value := range settings {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO app_settings(key,value,updated_at) VALUES(?,?,?)`, key, value, now); err != nil {
			return err
		}
	}
	return nil
}

// SeedStatic adds missing public static records. With overwrite it also
// restores the shipped profile, timeline and resource wording. Source
// snapshots, events, media and operator configuration are never touched.
func (s *Store) SeedStatic(ctx context.Context, overwrite bool) error {
	if s == nil || s.db == nil || !s.writable {
		return ErrReadOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	if err = seedStatic(ctx, tx, overwrite); err != nil {
		return ErrSchema
	}
	if overwrite {
		if err = bumpRevision(ctx, tx, workflowTime(time.Now())); err != nil {
			return ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

// SnapshotCounts reports how many rows each snapshot table holds.
func (s *Store) SnapshotCounts(ctx context.Context) (map[string]int, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	counts := map[string]int{}
	for _, table := range []string{"profiles", "posts", "events", "videos", "focus", "timeline", "resources", "assets", "media_assets", "media_links"} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return nil, ErrSchema
		}
		counts[table] = count
	}
	return counts, nil
}

// seedDefaultWorkflows gives a new installation two starting workflows. They
// start unscheduled: timed polling of public platforms is an explicit operator
// decision. An installation that already has, or deleted, its workflows is
// left alone.
func (s *Store) seedDefaultWorkflows(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var seeded, existing int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE key='workflows_seeded'`).Scan(&seeded); err != nil {
		return ErrSchema
	}
	if seeded > 0 {
		return nil
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflows`).Scan(&existing); err != nil {
		return ErrSchema
	}
	now := workflowTime(time.Now())
	if existing == 0 {
		for _, workflow := range []struct {
			id, name, steps string
			interval        int
		}{
			{"full-refresh", "Full refresh", `["x","youtube","bilibili","media","schedule","translate"]`, 60},
			{"sources-only", "Sources only", `["x","youtube","bilibili"]`, 30},
		} {
			if _, err = tx.ExecContext(ctx, `INSERT INTO workflows(id,name,steps_json,source_lookback_json,schedule_enabled,interval_minutes,created_at,updated_at) VALUES(?,?,?,'{"x":7,"youtube":14}',0,?,?,?)`, workflow.id, workflow.name, workflow.steps, workflow.interval, now, now); err != nil {
				return ErrSchema
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES('workflows_seeded','1',?)`, now); err != nil {
		return ErrSchema
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}
