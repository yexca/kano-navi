package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

// AdhocLiveWorkflow names the YouTube live-status probe runs.
const AdhocLiveWorkflow = "@live"

// The live-status probe interval is an operator setting: how often a stream
// on air, or a waiting room past its starting window, is checked. It may be
// shorter than a source schedule's (15 minutes) because a run requests at most
// ytsource.MaxLiveProbes watch pages and is skipped while nothing is due.
const (
	LiveProbeDefaultMinutes = 15
	LiveProbeMinMinutes     = 5
	LiveProbeMaxMinutes     = 360

	settingLiveProbeEnabled  = "youtube_live_probe_enabled"
	settingLiveProbeInterval = "youtube_live_probe_interval_minutes"
)

// The Bilibili live check has its own operator settings: whether it runs and
// its baseline interval, how often the room is asked about when no announced
// stream is due, so an unannounced stream is still seen. One check is one
// request about the account's room.
const (
	BilibiliLiveDefaultMinutes = 30
	BilibiliLiveMinMinutes     = 10
	BilibiliLiveMaxMinutes     = 360

	settingBilibiliLiveEnabled  = "bilibili_live_probe_enabled"
	settingBilibiliLiveInterval = "bilibili_live_probe_interval_minutes"
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
	// BilibiliEnabled and BilibiliIntervalMinutes are the Bilibili live
	// check's settings.
	BilibiliEnabled         bool `json:"bilibiliEnabled"`
	BilibiliIntervalMinutes int  `json:"bilibiliIntervalMinutes"`
}

// Freshness is how long a "live" or "upcoming" result counts on the board:
// two probe intervals, so one missed run does not drop a confirmed state.
// No phase waits longer than one interval between checks.
func (p LiveProbeSettings) Freshness() time.Duration {
	return 2 * time.Duration(p.IntervalMinutes) * time.Minute
}

// BilibiliFreshness is how long the Bilibili room's last check counts on the
// board: two check intervals.
func (p LiveProbeSettings) BilibiliFreshness() time.Duration {
	return 2 * time.Duration(p.BilibiliIntervalMinutes) * time.Minute
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
	out := LiveProbeSettings{Enabled: true, IntervalMinutes: LiveProbeDefaultMinutes, BilibiliEnabled: true, BilibiliIntervalMinutes: BilibiliLiveDefaultMinutes}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM app_settings WHERE key IN (?,?,?,?)`, settingLiveProbeEnabled, settingLiveProbeInterval, settingBilibiliLiveEnabled, settingBilibiliLiveInterval)
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
		case settingBilibiliLiveEnabled:
			out.BilibiliEnabled = value != "0"
		case settingBilibiliLiveInterval:
			if n, e := strconv.Atoi(value); e == nil && n >= BilibiliLiveMinMinutes && n <= BilibiliLiveMaxMinutes {
				out.BilibiliIntervalMinutes = n
			}
		}
	}
	if err = rows.Err(); err != nil {
		return out, dbErr(err)
	}
	return out, nil
}

// SaveLiveProbeSettings stores validated operator settings. A zero Bilibili
// interval leaves both Bilibili settings as stored.
func (s *Store) SaveLiveProbeSettings(ctx context.Context, settings LiveProbeSettings) error {
	if !s.Writable() {
		return ErrReadOnly
	}
	keepBilibili := settings.BilibiliIntervalMinutes == 0
	if settings.IntervalMinutes < LiveProbeMinMinutes || settings.IntervalMinutes > LiveProbeMaxMinutes ||
		!keepBilibili && (settings.BilibiliIntervalMinutes < BilibiliLiveMinMinutes || settings.BilibiliIntervalMinutes > BilibiliLiveMaxMinutes) {
		return ErrWorkflowValidation
	}
	flag := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	now := workflowTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	values := map[string]string{settingLiveProbeEnabled: flag(settings.Enabled), settingLiveProbeInterval: strconv.Itoa(settings.IntervalMinutes)}
	if !keepBilibili {
		values[settingBilibiliLiveEnabled], values[settingBilibiliLiveInterval] = flag(settings.BilibiliEnabled), strconv.Itoa(settings.BilibiliIntervalMinutes)
	}
	for key, value := range values {
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
// reservations, manually confirmed cancellations and the videos the probe
// stopped asking about (see SetLiveProbeStopped) are left out. Each target is
// due by its phase, counted from its last successful check or probe attempt,
// whichever is newer; the earliest due come first, and among equals
// the never-checked, then the least recently checked.
func (s *Store) LiveProbeTargets(ctx context.Context, now time.Time, settings LiveProbeSettings) ([]LiveProbeTarget, error) {
	attempts, stopped, err := s.liveProbeAttempts(ctx)
	if err != nil {
		return nil, err
	}
	stoppedJSON, _ := json.Marshal(stopped)
	n := now.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,v.title,COALESCE(v.published_at,''),COALESCE(v.scheduled_at,''),v.url,COALESCE(v.thumbnail_url,''),COALESCE(v.kind,''),v.is_upcoming,COALESCE(v.live_status,''),COALESCE(v.live_checked_at,''),v.is_premiere,COALESCE(v.availability,'') FROM videos v
 WHERE v.source='youtube' AND v.scheduled_at IS NOT NULL
 AND julianday(v.scheduled_at)<=julianday(?1,?2) AND julianday(v.scheduled_at)>=julianday(?1,?3)
 AND COALESCE(v.live_status,'')<>'ended'
 AND NOT EXISTS (SELECT 1 FROM events e WHERE (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.id))
   AND (e.cancellation_status='manual_confirmed' OR (e.deleted_at IS NULL AND julianday(e.ends_at)<=julianday(?1))))
 AND v.id NOT IN (SELECT value FROM json_each(?5))
 ORDER BY v.live_checked_at IS NOT NULL,julianday(v.live_checked_at),ABS(julianday(v.scheduled_at)-julianday(?1)),v.id
 LIMIT ?4`, n, sqliteModifier(liveProbeLead), sqliteModifier(-liveProbeLookback), liveProbeLimit, string(stoppedJSON))
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []LiveProbeTarget{}
	for rows.Next() {
		var v ytsource.Video
		var status, checked string
		var premiere sql.NullBool
		if err = rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ScheduledAt, &v.URL, &v.ThumbnailURL, &v.Kind, &v.IsUpcoming, &status, &checked, &premiere, &v.Availability); err != nil {
			return nil, dbErr(err)
		}
		// The stored verdict lets a Data API answer classify an ended
		// broadcast, as in the recent step.
		v.Premiere, v.PremiereKnown = premiere.Bool, premiere.Valid
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

