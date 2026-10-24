package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

// AdhocLiveWorkflow names the YouTube live-status probe runs.
const AdhocLiveWorkflow = "@live"

// The live-status probe interval is an operator setting. It may run more
// often than a saved workflow (15 minutes) because a run requests at most
// ytsource.MaxLiveProbes watch pages and is skipped while nothing is active.
const (
	LiveProbeDefaultMinutes = 30
	LiveProbeMinMinutes     = 5
	LiveProbeMaxMinutes     = 360

	settingLiveProbeEnabled  = "youtube_live_probe_enabled"
	settingLiveProbeInterval = "youtube_live_probe_interval_minutes"
)

// liveProbeLead is how soon a reservation must start to be probed.
const liveProbeLead = 15 * time.Minute

// liveProbeLookback bounds how long ago an active video may have started.
const liveProbeLookback = 24 * time.Hour

type LiveProbeSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

// Freshness is how long a "live" or "upcoming" result counts on the board:
// two probe intervals, so one missed run does not drop a confirmed state.
func (p LiveProbeSettings) Freshness() time.Duration {
	return 2 * time.Duration(p.IntervalMinutes) * time.Minute
}

// LiveProbeSettings reads the operator settings; absent or invalid values
// use the defaults.
func (s *Store) LiveProbeSettings(ctx context.Context) (LiveProbeSettings, error) {
	out := LiveProbeSettings{Enabled: true, IntervalMinutes: LiveProbeDefaultMinutes}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?,?)`, settingLiveProbeEnabled, settingLiveProbeInterval)
	if err != nil {
		return out, dbErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return out, dbErr(err)
		}
		switch key {
		case settingLiveProbeEnabled:
			out.Enabled = value != "0"
		case settingLiveProbeInterval:
			if n, e := strconv.Atoi(value); e == nil && n >= LiveProbeMinMinutes && n <= LiveProbeMaxMinutes {
				out.IntervalMinutes = n
			}
		}
	}
	if err = rows.Err(); err != nil {
		return out, dbErr(err)
	}
	return out, nil
}

// SaveLiveProbeSettings stores validated operator settings.
func (s *Store) SaveLiveProbeSettings(ctx context.Context, settings LiveProbeSettings) error {
	if !s.Writable() {
		return ErrReadOnly
	}
	if settings.IntervalMinutes < LiveProbeMinMinutes || settings.IntervalMinutes > LiveProbeMaxMinutes {
		return ErrWorkflowValidation
	}
	enabled := "0"
	if settings.Enabled {
		enabled = "1"
	}
	now := workflowTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	for key, value := range map[string]string{settingLiveProbeEnabled: enabled, settingLiveProbeInterval: strconv.Itoa(settings.IntervalMinutes)} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, now); err != nil {
			return ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

// LastLiveProbeAt is when the newest probe run was queued, or nil.
func (s *Store) LastLiveProbeAt(ctx context.Context) (*time.Time, error) {
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM workflow_runs WHERE workflow_id=? ORDER BY created_at DESC LIMIT 1`, AdhocLiveWorkflow).Scan(&created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrWorkflowStorage
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, ErrWorkflowStorage
	}
	return &t, nil
}

// YoutubeLiveCandidates selects the active videos a probe checks, at most
// limit: reservations that start within liveProbeLead or have started, and
// broadcasts without a known end, from the last liveProbeLookback. Videos the
// page already reported as over, ended reservations and manually confirmed
// cancellations are left out. The least recently checked go first.
func (s *Store) YoutubeLiveCandidates(ctx context.Context, now time.Time, limit int) ([]ytsource.Video, error) {
	if limit < 1 {
		return []ytsource.Video{}, nil
	}
	n := now.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,v.title,COALESCE(v.published_at,''),COALESCE(v.scheduled_at,''),v.url,COALESCE(v.thumbnail_url,''),COALESCE(v.kind,''),v.is_upcoming FROM videos v
 WHERE v.source='youtube' AND v.scheduled_at IS NOT NULL
 AND julianday(v.scheduled_at)<=julianday(?1,?2) AND julianday(v.scheduled_at)>=julianday(?1,?3)
 AND COALESCE(v.live_status,'')<>'ended'
 AND NOT EXISTS (SELECT 1 FROM events e WHERE (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.id))
   AND (e.cancellation_status='manual_confirmed' OR (e.deleted_at IS NULL AND julianday(e.ends_at)<=julianday(?1))))
 ORDER BY v.live_checked_at IS NOT NULL,julianday(v.live_checked_at),ABS(julianday(v.scheduled_at)-julianday(?1)),v.id
 LIMIT ?4`, n, sqliteModifier(liveProbeLead), sqliteModifier(-liveProbeLookback), limit)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []ytsource.Video{}
	for rows.Next() {
		var v ytsource.Video
		if err = rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ScheduledAt, &v.URL, &v.ThumbnailURL, &v.Kind, &v.IsUpcoming); err != nil {
			return nil, dbErr(err)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	return out, nil
}

func sqliteModifier(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign, d = "-", -d
	}
	return sign + strconv.Itoa(int(d/time.Minute)) + " minutes"
}

// WriteYoutubeLive stores what a probe read: the actual start, the reported
// end, the reservation and broadcast state, and the event status text. It
// never touches feed cursors or the detail queue, keeps manual locks and
// tombstones, and bumps the dashboard revision only when a public field
// changed. It reports that change.
func (s *Store) WriteYoutubeLive(ctx context.Context, result ytsource.ProbeResult) (bool, error) {
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
	videos := make([]ytsource.Video, 0, len(result.Videos))
	for _, v := range result.Videos {
		// A probe refreshes known videos only; it never adds one.
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos WHERE id=?`, v.ID).Scan(&exists); err != nil {
			return false, ErrSourceWrite
		}
		if exists > 0 {
			v.FeedOnly = false
			videos = append(videos, v)
		}
	}
	changed, err := writeYoutubeVideos(ctx, tx, videos, now)
	if err != nil {
		return false, err
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

// liveView is the broadcast state the board may trust. A reported end is
// final; "live" and "upcoming" count only when checked after cutoff.
func liveView(status, checked sql.NullString, cutoff time.Time) *string {
	if !status.Valid {
		return nil
	}
	switch status.String {
	case ytsource.LiveStatusEnded:
		return &status.String
	case ytsource.LiveStatusLive, ytsource.LiveStatusUpcoming:
		t, err := time.Parse(time.RFC3339Nano, checked.String)
		if checked.Valid && err == nil && t.After(cutoff) {
			return &status.String
		}
	}
	return nil
}
