package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

var calendarDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var ErrInvalidCalendarRange = errors.New("invalid calendar range")

// CalendarPage is the bounded public calendar read. Events are intentionally
// represented by the same store type used by the dashboard so both routes
// share the exact event fields, cancellation semantics and ordering rules.
type CalendarPage struct {
	Items          []DashboardEvent
	Page           int
	PageSize       int
	Total          int64
	TotalPages     int
	HasPrevious    bool
	HasNext        bool
	DayCounts      map[string]int64
	Revision       int64
	ScheduleImages []Asset
	Previous       *string
	Next           *string
}

func validCalendarDate(value string) bool {
	if !calendarDatePattern.MatchString(value) {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func validateCalendarRange(from, to string) error {
	if !validCalendarDate(from) || !validCalendarDate(to) {
		return ErrInvalidCalendarRange
	}
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", to)
	if end.Before(start) || end.Sub(start) > 6*24*time.Hour {
		return ErrInvalidCalendarRange
	}
	return nil
}

func (s *Store) GetCalendarPage(ctx context.Context, from, to string, page, pageSize int) (*CalendarPage, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if err := validateCalendarRange(from, to); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND starts_on BETWEEN ? AND ?`, from, to).Scan(&total); err != nil {
		return nil, dbErr(err)
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,source,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_at
		FROM events
		WHERE deleted_at IS NULL AND starts_on BETWEEN ? AND ?
		ORDER BY COALESCE(julianday(starts_at), julianday(starts_on || 'T23:59:59.999+09:00')) ASC, id ASC
		LIMIT ? OFFSET ?`, from, to, pageSize, offset)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	items := make([]DashboardEvent, 0, pageSize)
	for rows.Next() {
		event, scanErr := scanEvent(rows, time.Now())
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	if err := rows.Close(); err != nil {
		return nil, dbErr(err)
	}

	dayCounts := make(map[string]int64)
	countRows, err := s.db.QueryContext(ctx, `SELECT starts_on, COUNT(*) FROM events WHERE deleted_at IS NULL AND starts_on BETWEEN ? AND ? GROUP BY starts_on`, from, to)
	if err != nil {
		return nil, dbErr(err)
	}
	for countRows.Next() {
		var day string
		var count int64
		if err := countRows.Scan(&day, &count); err != nil {
			_ = countRows.Close()
			return nil, dbErr(err)
		}
		dayCounts[day] = count
	}
	if err := countRows.Err(); err != nil {
		_ = countRows.Close()
		return nil, dbErr(err)
	}
	_ = countRows.Close()

	var previous, next sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(starts_on) FROM events WHERE deleted_at IS NULL AND starts_on < ?`, from).Scan(&previous); err != nil {
		return nil, dbErr(err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(starts_on) FROM events WHERE deleted_at IS NULL AND starts_on > ?`, to).Scan(&next); err != nil {
		return nil, dbErr(err)
	}

	scheduleImages, err := s.queryScheduleAssets(ctx, from, to)
	if err != nil {
		return nil, err
	}
	revision, err := s.GetDashboardRevision(ctx)
	if err != nil {
		return nil, err
	}
	result := &CalendarPage{
		Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages,
		HasPrevious: page > 1, HasNext: page < totalPages, DayCounts: dayCounts,
		Revision: revision, ScheduleImages: scheduleImages,
	}
	if previous.Valid {
		result.Previous = &previous.String
	}
	if next.Valid {
		result.Next = &next.String
	}
	return result, nil
}
