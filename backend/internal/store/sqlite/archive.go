package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

// ErrInvalidAccount rejects a post archive filter that is not an X handle.
var ErrInvalidAccount = errors.New("invalid account")

// accountPattern accepts a stored account handle. X handles are letters,
// digits and underscores; a Bilibili post's handle is the uploader's display
// name, which may be any script and contain spaces.
var accountPattern = regexp.MustCompile(`^[\p{L}\p{N}_ .·-]{1,64}$`)

// ErrInvalidVideoCategory rejects a video archive filter outside
// VideoCategories.
var ErrInvalidVideoCategory = errors.New("invalid video category")

// VideoCategories are the video archive's filter groups: the content tags
// and other. A video is not stored with one; videoCategorySQL derives it on
// each read (video_tags.go).
var VideoCategories = append(append([]string{}, VideoTags...), "other")

func validVideoCategory(value string) bool {
	for _, category := range VideoCategories {
		if category == value {
			return true
		}
	}
	return false
}

// maxArchivePageSize bounds one archive page; every item resolves its media.
const maxArchivePageSize = 50

// ArchivePage is a bounded public read of stored posts or videos whose Japan
// date falls inside an inclusive range, newest first. Items use the same store
// types as the dashboard, so both routes share one public field boundary.
type ArchivePage[T any] struct {
	Items      []T
	Page       int
	PageSize   int
	Total      int64
	TotalPages int
	HasNext    bool
	Revision   int64
	// Previous and Next are the nearest Japan dates outside the range that
	// hold an item, so an empty month can offer a jump.
	Previous *string
	Next     *string
	// AccountCounts counts the range's posts per lowercase handle, ignoring
	// the account filter, so the filter can show every account's total.
	AccountCounts map[string]int64
	// CategoryCounts counts the range's videos per category the same way.
	CategoryCounts map[string]int64
}

// archiveBounds turns an inclusive Japan-date range into the UTC instants
// [start, end) that julianday comparisons use.
func archiveBounds(from, to string) (string, string, error) {
	if err := validateCalendarRange(from, to); err != nil {
		return "", "", err
	}
	japan := time.FixedZone("JST", 9*60*60)
	start, _ := time.ParseInLocation("2006-01-02", from, japan)
	end, _ := time.ParseInLocation("2006-01-02", to, japan)
	return start.UTC().Format(time.RFC3339), end.AddDate(0, 0, 1).UTC().Format(time.RFC3339), nil
}

