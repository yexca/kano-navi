package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	shopsource "github.com/yexca/kano-navi/backend/internal/source/shop"
)

// ShopSource is the source, timer and step that read the official shop's
// sale periods. Its entries are events with source = 'shop', one per period
// group: source_item_id is the group's normalized title and source_key the
// period (<opens>/<closes>). Its read bookkeeping is the sync_state row of
// source 'shop' and the shop's host, never returned by a public read.
const ShopSource = "shop"

type shopMetadata struct {
	LastAttemptAt string `json:"lastAttemptAt,omitempty"`
	BlockedUntil  string `json:"blockedUntil,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

// ShopState reads the shop's read bookkeeping; a shop never read has the
// zero state.
func (s *Store) ShopState(ctx context.Context) (shopsource.State, error) {
	var lastSuccess, meta string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(last_success_at,''),COALESCE(metadata_json,'{}') FROM sync_state WHERE source=? AND account_id=?`, ShopSource, shopsource.Host).Scan(&lastSuccess, &meta)
	if errors.Is(err, sql.ErrNoRows) {
		return shopsource.State{}, nil
	}
	if err != nil {
		return shopsource.State{}, ErrUnavailable
	}
	var data shopMetadata
	_ = json.Unmarshal([]byte(meta), &data)
	return shopsource.State{
		LastAttemptAt: parseInstant(data.LastAttemptAt), LastSuccessAt: parseInstant(lastSuccess),
		BlockedUntil: parseInstant(data.BlockedUntil), LastError: data.LastError,
	}, nil
}

// ShopSales are the stored period groups with their evidence, including
// those whose entry an operator locked or deleted, so a group keeps its
// identity whatever happened to its entry.
func (s *Store) ShopSales(ctx context.Context) ([]shopsource.Sale, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_item_id,source_key,COALESCE(raw_json,'{}') FROM event_sources WHERE source=? ORDER BY id`, ShopSource)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	out := []shopsource.Sale{}
	for rows.Next() {
		var key, period, raw string
		if rows.Scan(&key, &period, &raw) != nil {
			return nil, ErrUnavailable
		}
		var sale shopsource.Sale
		_ = json.Unmarshal([]byte(raw), &sale)
		opens, closes, ok := strings.Cut(period, "/")
		if !ok {
			continue
		}
		sale.Key = key
		sale.Period.Opens, sale.Period.Closes = opens, closes
		out = append(out, sale)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return out, nil
}

// ShopStatus is the console's view of the shop source.
type ShopStatus struct {
	Sales         int     `json:"sales"`
	LastAttemptAt *string `json:"lastAttemptAt"`
	LastSuccessAt *string `json:"lastSuccessAt"`
	BlockedUntil  *string `json:"blockedUntil"`
	LastError     *string `json:"lastError"`
}

func (s *Store) ShopStatus(ctx context.Context) (ShopStatus, error) {
	state, err := s.ShopState(ctx)
	if err != nil {
		return ShopStatus{}, err
	}
	optional := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	out := ShopStatus{
		LastAttemptAt: optional(formatInstant(state.LastAttemptAt)), LastSuccessAt: optional(formatInstant(state.LastSuccessAt)),
		BlockedUntil: optional(formatInstant(state.BlockedUntil)), LastError: optional(state.LastError),
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE source=? AND deleted_at IS NULL`, ShopSource).Scan(&out.Sales); err != nil {
		return ShopStatus{}, ErrUnavailable
	}
	return out, nil
}

// shopEventID is a new group's event ID, derived from its identity.
func shopEventID(key, period string) string {
	sum := sha256.Sum256([]byte(key + "\x00" + period))
	return "shop-" + hex.EncodeToString(sum[:12])
}

