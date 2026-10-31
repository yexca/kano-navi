package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Video merging attaches a Bilibili upload to the YouTube video of the same
// work, so readers list one video with both links. The decision lives in its
// own columns (merged_into, merge_source, merge_reason, merged_at), which no
// source writer names, so rereading either platform keeps it.
//
// merge_source has the owners of event merges: "rule" rows belong to
// ReconcileVideoMerges and are recomputed on every run; "manual" rows are
// operator decisions it never changes, and a manual row without a target is
// an undone merge that keeps the upload separate. A YouTube video receives at
// most one upload, whose title becomes the video's Bilibili platform
// translation into Simplified Chinese.
const (
	videoMergeReasonRule   = "rule:time_duration_title"
	videoMergeReasonManual = "manual"
	videoMergeWindow       = 6 * time.Hour
	// videoMergeTolerance is the largest length difference, in seconds.
	videoMergeTolerance = 3

	maxVideoMergeSuggestions = 50
	maxVideoMergeCandidates  = 5
)

var (
	ErrVideoNotMerged     = errors.New("video_not_merged")
	ErrVideoMergeInvalid  = errors.New("invalid_input")
	ErrVideoMergeConflict = errors.New("video_merge_conflict")
)

// VideoMergeCounts is the video part of the reconcile step's result.
type VideoMergeCounts struct {
	Considered int `json:"considered"`
	Merged     int `json:"merged"`
	Released   int `json:"released"`
	Kept       int `json:"kept"`
	Ambiguous  int `json:"ambiguous"`
}

// Changed reports whether the run wrote any merge decision.
func (c VideoMergeCounts) Changed() bool { return c.Merged+c.Released > 0 }

type mergeVideo struct {
	id, source, title       string
	mergedInto, mergeSource string
	at                      time.Time
	hasAt                   bool
	duration                int64
}

// ruleOwned uploads have no merge decision or one the reconciler made.
func (v mergeVideo) ruleOwned() bool { return v.mergeSource == "" || v.mergeSource == MergeSourceRule }

type videoQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// loadMergeVideos reads every Bilibili upload and YouTube video with its
// effective title and publication anchor: a YouTube reservation's start, or
// the publication time.
func loadMergeVideos(ctx context.Context, q videoQuerier) (bilibili, youtube []mergeVideo, err error) {
	rows, err := q.QueryContext(ctx, `SELECT id,source,COALESCE(manual_title,title),COALESCE(scheduled_at,published_at,''),COALESCE(duration_seconds,0),COALESCE(merged_into,''),COALESCE(merge_source,'')
		FROM videos WHERE source IN ('youtube','bilibili') ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v mergeVideo
		var at string
		if err = rows.Scan(&v.id, &v.source, &v.title, &at, &v.duration, &v.mergedInto, &v.mergeSource); err != nil {
			return nil, nil, err
		}
		v.at, v.hasAt = parseMergeInstant(at)
		if v.source == "bilibili" {
			bilibili = append(bilibili, v)
		} else {
			youtube = append(youtube, v)
		}
	}
	return bilibili, youtube, rows.Err()
}

// videoMatch compares one YouTube video with a Bilibili upload. delta is the
// length difference in seconds, or -1 when a length is unknown.
type videoMatch struct {
	youtube mergeVideo
	gap     time.Duration
	title   bool
	delta   int64
}

// complete matches satisfy every rule: published within six hours, lengths
// within three seconds and overlapping song titles.
func (m videoMatch) complete() bool { return m.title && m.delta >= 0 && m.delta <= videoMergeTolerance }

// videoMatches lists the YouTube videos published within six hours of the
// upload whose title or length matches it, complete matches first. Known
// lengths further apart rule a video out.
func videoMatches(b mergeVideo, youtube []mergeVideo) []videoMatch {
	out := []videoMatch{}
	if !b.hasAt {
		return out
	}
	for _, y := range youtube {
		if !y.hasAt {
			continue
		}
		gap := b.at.Sub(y.at).Abs()
		if gap > videoMergeWindow {
			continue
		}
		m := videoMatch{youtube: y, gap: gap, title: videoTitlesMatch(b, y), delta: -1}
		if b.duration > 0 && y.duration > 0 {
			if m.delta = max(b.duration-y.duration, y.duration-b.duration); m.delta > videoMergeTolerance {
				continue
			}
		}
		if m.title || m.delta >= 0 {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b videoMatch) int {
		if a.complete() != b.complete() {
			if a.complete() {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(a.gap, b.gap), strings.Compare(a.youtube.id, b.youtube.id))
	})
	return out
}

// mergeTargets are the YouTube videos the rules may merge into: those without
// an upload an operator, or another owner, merged into them.
func mergeTargets(bilibili, youtube []mergeVideo) []mergeVideo {
	taken := map[string]bool{}
	for _, b := range bilibili {
		if !b.ruleOwned() && b.mergedInto != "" {
			taken[b.mergedInto] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(youtube), func(y mergeVideo) bool { return taken[y.id] })
}

// planVideoMerges computes the rule-owned target of every upload from the
// rows alone, ignoring earlier rule decisions. An upload with several
// complete matches, or whose only match another upload also claims, is left
// unmerged and reported as ambiguous.
func planVideoMerges(bilibili, youtube []mergeVideo) (map[string]string, map[string]bool) {
	targets := mergeTargets(bilibili, youtube)
	desired, ambiguous := map[string]string{}, map[string]bool{}
	claims := map[string][]string{}
	for _, b := range bilibili {
		if !b.ruleOwned() {
			continue
		}
		complete := []string{}
		for _, m := range videoMatches(b, targets) {
			if m.complete() {
				complete = append(complete, m.youtube.id)
			}
		}
		switch len(complete) {
		case 0:
		case 1:
			claims[complete[0]] = append(claims[complete[0]], b.id)
		default:
			ambiguous[b.id] = true
		}
	}
	for target, ids := range claims {
		if len(ids) == 1 {
			desired[ids[0]] = target
			continue
		}
		for _, id := range ids {
			ambiguous[id] = true
		}
	}
	return desired, ambiguous
}

// ReconcileVideoMerges applies the merge rule to every Bilibili upload: it
// merges into the only YouTube video published within six hours of it whose
// length is within three seconds and whose song title overlaps its own.
// Unknown lengths, as on videos read before lengths were stored, leave the
// upload to the operator. Rule-owned decisions are recomputed from the
// current rows, so the step is idempotent and a merge whose basis
// disappeared is released; operator decisions are never changed. Every
// merge keeps its target's Bilibili translation current.
func (s *Store) ReconcileVideoMerges(ctx context.Context, now time.Time) (VideoMergeCounts, error) {
	if s == nil || !s.workflowWritable {
		return VideoMergeCounts{}, ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return VideoMergeCounts{}, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return VideoMergeCounts{}, err
	}
	bilibili, youtube, err := loadMergeVideos(ctx, tx)
	if err != nil {
		return VideoMergeCounts{}, ErrSourceWrite
	}
	byID := make(map[string]mergeVideo, len(youtube))
	for _, y := range youtube {
		byID[y.id] = y
	}
	desired, ambiguous := planVideoMerges(bilibili, youtube)
	counts := VideoMergeCounts{Ambiguous: len(ambiguous)}
	stamp := workflowTime(now)
	translated := false
	final := make(map[string]string, len(bilibili))
	for _, b := range bilibili {
		if !b.ruleOwned() {
			final[b.id] = b.mergedInto
			continue
		}
		counts.Considered++
		current, target := b.mergedInto, desired[b.id]
		final[b.id] = target
		if target == current {
			if current != "" {
				counts.Kept++
			}
			continue
		}
		if current != "" {
			removed, err := removeMergeTranslation(ctx, tx, current)
			if err != nil {
				return VideoMergeCounts{}, ErrSourceWrite
			}
			translated = translated || removed
		}
		if target == "" {
			_, err = tx.ExecContext(ctx, `UPDATE videos SET merged_into=NULL,merge_source=NULL,merge_reason=NULL,merged_at=NULL WHERE id=? AND (merge_source IS NULL OR merge_source=?)`, b.id, MergeSourceRule)
			counts.Released++
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE videos SET merged_into=?,merge_source=?,merge_reason=?,merged_at=? WHERE id=? AND (merge_source IS NULL OR merge_source=?)`,
				target, MergeSourceRule, videoMergeReasonRule, stamp, b.id, MergeSourceRule)
			counts.Merged++
		}
		if err != nil {
			return VideoMergeCounts{}, ErrSourceWrite
		}
	}
	// Translations follow the merges as they stand after every release, so a
	// released upload cannot remove the title its successor wrote. Operator
	// merges are refreshed as well.
	written := map[string]bool{}
	for _, b := range bilibili {
		y, ok := byID[final[b.id]]
		if !ok || written[y.id] {
			continue
		}
		written[y.id] = true
		changed, err := writeMergeTranslation(ctx, tx, b.title, y.id, y.title, stamp)
		if err != nil {
			return VideoMergeCounts{}, ErrSourceWrite
		}
		translated = translated || changed
	}
	if counts.Changed() || translated {
		if err = bumpRevision(ctx, tx, stamp); err != nil {
			return VideoMergeCounts{}, ErrSourceWrite
		}
	}
	if err = tx.Commit(); err != nil {
		return VideoMergeCounts{}, ErrSourceWrite
	}
	return counts, nil
}

