package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Event merging hides a duplicate event behind the event that describes the
// same activity more precisely. The decision lives in its own columns
// (merged_into, merge_source, merge_reason, merged_at), which no source upsert
// writes, so re-extracting a post or refreshing a reservation keeps it.
//
// merge_source says who owns the decision: "rule" rows belong to the
// deterministic reconciler and are recomputed on every run; "manual" rows are
// operator decisions the reconciler never changes. A manual row without a
// target is an undone merge that keeps the event separate. Another owner,
// such as a later model adjudication, is left untouched in the same way.
const (
	MergeSourceRule   = "rule"
	MergeSourceManual = "manual"

	mergeReasonTitle    = "rule_a:title"
	mergeReasonOnly     = "rule_a:only_stream"
	mergeReasonYoutube  = "rule_b:youtube_start"
	mergeWindowPastDays = 30
	mergeWindowNextDays = 60
	mergeStartTolerance = 30 * time.Minute
)

var (
	ErrEventNotMerged     = errors.New("event_not_merged")
	ErrEventMergeInvalid  = errors.New("invalid_input")
	ErrEventMergeConflict = errors.New("event_merge_conflict")
)

const mergeReasonManual = "manual"

// MergeCounts is the reconcile step's recorded result.
type MergeCounts struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Considered int    `json:"considered"`
	Merged     int    `json:"merged"`
	Retargeted int    `json:"retargeted"`
	Released   int    `json:"released"`
	Kept       int    `json:"kept"`
	Ambiguous  int    `json:"ambiguous"`
	// EventsTimed and EventsCreated count the YouTube events the step derived
	// from their videos before merging (see settleVideoEvents).
	EventsTimed   int `json:"eventsTimed"`
	EventsCreated int `json:"eventsCreated"`
}

// Changed reports whether the run wrote any merge decision or derived event.
func (c MergeCounts) Changed() bool {
	return c.Merged+c.Retargeted+c.Released+c.EventsTimed+c.EventsCreated > 0
}

type mergeEvent struct {
	id, source, title, startsOn, eventType, precision, provenance, cancellation string
	mergedInto, mergeSource                                                     string
	start                                                                       time.Time
	hasStart, locked                                                            bool
	// held events hold a merge another owner made, such as an operator's;
	// merging them away would chain that merge behind a hidden event.
	held bool
}

func (e mergeEvent) stream() bool { return e.eventType == "stream" || e.eventType == "member" }
func (e mergeEvent) overlay() bool {
	return e.cancellation == "llm_suspected" || e.cancellation == "manual_confirmed"
}

// protected events are never merged away: manual or locked ones, and those
// holding another owner's merge.
func (e mergeEvent) protected() bool { return e.locked || e.provenance == "manual" || e.held }

// ruleOwned events have no merge decision or one the reconciler made.
func (e mergeEvent) ruleOwned() bool { return e.mergeSource == "" || e.mergeSource == MergeSourceRule }

// timed events carry a usable start; anything else is date-only.
func (e mergeEvent) timed() bool { return e.hasStart }

// notice events are the timed X entries Rule B compares with YouTube: a
// stream, a release, or an entry of unnamed type whose exact time is all
// it says, such as 「このあと20時からはじまります！」.
func (e mergeEvent) notice() bool {
	return e.source == "x" && e.timed() &&
		(e.stream() || e.eventType == "release" || e.eventType == "event" && e.precision == "exact")
}

// movable events may be merged away. An operator's decision, including an
// undone merge, keeps the event where it is.
func movable(e mergeEvent) bool { return !e.overlay() && !e.protected() && e.ruleOwned() }

// receivable events may receive a merge. An event hidden by a decision the
// reconciler does not own cannot; a manual or locked event can.
func receivable(e mergeEvent) bool { return !e.overlay() && (e.ruleOwned() || e.mergedInto == "") }

// compatible lets a generic stream merge into a members-only stream, which a
// YouTube title or an operator can classify more specifically.
func compatible(source, target mergeEvent) bool {
	return source.eventType == target.eventType || source.eventType == "stream" && target.eventType == "member"
}

// noticeCompatible extends compatible for Rule B: a release notice matches
// only a YouTube release, and an entry of unnamed type only a stream.
func noticeCompatible(source, target mergeEvent) bool {
	if source.eventType == "event" {
		return target.stream()
	}
	return compatible(source, target)
}

