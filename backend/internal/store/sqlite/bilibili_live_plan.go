package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

// BilibiliLiveTarget is a scheduled stream the live check follows: densely
// when it is shown on Bilibili, sparsely otherwise (LivePhaseSparse).
type BilibiliLiveTarget struct {
	EventID, Title string
	StartsAt       time.Time
	Phase          string
	DueAt          time.Time
}

// BilibiliLivePlan is when the account's room is next asked about: the
// earliest due scheduled stream, or the baseline interval after the last
// check, and never during the live host's backoff.
type BilibiliLivePlan struct {
	Targets []BilibiliLiveTarget
	// Phase is the phase of the earliest due target, or LivePhaseBaseline.
	Phase string
	DueAt time.Time
	Live  BilibiliLive
}

// LiveNow is a broadcast of the account's room that no entry matched, as
// public reads list it.
type LiveNow struct {
	Platform, URL, Title, StartedAt string
}

const (
	// LivePhaseBaseline is a check no scheduled stream asked for.
	LivePhaseBaseline = "baseline"
	// LivePhaseSparse is a check of a scheduled stream that is not shown on
	// Bilibili, asking whether it is broadcast there as well.
	LivePhaseSparse = "sparse"
)

// A broadcast that starts at L is the stream shown on Bilibili starting at
// S when S-2h <= L <= S+3h: a stream may open a little early or start well
// late. Otherwise it is the YouTube broadcast that started within
// bilibiliSimulcastGap of L and had not ended before L.
const (
	bilibiliMatchEarly   = 2 * time.Hour
	bilibiliMatchLate    = 3 * time.Hour
	bilibiliSimulcastGap = 30 * time.Minute
	// bilibiliRestartGap is how soon after an observed end a broadcast is
	// still a restart of the same stream.
	bilibiliRestartGap = 30 * time.Minute
)

// A stream that is not shown on Bilibili gets a room check at about these
// offsets after its start, then one each Bilibili interval while the room is
// live.
var bilibiliSparseChecks = []time.Duration{5 * time.Minute, 20 * time.Minute}

// liveCandidateEvents selects the streams the room may be showing: active,
// timed stream or members-only entries that are not merged away and not a
// confirmed cancellation, whatever their platforms.
const liveCandidateEvents = `e.deleted_at IS NULL AND e.merged_into IS NULL AND e.event_type IN ('stream','member') AND e.starts_at IS NOT NULL AND COALESCE(e.cancellation_status,'none')<>'manual_confirmed'`

// liveCandidate is one such stream with what matching needs.
type liveCandidate struct {
	id, title, source string
	start, end        time.Time
	// bilibili: its platforms, with those its merged entries lend it,
	// include bilibili.
	bilibili bool
	// started: a YouTube event whose video a check saw live or ended, so its
	// broadcast actually began.
	started bool
}

// mergedPlatforms are what the automatic entries merged into each parent lend
// it: their announced platforms, and the watch link of a merged YouTube
// event. Only the merges that join platforms lend: Rule D (an X notice into
// the Bilibili dynamic's entry), Rule E (the Bilibili side of a simulcast)
// and an operator's merge. Rules A, B and C join duplicates of one entry on
// one platform.
type mergedPlatforms struct {
	platforms  map[string][]string
	youtubeURL map[string]string
}