// writeMergeTranslation stores a merged upload's title as its YouTube video's
// Bilibili translation and reports whether readers see a change.
func writeMergeTranslation(ctx context.Context, tx *sql.Tx, title, youtubeID, youtubeTitle, now string) (bool, error) {
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > MaxTranslationRunes {
		return removeMergeTranslation(ctx, tx, youtubeID)
	}
	return UpsertPlatformTranslationTx(ctx, tx, TranslationWrite{
		OwnerType: TranslationOwnerVideo, OwnerID: youtubeID, Field: "title", Locale: "zh-CN",
		Platform: "bilibili", Text: title, SourceText: youtubeTitle,
	}, now)
}

func removeMergeTranslation(ctx context.Context, tx *sql.Tx, youtubeID string) (bool, error) {
	return DeletePlatformTranslationTx(ctx, tx, TranslationOwnerVideo, youtubeID, "title", "zh-CN", "bilibili")
}

// refreshMergeTranslations rewrites the Bilibili translation of every merge
// the video takes part in after its title changed, inside the writer's
// transaction. It reports whether readers see a change.
func refreshMergeTranslations(ctx context.Context, tx *sql.Tx, videoID, now string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT COALESCE(b.manual_title,b.title),y.id,COALESCE(y.manual_title,y.title) FROM videos b JOIN videos y ON y.id=b.merged_into AND y.source='youtube'
		WHERE b.source='bilibili' AND (b.id=?1 OR y.id=?1)`, videoID)
	if err != nil {
		return false, err
	}
	type pair struct{ title, youtubeID, youtubeTitle string }
	pairs := []pair{}
	for rows.Next() {
		var p pair
		if err = rows.Scan(&p.title, &p.youtubeID, &p.youtubeTitle); err != nil {
			rows.Close()
			return false, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	changed := false
	for _, p := range pairs {
		written, err := writeMergeTranslation(ctx, tx, p.title, p.youtubeID, p.youtubeTitle, now)
		if err != nil {
			return false, err
		}
		changed = changed || written
	}
	return changed, nil
}

var (
	// bilibiliSongName is the work a Bilibili title names, as in 「翻唱《奏》」.
	bilibiliSongName = regexp.MustCompile(`《([^》]+)》`)
	videoTitleTags   = regexp.MustCompile(`【[^】]*】`)
	leadingTitleTags = regexp.MustCompile(`^(?:\s*【[^】]*】)+`)
	// Where a YouTube title stops naming the work, as in 「奏（スキマスイッチ）
	// - 鹿乃まほろ cover.」 or 「高嶺の花子さん 歌ってみた【鹿乃まほろ】」.
	youtubeTitleEnds = []string{"(", " - ", "歌ってみた", "【"}
	// Title parts that name the performer or the format rather than the work.
	videoGenericWords = []string{"鹿乃まほろ", "鹿乃", "まほろ", "kano", "mahoro", "翻唱", "歌ってみた", "cover", "covered", "by", "mv"}
)

// foldWidth turns full-width ASCII forms and the ideographic space into their
// ASCII equivalents, so （ and ( compare alike.
func foldWidth(title string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '　':
			return ' '
		case r >= '！' && r <= '～':
			return r - 0xfee0
		}
		return r
	}, title)
}

// videoSongTitle is the part of a title that names the work: the 《…》 name of
// a Bilibili upload, or a YouTube title up to （, " - ", 歌ってみた or 【.
func videoSongTitle(source, title string) string {
	title = foldWidth(title)
	if source == "bilibili" {
		if match := bilibiliSongName.FindStringSubmatch(title); match != nil {
			return match[1]
		}
		return videoTitleTags.ReplaceAllString(title, " ")
	}
	title = leadingTitleTags.ReplaceAllString(title, "")
	end := len(title)
	for _, separator := range youtubeTitleEnds {
		if i := strings.Index(title, separator); i >= 0 && i < end {
			end = i
		}
	}
	return title[:end]
}

// videoTitleParts splits a song title at punctuation and brackets, dropping
// the parts that only name the performer or the format.
func videoTitleParts(title string) []string {
	out := []string{}
	for _, part := range mergeTitleSeparators.Split(strings.ToLower(title), -1) {
		if part != "" && !slices.Contains(videoGenericWords, part) {
			out = append(out, part)
		}
	}
	return out
}

// videoTitlesMatch compares the song titles of an upload and a video: the
// same name, however short (《奏》 and 「奏（スキマスイッチ）」), or a shared
// token of at least two characters.
func videoTitlesMatch(a, b mergeVideo) bool {
	pa, pb := videoTitleParts(videoSongTitle(a.source, a.title)), videoTitleParts(videoSongTitle(b.source, b.title))
	ka, kb := strings.Join(pa, ""), strings.Join(pb, "")
	if ka == "" || kb == "" {
		return false
	}
	if ka == kb {
		return true
	}
	for _, part := range pa {
		if utf8.RuneCountInString(part) >= 2 && strings.Contains(kb, part) {
			return true
		}
	}
	for _, part := range pb {
		if utf8.RuneCountInString(part) >= 2 && strings.Contains(ka, part) {
			return true
		}
	}
	return false
}

// attachVideoAlternates lists the public link of every upload merged into
// each video, oldest first; a video without one gets an empty list.
func (s *Store) attachVideoAlternates(ctx context.Context, videos ...*Video) error {
	index := map[string][]*Video{}
	ids := []string{}
	for _, video := range videos {
		if video == nil {
			continue
		}
		video.Alternates = []VideoAlternate{}
		if index[video.ID] == nil {
			ids = append(ids, video.ID)
		}
		index[video.ID] = append(index[video.ID], video)
	}
	for _, chunk := range translationChunks(ids) {
		rows, err := s.db.QueryContext(ctx, `SELECT merged_into,source,url FROM videos WHERE source='bilibili' AND merged_into IN (`+placeholders(len(chunk))+`)
			ORDER BY julianday(published_at),id`, idArgs(chunk)...)
		if err != nil {
			return dbErr(err)
		}
		for rows.Next() {
			var target string
			var alternate VideoAlternate
			if err = rows.Scan(&target, &alternate.Source, &alternate.URL); err != nil {
				rows.Close()
				return dbErr(err)
			}
			for _, video := range index[target] {
				video.Alternates = append(video.Alternates, alternate)
			}
		}
		if err = rows.Close(); err != nil {
			return dbErr(err)
		}
	}
	return nil
}

// youtubeMergeTarget reads a YouTube video ID or watch link.
func youtubeMergeTarget(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if youtubeVideoID.MatchString(input) {
		return input, true
	}
	if identity := streamIdentity(input); strings.HasPrefix(identity, "youtube:") {
		return strings.TrimPrefix(identity, "youtube:"), true
	}
	return "", false
}

// MergeVideo attaches a Bilibili upload to a YouTube video, given by its ID
// or watch link, as an operator decision the reconciler never changes. A rule
// merge of another upload into the same video gives way to it; another
// operator merge into that video is a conflict.
func (s *Store) MergeVideo(ctx context.Context, id, into string, now time.Time) (AdminVideo, error) {
	if !s.Writable() {
		return AdminVideo{}, ErrReadOnly
	}
	target, ok := youtubeMergeTarget(into)
	if !ok {
		return AdminVideo{}, ErrVideoMergeInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	defer tx.Rollback()
	var source, title, current, owner string
	err = tx.QueryRowContext(ctx, `SELECT source,COALESCE(manual_title,title),COALESCE(merged_into,''),COALESCE(merge_source,'') FROM videos WHERE id=?`, id).Scan(&source, &title, &current, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminVideo{}, ErrVideoNotFound
	}
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	var targetSource, targetTitle string
	err = tx.QueryRowContext(ctx, `SELECT source,COALESCE(manual_title,title) FROM videos WHERE id=?`, target).Scan(&targetSource, &targetTitle)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (source != "bilibili" || targetSource != "youtube") {
		return AdminVideo{}, ErrVideoMergeInvalid
	}
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(merge_source,'') FROM videos WHERE merged_into=? AND id<>?`, target, id)
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	others := []string{}
	for rows.Next() {
		var other, otherOwner string
		if err = rows.Scan(&other, &otherOwner); err != nil {
			rows.Close()
			return AdminVideo{}, ErrUnavailable
		}
		if otherOwner != MergeSourceRule && otherOwner != "" {
			rows.Close()
			return AdminVideo{}, ErrVideoMergeConflict
		}
		others = append(others, other)
	}
	rows.Close()
	if rows.Err() != nil {
		return AdminVideo{}, ErrUnavailable
	}
	stamp := workflowTime(now)
	changed := current != target || owner != MergeSourceManual
	for _, other := range others {
		if _, err = tx.ExecContext(ctx, `UPDATE videos SET merged_into=NULL,merge_source=NULL,merge_reason=NULL,merged_at=NULL WHERE id=?`, other); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
	}
	if current != "" && current != target {
		if _, err = removeMergeTranslation(ctx, tx, current); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
	}
	if changed {
		if _, err = tx.ExecContext(ctx, `UPDATE videos SET merged_into=?,merge_source=?,merge_reason=?,merged_at=? WHERE id=?`,
			target, MergeSourceManual, videoMergeReasonManual, stamp, id); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
	}
	translated, err := writeMergeTranslation(ctx, tx, title, target, targetTitle, stamp)
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	if changed || translated {
		if err = bumpRevision(ctx, tx, stamp); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
	}
	if err = tx.Commit(); err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	return s.AdminVideoByID(ctx, id)
}