// ReconcileEventMerges first derives the YouTube events of stored videos
// (settleVideoEvents), then applies the deterministic merge rules to active
// events in a bounded window of Japan dates around now:
//
//   - Rule B: an automatic X stream or release with an exact or approximate
//     time, or an X entry of unnamed type with an exact time, merges into the
//     only compatible YouTube event on the same date that starts within 30
//     minutes. A release matches only a release, an unnamed entry only a
//     stream.
//   - Rule A: a date-only automatic stream merges into the timed stream on the
//     same date whose normalized title overlaps its own, or, when the day has
//     exactly one date-only and one timed stream, into that stream. A YouTube
//     event never merges into an X entry.
//
// Manual or locked events, and events holding another owner's merge, are
// never merged away, so no merge points at a merged event. Events with a cancellation
// overlay, and merges into an overlaid target, keep their current state.
// Several plausible targets leave the event unmerged. Rule-owned decisions
// are recomputed from the current rows, so the step is idempotent and a merge
// whose basis disappeared is released.
func (s *Store) ReconcileEventMerges(ctx context.Context, now time.Time) (MergeCounts, error) {
	if s == nil || !s.workflowWritable {
		return MergeCounts{}, ErrSourceWrite
	}
	japan := now.In(time.FixedZone("JST", 9*3600))
	counts := MergeCounts{
		From: japan.AddDate(0, 0, -mergeWindowPastDays).Format("2006-01-02"),
		To:   japan.AddDate(0, 0, mergeWindowNextDays).Format("2006-01-02"),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MergeCounts{}, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return MergeCounts{}, err
	}
	timestamp := workflowTime(now)
	if counts.EventsTimed, counts.EventsCreated, err = settleVideoEvents(ctx, tx, counts.From, counts.To, timestamp); err != nil {
		return MergeCounts{}, ErrSourceWrite
	}
	events, err := loadMergeEvents(ctx, tx, counts.From, counts.To)
	if err != nil {
		return MergeCounts{}, ErrSourceWrite
	}
	byID := make(map[string]mergeEvent, len(events))
	for _, event := range events {
		byID[event.id] = event
	}
	desired, reasons, ambiguous := planMerges(events)
	counts.Ambiguous = ambiguous
	for _, event := range events {
		if !event.ruleOwned() {
			continue
		}
		if movable(event) && (event.stream() || event.notice()) {
			counts.Considered++
		}
		current := ""
		if event.mergeSource == MergeSourceRule {
			current = event.mergedInto
		}
		target := desired[event.id]
		// A cancellation overlay freezes the decision on either side of it,
		// but a protected event is never left merged away.
		if !event.protected() && (event.overlay() || current != "" && byID[current].overlay()) {
			target = current
		}
		switch target {
		case current:
			if current != "" {
				counts.Kept++
			}
			continue
		case "":
			_, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=NULL,merge_source=NULL,merge_reason=NULL,merged_at=NULL WHERE id=? AND merge_source=?`, event.id, MergeSourceRule)
			counts.Released++
		default:
			_, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=?,merge_source=?,merge_reason=?,merged_at=? WHERE id=? AND (merge_source IS NULL OR merge_source=?)`,
				target, MergeSourceRule, reasons[event.id], timestamp, event.id, MergeSourceRule)
			if current == "" {
				counts.Merged++
			} else {
				counts.Retargeted++
			}
		}
		if err != nil {
			return MergeCounts{}, ErrSourceWrite
		}
	}
	if counts.Changed() {
		if err = bumpRevision(ctx, tx, timestamp); err != nil {
			return MergeCounts{}, ErrSourceWrite
		}
	}
	if err = tx.Commit(); err != nil {
		return MergeCounts{}, ErrSourceWrite
	}
	return counts, nil
}