// WriteShopRead stores one read's state and, when it succeeded, its period
// groups, in one transaction. A failed or skipped read stores only the
// state, so every known entry stays. A group is written as an automatic
// sale; manual locks and tombstones keep the entry as the operator left it,
// while its evidence follows the shop. Nothing is retired: a product that
// leaves the listing keeps its entry. It reports whether an entry changed.
func (s *Store) WriteShopRead(ctx context.Context, result shopsource.Result) (bool, error) {
	if s == nil || !s.workflowWritable {
		return false, ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return false, err
	}
	now := workflowTime(time.Now())
	changed := false
	for _, sale := range result.Sales {
		written, writeErr := writeShopSale(ctx, tx, sale, now)
		if writeErr != nil {
			return false, writeErr
		}
		changed = changed || written
	}
	meta, err := json.Marshal(shopMetadata{
		LastAttemptAt: formatInstant(result.State.LastAttemptAt), BlockedUntil: formatInstant(result.State.BlockedUntil),
		LastError: result.State.LastError,
	})
	if err != nil {
		return false, ErrSourceWrite
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,last_success_at,metadata_json) VALUES(?,?,?,?)
 ON CONFLICT(source,account_id) DO UPDATE SET last_success_at=COALESCE(excluded.last_success_at,sync_state.last_success_at),metadata_json=excluded.metadata_json`,
		ShopSource, shopsource.Host, nullable(formatInstant(result.State.LastSuccessAt)), string(meta)); err != nil {
		return false, ErrSourceWrite
	}
	if changed {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return false, ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return false, ErrSourceWrite
	}
	return changed, nil
}

func writeShopSale(ctx context.Context, tx *sql.Tx, sale shopsource.Sale, now string) (bool, error) {
	period := sale.Period.Key()
	if sale.Key == "" || sale.Title == "" || sale.Period.Opens == "" || sale.Period.Closes == "" {
		return false, ErrSourceWrite
	}
	raw, err := json.Marshal(sale)
	if err != nil {
		return false, ErrSourceWrite
	}
	eventID := shopEventID(sale.Key, period)
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE source=? AND source_item_id=? AND source_key=?
 UNION ALL SELECT event_id FROM event_sources WHERE source=? AND source_item_id=? AND source_key=? LIMIT 1`,
		ShopSource, sale.Key, period, ShopSource, sale.Key, period).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, ErrSourceWrite
	}
	if existing != "" {
		eventID = existing
	}
	var startsAt any
	precision := "unknown"
	if at, ok := sale.StartsAt(); ok {
		startsAt, precision = workflowTime(at), "exact"
	}
	// The close time is not in the product JSON, so ends_at stays null and an
	// operator's close time is kept by the lock that saving it sets.
	result, err := tx.ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,ends_on,timezone,time_precision,status,event_type,url,is_upcoming,provenance,manual_locked,created_at,updated_at,raw_json,platform,platforms,platforms_inferred)
 VALUES(?,?,?,?,?,NULL,?,?,NULL,?,'Asia/Tokyo',?,NULL,'sale',?,0,'automatic',0,?,?,?,'other','["other"]',0)
 ON CONFLICT(id) DO UPDATE SET title=excluded.title,starts_on=excluded.starts_on,starts_at=excluded.starts_at,ends_on=excluded.ends_on,time_precision=excluded.time_precision,
 event_type=excluded.event_type,url=excluded.url,platform=excluded.platform,platforms=excluded.platforms,platforms_inferred=excluded.platforms_inferred,raw_json=excluded.raw_json,deleted_at=NULL,updated_at=excluded.updated_at
 WHERE events.manual_locked=0 AND (events.deleted_at IS NULL OR `+restorableEventSQL("events")+`) AND (events.deleted_at IS NOT NULL OR events.title IS NOT excluded.title
 OR events.starts_on IS NOT excluded.starts_on OR events.starts_at IS NOT excluded.starts_at OR events.ends_on IS NOT excluded.ends_on
 OR events.time_precision IS NOT excluded.time_precision OR events.event_type IS NOT excluded.event_type OR events.url IS NOT excluded.url OR events.platform IS NOT excluded.platform OR events.platforms IS NOT excluded.platforms OR events.platforms_inferred IS NOT excluded.platforms_inferred)`,
		eventID, ShopSource, sale.Key, period, sale.Title, sale.Period.Opens, startsAt, sale.Period.Closes, precision, sale.URL, now, now, string(raw))
	if err != nil {
		return false, ErrSourceWrite
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, ErrSourceWrite
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,deleted_at,created_at,updated_at) VALUES(?,?,?,?,?,?,NULL,?,?)
 ON CONFLICT(source,source_item_id,source_key) DO UPDATE SET url=excluded.url,raw_json=excluded.raw_json,deleted_at=NULL,updated_at=excluded.updated_at
 WHERE event_sources.url IS NOT excluded.url OR event_sources.raw_json IS NOT excluded.raw_json OR event_sources.deleted_at IS NOT NULL`,
		eventID, ShopSource, sale.Key, period, sale.URL, string(raw), now, now); err != nil {
		return false, ErrSourceWrite
	}
	return count > 0, nil
}
