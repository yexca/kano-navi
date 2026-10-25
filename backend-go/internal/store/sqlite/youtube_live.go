package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

// AdhocLiveWorkflow names the YouTube live-status probe runs.
const AdhocLiveWorkflow = "@live"

// The live-status probe interval is an operator setting: how often a stream
// on air, or a waiting room past its starting window, is checked. It may be
// shorter than a saved workflow's (15 minutes) because a run requests at most
// ytsource.MaxLiveProbes watch pages and is skipped while nothing is due.
const (
	LiveProbeDefaultMinutes = 15
	LiveProbeMinMinutes     = 5
	LiveProbeMaxMinutes     = 360

	settingLiveProbeEnabled  = "youtube_live_probe_enabled"
	settingLiveProbeInterval = "youtube_live_probe_interval_minutes"
)

// liveProbeLead is how soon a reservation must start to be probed.
const liveProbeLead = 15 * time.Minute

// liveProbeLookback bounds how long ago an active video may have started.
const liveProbeLookback = 24 * time.Hour

// From its start until it is confirmed live, a reservation is checked every
// liveProbeStartingInterval for liveProbeStartingWindow, whatever the
// operator interval, so the board learns of an on-time start within minutes.
const (
	liveProbeStartingInterval = 3 * time.Minute
	liveProbeStartingWindow   = 45 * time.Minute
)

// LiveProbeMinGap separates probe runs, so a page that keeps failing before
// its attempt is recorded cannot start a run on every timer tick.
const LiveProbeMinGap = 2 * time.Minute

// liveProbeLimit bounds the active videos one plan considers; a run still
// requests at most ytsource.MaxLiveProbes of them.
const liveProbeLimit = 50

// liveAttemptSource names the sync_state row that remembers when the probe
// last asked for each video, answered or not.
const liveAttemptSource = "youtube-live"

// Probe phases: where an active video stands against its start.
const (
	LivePhasePrestart = "prestart"
	LivePhaseStarting = "starting"
	LivePhaseLive     = "live"
	LivePhaseOverdue  = "overdue"
)

// LiveProbeTarget is an active video with the probe's plan for it.
type LiveProbeTarget struct {
	Video ytsource.Video
	Phase string
	DueAt time.Time
}

type LiveProbeSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

// Freshness is how long a "live" or "upcoming" result counts on the board:
// two probe intervals, so one missed run does not drop a confirmed state.
// No phase waits longer than one interval between checks.
func (p LiveProbeSettings) Freshness() time.Duration {
	return 2 * time.Duration(p.IntervalMinutes) * time.Minute
}

// liveProbeDue places an active video in its phase and says when it is next
// due, from its reserved (or, once on air, actual) start, the broadcast state
// the last page reported and the last time its page was read or asked for
// (zero when never).
func (p LiveProbeSettings) liveProbeDue(start, last time.Time, status string, now time.Time) (string, time.Time) {
	interval := time.Duration(p.IntervalMinutes) * time.Minute
	switch {
	case status == ytsource.LiveStatusLive:
		return LivePhaseLive, last.Add(interval)
	case now.Before(start):
		// One check once the reservation is near, then none until it starts.
		if last.Before(start.Add(-liveProbeLead)) {
			return LivePhasePrestart, start.Add(-liveProbeLead)
		}
		return LivePhasePrestart, start
	case now.Before(start.Add(liveProbeStartingWindow)):
		return LivePhaseStarting, last.Add(liveProbeStartingInterval)
	default:
		return LivePhaseOverdue, last.Add(interval)
	}
}