func loadMergeEvents(ctx context.Context, tx *sql.Tx, from, to string) ([]mergeEvent, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,source,title,starts_on,COALESCE(starts_at,''),time_precision,COALESCE(event_type,'event'),provenance,manual_locked,COALESCE(cancellation_status,'none'),COALESCE(merged_into,''),COALESCE(merge_source,''),
		EXISTS(SELECT 1 FROM events m WHERE m.merged_into=events.id AND m.deleted_at IS NULL AND COALESCE(m.merge_source,'')<>?)
		FROM events WHERE deleted_at IS NULL AND starts_on BETWEEN ? AND ? ORDER BY starts_on,id`, MergeSourceRule, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []mergeEvent{}
	for rows.Next() {
		var e mergeEvent
		var startsAt, precision string
		var locked, held int
		if err = rows.Scan(&e.id, &e.source, &e.title, &e.startsOn, &startsAt, &precision, &e.eventType, &e.provenance, &locked, &e.cancellation, &e.mergedInto, &e.mergeSource, &held); err != nil {
			return nil, err
		}
		e.locked, e.held, e.precision = locked != 0, held != 0, precision
		if start, ok := parseMergeInstant(startsAt); ok && precision != "unknown" {
			e.start, e.hasStart = start, true
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func parseMergeInstant(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// planMerges computes the rule-owned target of every event from the rows
// alone, ignoring earlier rule decisions. It returns the targets, their
// reasons and the number of events left unmerged because of ambiguity.
func planMerges(events []mergeEvent) (map[string]string, map[string]string, int) {
	desired, reasons := map[string]string{}, map[string]string{}
	ambiguous := 0
	byDay := map[string][]mergeEvent{}
	for _, e := range events {
		byDay[e.startsOn] = append(byDay[e.startsOn], e)
	}
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	slices.Sort(days)
	for _, day := range days {
		list := byDay[day]
		// Rule B: a timed X notice and the YouTube event of the same stream or
		// release.
		for _, s := range list {
			if !s.notice() || !movable(s) {
				continue
			}
			matches := []string{}
			for _, t := range list {
				if t.id == s.id || !receivable(t) || t.source != "youtube" || !t.timed() || !noticeCompatible(s, t) {
					continue
				}
				if gap := s.start.Sub(t.start); gap <= mergeStartTolerance && gap >= -mergeStartTolerance {
					matches = append(matches, t.id)
				}
			}
			switch len(matches) {
			case 0:
			case 1:
				desired[s.id], reasons[s.id] = matches[0], mergeReasonYoutube
			default:
				ambiguous++
			}
		}
		// Rule A: a date-only stream entry, usually from a weekly schedule image.
		dateOnly, timed := []mergeEvent{}, []mergeEvent{}
		for _, e := range list {
			switch {
			case !e.stream():
			case !e.timed() && movable(e):
				dateOnly = append(dateOnly, e)
			case e.timed() && receivable(e) && desired[e.id] == "":
				timed = append(timed, e)
			}
		}
		for _, s := range dateOnly {
			// A YouTube event outranks every X entry, so it only merges into
			// another YouTube event.
			targets := slices.DeleteFunc(slices.Clone(timed), func(t mergeEvent) bool { return s.source == "youtube" && t.source != "youtube" })
			if len(targets) == 0 {
				continue
			}
			matches := []string{}
			for _, t := range targets {
				if compatible(s, t) && titlesOverlap(s.title, t.title) {
					matches = append(matches, t.id)
				}
			}
			switch {
			case len(matches) == 1:
				desired[s.id], reasons[s.id] = matches[0], mergeReasonTitle
			case len(matches) > 1:
				ambiguous++
			case len(dateOnly) == 1 && len(timed) == 1 && len(targets) == 1 && compatible(s, timed[0]):
				desired[s.id], reasons[s.id] = timed[0].id, mergeReasonOnly
			default:
				ambiguous++
			}
		}
	}
	return desired, reasons, ambiguous
}

var (
	mergeTitleSeparators = regexp.MustCompile(`[\s「」『』【】〔〕()（）［］\[\]〈〉《》<>＜＞・:：,.，。、!?！？~〜～\-—_/／|｜#＃♪☆★◆◇■□●○*＊&＆+＋×]+`)
	// Words that describe the medium rather than the activity.
	mergeGenericWords = []string{"生配信", "配信", "生放送", "放送", "予定", "youtube", "live", "stream"}
)

func mergeTokens(title string) []string {
	out := []string{}
	for _, part := range mergeTitleSeparators.Split(strings.ToLower(title), -1) {
		for _, word := range mergeGenericWords {
			part = strings.ReplaceAll(part, word, "")
		}
		if utf8.RuneCountInString(part) >= 2 {
			out = append(out, part)
		}
	}
	return out
}

// titlesOverlap compares titles without punctuation, brackets or medium
// words: one contains the other, or they share a token of at least two
// characters, such as 「歌枠リレー」 in 「秋を奏でる歌枠リレー」.
func titlesOverlap(a, b string) bool {
	ta, tb := mergeTokens(a), mergeTokens(b)
	ja, jb := strings.Join(ta, ""), strings.Join(tb, "")
	if ja == "" || jb == "" {
		return false
	}
	if strings.Contains(ja, jb) || strings.Contains(jb, ja) {
		return true
	}
	for _, token := range ta {
		if strings.Contains(jb, token) {
			return true
		}
	}
	for _, token := range tb {
		if strings.Contains(ja, token) {
			return true
		}
	}
	return false
}

// MergeEvent hides an event behind another, given by its ID, as an operator
// decision the reconciler never changes. Any visible event can be the target,
// on any date; a merged or deleted one cannot, and neither can the event
// itself. Rule merges into the merged event are released, since a duplicate
// is no target; an operator's or another owner's merge into it is a conflict,
// so no merge ever points at a merged event.
func (s *Store) MergeEvent(ctx context.Context, id, into string) (AdminEvent, error) {
	into = strings.TrimSpace(into)
	tx, err := s.eventTx(ctx)
	if err != nil {
		return AdminEvent{}, err
	}
	defer tx.Rollback()
	var current, owner string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(merged_into,''),COALESCE(merge_source,'') FROM events WHERE id=? AND deleted_at IS NULL`, id).Scan(&current, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminEvent{}, sql.ErrNoRows
	}
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if into == "" || into == id {
		return AdminEvent{}, ErrEventMergeInvalid
	}
	var targetInto string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(merged_into,'') FROM events WHERE id=? AND deleted_at IS NULL`, into).Scan(&targetInto)
	if errors.Is(err, sql.ErrNoRows) || err == nil && targetInto != "" {
		return AdminEvent{}, ErrEventMergeInvalid
	}
	if err != nil {
		return AdminEvent{}, fmt.Errorf("%w: target", ErrUnavailable)
	}
	var others int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE merged_into=? AND deleted_at IS NULL AND COALESCE(merge_source,'')<>?`, id, MergeSourceRule).Scan(&others); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: merges", ErrUnavailable)
	}
	if others > 0 {
		return AdminEvent{}, ErrEventMergeConflict
	}
	if current == into && owner == MergeSourceManual {
		_ = tx.Rollback()
		return s.event(ctx, id)
	}
	now := ISOTime(time.Now())
	if _, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=NULL,merge_source=NULL,merge_reason=NULL,merged_at=NULL WHERE merged_into=? AND merge_source=?`, id, MergeSourceRule); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: merges", ErrUnavailable)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=?,merge_source=?,merge_reason=?,merged_at=? WHERE id=?`,
		into, MergeSourceManual, mergeReasonManual, now, id); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return s.event(ctx, id)
}

// UnmergeEvent undoes a merge as an operator decision. The event keeps
// merge_source = manual without a target, so the reconciler never merges it
// again; the previous target stays in merge_reason for the audit trail.
func (s *Store) UnmergeEvent(ctx context.Context, id string) (AdminEvent, error) {
	tx, err := s.eventTx(ctx)
	if err != nil {
		return AdminEvent{}, err
	}
	defer tx.Rollback()
	var target sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT merged_into FROM events WHERE id=? AND deleted_at IS NULL`, id).Scan(&target)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AdminEvent{}, sql.ErrNoRows
		}
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if !target.Valid || target.String == "" {
		return AdminEvent{}, ErrEventNotMerged
	}
	now := ISOTime(time.Now())
	if _, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=NULL,merge_source=?,merge_reason=?,merged_at=? WHERE id=?`,
		MergeSourceManual, "unmerged_from:"+target.String, now, id); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: event", ErrUnavailable)
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: revision", ErrUnavailable)
	}
	if err = tx.Commit(); err != nil {
		return AdminEvent{}, fmt.Errorf("%w: commit", ErrUnavailable)
	}
	return s.event(ctx, id)
}
