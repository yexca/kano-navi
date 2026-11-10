package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
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

	mergeReasonTitle     = "rule_a:title"
	mergeReasonOnly      = "rule_a:only_stream"
	mergeReasonYoutube   = "rule_b:youtube_start"
	mergeReasonSale      = "rule_c:sale_period"
	mergeReasonBilibili  = "rule_d:bilibili_start"
	mergeReasonSimulcast = "rule_e:simulcast"
	mergeWindowPastDays  = 30
	mergeWindowNextDays  = 60
	mergeStartTolerance  = 30 * time.Minute
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
	// Tagged counts the surviving events whose label changed to the label of
	// a notice merged into them (see inheritEventTags).
	Tagged int `json:"tagged"`
	// ReleasesPaired counts the Bilibili releases whose link moved to the
	// upload they announced (see pairBilibiliReleases).
	ReleasesPaired int `json:"releasesPaired"`
}

// Changed reports whether the run wrote any merge decision, derived event or
// inherited label.
func (c MergeCounts) Changed() bool {
	return c.Merged+c.Retargeted+c.Released+c.EventsTimed+c.EventsCreated+c.Tagged+c.ReleasesPaired > 0
}

type mergeEvent struct {
	id, source, title, startsOn, eventType, precision, provenance, cancellation string
	mergedInto, mergeSource                                                     string
	// tag is the calendar label of the entry; an X notice may carry one that
	// the YouTube event it merges into does not.
	tag       string
	tagSource string
	start     time.Time
	hasStart  bool
	// A sale's close date, whether its start and close times are stored, and
	// the link that names the shop.
	endsOn                    string
	startTimeSet, endsTimeSet bool
	url                       string
	// platform is where the entry happens, "" when unknown; platforms are
	// its announced platforms, inferred when they came from a source
	// default.
	platform  string
	platforms []string
	inferred  bool
	locked    bool
	// held events hold a merge another owner made, such as an operator's;
	// merging them away would chain that merge behind a hidden event.
	held bool
}

func (e mergeEvent) stream() bool { return e.eventType == "stream" || e.eventType == "member" }

// simulcast entries may be the Bilibili side of a YouTube broadcast: their
// platforms are bilibili by a source default, or name both platforms.
// Bilibili-only wording (a stated [bilibili]) never is.
func (e mergeEvent) simulcast() bool {
	return e.bothPlatforms() || e.inferred && len(e.platforms) == 1 && e.platforms[0] == PlatformBilibili
}

// bothPlatforms entries name YouTube and Bilibili; as streams only Rule E
// moves them, whichever platform they name first.
func (e mergeEvent) bothPlatforms() bool {
	return hasPlatform(e.platforms, PlatformYouTube) && hasPlatform(e.platforms, PlatformBilibili)
}

// statedBilibili entries are Bilibili-only by their wording, such as
// 「B站限定」.
func (e mergeEvent) statedBilibili() bool {
	return !e.inferred && len(e.platforms) == 1 && e.platforms[0] == PlatformBilibili
}
func (e mergeEvent) sale() bool { return e.eventType == "sale" }

// saleDeadline reports a sale period whose opening is not known: its close
// date is its start date and it has no start time.
func (e mergeEvent) saleDeadline() bool {
	return e.endsOn != "" && e.endsOn == e.startsOn && !e.startTimeSet
}

// saleClose is the last date a sale covers: its close date, or its opening
// date when the close is not known.
func (e mergeEvent) saleClose() string {
	if e.endsOn != "" {
		return e.endsOn
	}
	return e.startsOn
}

