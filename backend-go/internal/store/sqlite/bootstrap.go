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
// Regenerate from the disposable exporter when the TypeScript contract changes.
//
//go:embed bootstrap.sql
var bootstrapSchema string

//go:embed static-seed.json
var publicStaticSeed []byte

// OpenInitialized creates a fresh current-schema database or seeds missing
// static records in an existing current-schema snapshot. It never reconstructs
// unknown/legacy tables, clears snapshots, or changes provider configuration.
func OpenInitialized(filename string, adminWrites, workflowWrites bool) (*Store, error) {
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
		// Validate before making any seed write. Existing legacy upgrades still
		// belong to the original migrator until their Go parity is implemented.
		if err = verifySchema(ctx, dbInTransaction{tx}); err != nil {
			return nil, err
		}
	}
	if err = seedStatic(ctx, tx); err != nil {
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

func seedStatic(ctx context.Context, tx *sql.Tx) error {
	var data struct {
		Profile   map[string]any   `json:"profile"`
		Timeline  []map[string]any `json:"timeline"`
		Resources []map[string]any `json:"resources"`
	}
	if json.Unmarshal(publicStaticSeed, &data) != nil {
		return ErrSchema
	}
	p := data.Profile
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO profiles(id,display_name,romanized_name,bio,avatar_url,banner_url,x_url,youtube_url,updated_at,raw_json) VALUES(?,?,?,?,?,?,?,?,?,NULL)`, p["id"], p["display_name"], p["romanized_name"], p["bio"], p["avatar_url"], p["banner_url"], p["x_url"], p["youtube_url"], p["updated_at"]); err != nil {
		return err
	}
	for _, v := range data.Timeline {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO timeline(id,year,title,detail,sort_order,raw_json) VALUES(?,?,?,?,?,NULL)`, v["id"], v["year"], v["title"], v["detail"], v["sort_order"]); err != nil {
			return err
		}
	}
	for _, v := range data.Resources {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO resources(id,title,detail,icon,tone,url,sort_order,raw_json) VALUES(?,?,?,?,?,?,?,NULL)`, v["id"], v["title"], v["detail"], v["icon"], v["tone"], v["url"], v["sort_order"]); err != nil {
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
