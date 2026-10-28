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
// site. A missing file is created and seeded. An existing snapshot is
// upgraded in one transaction: missing tables, columns
// and indexes are added and their documented backfills run. The only rebuild
// is the events table of a snapshot that predates event sources, which keeps
// every row. No snapshot is cleared, and an unrecognized schema is left
// untouched.
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
	// Only a recognizable snapshot is upgraded.
	for _, table := range []string{"profiles", "posts", "events", "videos", "app_settings", "sync_runs"} {
		if _, ok := existingTables[table]; !ok {
			return fmt.Errorf("%w: snapshot is not a recognized schema", ErrSchema)
		}
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
	eventColumns, err := tableColumns(ctx, tx, "events")
	if err != nil {
		return ErrSchema
	}
	if !hasColumn(eventColumns, "starts_on") {
		if err = rebuildLegacyEvents(ctx, tx, eventColumns, wantTables["events"], wantTables["event_sources"]); err != nil {
			return err
		}
		existingTables["event_sources"] = wantTables["event_sources"]
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

// rebuildLegacyEvents converts the first events layout, which had one source
// per event and no calendar date, into the current tables. Every row is kept
// under its ID and becomes its own source evidence. Events that were curated
// from X are locked so automatic extraction cannot replace them.
func rebuildLegacyEvents(ctx context.Context, tx *sql.Tx, legacy []schemaColumn, eventsDDL, sourcesDDL string) error {
	fail := func() error { return fmt.Errorf("%w: legacy events could not be rebuilt", ErrSchema) }
	if eventsDDL == "" || sourcesDDL == "" {
		return fail()
	}
	names := []string{"id", "source", "title", "detail", "starts_at", "ends_at", "status", "event_type", "url", "is_upcoming", "raw_json"}
	selected := make([]string, len(names))
	for i, name := range names {
		selected[i] = "NULL"
		if hasColumn(legacy, name) {
			selected[i] = `"` + name + `"`
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+strings.Join(selected, ",")+" FROM events ORDER BY id")
	if err != nil {
		return fail()
	}
	type legacyEvent struct {
		id, source, title                                     string
		detail, startsAt, endsAt, status, eventType, url, raw sql.NullString
		upcoming                                              sql.NullInt64
	}
	events := []legacyEvent{}
	for rows.Next() {
		var event legacyEvent
		var id, source, title sql.NullString
		if err = rows.Scan(&id, &source, &title, &event.detail, &event.startsAt, &event.endsAt, &event.status, &event.eventType, &event.url, &event.upcoming, &event.raw); err != nil {
			rows.Close()
			return fail()
		}
		event.id, event.source, event.title = id.String, source.String, title.String
		events = append(events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail()
	}
	for _, statement := range []string{
		`DROP TABLE IF EXISTS event_sources`, `DROP INDEX IF EXISTS events_starts_at_idx`,
		`ALTER TABLE events RENAME TO events_legacy`, eventsDDL, sourcesDDL,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return fail()
		}
	}
	now := workflowTime(time.Now())
	for _, event := range events {
		startsOn := event.startsAt.String
		if instant, parseErr := time.Parse(time.RFC3339Nano, startsOn); parseErr == nil {
			startsOn = instant.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02")
		} else if len(startsOn) > 10 {
			startsOn = startsOn[:10]
		}
		provenance, locked := "automatic", 0
		if event.source == "x" {
			provenance, locked = "manual", 1
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,is_upcoming,provenance,manual_locked,created_at,updated_at,raw_json) VALUES(?,?,?,?,?,?,?,?,?,'Asia/Tokyo','exact',?,?,?,?,?,?,?,?,?)`,
			event.id, event.source, event.id, event.id, event.title, event.detail, startsOn, event.startsAt, event.endsAt, event.status, event.eventType, event.url, event.upcoming.Int64, provenance, locked, now, now, event.raw); err != nil {
			return fail()
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,deleted_at,created_at,updated_at) VALUES(?,?,?,?,?,?,NULL,?,?)`,
			event.id, event.source, event.id, event.id, event.url, event.raw, now, now); err != nil {
			return fail()
		}
	}
	if _, err = tx.ExecContext(ctx, `DROP TABLE events_legacy`); err != nil {
		return fail()
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
	// Superseded seed wording. Only rows that still carry the exact seeded
	// values change, so operator edits are preserved.
	for _, statement := range []string{
		`UPDATE resources SET title='official site',detail=NULL WHERE id=9 AND title='旧 official site' AND detail='更新截至 2022 · 资料存档'`,
		`UPDATE timeline SET detail='8 月加入ミリプロSONA，以新形象开始配信。' WHERE id=1 AND detail='加入ミリプロSONA，开始 VTuber 活动。'`,
		`UPDATE timeline SET sort_order=CASE id WHEN 3 THEN 5 ELSE 6 END WHERE (id=3 AND title='Major debut' AND sort_order=3) OR (id=4 AND title='第一首投稿' AND sort_order=4)`,
		`UPDATE timeline SET sort_order=7 WHERE id=4 AND year='2010' AND title='第一首投稿' AND detail='从 NicoNico 的歌唱投稿开始。' AND sort_order=6`,
		`UPDATE resources SET url='https://milpr.com/talents/kano-mahoro',detail='事务所个人页 · 资料与公告' WHERE id=7 AND url='https://milpr.com/news/mahoro_debut' AND detail='所属与官方公告'`,
	} {
		if err := exec(statement); err != nil {
			return err
		}
	}
	if err := backfillEventCodes(ctx, tx); err != nil {
		return err
	}
	if err := backfillYoutubeEventTimePrecision(ctx, tx); err != nil {
		return err
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
	if err := backfillQuotes(ctx, tx); err != nil {
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
	var retired int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE key='llm_default_model_retired'`).Scan(&retired); err != nil {
		return ErrSchema
	}
	// Until retireDefaultModel has run, the provider default model also
	// appears in the model catalog. Afterwards a removed catalog model stays
	// removed.
	if retired == 0 {
		if err := exec(`INSERT OR IGNORE INTO llm_models(provider_id,model_id,name,tags_json,enabled,origin,owned_by,created_at,updated_at) SELECT id,model,NULL,capabilities_json,1,'legacy',NULL,created_at,created_at FROM llm_providers WHERE model<>''`); err != nil {
			return err
		}
	}
	if err := mergeRouteQueue(ctx, tx, now); err != nil {
		return err
	}
	if retired == 0 {
		return retireDefaultModel(ctx, tx, now)
	}
	return nil
}

// backfillEventCodes replaces generated Chinese status and detail text on
// events with the stored codes and neutral wording.
// Only rows synchronization still owns change; manual locks and tombstones
// keep their text, which the frontend translates for display.
func backfillEventCodes(ctx context.Context, tx *sql.Tx) error {
	const owned = ` AND provenance='automatic' AND manual_locked=0 AND deleted_at IS NULL`
	changed := int64(0)
	for _, statement := range []string{
		`UPDATE events SET status=CASE status WHEN '已完成' THEN 'ended' WHEN '直播中' THEN 'live' ELSE 'scheduled' END WHERE source='youtube' AND status IN ('已完成','直播中','已预约','会员限定')` + owned,
		`UPDATE events SET detail=CASE detail WHEN 'YouTube Premiere / 预约' THEN 'YouTube Premiere' ELSE 'YouTube Live' END WHERE source='youtube' AND detail IN ('YouTube Live / 预约','YouTube Premiere / 预约','メンバーシップ限定 / YouTube')` + owned,
		`UPDATE events SET status='pending' WHERE status='待确认'` + owned,
	} {
		result, err := tx.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("%w: upgrade backfill failed", ErrSchema)
		}
		count, _ := result.RowsAffected()
		changed += count
	}
	if changed > 0 {
		if err := bumpRevision(ctx, tx, workflowTime(time.Now())); err != nil {
			return fmt.Errorf("%w: upgrade backfill failed", ErrSchema)
		}
	}
	return nil
}

// backfillYoutubeEventTimePrecision repairs automatic YouTube rows that keep
// unknown precision although they carry a timestamp.
// A timestamp supplied by the YouTube reservation path is exact; manual rows,
// tombstones and other sources keep their operator-owned precision.
func backfillYoutubeEventTimePrecision(ctx context.Context, tx *sql.Tx) error {
	result, err := tx.ExecContext(ctx, `UPDATE events SET time_precision='exact'
WHERE source='youtube' AND starts_at IS NOT NULL AND TRIM(starts_at)<>''
  AND time_precision='unknown' AND provenance='automatic' AND manual_locked=0 AND deleted_at IS NULL`)
	if err != nil {
		return fmt.Errorf("%w: upgrade backfill failed", ErrSchema)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	if err = bumpRevision(ctx, tx, workflowTime(time.Now())); err != nil {
		return fmt.Errorf("%w: upgrade backfill failed", ErrSchema)
	}
	return nil
}

// retireDefaultModel runs once: queue targets that followed the provider
// default name that model explicitly, and when that pair is queued twice the
// earlier position stays. A target whose provider never had a default is
// dropped because it could not be called. llm_providers.model is kept as it
// was.
func retireDefaultModel(ctx context.Context, tx *sql.Tx, now string) error {
	for _, statement := range []string{
		`DELETE FROM llm_route_targets WHERE model_id<>'' AND EXISTS (SELECT 1 FROM llm_route_targets e
 JOIN llm_providers p ON p.id=e.provider_id
 WHERE e.route=llm_route_targets.route AND e.provider_id=llm_route_targets.provider_id AND e.model_id=''
 AND p.model=llm_route_targets.model_id AND e.priority<llm_route_targets.priority)`,
		`DELETE FROM llm_route_targets WHERE model_id='' AND EXISTS (SELECT 1 FROM llm_providers p
 JOIN llm_route_targets t ON t.route=llm_route_targets.route AND t.provider_id=p.id AND t.model_id=p.model
 WHERE p.id=llm_route_targets.provider_id AND p.model<>'')`,
		`UPDATE llm_route_targets SET model_id=(SELECT p.model FROM llm_providers p WHERE p.id=llm_route_targets.provider_id), updated_at=?1
 WHERE model_id='' AND EXISTS (SELECT 1 FROM llm_providers p WHERE p.id=llm_route_targets.provider_id AND p.model<>'')`,
		`DELETE FROM llm_route_targets WHERE model_id=''`,
		`INSERT INTO app_settings(key,value,updated_at) VALUES('llm_default_model_retired','1',?1)`,
	} {
		args := []any{}
		if strings.Contains(statement, "?1") {
			args = append(args, now)
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return err
		}
	}
	return nil
}

// mergeRouteQueue folds the older per-stage routes into the single priority
// queue: message, then board, then vision targets in their saved order, each
// provider/model pair once. Stages pick capable targets from the queue, so
// no configured target is lost. An existing queue is kept as it is.
func mergeRouteQueue(ctx context.Context, tx *sql.Tx, now string) error {
	const queue = "schedule"
	stages := []string{"schedule_message", "schedule_board", "schedule_vision"}
	rows, err := tx.QueryContext(ctx, `SELECT route,provider_id,model_id FROM llm_route_targets
 WHERE route IN (?,?,?) ORDER BY CASE route WHEN ? THEN 0 WHEN ? THEN 1 ELSE 2 END,priority,provider_id,model_id`,
		stages[0], stages[1], stages[2], stages[0], stages[1])
	if err != nil {
		return ErrSchema
	}
	type target struct{ provider, model string }
	var targets []target
	seen := map[target]bool{}
	for rows.Next() {
		var route string
		var item target
		if err = rows.Scan(&route, &item.provider, &item.model); err != nil {
			rows.Close()
			return ErrSchema
		}
		if !seen[item] {
			seen[item] = true
			targets = append(targets, item)
		}
	}
	if err = rows.Close(); err != nil {
		return ErrSchema
	}
	if err = rows.Err(); err != nil || len(targets) == 0 {
		return err
	}
	var queued int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_route_targets WHERE route=?`, queue).Scan(&queued); err != nil {
		return ErrSchema
	}
	if queued == 0 {
		for priority, item := range targets {
			if _, err = tx.ExecContext(ctx, `INSERT INTO llm_route_targets(route,provider_id,model_id,priority,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
				queue, item.provider, item.model, priority, now, now); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM llm_route_targets WHERE route IN (?,?,?)`, stages[0], stages[1], stages[2])
	return err
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