// liveState is the metadata of the liveAttemptSource row: when the probe
// last requested each video's page, and the videos it no longer asks about.
type liveState struct {
	Attempts map[string]string   `json:"attempts"`
	Stopped  map[string]liveStop `json:"stopped,omitempty"`
}

// liveStop is why and when the probe stopped asking about one video.
type liveStop struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Why the probe stopped asking about a video: YouTube reported it private or
// removed, or an operator stopped it.
const (
	LiveStopUnavailable = "unavailable"
	LiveStopOperator    = "operator"
)

// LiveProbeStop is a stopped video as the console lists it.
type LiveProbeStop struct {
	Video  ytsource.Video
	At     time.Time
	Reason string
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readLiveState(ctx context.Context, q rowQuerier) (liveState, error) {
	var meta string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}') FROM sync_state WHERE source=? AND account_id=''`, liveAttemptSource).Scan(&meta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return liveState{}, err
	}
	var data liveState
	_ = json.Unmarshal([]byte(meta), &data)
	return data, nil
}

// writeLiveState stores the row, forgetting attempts and stops older than
// twice the lookback, so it stays as small as the active set.
func writeLiveState(ctx context.Context, tx *sql.Tx, data liveState, now time.Time) error {
	cutoff := now.Add(-2 * liveProbeLookback)
	fresh := func(at string) bool {
		t, e := time.Parse(time.RFC3339Nano, at)
		return e == nil && t.After(cutoff)
	}
	out := liveState{Attempts: map[string]string{}, Stopped: map[string]liveStop{}}
	for id, at := range data.Attempts {
		if fresh(at) {
			out.Attempts[id] = at
		}
	}
	for id, stop := range data.Stopped {
		if fresh(stop.At) {
			out.Stopped[id] = stop
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,metadata_json) VALUES(?,'',?) ON CONFLICT(source,account_id) DO UPDATE SET metadata_json=excluded.metadata_json`, liveAttemptSource, string(encoded))
	return err
}

func (s *Store) liveProbeAttempts(ctx context.Context) (map[string]time.Time, []string, error) {
	data, err := readLiveState(ctx, s.db)
	if err != nil {
		return nil, nil, dbErr(err)
	}
	out := map[string]time.Time{}
	for id, at := range data.Attempts {
		if t, e := time.Parse(time.RFC3339Nano, at); e == nil {
			out[id] = t
		}
	}
	stopped := []string{}
	for id := range data.Stopped {
		stopped = append(stopped, id)
	}
	return out, stopped, nil
}

// recordLiveAttempts stamps the requested videos and stops the unavailable
// ones.
func recordLiveAttempts(ctx context.Context, tx *sql.Tx, ids, unavailable []string, now time.Time) error {
	if len(ids) == 0 && len(unavailable) == 0 {
		return nil
	}
	data, err := readLiveState(ctx, tx)
	if err != nil {
		return ErrSourceWrite
	}
	if data.Attempts == nil {
		data.Attempts = map[string]string{}
	}
	if data.Stopped == nil {
		data.Stopped = map[string]liveStop{}
	}
	for _, id := range ids {
		data.Attempts[id] = workflowTime(now)
	}
	for _, id := range unavailable {
		data.Stopped[id] = liveStop{At: workflowTime(now), Reason: LiveStopUnavailable}
	}
	if writeLiveState(ctx, tx, data, now) != nil {
		return ErrSourceWrite
	}
	return nil
}

// liftUnavailableStops lets the probe ask again about videos it stopped as
// unavailable once a later check records them watchable: public, unlisted or
// members, from a checked video or a failed page that told. An operator's stop
// stays.
func liftUnavailableStops(ctx context.Context, tx *sql.Tx, videos []ytsource.Video, availability map[string]string, now time.Time) error {
	watchable := map[string]bool{}
	mark := func(id, value string) {
		switch value {
		case ytsource.AvailabilityPublic, ytsource.AvailabilityUnlisted, ytsource.AvailabilityMembers:
			watchable[id] = true
		}
	}
	for _, v := range videos {
		mark(v.ID, v.Availability)
	}
	for id, value := range availability {
		mark(id, value)
	}
	if len(watchable) == 0 {
		return nil
	}
	data, err := readLiveState(ctx, tx)
	if err != nil {
		return ErrSourceWrite
	}
	lifted := false
	for id, stop := range data.Stopped {
		if watchable[id] && stop.Reason == LiveStopUnavailable {
			delete(data.Stopped, id)
			lifted = true
		}
	}
	if !lifted {
		return nil
	}
	if writeLiveState(ctx, tx, data, now) != nil {
		return ErrSourceWrite
	}
	return nil
}

// SetLiveProbeStopped stops or resumes the probe for one stored YouTube video
// on an operator's request. Stopping leaves the video's stored broadcast
// state alone; resuming makes it a target again when its start allows.
func (s *Store) SetLiveProbeStopped(ctx context.Context, id string, stopped bool, now time.Time) error {
	if !s.Writable() {
		return ErrReadOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos WHERE id=? AND source='youtube'`, id).Scan(&exists); err != nil {
		return ErrUnavailable
	}
	if exists == 0 {
		return ErrWorkflowNotFound
	}
	data, err := readLiveState(ctx, tx)
	if err != nil {
		return ErrUnavailable
	}
	if data.Stopped == nil {
		data.Stopped = map[string]liveStop{}
	}
	if stopped {
		data.Stopped[id] = liveStop{At: workflowTime(now), Reason: LiveStopOperator}
	} else {
		delete(data.Stopped, id)
	}
	if writeLiveState(ctx, tx, data, now) != nil || tx.Commit() != nil {
		return ErrUnavailable
	}
	return nil
}

