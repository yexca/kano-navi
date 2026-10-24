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

var ErrEventNotMerged = errors.New("event_not_merged")

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
}

// Changed reports whether the run wrote any merge decision.
func (c MergeCounts) Changed() bool { return c.Merged+c.Retargeted+c.Released > 0 }

type mergeEvent struct {
	id, source, title, startsOn, eventType, provenance, cancellation string
	mergedInto, mergeSource                                          string
	start                                                            time.Time
	hasStart, exact, locked                                          bool
}

func (e mergeEvent) stream() bool { return e.eventType == "stream" || e.eventType == "member" }
func (e mergeEvent) overlay() bool {
	return e.cancellation == "llm_suspected" || e.cancellation == "manual_confirmed"
}
func (e mergeEvent) protected() bool { return e.locked || e.provenance == "manual" }

// ruleOwned events have no merge decision or one the reconciler made.
func (e mergeEvent) ruleOwned() bool { return e.mergeSource == "" || e.mergeSource == MergeSourceRule }

// timed events carry a usable start; anything else is date-only.
func (e mergeEvent) timed() bool { return e.hasStart }

// mergeSource events may be merged away. An operator's decision, including
// an undone merge, keeps the event where it is.
func mergeSource(e mergeEvent) bool {
	return e.stream() && !e.overlay() && !e.protected() && e.ruleOwned()
}

// mergeTarget events may receive a merge. An event hidden by a decision the
// reconciler does not own cannot; a manual or locked event can.
func mergeTarget(e mergeEvent) bool {
	return e.stream() && !e.overlay() && (e.ruleOwned() || e.mergedInto == "")
}

// compatible lets a generic stream merge into a members-only stream, which a
// YouTube title or an operator can classify more specifically.
func compatible(source, target mergeEvent) bool {
	return source.eventType == target.eventType || source.eventType == "stream" && target.eventType == "member"
}

// ReconcileEventMerges applies the deterministic merge rules to active
// stream/member events in a bounded window of Japan dates around now:
//
//   - Rule B: an automatic X event with an exact time merges into the only
//     compatible YouTube event on the same date that starts within 30 minutes.
//   - Rule A: a date-only automatic event merges into the timed stream on the
//     same date whose normalized title overlaps its own, or, when the day has
//     exactly one date-only and one timed stream, into that stream.
//
// Manual or locked events are never merged away. Events with a cancellation
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
	timestamp := workflowTime(now)
	for _, event := range events {
		if !event.ruleOwned() {
			continue
		}
		if mergeSource(event) {
			counts.Considered++
		}
		current := ""
		if event.mergeSource == MergeSourceRule {
			current = event.mergedInto
		}
		target := desired[event.id]
		// A cancellation overlay freezes the decision on either side of it,
		// but a manual or locked event is never left merged away.
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
	rows, err := tx.QueryContext(ctx, `SELECT id,source,title,starts_on,COALESCE(starts_at,''),time_precision,COALESCE(event_type,'event'),provenance,manual_locked,COALESCE(cancellation_status,'none'),COALESCE(merged_into,''),COALESCE(merge_source,'')
		FROM events WHERE deleted_at IS NULL AND starts_on BETWEEN ? AND ? ORDER BY starts_on,id`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []mergeEvent{}
	for rows.Next() {
		var e mergeEvent
		var startsAt, precision string
		var locked int
		if err = rows.Scan(&e.id, &e.source, &e.title, &e.startsOn, &startsAt, &precision, &e.eventType, &e.provenance, &locked, &e.cancellation, &e.mergedInto, &e.mergeSource); err != nil {
			return nil, err
		}
		e.locked = locked != 0
		if start, ok := parseMergeInstant(startsAt); ok && precision != "unknown" {
			e.start, e.hasStart, e.exact = start, true, precision == "exact"
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
	source, target := mergeSource, mergeTarget
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
		// Rule B: an exact X notice and the YouTube reservation of the stream.
		for _, s := range list {
			if !source(s) || s.source != "x" || !s.exact {
				continue
			}
			matches := []string{}
			for _, t := range list {
				if t.id == s.id || !target(t) || t.source != "youtube" || !t.timed() || !compatible(s, t) {
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
		// Rule A: a date-only entry, usually from a weekly schedule image.
		dateOnly, timed := []mergeEvent{}, []mergeEvent{}
		for _, e := range list {
			switch {
			case !e.timed() && source(e):
				dateOnly = append(dateOnly, e)
			case e.timed() && target(e) && desired[e.id] == "":
				timed = append(timed, e)
			}
		}
		for _, s := range dateOnly {
			matches := []string{}
			for _, t := range timed {
				if compatible(s, t) && titlesOverlap(s.title, t.title) {
					matches = append(matches, t.id)
				}
			}
			switch {
			case len(matches) == 1:
				desired[s.id], reasons[s.id] = matches[0], mergeReasonTitle
			case len(matches) > 1:
				ambiguous++
			case len(dateOnly) == 1 && len(timed) == 1 && compatible(s, timed[0]):
				desired[s.id], reasons[s.id] = timed[0].id, mergeReasonOnly
			case len(timed) > 0:
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