// saleCovers reports whether period t covers period s, by Japan dates. A
// deadline covers only the same deadline, and is covered by a period that
// closes on that date. A period with a close covers one that opens no
// earlier and closes no later; a sale whose close is not known is covered
// when its opening day falls in the period.
func saleCovers(t, s mergeEvent) bool {
	switch {
	case t.saleDeadline():
		return s.saleDeadline() && s.endsOn == t.endsOn
	case s.saleDeadline():
		return t.endsOn == s.endsOn && t.startsOn <= s.endsOn
	case s.endsOn != "" && t.endsOn == "":
		return false
	}
	return t.startsOn <= s.startsOn && t.saleClose() >= s.saleClose()
}

// saleDetail counts what a sale states: a known opening, a close date, a
// start time and a close time.
func saleDetail(e mergeEvent) int {
	detail := 0
	for _, known := range []bool{!e.saleDeadline(), e.endsOn != "", e.startTimeSet, e.endsTimeSet} {
		if known {
			detail++
		}
	}
	return detail
}

// saleOutranks reports whether s may merge into t: t covers s, and when both
// cover each other (the same period) t states more, then has the smaller ID,
// so the order is strict and decisions never form a cycle.
func saleOutranks(t, s mergeEvent) bool {
	if !saleCovers(t, s) {
		return false
	}
	if !saleCovers(s, t) {
		return true
	}
	if dt, ds := saleDetail(t), saleDetail(s); dt != ds {
		return dt > ds
	}
	return t.id < s.id
}

// shopLink is the identity of the page a sale links to: the lowercased host
// without www, the path without a trailing slash, and the query; "" when the
// entry links an X post (the extraction's fallback) or nothing usable.
func shopLink(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	switch host {
	case "x.com", "twitter.com", "mobile.twitter.com", "mobile.x.com":
		return ""
	}
	link := host + strings.TrimSuffix(parsed.EscapedPath(), "/")
	if parsed.RawQuery != "" {
		link += "?" + parsed.RawQuery
	}
	return link
}