func archivePaging(total int64, page, pageSize int) (int, int, int) {
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > maxArchivePageSize {
		pageSize = maxArchivePageSize
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	return page, pageSize, totalPages
}

// adjacentDates returns the Japan dates of the nearest items before start and
// at or after end. instant is the SQL expression of an item's time.
func (s *Store) adjacentDates(ctx context.Context, table, instant, filter string, args []any, start, end string) (*string, *string, error) {
	var previous, next sql.NullString
	query := `SELECT date(MAX(` + instant + `) + 0.375) FROM ` + table + ` WHERE ` + instant + ` < julianday(?)` + filter
	if err := s.db.QueryRowContext(ctx, query, append([]any{start}, args...)...).Scan(&previous); err != nil {
		return nil, nil, dbErr(err)
	}
	query = `SELECT date(MIN(` + instant + `) + 0.375) FROM ` + table + ` WHERE ` + instant + ` >= julianday(?)` + filter
	if err := s.db.QueryRowContext(ctx, query, append([]any{end}, args...)...).Scan(&next); err != nil {
		return nil, nil, dbErr(err)
	}
	var before, after *string
	if previous.Valid {
		before = &previous.String
	}
	if next.Valid {
		after = &next.String
	}
	return before, after, nil
}

// GetPostArchive reads stored posts published in a Japan-date range, newest
// first, optionally for one account.
func (s *Store) GetPostArchive(ctx context.Context, from, to, account string, page, pageSize int) (*ArchivePage[Post], error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	ctx = withReadMemo(ctx)
	start, end, err := archiveBounds(from, to)
	if err != nil {
		return nil, err
	}
	if account != "" && !accountPattern.MatchString(account) {
		return nil, ErrInvalidAccount
	}
	const instant = `julianday(published_at)`
	inRange := ` WHERE hidden_at IS NULL AND ` + instant + ` >= julianday(?) AND ` + instant + ` < julianday(?)`
	filter, filterArgs := "", []any{}
	if account != "" {
		filter, filterArgs = ` AND lower(account_handle)=lower(?)`, []any{account}
	}
	args := append([]any{start, end}, filterArgs...)

	counts := map[string]int64{}
	countRows, err := s.db.QueryContext(ctx, `SELECT lower(COALESCE(account_handle,'')), COUNT(*) FROM posts`+inRange+` GROUP BY 1`, start, end)
	if err != nil {
		return nil, dbErr(err)
	}
	for countRows.Next() {
		var handle string
		var count int64
		if err := countRows.Scan(&handle, &count); err != nil {
			_ = countRows.Close()
			return nil, dbErr(err)
		}
		if handle != "" {
			counts[handle] = count
		}
	}
	if err := countRows.Err(); err != nil {
		_ = countRows.Close()
		return nil, dbErr(err)
	}
	_ = countRows.Close()

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts`+inRange+filter, args...).Scan(&total); err != nil {
		return nil, dbErr(err)
	}
	page, pageSize, totalPages := archivePaging(total, page, pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,CASE WHEN quote_hidden_at IS NULL THEN quote_json END FROM posts`+inRange+filter+` ORDER BY `+instant+` DESC,id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, dbErr(err)
	}
	postRows := make([]postRow, 0, pageSize)
	for rows.Next() {
		var r postRow
		var handle, label, mu, ma sql.NullString
		if err := rows.Scan(&r.id, &r.source, &handle, &r.typ, &label, &r.text, &r.publishedAt, &r.url, &r.likes, &r.reposts, &r.replies, &mu, &ma, &r.quoteJSON); err != nil {
			_ = rows.Close()
			return nil, dbErr(err)
		}
		r.accountHandle, r.hasAccount = handle.String, handle.Valid
		r.label, r.hasLabel = label.String, label.Valid
		r.mediaURL, r.hasMediaURL = mu.String, mu.Valid
		r.mediaAlt, r.hasMediaAlt = ma.String, ma.Valid
		postRows = append(postRows, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err)
	}
	// Media resolution runs its own queries; release this cursor first.
	_ = rows.Close()
	if err = s.prefetchPostRows(ctx, postRows); err != nil {
		return nil, err
	}
	items := make([]Post, 0, len(postRows))
	for _, row := range postRows {
		post, mapErr := s.mapPost(ctx, row)
		if mapErr != nil {
			return nil, mapErr
		}
		items = append(items, post)
	}
	refs := make([]*Post, len(items))
	for i := range items {
		refs[i] = &items[i]
	}
	if err = s.attachPostTranslations(ctx, refs...); err != nil {
		return nil, err
	}

	previous, next, err := s.adjacentDates(ctx, "posts", instant, filter, filterArgs, start, end)
	if err != nil {
		return nil, err
	}
	revision, err := s.GetDashboardRevision(ctx)
	if err != nil {
		return nil, err
	}
	return &ArchivePage[Post]{
		Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
		HasNext: page < totalPages, Revision: revision, Previous: previous, Next: next, AccountCounts: counts,
	}, nil
}

// ArchiveMonth is one Japan month that holds stored posts or videos: its
// total and its records per key, a lowercase handle for posts and a category
// for videos.
type ArchiveMonth struct {
	Month  string
	Total  int64
	Counts map[string]int64
}

// GetPostMonths lists every Japan month holding a shown post, oldest first,
// so the archive can offer a jump to any of them. Accounts are not filtered:
// the counts let the board narrow each month to the chosen account.
func (s *Store) GetPostMonths(ctx context.Context) ([]ArchiveMonth, int64, error) {
	return s.archiveMonths(ctx, `SELECT strftime('%Y-%m', julianday(published_at) + 0.375) AS month,
	     lower(COALESCE(account_handle,'')), COUNT(*)
	   FROM posts WHERE hidden_at IS NULL AND julianday(published_at) IS NOT NULL
	   GROUP BY 1, 2 ORDER BY 1, 2`)
}

// GetVideoMonths lists every Japan month holding a public video, oldest
// first, with its videos per category, so the video timeline can offer a jump
// to any month and narrow the counts to the chosen category.
func (s *Store) GetVideoMonths(ctx context.Context) ([]ArchiveMonth, int64, error) {
	return s.archiveMonths(ctx, `SELECT strftime('%Y-%m', `+videoInstant+` + 0.375) AS month,
	     `+videoCategorySQL+`, COUNT(*)
	   FROM videos WHERE `+publicVideoScope+` AND `+videoInstant+` IS NOT NULL
	   GROUP BY 1, 2 ORDER BY 1, 2`)
}

// archiveMonths folds (month, key, count) rows into months; an empty key adds
// to the total only.
func (s *Store) archiveMonths(ctx context.Context, query string) ([]ArchiveMonth, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, 0, dbErr(err)
	}
	months := []ArchiveMonth{}
	for rows.Next() {
		var month, key string
		var count int64
		if err := rows.Scan(&month, &key, &count); err != nil {
			_ = rows.Close()
			return nil, 0, dbErr(err)
		}
		if n := len(months); n == 0 || months[n-1].Month != month {
			months = append(months, ArchiveMonth{Month: month, Counts: map[string]int64{}})
		}
		current := &months[len(months)-1]
		current.Total += count
		if key != "" {
			current.Counts[key] = count
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, dbErr(err)
	}
	_ = rows.Close()
	revision, err := s.GetDashboardRevision(ctx)
	if err != nil {
		return nil, 0, err
	}
	return months, revision, nil
}

// videoInstant is the SQL time of a video: its scheduled start, else its
// publication.
const videoInstant = `julianday(COALESCE(scheduled_at,published_at))`

// GetVideoArchive reads stored videos whose scheduled or published time falls
// in a Japan-date range, newest first, optionally of one category.
func (s *Store) GetVideoArchive(ctx context.Context, from, to, category string, page, pageSize int, now time.Time) (*ArchivePage[Video], error) {
	start, end, err := archiveBounds(from, to)
	if err != nil {
		return nil, err
	}
	return s.videoArchive(ctx, start, end, category, page, pageSize, now)
}

// GetVideoTimeline reads stored videos whose scheduled or published time
// falls on or before an inclusive Japan date, or every dated video when until
// is empty, newest first, optionally of one category. Its pages continue
// through every month, so the board can scroll back without a date range.
func (s *Store) GetVideoTimeline(ctx context.Context, until, category string, page, pageSize int, now time.Time) (*ArchivePage[Video], error) {
	end := ""
	if until != "" {
		if !validCalendarDate(until) {
			return nil, ErrInvalidCalendarRange
		}
		day, _ := time.ParseInLocation("2006-01-02", until, time.FixedZone("JST", 9*60*60))
		end = day.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	}
	return s.videoArchive(ctx, "", end, category, page, pageSize, now)
}

// videoArchive reads one page of videos in the UTC instants [start, end); an
// empty bound leaves that side open.
func (s *Store) videoArchive(ctx context.Context, start, end, category string, page, pageSize int, now time.Time) (*ArchivePage[Video], error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	ctx = withReadMemo(ctx)
	if category != "" && !validVideoCategory(category) {
		return nil, ErrInvalidVideoCategory
	}
	const instant = videoInstant
	conditions, bounds := []string{instant + ` IS NOT NULL`}, []any{}
	if start != "" {
		conditions, bounds = append(conditions, instant+` >= julianday(?)`), append(bounds, start)
	}
	if end != "" {
		conditions, bounds = append(conditions, instant+` < julianday(?)`), append(bounds, end)
	}
	inRange := ` WHERE ` + strings.Join(append(conditions, publicVideoScope), " AND ")
	filter, filterArgs := "", []any{}
	if category != "" {
		filter, filterArgs = ` AND `+videoCategorySQL+`=?`, []any{category}
	}
	args := append(append([]any{}, bounds...), filterArgs...)

	counts := map[string]int64{}
	countRows, err := s.db.QueryContext(ctx, `SELECT `+videoCategorySQL+`, COUNT(*) FROM videos`+inRange+` GROUP BY 1`, bounds...)
	if err != nil {
		return nil, dbErr(err)
	}
	for countRows.Next() {
		var name string
		var count int64
		if err := countRows.Scan(&name, &count); err != nil {
			_ = countRows.Close()
			return nil, dbErr(err)
		}
		counts[name] = count
	}
	if err := countRows.Err(); err != nil {
		_ = countRows.Close()
		return nil, dbErr(err)
	}
	_ = countRows.Close()

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos`+inRange+filter, args...).Scan(&total); err != nil {
		return nil, dbErr(err)
	}
	page, pageSize, totalPages := archivePaging(total, page, pageSize)
	probe, err := s.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	liveCutoff := now.Add(-probe.Freshness())
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,COALESCE(manual_title,title),published_at,scheduled_at,url,thumbnail_url,COALESCE(manual_kind,kind),is_upcoming,live_status,live_checked_at,duration_seconds,COALESCE(manual_is_short,is_short),`+videoTagSQL("videos")+`,COALESCE(manual_availability,availability) FROM videos`+inRange+filter+` ORDER BY `+instant+` DESC,id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, dbErr(err)
	}
	items := make([]Video, 0, pageSize)
	for rows.Next() {
		var v Video
		var pub, sch, thumb, kind, liveStatus, liveChecked, tag, availability sql.NullString
		var upcoming int64
		var duration sql.NullInt64
		var short sql.NullBool
		if err := rows.Scan(&v.ID, &v.Source, &v.Title, &pub, &sch, &v.URL, &thumb, &kind, &upcoming, &liveStatus, &liveChecked, &duration, &short, &tag, &availability); err != nil {
			_ = rows.Close()
			return nil, dbErr(err)
		}
		v.DurationSeconds = nullInt(duration)
		v.IsShort = nullBool(short)
		v.Tag = nullString(tag)
		v.Availability = nullString(availability)
		if pub.Valid {
			v.PublishedAt = &pub.String
		}
		if sch.Valid {
			v.ScheduledAt = &sch.String
		}
		if thumb.Valid {
			v.ThumbnailURL = &thumb.String
		}
		if kind.Valid {
			v.Kind = &kind.String
		}
		v.IsUpcoming = upcoming != 0
		v.LiveStatus = liveView(liveStatus, liveChecked, liveCutoff)
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err)
	}
	_ = rows.Close()
	if err = s.resolveVideoThumbnails(ctx, items); err != nil {
		return nil, err
	}
	cancelled, err := s.cancelledVideos(ctx, items)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].IsUpcoming = videoUpcoming(items[i], now)
		items[i].IsCancelled = cancelled[items[i].ID]
	}

	if err = s.pinVideoThumbnails(ctx, items); err != nil {
		return nil, err
	}
	refs := make([]*Video, len(items))
	for i := range items {
		refs[i] = &items[i]
	}
	if err = s.attachVideoTranslations(ctx, refs...); err != nil {
		return nil, err
	}
	if err = s.attachVideoAlternates(ctx, refs...); err != nil {
		return nil, err
	}
	// An open bound has no neighbor: julianday('') is NULL and matches nothing.
	previous, next, err := s.adjacentDates(ctx, "videos", instant, " AND "+publicVideoScope+filter, filterArgs, start, end)
	if err != nil {
		return nil, err
	}
	revision, err := s.GetDashboardRevision(ctx)
	if err != nil {
		return nil, err
	}
	return &ArchivePage[Video]{
		Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
		HasNext: page < totalPages, Revision: revision, Previous: previous, Next: next, CategoryCounts: counts,
	}, nil
}
