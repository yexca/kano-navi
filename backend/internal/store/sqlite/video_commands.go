package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

var (
	ErrVideoNotFound = errors.New("video not found")
	ErrVideoInvalid  = errors.New("invalid_input")
)

// VideoKinds are the kinds an operator may set; they match the labels the
// board translates.
var VideoKinds = []string{"VIDEO", "MUSIC", "LIVE ARCHIVE", "GAMEPLAY", "TALK"}

// maxVideoTitle bounds an operator's corrected title, in characters.
const maxVideoTitle = 300

// VideoOverrides changes an operator's corrections of the original video.
// A Set flag with a nil value clears that correction; an unset field keeps
// it. Synchronization keeps writing the platform fields underneath, and the
// board shows a correction in their place.
type VideoOverrides struct {
	SetTitle, SetKind, SetThumbnail, SetShort bool
	Title, Kind                               *string
	// Short marks the video a Short (true) or not (false) over the feed's
	// format link.
	Short *bool
	// ThumbnailMediaID and ThumbnailSHA256 name one cached file from the
	// video's history (VideoHistory.Thumbnails).
	ThumbnailMediaID, ThumbnailSHA256 *string
}

// SetVideoOverrides validates and stores operator corrections and advances
// the dashboard revision when one changed. It returns the updated row.
func (s *Store) SetVideoOverrides(ctx context.Context, id string, in VideoOverrides, now time.Time) (AdminVideo, error) {
	if !s.Writable() {
		return AdminVideo{}, ErrReadOnly
	}
	if in.SetTitle && in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" || utf8.RuneCountInString(title) > maxVideoTitle {
			return AdminVideo{}, ErrVideoInvalid
		}
		in.Title = &title
	}
	if in.SetKind && in.Kind != nil {
		kind := strings.ToUpper(strings.TrimSpace(*in.Kind))
		valid := false
		for _, allowed := range VideoKinds {
			valid = valid || kind == allowed
		}
		if !valid {
			return AdminVideo{}, ErrVideoInvalid
		}
		in.Kind = &kind
	}
	if in.SetThumbnail && (in.ThumbnailMediaID == nil) != (in.ThumbnailSHA256 == nil) {
		return AdminVideo{}, ErrVideoInvalid
	}
	if in.SetThumbnail && in.ThumbnailMediaID != nil {
		choices, err := s.videoThumbnails(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return AdminVideo{}, ErrVideoNotFound
		}
		if err != nil {
			return AdminVideo{}, ErrUnavailable
		}
		known := false
		for _, c := range choices {
			known = known || c.MediaID == *in.ThumbnailMediaID && c.SHA256 == *in.ThumbnailSHA256
		}
		if !known {
			return AdminVideo{}, ErrVideoInvalid
		}
	}
	stamp := workflowTime(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	defer tx.Rollback()
	var title, kind, media, hash sql.NullString
	var short sql.NullBool
	err = tx.QueryRowContext(ctx, `SELECT manual_title,manual_kind,manual_thumbnail_media_id,manual_thumbnail_sha256,manual_is_short FROM videos WHERE id=?`, id).Scan(&title, &kind, &media, &hash, &short)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminVideo{}, ErrVideoNotFound
	}
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	before, shortBefore := [4]sql.NullString{title, kind, media, hash}, short
	set := func(target *sql.NullString, value *string) {
		*target = sql.NullString{}
		if value != nil {
			*target = sql.NullString{String: *value, Valid: true}
		}
	}
	if in.SetTitle {
		set(&title, in.Title)
	}
	if in.SetKind {
		set(&kind, in.Kind)
	}
	if in.SetThumbnail {
		set(&media, in.ThumbnailMediaID)
		set(&hash, in.ThumbnailSHA256)
	}
	if in.SetShort {
		short = sql.NullBool{}
		if in.Short != nil {
			short = sql.NullBool{Bool: *in.Short, Valid: true}
		}
	}
	if before != [4]sql.NullString{title, kind, media, hash} || shortBefore != short {
		if _, err = tx.ExecContext(ctx, `UPDATE videos SET manual_title=?,manual_kind=?,manual_thumbnail_media_id=?,manual_thumbnail_sha256=?,manual_is_short=?,manual_updated_at=? WHERE id=?`, title, kind, media, hash, short, stamp, id); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
		// A corrected title of a merged upload is its video's translation.
		if _, err = refreshMergeTranslations(ctx, tx, id, stamp); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
		if err = bumpRevision(ctx, tx, stamp); err != nil {
			return AdminVideo{}, ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return AdminVideo{}, ErrUnavailable
	}
	return s.AdminVideoByID(ctx, id)
}