// LiveProbeStopped lists the stopped videos that are still stored, newest
// stop first.
func (s *Store) LiveProbeStopped(ctx context.Context) ([]LiveProbeStop, error) {
	data, err := readLiveState(ctx, s.db)
	if err != nil {
		return nil, dbErr(err)
	}
	out := []LiveProbeStop{}
	for id, stop := range data.Stopped {
		at, e := time.Parse(time.RFC3339Nano, stop.At)
		if e != nil {
			continue
		}
		var v ytsource.Video
		err = s.db.QueryRowContext(ctx, `SELECT id,title,url,COALESCE(scheduled_at,'') FROM videos WHERE id=? AND source='youtube'`, id).Scan(&v.ID, &v.Title, &v.URL, &v.ScheduledAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, dbErr(err)
		}
		out = append(out, LiveProbeStop{Video: v, At: at, Reason: stop.Reason})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].Video.ID < out[j].Video.ID
	})
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
// end, the reservation and broadcast state, the event status text, the
// localized titles a Data API answer named, and when it asked for each page,
// answered or not. It never touches feed cursors
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
	if err = recordLiveAttempts(ctx, tx, result.Attempted, result.Unavailable, at); err != nil {
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
	translated, err := writeYoutubeLocalizations(ctx, tx, result.Localized, now)
	if err != nil {
		return false, err
	}
	availability, err := writeVideoAvailability(ctx, tx, result.Availability)
	if err != nil {
		return false, err
	}
	if err = liftUnavailableStops(ctx, tx, videos, result.Availability, at); err != nil {
		return false, err
	}
	changed = changed || translated || availability
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
