package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

// ErrInvalidAccount rejects a post archive filter that is not an X handle.
var ErrInvalidAccount = errors.New("invalid account")

var accountPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)

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
	start, end, err := archiveBounds(from, to)
	if err != nil {
		return nil, err
	}
	if account != "" && !accountPattern.MatchString(account) {
		return nil, ErrInvalidAccount
	}
	const instant = `julianday(published_at)`
	inRange := ` WHERE ` + instant + ` >= julianday(?) AND ` + instant + ` < julianday(?)`
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt FROM posts`+inRange+filter+` ORDER BY `+instant+` DESC,id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, dbErr(err)
	}
	postRows := make([]postRow, 0, pageSize)
	for rows.Next() {
		var r postRow
		var handle, label, mu, ma sql.NullString
		if err := rows.Scan(&r.id, &r.source, &handle, &r.typ, &label, &r.text, &r.publishedAt, &r.url, &r.likes, &r.reposts, &r.replies, &mu, &ma); err != nil {
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

// GetVideoArchive reads stored videos whose scheduled or published time falls
// in a Japan-date range, newest first.
func (s *Store) GetVideoArchive(ctx context.Context, from, to string, page, pageSize int, now time.Time) (*ArchivePage[Video], error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	start, end, err := archiveBounds(from, to)
	if err != nil {
		return nil, err
	}
	const instant = `julianday(COALESCE(scheduled_at,published_at))`
	inRange := ` WHERE ` + instant + ` >= julianday(?) AND ` + instant + ` < julianday(?) AND ` + publicVideoScope
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos`+inRange, start, end).Scan(&total); err != nil {
		return nil, dbErr(err)
	}
	page, pageSize, totalPages := archivePaging(total, page, pageSize)
	probe, err := s.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	liveCutoff := now.Add(-probe.Freshness())
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,COALESCE(manual_title,title),published_at,scheduled_at,url,thumbnail_url,COALESCE(manual_kind,kind),is_upcoming,live_status,live_checked_at,duration_seconds FROM videos`+inRange+` ORDER BY `+instant+` DESC,id ASC LIMIT ? OFFSET ?`, start, end, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, dbErr(err)
	}
	items := make([]Video, 0, pageSize)
	for rows.Next() {
		var v Video
		var pub, sch, thumb, kind, liveStatus, liveChecked sql.NullString
		var upcoming int64
		var duration sql.NullInt64
		if err := rows.Scan(&v.ID, &v.Source, &v.Title, &pub, &sch, &v.URL, &thumb, &kind, &upcoming, &liveStatus, &liveChecked, &duration); err != nil {
			_ = rows.Close()
			return nil, dbErr(err)
		}
		v.DurationSeconds = nullInt(duration)
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
	for i := range items {
		items[i].IsUpcoming = videoUpcoming(items[i], now)
		ref, refErr := s.resolveMedia(ctx, items[i].ThumbnailURL)
		if refErr != nil {
			return nil, refErr
		}
		items[i].ThumbnailID, items[i].ThumbnailStatus, items[i].ThumbnailSourceURL = ref.ID, ref.Status, ref.SourceURL
		items[i].ThumbnailURL = ref.PublicURL
		if items[i].IsCancelled, err = s.videoCancelled(ctx, items[i]); err != nil {
			return nil, err
		}
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
	previous, next, err := s.adjacentDates(ctx, "videos", instant, " AND "+publicVideoScope, nil, start, end)
	if err != nil {
		return nil, err
	}
	revision, err := s.GetDashboardRevision(ctx)
	if err != nil {
		return nil, err
	}
	return &ArchivePage[Video]{
		Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
		HasNext: page < totalPages, Revision: revision, Previous: previous, Next: next,
	}, nil
}