// refreshRequestTTL is how long an operator's refresh request waits for a
// successful check before the YouTube step stops trying it first.
const refreshRequestTTL = 24 * time.Hour

// RequestVideoRefresh asks the next YouTube step to read this video's watch
// page before any other detail work. A successful check clears the request.
func (s *Store) RequestVideoRefresh(ctx context.Context, id string, now time.Time) (AdminVideo, error) {
	if !s.Writable() {
		return AdminVideo{}, ErrReadOnly
	}
	var source string
	err := s.db.QueryRowContext(ctx, `SELECT source FROM videos WHERE id=?`, id).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminVideo{}, ErrVideoNotFound
	}
	if err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	if source != "youtube" {
		return AdminVideo{}, ErrVideoInvalid
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE videos SET refresh_requested_at=? WHERE id=?`, workflowTime(now), id); err != nil {
		return AdminVideo{}, ErrUnavailable
	}
	return s.AdminVideoByID(ctx, id)
}

// maxFollowUps bounds the follow-up checks one YouTube step queues.
const maxFollowUps = 10

// youtubeRefreshWork reads the operator's open refresh requests and the
// videos whose next follow-up check is due (ytsource.FollowUpDue), and adds
// their rows to videos so a check keeps their stored publication time.
func (s *Store) youtubeRefreshWork(ctx context.Context, now time.Time, videos map[string]ytsource.Video) (refresh, followUps []string, err error) {
	requested := workflowTime(now.Add(-refreshRequestTTL))
	// A broadcast's end can be hours after its start, so the start bound is a
	// day wider than the follow-up window.
	window := workflowTime(now.Add(-ytsource.FollowUpWindow - 24*time.Hour))
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,v.title,COALESCE(v.published_at,''),COALESCE(v.scheduled_at,''),v.url,COALESCE(v.thumbnail_url,''),COALESCE(v.kind,''),v.is_upcoming,
 COALESCE(v.live_checked_at,''),julianday(v.refresh_requested_at)>=julianday(?1),
 COALESCE((SELECT MAX(e.ends_at) FROM events e WHERE e.deleted_at IS NULL AND (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id))),'')
 FROM videos v WHERE v.source='youtube' AND (julianday(v.refresh_requested_at)>=julianday(?1)
  OR (COALESCE(v.is_upcoming,0)=0 AND julianday(COALESCE(v.scheduled_at,v.published_at))>=julianday(?2)))
 ORDER BY v.refresh_requested_at IS NULL,julianday(v.refresh_requested_at),julianday(COALESCE(v.scheduled_at,v.published_at)) DESC,v.id`, requested, window)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var v ytsource.Video
		var checked, ended string
		var wanted sql.NullBool
		if rows.Scan(&v.ID, &v.Title, &v.PublishedAt, &v.ScheduledAt, &v.URL, &v.ThumbnailURL, &v.Kind, &v.IsUpcoming, &checked, &wanted, &ended) != nil {
			return nil, nil, ErrUnavailable
		}
		anchor := firstInstant(ended, v.ScheduledAt, v.PublishedAt)
		last, _ := time.Parse(time.RFC3339Nano, checked)
		switch {
		case wanted.Valid && wanted.Bool:
			refresh = append(refresh, v.ID)
		case !v.IsUpcoming && len(followUps) < maxFollowUps && ytsource.FollowUpDue(anchor, last, now):
			followUps = append(followUps, v.ID)
		default:
			continue
		}
		if _, known := videos[v.ID]; !known {
			videos[v.ID] = v
		}
	}
	if rows.Err() != nil {
		return nil, nil, ErrUnavailable
	}
	return refresh, followUps, nil
}

// firstInstant parses the first valid timestamp, or returns the zero time.
func firstInstant(values ...string) time.Time {
	for _, value := range values {
		if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return t
		}
	}
	return time.Time{}
}
