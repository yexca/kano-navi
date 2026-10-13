package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OpenStandalone opens the runtime database for the process that owns the
// site. A missing file is created and seeded. An existing snapshot written by
// an earlier release receives additive upgrades only: missing tables, columns
// and indexes are added and their documented backfills run. Nothing is
// dropped, rebuilt or cleared, and an unrecognized schema is left untouched.
func OpenStandalone(filename string) (*Store, error) {
	store, err := openInitialized(filename, true, true, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = store.repairSelectedProfiles(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

type schemaColumn struct {
	name, kind string
	notNull    bool
	fallback   sql.NullString
}

type schemaReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func tableColumns(ctx context.Context, q schemaReader, table string) ([]schemaColumn, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []schemaColumn{}
	for rows.Next() {
		var column schemaColumn
		var cid, notNull, primaryKey int
		if err = rows.Scan(&cid, &column.name, &column.kind, &notNull, &column.fallback, &primaryKey); err != nil {
			return nil, err
		}
		column.notNull = notNull != 0
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func schemaObjects(ctx context.Context, q schemaReader, kind string) (map[string]string, []string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name,sql FROM sqlite_master WHERE type=? AND sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY name`, kind)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	objects, order := map[string]string{}, []string{}
	for rows.Next() {
		var name, ddl string
		if err = rows.Scan(&name, &ddl); err != nil {
			return nil, nil, err
		}
		objects[name] = ddl
		order = append(order, name)
	}
	return objects, order, rows.Err()
}

// upgradeSchema compares the snapshot with the canonical fresh schema and
// applies only additive differences inside the caller's transaction.
func upgradeSchema(ctx context.Context, tx *sql.Tx) error {
	existingTables, _, err := schemaObjects(ctx, tx, "table")
	if err != nil {
		return ErrSchema
	}
	// Only a recognizable snapshot is upgraded. The pre-event-source layout
	// needs the original rebuilding migration and is refused unchanged.
	for _, table := range []string{"profiles", "posts", "events", "videos", "app_settings", "sync_runs"} {
		if _, ok := existingTables[table]; !ok {
			return fmt.Errorf("%w: snapshot is not a recognized schema", ErrSchema)
		}
	}
	eventColumns, err := tableColumns(ctx, tx, "events")
	if err != nil {
		return ErrSchema
	}
	if !hasColumn(eventColumns, "starts_on") {
		return fmt.Errorf("%w: snapshot predates additive upgrades", ErrSchema)
	}

	reference, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return ErrSchema
	}
	defer reference.Close()
	reference.SetMaxOpenConns(1)
	if _, err = reference.ExecContext(ctx, bootstrapSchema); err != nil {
		return ErrSchema
	}
	wantTables, tableOrder, err := schemaObjects(ctx, reference, "table")
	if err != nil {
		return ErrSchema
	}
	added := map[string]bool{}
	for _, table := range tableOrder {
		if _, exists := existingTables[table]; !exists {
			if _, err = tx.ExecContext(ctx, wantTables[table]); err != nil {
				return fmt.Errorf("%w: table %s could not be created", ErrSchema, table)
			}
			continue
		}
		want, err := tableColumns(ctx, reference, table)
		if err != nil {
			return ErrSchema
		}
		have, err := tableColumns(ctx, tx, table)
		if err != nil {
			return ErrSchema
		}
		for _, column := range want {
			if hasColumn(have, column.name) {
				continue
			}
			definition := `"` + column.name + `" ` + column.kind
			switch {
			case column.notNull && !column.fallback.Valid:
				return fmt.Errorf("%w: column %s.%s cannot be added", ErrSchema, table, column.name)
			case column.notNull:
				definition += " NOT NULL DEFAULT " + column.fallback.String
			case column.fallback.Valid:
				definition += " DEFAULT " + column.fallback.String
			}
			if _, err = tx.ExecContext(ctx, `ALTER TABLE "`+table+`" ADD COLUMN `+definition); err != nil {
				return fmt.Errorf("%w: column %s.%s could not be added", ErrSchema, table, column.name)
			}
			added[table+"."+column.name] = true
		}
	}
	if err = backfillUpgrade(ctx, tx, added); err != nil {
		return err
	}
	wantIndexes, indexOrder, err := schemaObjects(ctx, reference, "index")
	if err != nil {
		return ErrSchema
	}
	haveIndexes, _, err := schemaObjects(ctx, tx, "index")
	if err != nil {
		return ErrSchema
	}
	for _, index := range indexOrder {
		if _, exists := haveIndexes[index]; exists {
			continue
		}
		if _, err = tx.ExecContext(ctx, wantIndexes[index]); err != nil {
			return fmt.Errorf("%w: index %s could not be created", ErrSchema, index)
		}
	}
	return nil
}

func hasColumn(columns []schemaColumn, name string) bool {
	for _, column := range columns {
		if column.name == name {
			return true
		}
	}
	return false
}

// backfillUpgrade fills values that an added column or table cannot default.
// Each statement is idempotent and touches only rows still in the old shape.
func backfillUpgrade(ctx context.Context, tx *sql.Tx, added map[string]bool) error {
	exec := func(query string, args ...any) error {
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("%w: upgrade backfill failed", ErrSchema)
		}
		return nil
	}
	// Seeded wording that earlier releases shipped. Only rows that still carry
	// the exact seeded values change, so operator edits are preserved.
	for _, statement := range []string{
		`UPDATE resources SET title='official site',detail=NULL WHERE id=9 AND title='旧 official site' AND detail='更新截至 2022 · 资料存档'`,
		`UPDATE timeline SET detail='8 月加入ミリプロSONA，以新形象开始配信。' WHERE id=1 AND detail='加入ミリプロSONA，开始 VTuber 活动。'`,
		`UPDATE timeline SET sort_order=CASE id WHEN 3 THEN 5 ELSE 6 END WHERE (id=3 AND title='Major debut' AND sort_order=3) OR (id=4 AND title='第一首投稿' AND sort_order=4)`,
		`UPDATE resources SET url='https://milpr.com/talents/kano-mahoro',detail='事务所个人页 · 资料与公告' WHERE id=7 AND url='https://milpr.com/news/mahoro_debut' AND detail='所属与官方公告'`,
	} {
		if err := exec(statement); err != nil {
			return err
		}
	}
	if added["assets.period_start"] {
		if err := exec(`UPDATE assets SET period_start=week_start,period_end=date(week_start,'+6 days') WHERE kind='schedule'`); err != nil {
			return err
		}
	}
	if added["focus.video_id"] {
		if err := backfillRows(ctx, tx, `SELECT id,url FROM focus`, `UPDATE focus SET video_id=? WHERE id=?`, func(url string) any {
			if identity := streamIdentity(url); strings.HasPrefix(identity, "youtube:") {
				return strings.TrimPrefix(identity, "youtube:")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if err := backfillRows(ctx, tx, `SELECT id,url FROM posts WHERE account_handle IS NULL OR account_handle=''`, `UPDATE posts SET account_handle=? WHERE id=?`, func(url string) any {
		if handle := xHandleFromURL(url); handle != "" {
			return handle
		}
		return nil
	}); err != nil {
		return err
	}
	if added["llm_providers.capabilities_json"] {
		if err := exec(`UPDATE llm_providers SET capabilities_json=CASE WHEN vision_capable=0 THEN '["text"]' ELSE '["text","image"]' END`); err != nil {
			return err
		}
	}
	// Older databases defaulted this field to zero; the failover contract is
	// deterministic.
	if err := exec(`UPDATE llm_providers SET max_retries=2 WHERE max_retries IS NULL OR max_retries<>2`); err != nil {
		return err
	}
	// Routes target a provider plus a catalog model. Copy the older
	// provider-only order once; an empty model ID means the provider default.
	var migrated int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE key='llm_route_targets_migrated'`).Scan(&migrated); err != nil {
		return ErrSchema
	}
	now := workflowTime(time.Now())
	if migrated == 0 {
		if err := exec(`INSERT OR IGNORE INTO llm_route_targets(route,provider_id,model_id,priority,created_at,updated_at) SELECT route,provider_id,'',priority,created_at,created_at FROM llm_route_providers ORDER BY route,priority`); err != nil {
			return err
		}
		if err := exec(`INSERT INTO app_settings(key,value,updated_at) VALUES('llm_route_targets_migrated','1',?)`, now); err != nil {
			return err
		}
	}
	// Every provider default model also appears in the model catalog.
	if err := exec(`INSERT OR IGNORE INTO llm_models(provider_id,model_id,name,tags_json,enabled,origin,owned_by,created_at,updated_at) SELECT id,model,NULL,capabilities_json,1,'legacy',NULL,created_at,created_at FROM llm_providers WHERE model<>''`); err != nil {
		return err
	}
	// The original implementation had one vision route. Both detectors keep
	// that order until an operator separates them.
	for _, route := range []string{"schedule_message", "schedule_board"} {
		if err := exec(`INSERT INTO llm_route_targets(route,provider_id,model_id,priority,created_at,updated_at)
 SELECT ?,provider_id,model_id,priority,?,? FROM llm_route_targets WHERE route='schedule_vision'
 AND NOT EXISTS(SELECT 1 FROM llm_route_targets WHERE route=?)`, route, now, now, route); err != nil {
			return err
		}
	}
	return nil
}

// backfillRows derives one value per row from its URL. Rows are read fully
// before the first update because the transaction owns the only connection.
func backfillRows(ctx context.Context, tx *sql.Tx, query, update string, derive func(string) any) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return ErrSchema
	}
	type pending struct {
		id    any
		value any
	}
	updates := []pending{}
	for rows.Next() {
		var id any
		var url sql.NullString
		if err = rows.Scan(&id, &url); err != nil {
			rows.Close()
			return ErrSchema
		}
		if value := derive(url.String); value != nil {
			updates = append(updates, pending{id, value})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrSchema
	}
	for _, item := range updates {
		if _, err = tx.ExecContext(ctx, update, item.value, item.id); err != nil {
			return ErrSchema
		}
	}
	return nil
}

// repairSelectedProfiles reconciles the stored hash and MIME type of each
// selected profile image with the file actually on disk. Older selected files
// can differ from the latest candidate; the candidate, the manual choice and
// unrelated state never change.
func (s *Store) repairSelectedProfiles(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,active_cache_path,active_sha256,active_mime_type FROM profile_media WHERE is_active=1`)
	if err != nil {
		return fmt.Errorf("%w: profile media unavailable", ErrSchema)
	}
	type selected struct {
		id               string
		path, hash, mime sql.NullString
	}
	items := []selected{}
	for rows.Next() {
		var item selected
		if err = rows.Scan(&item.id, &item.path, &item.hash, &item.mime); err != nil {
			rows.Close()
			return fmt.Errorf("%w: profile media unavailable", ErrSchema)
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("%w: profile media unavailable", ErrSchema)
	}
	type repair struct {
		id         string
		hash, mime any
	}
	repairs := []repair{}
	for _, item := range items {
		var hash, mime any
		if file, err := s.resolveProfileMediaPath(item.path.String); err == nil {
			if content, _, err := readContainedFile(file, 15*1024*1024); err == nil {
				if sniffed := sniffImageMIME(content); sniffed != "" {
					sum := sha256.Sum256(content)
					hash, mime = hex.EncodeToString(sum[:]), sniffed
				}
			}
		}
		sameHash := hash == nil && !item.hash.Valid || hash != nil && item.hash.Valid && hash == item.hash.String
		sameMIME := mime == nil && !item.mime.Valid || mime != nil && item.mime.Valid && mime == item.mime.String
		if !sameHash || !sameMIME {
			repairs = append(repairs, repair{item.id, hash, mime})
		}
	}
	if len(repairs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: transaction", ErrUnavailable)
	}
	defer tx.Rollback()
	for _, item := range repairs {
		if _, err = tx.ExecContext(ctx, `UPDATE profile_media SET active_sha256=?,active_mime_type=? WHERE id=?`, item.hash, item.mime, item.id); err != nil {
			return fmt.Errorf("%w: profile media repair", ErrUnavailable)
		}
	}
	if err = bumpRevision(ctx, tx, workflowTime(time.Now())); err != nil {
		return fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("%w: profile media repair", ErrUnavailable)
	}
	return nil
}