// sameSale reports whether two sale entries describe the same sale: the same
// shop link when both have one, otherwise titles that overlap in what names
// the goods or the event (see saleTitlesOverlap).
func sameSale(a, b mergeEvent) bool {
	la, lb := shopLink(a.url), shopLink(b.url)
	if la != "" && lb != "" {
		return la == lb
	}
	return saleTitlesOverlap(a.title, b.title)
}
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
	// A Bilibili dynamic's entry of a YouTube stream is a notice as well. A
	// stream on both platforms is Rule E's.
	if e.stream() && e.bothPlatforms() {
		return false
	}
	return (e.source == "x" && e.platform != "bilibili" || e.source == "bilibili" && e.platform == "youtube") && e.timed() &&
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
//   - Rule C: an automatic sale merges into the sale of the same shop whose
//     period covers its own (see planSaleMerges).
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
	if counts.ReleasesPaired, err = pairBilibiliReleases(ctx, tx, counts.From, counts.To, timestamp); err != nil {
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
	// targets holds every rule merge the run leaves in place, kept or new.
	targets := map[string]string{}
	for _, event := range events {
		if !event.ruleOwned() {
			continue
		}
		if movable(event) && (event.stream() || event.notice() || event.sale()) {
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
		if target != "" {
			targets[event.id] = target
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
	if counts.Tagged, err = inheritEventTags(ctx, tx, byID, targets); err != nil {
		return MergeCounts{}, ErrSourceWrite
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

// inheritEventTags copies the label of a notice onto the event a rule merge
// hides it behind, since readers only see the surviving entry. It runs for
// every rule merge left in place, kept ones included, so a notice labelled
// after its merge still lends its label. The notice's label is read from the
// post itself, so it replaces an empty label or one the title inference
// guessed, and a content label replaces an inherited form label; an
// operator's or locked row, and any other label from the extraction prompt,
// stay. A form label never replaces a content label. When several labelled
// notices share a target, a content label lends before a form label, then
// the smallest notice ID, and an inherited label is replaced only by content
// over form, so repeated runs settle instead of alternating. The copied
// label stays if the merge is released later: it described the same
// activity. It returns how many targets' labels changed.
func inheritEventTags(ctx context.Context, tx *sql.Tx, byID map[string]mergeEvent, targets map[string]string) (int, error) {
	lenders := map[string]mergeEvent{}
	for id, target := range targets {
		from := byID[id]
		if from.tag == "" {
			continue
		}
		best, ok := lenders[target]
		if !ok || formTag(best.tag) && !formTag(from.tag) || formTag(best.tag) == formTag(from.tag) && from.id < best.id {
			lenders[target] = from
		}
	}
	changed := 0
	for target, from := range lenders {
		to, ok := byID[target]
		if !ok || to.locked || to.provenance == "manual" || !inheritsTag(to, from.tag) {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE events SET tag=?,tag_source=? WHERE id=? AND manual_locked=0 AND provenance<>'manual'
			AND NOT (COALESCE(tag,'') IN `+contentTagSQL+` AND ? IN `+formTagSQL+`)
			AND (COALESCE(tag_source,'') IN ('',?) OR tag_source=? AND tag IN `+formTagSQL+` AND ? IN `+contentTagSQL+`)`,
			from.tag, TagSourceLLM, to.id, from.tag, TagSourceTitle, TagSourceLLM, from.tag)
		if err != nil {
			return 0, err
		}
		if rows, _ := result.RowsAffected(); rows > 0 && to.tag != from.tag {
			changed++
		}
	}
	return changed, nil
}

// inheritsTag reports whether a notice's label tag may replace the label of
// the automatic target to: an empty or title label unless a form would
// replace content, or an inherited form label that tag gives content to.
func inheritsTag(to mergeEvent, tag string) bool {
	if to.tag != "" && !formTag(to.tag) && formTag(tag) {
		return false
	}
	switch to.tagSource {
	case "", TagSourceTitle:
		return true
	case TagSourceLLM:
		return formTag(to.tag) && !formTag(tag)
	}
	return false
}

func loadMergeEvents(ctx context.Context, tx *sql.Tx, from, to string) ([]mergeEvent, error) {
	// A sale period counts in the window by its close date too, so a long
	// period that opened before the window still meets its reminders.
	rows, err := tx.QueryContext(ctx, `SELECT id,source,title,starts_on,COALESCE(starts_at,''),time_precision,COALESCE(event_type,'event'),COALESCE(tag,''),COALESCE(tag_source,''),provenance,manual_locked,COALESCE(cancellation_status,'none'),COALESCE(merged_into,''),COALESCE(merge_source,''),
		EXISTS(SELECT 1 FROM events m WHERE m.merged_into=events.id AND m.deleted_at IS NULL AND COALESCE(m.merge_source,'')<>?),
		COALESCE(ends_on,''),ends_at IS NOT NULL,COALESCE(url,''),platform,platforms,COALESCE(platforms_inferred,0)
		FROM events WHERE deleted_at IS NULL AND (starts_on BETWEEN ? AND ? OR (event_type='sale' AND ends_on BETWEEN ? AND ?)) ORDER BY starts_on,id`, MergeSourceRule, from, to, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []mergeEvent{}
	for rows.Next() {
		var e mergeEvent
		var startsAt, precision string
		var locked, held, endsTime, inferred int
		var platform, platforms sql.NullString
		if err = rows.Scan(&e.id, &e.source, &e.title, &e.startsOn, &startsAt, &precision, &e.eventType, &e.tag, &e.tagSource, &e.provenance, &locked, &e.cancellation, &e.mergedInto, &e.mergeSource, &held, &e.endsOn, &endsTime, &e.url, &platform, &platforms, &inferred); err != nil {
			return nil, err
		}
		var room bool
		e.platforms, room = readPlatforms(platforms, platform, e.url)
		e.platform, e.inferred = platform.String, inferred != 0
		if room {
			e.platform = PrimaryPlatform(e.platforms)
		}
		e.locked, e.held, e.precision = locked != 0, held != 0, precision
		e.startTimeSet, e.endsTimeSet = startsAt != "", endsTime != 0
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
		// Rule D: a timed X notice of a Bilibili stream or release and the
		// Bilibili dynamic's entry of it, the platform's first-party post.
		for _, s := range list {
			if s.source != "x" || !hasPlatform(s.platforms, PlatformBilibili) || !s.timed() || !movable(s) || desired[s.id] != "" ||
				!(s.stream() || s.eventType == "release") {
				continue
			}
			matches := []string{}
			for _, t := range list {
				if t.id == s.id || t.source != "bilibili" || !receivable(t) || !t.timed() || !compatible(s, t) {
					continue
				}
				if gap := s.start.Sub(t.start); gap <= mergeStartTolerance && gap >= -mergeStartTolerance {
					matches = append(matches, t.id)
				}
			}
			switch len(matches) {
			case 0:
			case 1:
				desired[s.id], reasons[s.id] = matches[0], mergeReasonBilibili
			default:
				ambiguous++
			}
		}
		// Rule E: the Bilibili side of a simulcast and the YouTube event of
		// the same broadcast.
		for _, s := range list {
			if s.source == "youtube" || !s.stream() || !s.timed() || !s.simulcast() || !movable(s) || desired[s.id] != "" {
				continue
			}
			matches := []string{}
			for _, t := range list {
				if t.id == s.id || t.source != "youtube" || !receivable(t) || !t.timed() || !t.stream() || !compatible(s, t) {
					continue
				}
				if gap := s.start.Sub(t.start); gap <= mergeStartTolerance && gap >= -mergeStartTolerance {
					matches = append(matches, t.id)
				}
			}
			switch len(matches) {
			case 0:
			case 1:
				desired[s.id], reasons[s.id] = matches[0], mergeReasonSimulcast
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
			// another YouTube event, and a Bilibili dynamic's entry never
			// merges into an X entry.
			targets := slices.DeleteFunc(slices.Clone(timed), func(t mergeEvent) bool {
				return s.source == "youtube" && t.source != "youtube" || s.source == "bilibili" && t.source == "x"
			})
			if len(targets) == 0 {
				continue
			}
			// A Bilibili-only entry never joins a stream that is not on
			// Bilibili.
			if s.statedBilibili() {
				targets = slices.DeleteFunc(targets, func(t mergeEvent) bool { return !hasPlatform(t.platforms, PlatformBilibili) })
				if len(targets) == 0 {
					continue
				}
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
			// Without a title the only timed stream is the target, unless it
			// is a Bilibili stream and the date-only entry is not.
			case len(dateOnly) == 1 && len(timed) == 1 && len(targets) == 1 && compatible(s, timed[0]) &&
				(timed[0].platform != "bilibili" || s.platform == "bilibili"):
				desired[s.id], reasons[s.id] = timed[0].id, mergeReasonOnly
			default:
				ambiguous++
			}
		}
	}
	ambiguous += planSaleMerges(events, desired, reasons)
	sources := make(map[string]string, len(events))
	for _, e := range events {
		sources[e.id] = e.source
	}
	for id, target := range desired {
		if sources[id] == "bilibili" && sources[target] == "x" {
			delete(desired, id)
			delete(reasons, id)
		}
	}
	// No merge chains: an entry whose target is merged away itself goes on
	// to the final target, whichever rules made the two merges.
	for id, target := range desired {
		seen := map[string]bool{id: true}
		for desired[target] != "" && !seen[target] {
			seen[target] = true
			target = desired[target]
		}
		if seen[target] {
			delete(desired, id)
			delete(reasons, id)
			continue
		}
		desired[id] = target
	}
	return desired, reasons, ambiguous
}

// planSaleMerges is Rule C: an automatic sale merges into the sale of the
// same shop (see sameSale) whose period covers its own (see saleOutranks),
// such as a deadline reminder into the announced period. Among several such
// sales the one no other outranks is taken; two that do not cover each other
// leave the entry unmerged. A target that is itself merged away passes the
// entry on to its own target, so no merge points at a merged event. It
// returns the number of ambiguous entries.
func planSaleMerges(events []mergeEvent, desired, reasons map[string]string) int {
	sales := []mergeEvent{}
	for _, e := range events {
		if e.sale() {
			sales = append(sales, e)
		}
	}
	ambiguous := 0
	for _, s := range sales {
		if !movable(s) {
			continue
		}
		candidates := []mergeEvent{}
		for _, t := range sales {
			if t.id != s.id && receivable(t) && sameSale(s, t) && saleOutranks(t, s) {
				candidates = append(candidates, t)
			}
		}
		top := slices.DeleteFunc(slices.Clone(candidates), func(t mergeEvent) bool {
			return slices.ContainsFunc(candidates, func(u mergeEvent) bool { return u.id != t.id && saleOutranks(u, t) })
		})
		switch len(top) {
		case 0:
		case 1:
			desired[s.id], reasons[s.id] = top[0].id, mergeReasonSale
		default:
			ambiguous++
		}
	}
	for _, s := range sales {
		if reasons[s.id] != mergeReasonSale {
			continue
		}
		target, seen := desired[s.id], map[string]bool{s.id: true}
		for reasons[target] == mergeReasonSale && desired[target] != "" && !seen[target] {
			seen[target] = true
			target = desired[target]
		}
		desired[s.id] = target
	}
	return ambiguous
}

var (
	mergeTitleSeparators = regexp.MustCompile(`[\s「」『』【】〔〕()（）［］\[\]〈〉《》<>＜＞・:：,.，。、!?！？~〜～\-—_/／|｜#＃♪☆★◆◇■□●○*＊&＆+＋×]+`)
	// Words that describe the medium rather than the activity.
	mergeGenericWords = []string{"生配信", "配信", "生放送", "放送", "予定", "youtube", "live", "stream"}
	// Words and counts that every sale title uses, such as 受注, 締切 or
	// 残り2日, and Kano's own name, which nearly every sale title carries, so
	// that only what names the goods or the event is compared. Her full name
	// comes before its parts.
	saleGenericWords = []string{"鹿乃まほろ", "かのまほろ", "kanomahoro", "鹿乃", "まほろ", "mahoro", "kano", "本日まで", "今日まで", "明日まで", "まで", "受け付け", "受注", "受付", "予約", "通販", "販売", "発売", "抽選", "申し込み", "申込", "先行", "一般", "期間", "締め切り", "締切", "開始", "終了", "延長", "残り", "再販", "グッズ", "チケット", "pre", "order", "sale", "goods", "merch", "shop", "ticket"}
	saleTitleCounts  = regexp.MustCompile(`[0-9０-９]+[月日時分]?`)
)

func mergeTokens(title string, generic []string) []string {
	out := []string{}
	for _, part := range mergeTitleSeparators.Split(strings.ToLower(title), -1) {
		for _, word := range generic {
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
	return tokensOverlap(mergeTokens(a, mergeGenericWords), mergeTokens(b, mergeGenericWords))
}

// saleTitlesOverlap compares sale titles as titlesOverlap does, without the
// words and counts every sale title uses as well: 「受注 本日まで」 names no
// goods and matches no period, while 「コラボグッズ受付 本日まで」 matches
// 「コラボグッズ受付」 by コラボ.
func saleTitlesOverlap(a, b string) bool {
	generic := append(slices.Clone(mergeGenericWords), saleGenericWords...)
	tokens := func(title string) []string {
		return mergeTokens(saleTitleCounts.ReplaceAllString(strings.ToLower(title), ""), generic)
	}
	return tokensOverlap(tokens(a), tokens(b))
}

func tokensOverlap(ta, tb []string) bool {
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