// NextLiveProbeAt is when the probe should run for these targets: the
// earliest due time, no sooner than LiveProbeMinGap after the previous run
// (last, nil when none) and never before now. It is nil while nothing is
// active.
func NextLiveProbeAt(targets []LiveProbeTarget, last *time.Time, now time.Time) *time.Time {
	if len(targets) == 0 {
		return nil
	}
	next := targets[0].DueAt
	for _, t := range targets[1:] {
		if t.DueAt.Before(next) {
			next = t.DueAt
		}
	}
	if last != nil && next.Before(last.Add(LiveProbeMinGap)) {
		next = last.Add(LiveProbeMinGap)
	}
	if next.Before(now) {
		next = now
	}
	return &next
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

// LiveProbeTargets plans the active videos: reservations that start within
// liveProbeLead or have started, and broadcasts without a known end, from the
// last liveProbeLookback. Videos the page already reported as over, ended
// reservations and manually confirmed cancellations are left out. Each target
// is due by its phase, counted from its last successful check or probe
// attempt, whichever is newer; the earliest due come first, and among equals
// the never-checked, then the least recently checked.
func (s *Store) LiveProbeTargets(ctx context.Context, now time.Time, settings LiveProbeSettings) ([]LiveProbeTarget, error) {
	attempts, err := s.liveProbeAttempts(ctx)
	if err != nil {
		return nil, err
	}
	n := now.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,v.title,COALESCE(v.published_at,''),COALESCE(v.scheduled_at,''),v.url,COALESCE(v.thumbnail_url,''),COALESCE(v.kind,''),v.is_upcoming,COALESCE(v.live_status,''),COALESCE(v.live_checked_at,'') FROM videos v
 WHERE v.source='youtube' AND v.scheduled_at IS NOT NULL
 AND julianday(v.scheduled_at)<=julianday(?1,?2) AND julianday(v.scheduled_at)>=julianday(?1,?3)
 AND COALESCE(v.live_status,'')<>'ended'
 AND NOT EXISTS (SELECT 1 FROM events e WHERE (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.id))
   AND (e.cancellation_status='manual_confirmed' OR (e.deleted_at IS NULL AND julianday(e.ends_at)<=julianday(?1))))
 ORDER BY v.live_checked_at IS NOT NULL,julianday(v.live_checked_at),ABS(julianday(v.scheduled_at)-julianday(?1)),v.id
 LIMIT ?4`, n, sqliteModifier(liveProbeLead), sqliteModifier(-liveProbeLookback), liveProbeLimit)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []LiveProbeTarget{}
	for rows.Next() {
		var v ytsource.Video
		var status, checked string
		if err = rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ScheduledAt, &v.URL, &v.ThumbnailURL, &v.Kind, &v.IsUpcoming, &status, &checked); err != nil {
			return nil, dbErr(err)
		}
		start, _ := time.Parse(time.RFC3339Nano, v.ScheduledAt)
		last, _ := time.Parse(time.RFC3339Nano, checked)
		if attempted := attempts[v.ID]; attempted.After(last) {
			last = attempted
		}
		phase, due := settings.liveProbeDue(start, last, status, now)
		out = append(out, LiveProbeTarget{Video: v, Phase: phase, DueAt: due})
	}
	if err = rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DueAt.Before(out[j].DueAt) })
	return out, nil
}

// liveAttempts is the metadata of the liveAttemptSource row: when the probe
// last requested each video's page.
type liveAttempts struct {
	Attempts map[string]string `json:"attempts"`
}

func (s *Store) liveProbeAttempts(ctx context.Context) (map[string]time.Time, error) {
	var meta string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source=? AND account_id=''`, liveAttemptSource).Scan(&meta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, dbErr(err)
	}
	var data liveAttempts
	_ = json.Unmarshal([]byte(meta), &data)
	out := map[string]time.Time{}
	for id, at := range data.Attempts {
		if t, e := time.Parse(time.RFC3339Nano, at); e == nil {
			out[id] = t
		}
	}
	return out, nil
}

// recordLiveAttempts stamps the requested videos and forgets attempts older
// than twice the lookback, so the row stays as small as the active set.
func recordLiveAttempts(ctx context.Context, tx *sql.Tx, ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	var meta string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source=? AND account_id=''`, liveAttemptSource).Scan(&meta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrSourceWrite
	}
	var data liveAttempts
	_ = json.Unmarshal([]byte(meta), &data)
	kept := map[string]string{}
	cutoff := now.Add(-2 * liveProbeLookback)
	for id, at := range data.Attempts {
		if t, e := time.Parse(time.RFC3339Nano, at); e == nil && t.After(cutoff) {
			kept[id] = at
		}
	}
	for _, id := range ids {
		kept[id] = workflowTime(now)
	}
	encoded, err := json.Marshal(liveAttempts{Attempts: kept})
	if err != nil {
		return ErrSourceWrite
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,metadata_json) VALUES(?,'',?) ON CONFLICT(source,account_id) DO UPDATE SET metadata_json=excluded.metadata_json`, liveAttemptSource, string(encoded)); err != nil {
		return ErrSourceWrite
	}
	return nil
}

func sqliteModifier(d time.Duration) string {
	sign := "+"
	if d < 0 {
		sign, d = "-", -d
	}
	return sign + strconv.Itoa(int(d/time.Minute)) + " minutes"
}

// WriteYoutubeLive stores what a probe read: the actual start, the reported
// end, the reservation and broadcast state, and the event status text, and
// when it asked for each page, answered or not. It never touches feed cursors
// or the detail queue, keeps manual locks and tombstones, and bumps the
// dashboard revision only when a public field changed. It reports that change.
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
	at := time.Now()
	now := workflowTime(at)
	if err = recordLiveAttempts(ctx, tx, result.Attempted, at); err != nil {
		return false, err
	}
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