// UnmergeVideo undoes a merge as an operator decision. The upload keeps
// merge_source = manual without a target, so the reconciler never merges it
// again; the previous target stays in merge_reason for the audit trail.
func (s *Store) UnmergeVideo(ctx context.Context, id string, now time.Time) (AdminVideo, error) {
	if !s.Writable() {
		return AdminVideo{}, ErrReadOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	defer tx.Rollback()
	var target sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT merged_into FROM videos WHERE id=?`, id).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminVideo{}, ErrVideoNotFound
	}
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	if !target.Valid || target.String == "" {
		return AdminVideo{}, ErrVideoNotMerged
	}
	stamp := workflowTime(now)
	if _, err = tx.ExecContext(ctx, `UPDATE videos SET merged_into=NULL,merge_source=?,merge_reason=?,merged_at=? WHERE id=?`,
		MergeSourceManual, "unmerged_from:"+target.String, stamp, id); err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	if _, err = removeMergeTranslation(ctx, tx, target.String); err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	if err = bumpRevision(ctx, tx, stamp); err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	if err = tx.Commit(); err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	return s.AdminVideoByID(ctx, id)
}

// VideoMergeSuggestion is an unmerged Bilibili upload with the YouTube videos
// that may be its copy.
type VideoMergeSuggestion struct {
	Bilibili AdminVideo `json:"bilibili"`
	// Status is rule (the next reconcile step merges it into the first
	// candidate), ambiguous (several complete matches, or a match another
	// upload claims too) or unverified (no complete match: a length is
	// unknown or the titles differ). A single requested upload may also be
	// merged, separate (an operator undid its merge) or none.
	Status     string                `json:"status"`
	Candidates []VideoMergeCandidate `json:"candidates"`
}

// VideoMergeCandidate is one YouTube video published within six hours of
// the upload. DurationDelta is nil when a length is unknown.
type VideoMergeCandidate struct {
	YouTube       AdminVideo `json:"youtube"`
	Complete      bool       `json:"complete"`
	TitleMatch    bool       `json:"titleMatch"`
	DurationDelta *int64     `json:"durationDelta"`
	GapMinutes    int        `json:"gapMinutes"`
}

// VideoMergeSuggestions lists the unmerged uploads the rules would merge or
// leave to the operator, newest first, without writing; uploads an operator
// kept separate are left out. only names one upload, which is listed
// whatever its state.
func (s *Store) VideoMergeSuggestions(ctx context.Context, only string) ([]VideoMergeSuggestion, error) {
	bilibili, youtube, err := loadMergeVideos(ctx, s.db)
	if err != nil {
		return nil, dbErr(err)
	}
	desired, ambiguous := planVideoMerges(bilibili, youtube)
	targets := mergeTargets(bilibili, youtube)
	slices.SortStableFunc(bilibili, func(a, b mergeVideo) int { return b.at.Compare(a.at) })
	type pending struct {
		id, status string
		matches    []videoMatch
	}
	list := []pending{}
	for _, b := range bilibili {
		if only != "" && b.id != only {
			continue
		}
		p := pending{id: b.id, status: "none"}
		switch {
		case b.mergedInto != "":
			p.status = "merged"
		case !b.ruleOwned():
			p.status = "separate"
		case desired[b.id] != "":
			p.status = "rule"
		case ambiguous[b.id]:
			p.status = "ambiguous"
		}
		if p.status != "merged" {
			p.matches = videoMatches(b, targets)
		}
		switch p.status {
		case "rule", "ambiguous":
			p.matches = slices.DeleteFunc(p.matches, func(m videoMatch) bool { return !m.complete() })
		case "none":
			if len(p.matches) > 0 {
				p.status = "unverified"
			}
		}
		if only == "" && (p.status == "merged" || p.status == "separate" || p.status == "none") {
			continue
		}
		p.matches = p.matches[:min(len(p.matches), maxVideoMergeCandidates)]
		list = append(list, p)
		if len(list) == maxVideoMergeSuggestions {
			break
		}
	}
	ids := []string{}
	for _, p := range list {
		ids = append(ids, p.id)
		for _, m := range p.matches {
			ids = append(ids, m.youtube.id)
		}
	}
	videos, err := s.adminVideosByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []VideoMergeSuggestion{}
	for _, p := range list {
		item := VideoMergeSuggestion{Bilibili: videos[p.id], Status: p.status, Candidates: []VideoMergeCandidate{}}
		for _, m := range p.matches {
			candidate := VideoMergeCandidate{YouTube: videos[m.youtube.id], Complete: m.complete(), TitleMatch: m.title, GapMinutes: int(m.gap.Round(time.Minute) / time.Minute)}
			if m.delta >= 0 {
				delta := m.delta
				candidate.DurationDelta = &delta
			}
			item.Candidates = append(item.Candidates, candidate)
		}
		out = append(out, item)
	}
	return out, nil
}
