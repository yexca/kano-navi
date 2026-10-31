package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// X API chain state (ADR-0004) lives in sync_state under source x-api: one
// row per account with its consecutive timeline failures, and the usage row
// with the posts X API reads returned on the current Japan day.
const (
	xAPIStateSource = "x-api"
	xAPIUsageKey    = "@usage"
)

var japan = time.FixedZone("Asia/Tokyo", 9*60*60)

// JapanDay is the Asia/Tokyo calendar day of t, which the X API daily cap
// counts by.
func JapanDay(t time.Time) string { return t.In(japan).Format(time.DateOnly) }

// XAPIUsage is how many posts X API reads returned on a Japan day.
type XAPIUsage struct {
	Day   string `json:"day"`
	Posts int    `json:"posts"`
}

// XAPIState is the console's view of the X API in the chain: today's usage
// and each account's current timeline failure streak.
type XAPIState struct {
	Usage            XAPIUsage      `json:"usage"`
	TimelineFailures map[string]int `json:"timelineFailures"`
}

func (s *Store) xAPIMetadata(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) (map[string]any, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source=? AND account_id=?`, xAPIStateSource, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, ErrSourceWrite
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

// XAPIUsageOn reads the posts X API reads returned on day; another day's
// count reads as zero.
func (s *Store) XAPIUsageOn(ctx context.Context, day string) (XAPIUsage, error) {
	if s == nil || s.db == nil {
		return XAPIUsage{}, ErrUnavailable
	}
	meta, err := s.xAPIMetadata(ctx, s.db, xAPIUsageKey)
	if err != nil {
		return XAPIUsage{}, err
	}
	usage := XAPIUsage{Day: day}
	if stored, _ := meta["day"].(string); stored == day {
		posts, _ := meta["posts"].(float64)
		usage.Posts = int(posts)
	}
	return usage, nil
}

// AddXAPIUsage counts posts an X API read returned toward day, timed or
// manual, and starts over on a new day.
func (s *Store) AddXAPIUsage(ctx context.Context, day string, posts int) error {
	if posts <= 0 {
		return nil
	}
	if s == nil || s.db == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	meta, err := s.xAPIMetadata(ctx, tx, xAPIUsageKey)
	if err != nil {
		return err
	}
	total := posts
	if stored, _ := meta["day"].(string); stored == day {
		previous, _ := meta["posts"].(float64)
		total += int(previous)
	}
	if err = writeXAPIMetadata(ctx, tx, xAPIUsageKey, map[string]any{"day": day, "posts": total}); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

// XTimelineFailures is an account's current streak of runs whose timeline
// failed.
func (s *Store) XTimelineFailures(ctx context.Context, account string) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	meta, err := s.xAPIMetadata(ctx, s.db, account)
	if err != nil {
		return 0, err
	}
	failures, _ := meta["timelineFailures"].(float64)
	return int(failures), nil
}

// SetXTimelineFailures stores an account's timeline failure streak.
func (s *Store) SetXTimelineFailures(ctx context.Context, account string, failures int) error {
	if s == nil || s.db == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	if err = writeXAPIMetadata(ctx, tx, account, map[string]any{"timelineFailures": max(failures, 0)}); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

// XAPIStatus reads today's usage and the failure streak of each account.
func (s *Store) XAPIStatus(ctx context.Context, accounts []string, now time.Time) (XAPIState, error) {
	usage, err := s.XAPIUsageOn(ctx, JapanDay(now))
	if err != nil {
		return XAPIState{}, err
	}
	out := XAPIState{Usage: usage, TimelineFailures: map[string]int{}}
	for _, account := range accounts {
		if out.TimelineFailures[account], err = s.XTimelineFailures(ctx, account); err != nil {
			return XAPIState{}, err
		}
	}
	return out, nil
}

func writeXAPIMetadata(ctx context.Context, tx *sql.Tx, key string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return ErrSourceWrite
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES(?,?,NULL,NULL,NULL,?) ON CONFLICT(source,account_id) DO UPDATE SET metadata_json=excluded.metadata_json`, xAPIStateSource, key, string(encoded)); err != nil {
		return ErrSourceWrite
	}
	return nil
}