// readMergedPlatforms reads what the automatic entries merged into parents
// lend them.
func readMergedPlatforms(ctx context.Context, q schemaQuerier, parents []string) (mergedPlatforms, error) {
	out := mergedPlatforms{platforms: map[string][]string{}, youtubeURL: map[string]string{}}
	for start := 0; start < len(parents); start += memoChunk {
		chunk := parents[start:min(len(parents), start+memoChunk)]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		args = append(args, MergeSourceManual, mergeReasonBilibili, mergeReasonSimulcast)
		rows, err := q.QueryContext(ctx, `SELECT merged_into,source,platform,platforms,COALESCE(url,'') FROM events
 WHERE deleted_at IS NULL AND provenance='automatic' AND merged_into IN (?`+strings.Repeat(",?", len(chunk)-1)+`)
 AND (merge_source=? OR merge_reason IN (?,?)) ORDER BY id`, args...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var parent, source, link string
			var platform, platforms sql.NullString
			if err = rows.Scan(&parent, &source, &platform, &platforms, &link); err != nil {
				_ = rows.Close()
				return out, err
			}
			announced, _ := readPlatforms(platforms, platform, link)
			out.platforms[parent] = NormalizePlatforms(append(out.platforms[parent], announced...))
			if source == "youtube" && out.youtubeURL[parent] == "" && strings.HasPrefix(streamIdentity(link), "youtube:") {
				out.youtubeURL[parent] = link
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return out, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// loadLiveCandidates reads the candidate streams starting from from to to.
func loadLiveCandidates(ctx context.Context, q schemaQuerier, from, to time.Time) ([]liveCandidate, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.id,e.title,e.source,e.starts_at,COALESCE(e.ends_at,''),e.platform,e.platforms,COALESCE(e.url,''),
 e.source='youtube' AND EXISTS(SELECT 1 FROM videos v WHERE v.id=e.source_item_id AND v.live_status IN ('live','ended'))
 FROM events e WHERE `+liveCandidateEvents+`
 AND julianday(e.starts_at)>=julianday(?) AND julianday(e.starts_at)<=julianday(?)
 ORDER BY julianday(e.starts_at),e.id LIMIT 60`, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	out := []liveCandidate{}
	for rows.Next() {
		var c liveCandidate
		var start, end, link string
		var platform, platforms sql.NullString
		if err = rows.Scan(&c.id, &c.title, &c.source, &start, &end, &platform, &platforms, &link, &c.started); err != nil {
			_ = rows.Close()
			return nil, err
		}
		announced, _ := readPlatforms(platforms, platform, link)
		c.bilibili = hasPlatform(announced, PlatformBilibili)
		c.end = parseInstant(end)
		if c.start = parseInstant(start); !c.start.IsZero() {
			out = append(out, c)
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	ids := make([]string, len(out))
	for i, c := range out {
		ids[i] = c.id
	}
	merged, err := readMergedPlatforms(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].bilibili = out[i].bilibili || hasPlatform(merged.platforms[out[i].id], PlatformBilibili)
	}
	return out, nil
}

// roomMatch is the entry a broadcast of the room matched: one shown on
// Bilibili, or a YouTube broadcast it is the simulcast of (observed).
type roomMatch struct {
	id       string
	observed bool
}

// matchRoomBroadcast matches a broadcast starting at at. First the nearest
// stream shown on Bilibili with S-2h <= L <= S+3h that had not ended before
// L; otherwise the YouTube stream nearest L whose video a check saw live or
// ended, that started within 30 minutes of L and had not ended before it.
// One broadcast matches one entry at most, and exclude (the entry an
// earlier, ended broadcast matched) matches none.
func matchRoomBroadcast(at time.Time, candidates []liveCandidate, exclude string) (roomMatch, bool) {
	if at.IsZero() {
		return roomMatch{}, false
	}
	ended := func(c liveCandidate) bool { return c.id == exclude || !c.end.IsZero() && c.end.Before(at) }
	best, bestGap := -1, time.Duration(0)
	for i, c := range candidates {
		gap := at.Sub(c.start)
		if !c.bilibili || gap < -bilibiliMatchEarly || gap > bilibiliMatchLate || ended(c) {
			continue
		}
		if best < 0 || gap.Abs() < bestGap {
			best, bestGap = i, gap.Abs()
		}
	}
	if best >= 0 {
		return roomMatch{id: candidates[best].id}, true
	}
	for i, c := range candidates {
		gap := at.Sub(c.start)
		if c.source != "youtube" || !c.started || gap.Abs() > bilibiliSimulcastGap || ended(c) {
			continue
		}
		if best < 0 || gap.Abs() < bestGap {
			best, bestGap = i, gap.Abs()
		}
	}
	if best >= 0 {
		return roomMatch{id: candidates[best].id, observed: true}, true
	}
	return roomMatch{}, false
}

// roomBroadcastMatch matches the room's last broadcast against every stored
// candidate, not only those a read returned, so every page agrees and a page
// that reads a narrow window does not list a matched broadcast as
// unannounced.
//
// An entry whose broadcast the room showed and a later check saw end never
// takes a broadcast that starts more than bilibiliRestartGap after that end:
// it is another broadcast, not a restart of the same one.
func roomBroadcastMatch(ctx context.Context, q schemaQuerier, live BilibiliLive) (roomMatch, bool, error) {
	if live.LastBroadcast.IsZero() {
		return roomMatch{}, false, nil
	}
	exclude := ""
	if previous := live.PreviousBroadcast; !previous.IsZero() && !live.PreviousEndedAt.IsZero() &&
		live.LastBroadcast.Sub(live.PreviousEndedAt) > bilibiliRestartGap {
		candidates, err := loadLiveCandidates(ctx, q, previous.Add(-bilibiliMatchLate), previous.Add(bilibiliMatchEarly))
		if err != nil {
			return roomMatch{}, false, err
		}
		if match, ok := matchRoomBroadcast(previous, candidates, ""); ok {
			exclude = match.id
		}
	}
	candidates, err := loadLiveCandidates(ctx, q, live.LastBroadcast.Add(-bilibiliMatchLate), live.LastBroadcast.Add(bilibiliMatchEarly))
	if err != nil {
		return roomMatch{}, false, err
	}
	match, ok := matchRoomBroadcast(live.LastBroadcast, candidates, exclude)
	return match, ok, nil
}

// BilibiliLivePlan plans the next check of the account's room. Scheduled
// streams starting within liveProbeLead or in the last liveProbeLookback are
// followed: one shown on Bilibili through the same phases as YouTube videos
// at the Bilibili interval, any other at about 5 and 20 minutes after its
// start and then each interval while the room is live. A stream whose
// broadcast the room already showed and ended, ended entries and manually
// confirmed cancellations are left out.
func (s *Store) BilibiliLivePlan(ctx context.Context, account string, now time.Time, settings LiveProbeSettings) (BilibiliLivePlan, error) {
	live, err := readBilibiliLive(ctx, s.db, account)
	if err != nil {
		return BilibiliLivePlan{}, dbErr(err)
	}
	plan := BilibiliLivePlan{Targets: []BilibiliLiveTarget{}, Live: live}
	interval := time.Duration(settings.BilibiliIntervalMinutes) * time.Minute
	last := live.State.CheckedAt
	if live.State.LastAttemptAt.After(last) {
		last = live.State.LastAttemptAt
	}
	plan.Phase, plan.DueAt = LivePhaseBaseline, now
	if !last.IsZero() {
		plan.DueAt = last.Add(interval)
	}
	candidates, err := loadLiveCandidates(ctx, s.db, now.Add(-liveProbeLookback), now.Add(liveProbeLead))
	if err != nil {
		return BilibiliLivePlan{}, dbErr(err)
	}
	ended := ""
	if !live.EndedAt.IsZero() {
		match, ok, matchErr := roomBroadcastMatch(ctx, s.db, live)
		if matchErr != nil {
			return BilibiliLivePlan{}, dbErr(matchErr)
		}
		if ok {
			ended = match.id
		}
	}
	phases := LiveProbeSettings{IntervalMinutes: settings.BilibiliIntervalMinutes}
	onAir := live.State.Room.LiveStatus == bilisource.RoomLive
	status := ""
	if onAir {
		status = LivePhaseLive
	}
	for _, c := range candidates {
		if c.id == ended || !c.end.IsZero() && !c.end.After(now) {
			continue
		}
		target := BilibiliLiveTarget{EventID: c.id, Title: c.title, StartsAt: c.start}
		if c.bilibili {
			target.Phase, target.DueAt = phases.liveProbeDue(c.start, last, status, now)
		} else if due, ok := sparseRoomCheck(c.start, last, interval, onAir); ok {
			target.Phase, target.DueAt = LivePhaseSparse, due
		} else {
			continue
		}
		plan.Targets = append(plan.Targets, target)
		if target.DueAt.Before(plan.DueAt) {
			plan.Phase, plan.DueAt = target.Phase, target.DueAt
		}
	}
	if plan.DueAt.Before(live.State.BlockedUntil) {
		plan.DueAt = live.State.BlockedUntil
	}
	return plan, nil
}

// sparseRoomCheck is when a stream not shown on Bilibili wants the room
// checked: the first sparse offset after its start that no check has passed
// yet, then each interval while the room is live; none otherwise.
func sparseRoomCheck(start, last time.Time, interval time.Duration, onAir bool) (time.Time, bool) {
	for _, offset := range bilibiliSparseChecks {
		if due := start.Add(offset); last.Before(due) {
			return due, true
		}
	}
	if onAir {
		return last.Add(interval), true
	}
	return time.Time{}, false
}

// eventPlatformState is what attachEventPlatforms projects onto one read
// event before its links are built.
type eventPlatformState struct {
	youtubeURL    string
	youtubeStatus *string
	roomStatus    *string
}

// attachEventPlatforms projects platforms onto read events. An event's
// shown platforms are its announced platforms, those of the automatic
// entries merged into it, and bilibili when the room's broadcast is the
// simulcast of its YouTube broadcast (observed); nothing observed is
// written. Each shown platform gets a link on that platform, or none:
// YouTube the watch link (the event's url, or a merged YouTube event's),
// Bilibili the account's room for a stream once a check learned it (or a
// stored room link) and the upload page of a release, other the entry's
// url. The announcement link stays the entry's url. The entry is live while
// any of its platforms is. The room's broadcast state goes to the
// matched entry: live while the check is younger than two Bilibili
// intervals, ended once a later check saw the room off air. Bilibili has no
// reservation state, so no entry is upcoming from the room. A fresh
// broadcast that matches no entry is returned as LiveNow, and no entry is
// created for it. Callers set LiveStatus from the linked videos first.
func (s *Store) attachEventPlatforms(ctx context.Context, events []DashboardEvent, now time.Time) (*LiveNow, error) {
	if len(events) == 0 {
		return s.unmatchedLiveNow(ctx, now)
	}
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	merged, err := readMergedPlatforms(ctx, s.db, ids)
	if err != nil {
		return nil, dbErr(err)
	}
	states := make([]eventPlatformState, len(events))
	childLinks := []string{}
	for i := range events {
		e := &events[i]
		e.Platforms = NormalizePlatforms(append(append([]string{}, e.announced...), merged.platforms[e.ID]...))
		state := &states[i]
		if e.URL != nil && strings.HasPrefix(streamIdentity(*e.URL), "youtube:") {
			state.youtubeURL, state.youtubeStatus = *e.URL, e.LiveStatus
		} else if link := merged.youtubeURL[e.ID]; link != "" {
			state.youtubeURL = link
			childLinks = append(childLinks, link)
		}
	}
	if len(childLinks) > 0 {
		statuses, statusErr := s.videoLiveStatuses(ctx, childLinks, now)
		if statusErr != nil {
			return nil, statusErr
		}
		for i := range states {
			if states[i].youtubeStatus == nil && states[i].youtubeURL != "" {
				states[i].youtubeStatus = statuses[streamIdentity(states[i].youtubeURL)]
				// The entry is live while any of its platforms is.
				if status := states[i].youtubeStatus; status != nil && (events[i].LiveStatus == nil || *status == "live") {
					events[i].LiveStatus = status
				}
			}
		}
	}
	liveNow, roomLink, err := s.projectRoom(ctx, events, states, now)
	if err != nil {
		return nil, err
	}
	for i := range events {
		buildEventLinks(&events[i], states[i], roomLink)
	}
	return liveNow, nil
}

// projectRoom attaches the room's broadcast to the read events and returns
// the unmatched broadcast and the room's link ("" until a check learned it).
func (s *Store) projectRoom(ctx context.Context, events []DashboardEvent, states []eventPlatformState, now time.Time) (*LiveNow, string, error) {
	// A snapshot without live bookkeeping has never seen the room.
	if exists, err := s.hasTable(ctx, "sync_state"); err != nil || !exists {
		return nil, "", err
	}
	live, err := readBilibiliLive(ctx, s.db, bilisource.OptionsFromEnv().Account)
	if err != nil {
		return nil, "", dbErr(err)
	}
	room := live.State.Room
	if room.RoomID == "" {
		return nil, "", nil
	}
	settings, err := s.LiveProbeSettings(ctx)
	if err != nil {
		return nil, "", err
	}
	link := bilisource.RoomURL(room.RoomID)
	fresh := live.State.CheckedAt.After(now.Add(-settings.BilibiliFreshness()))
	onAir := fresh && room.LiveStatus == bilisource.RoomLive && !live.LastBroadcast.IsZero()
	match, matched, err := roomBroadcastMatch(ctx, s.db, live)
	if err != nil {
		return nil, "", dbErr(err)
	}
	var status *string
	switch {
	case matched && onAir:
		status = strPtr("live")
	case matched && !live.EndedAt.IsZero():
		status = strPtr("ended")
	}
	for i := range events {
		e := &events[i]
		if !matched || e.ID != match.id {
			continue
		}
		if match.observed {
			e.Platforms = NormalizePlatforms(append(e.Platforms, PlatformBilibili))
		}
		states[i].roomStatus = status
		// The room's state is the entry's unless its YouTube broadcast says
		// more: a live room makes it live, an ended room only fills a gap.
		if status != nil && (*status == "live" || e.LiveStatus == nil) {
			e.LiveStatus = status
		}
	}
	if !onAir || matched {
		return nil, link, nil
	}
	return &LiveNow{Platform: PlatformBilibili, URL: link, Title: room.Title, StartedAt: workflowTime(live.LastBroadcast)}, link, nil
}

// unmatchedLiveNow is LiveNow for a read without events.
func (s *Store) unmatchedLiveNow(ctx context.Context, now time.Time) (*LiveNow, error) {
	liveNow, _, err := s.projectRoom(ctx, nil, nil, now)
	return liveNow, err
}

// buildEventLinks writes an event's LiveURL and Links from its shown
// platforms, the entry's own platform first.
func buildEventLinks(e *DashboardEvent, state eventPlatformState, roomLink string) {
	stream := e.EventType != nil && (*e.EventType == "stream" || *e.EventType == "member")
	lead := PlatformUnknown
	if e.Platform != nil {
		lead = *e.Platform
	}
	e.Platforms = orderPlatforms(e.Platforms, lead)
	e.Links = make([]EventLink, 0, len(e.Platforms))
	for _, platform := range e.Platforms {
		// A platform's link is a page on that platform; the announcement
		// stays the entry's own url, never labelled as a platform.
		link := EventLink{Platform: platform}
		switch platform {
		case PlatformYouTube:
			if state.youtubeURL != "" {
				link.URL, link.LiveStatus = strPtr(state.youtubeURL), state.youtubeStatus
			}
		case PlatformBilibili:
			switch {
			case stream && roomLink != "":
				link.URL = strPtr(roomLink)
				e.LiveURL = strPtr(roomLink)
			case stream && e.roomURL != "":
				link.URL = strPtr(e.roomURL)
			case !stream && e.URL != nil && isBilibiliVideoURL(*e.URL):
				link.URL = e.URL
			}
			link.LiveStatus = state.roomStatus
		case PlatformOther:
			link.URL = e.URL
		}
		e.Links = append(e.Links, link)
	}
}

// isBilibiliVideoURL reports a Bilibili upload page, such as the upload a
// release was paired with.
func isBilibiliVideoURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return (host == "www.bilibili.com" || host == "bilibili.com" || host == "m.bilibili.com") && strings.HasPrefix(parsed.Path, "/video/")
}

// videoLiveStatuses reads the trusted broadcast state of the videos the
// links name, by stream identity.
func (s *Store) videoLiveStatuses(ctx context.Context, links []string, now time.Time) (map[string]*string, error) {
	out := map[string]*string{}
	identities := []any{}
	for _, link := range links {
		identities = append(identities, streamIdentity(link))
	}
	if len(identities) == 0 {
		return out, nil
	}
	probe, err := s.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-probe.Freshness())
	rows, err := s.db.QueryContext(ctx, `SELECT url,live_status,live_checked_at FROM videos WHERE live_status IS NOT NULL AND stream_identity(url) IN (?`+strings.Repeat(",?", len(identities)-1)+`)`, identities...)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var link string
		var status, checked sql.NullString
		if err := rows.Scan(&link, &status, &checked); err != nil {
			return nil, dbErr(err)
		}
		if view := liveView(status, checked, cutoff); view != nil {
			out[streamIdentity(link)] = view
		}
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	return out, nil
}
