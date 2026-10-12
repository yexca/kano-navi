package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type AdminPost struct {
	ID                    string  `json:"id"`
	Source                string  `json:"source"`
	AccountHandle         *string `json:"accountHandle"`
	Text                  string  `json:"text"`
	PublishedAt           string  `json:"publishedAt"`
	URL                   string  `json:"url"`
	MediaURL              *string `json:"mediaUrl"`
	MediaAlt              *string `json:"mediaAlt"`
	LLMStatus             string  `json:"llmStatus"`
	LLMRoute              *string `json:"llmRoute"`
	LLMLastAttemptAt      *string `json:"llmLastAttemptAt"`
	LLMLastProcessedAt    *string `json:"llmLastProcessedAt"`
	LLMLastError          *string `json:"llmLastError"`
	LLMLastExtractionID   *int64  `json:"llmLastExtractionId"`
	LLMReprocessRequested bool    `json:"llmReprocessRequested"`
}

type AdminVideo struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Title        string  `json:"title"`
	PublishedAt  *string `json:"publishedAt"`
	ScheduledAt  *string `json:"scheduledAt"`
	URL          string  `json:"url"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	Kind         *string `json:"kind"`
	IsUpcoming   bool    `json:"isUpcoming"`
}

type AdminEvent struct {
	ID                       string   `json:"id"`
	Source                   string   `json:"source"`
	SourceItemID             *string  `json:"sourceItemId"`
	SourceKey                *string  `json:"sourceKey"`
	Title                    string   `json:"title"`
	Detail                   *string  `json:"detail"`
	StartsOn                 string   `json:"startsOn"`
	StartsAt                 *string  `json:"startsAt"`
	EndsAt                   *string  `json:"endsAt"`
	Timezone                 string   `json:"timezone"`
	TimePrecision            string   `json:"timePrecision"`
	Status                   *string  `json:"status"`
	EventType                *string  `json:"eventType"`
	URL                      *string  `json:"url"`
	Provenance               string   `json:"provenance"`
	ManualLocked             bool     `json:"manualLocked"`
	Confidence               *float64 `json:"confidence"`
	ExtractionID             *int64   `json:"extractionId"`
	CancellationStatus       string   `json:"cancellationStatus"`
	CancellationSource       *string  `json:"cancellationSource"`
	CancellationReason       *string  `json:"cancellationReason"`
	CancellationEvidence     *string  `json:"cancellationEvidence"`
	CancellationSourceItemID *string  `json:"cancellationSourceItemId"`
	CancellationConfidence   *float64 `json:"cancellationConfidence"`
	CancellationAt           *string  `json:"cancellationAt"`
	DeletedAt                *string  `json:"deletedAt"`
	CreatedAt                string   `json:"createdAt"`
	UpdatedAt                string   `json:"updatedAt"`
}

type AdminRun struct {
	ID          int64          `json:"id"`
	Source      string         `json:"source"`
	StartedAt   string         `json:"startedAt"`
	FinishedAt  *string        `json:"finishedAt"`
	Status      string         `json:"status"`
	Message     *string        `json:"message"`
	Counts      map[string]any `json:"counts"`
	TriggeredBy *string        `json:"triggeredBy"`
	JobID       *string        `json:"jobId"`
}

type AdminPostsPage struct {
	Posts       []AdminPost `json:"posts"`
	Page        int         `json:"page"`
	PageSize    int         `json:"pageSize"`
	Total       int         `json:"total"`
	TotalPages  int         `json:"totalPages"`
	HasPrevious bool        `json:"hasPrevious"`
	HasNext     bool        `json:"hasNext"`
}
type AdminVideosPage struct {
	Videos          []AdminVideo `json:"videos"`
	Page            int          `json:"page"`
	PageSize        int          `json:"pageSize"`
	Total           int          `json:"total"`
	TotalPages      int          `json:"totalPages"`
	HasPrevious     bool         `json:"hasPrevious"`
	HasNext         bool         `json:"hasNext"`
	FeaturedVideoID *string      `json:"featuredVideoId"`
}
type AdminEventsPage struct {
	Items       []AdminEvent `json:"items"`
	Events      []AdminEvent `json:"events"`
	Page        int          `json:"page"`
	PageSize    int          `json:"pageSize"`
	Total       int          `json:"total"`
	TotalPages  int          `json:"totalPages"`
	HasPrevious bool         `json:"hasPrevious"`
	HasNext     bool         `json:"hasNext"`
}
type AdminRunsPage struct {
	Runs        []AdminRun `json:"runs"`
	Page        int        `json:"page"`
	PageSize    int        `json:"pageSize"`
	Total       int        `json:"total"`
	TotalPages  int        `json:"totalPages"`
	HasPrevious bool       `json:"hasPrevious"`
	HasNext     bool       `json:"hasNext"`
}

var adminDateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func boundPage(page, pageSize, max, defaultSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultSize
	}
	if pageSize > max {
		pageSize = max
	}
	return page, pageSize
}
func pageCount(total, size int) int {
	n := (total + size - 1) / size
	if n < 1 {
		return 1
	}
	return n
}
func safePage(page, totalPages int) int {
	if page > totalPages {
		return totalPages
	}
	return page
}
func likePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return `%` + value + `%`
}
func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	n := v.Float64
	return &n
}

func (s *Store) ListAdminPosts(ctx context.Context, page, pageSize int, source, account, from, to, search, media, status string, reprocess bool) (AdminPostsPage, error) {
	page, pageSize = boundPage(page, pageSize, 200, 50)
	clauses := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(source) != "" {
		clauses = append(clauses, "p.source = ?")
		args = append(args, strings.TrimSpace(source))
	}
	if strings.TrimSpace(account) != "" {
		clauses = append(clauses, "p.account_handle = ?")
		args = append(args, strings.TrimSpace(account))
	}
	if adminDateRE.MatchString(from) {
		clauses = append(clauses, "substr(p.published_at,1,10) >= ?")
		args = append(args, from)
	}
	if adminDateRE.MatchString(to) {
		clauses = append(clauses, "substr(p.published_at,1,10) <= ?")
		args = append(args, to)
	}
	if strings.TrimSpace(search) != "" {
		pattern := likePattern(strings.TrimSpace(search))
		clauses = append(clauses, "(p.text LIKE ? ESCAPE '\\' OR p.url LIKE ? ESCAPE '\\' OR p.media_alt LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern, pattern)
	}
	if media == "1" || strings.EqualFold(media, "true") || strings.EqualFold(media, "yes") || strings.EqualFold(media, "ready") || strings.EqualFold(media, "present") {
		clauses = append(clauses, "p.media_url IS NOT NULL AND trim(p.media_url) <> ''")
	} else if strings.EqualFold(media, "missing") || strings.EqualFold(media, "none") || strings.EqualFold(media, "false") || media == "0" {
		clauses = append(clauses, "(p.media_url IS NULL OR trim(p.media_url) = '')")
	}
	if status != "" {
		clauses = append(clauses, "COALESCE(s.status, 'never') = ?")
		args = append(args, status)
	}
	if reprocess {
		clauses = append(clauses, "COALESCE(s.reprocess_requested, 0) = 1")
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts p LEFT JOIN post_llm_states s ON s.post_id=p.id WHERE "+where, args...).Scan(&total); err != nil {
		return AdminPostsPage{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	offset := (page - 1) * pageSize
	query := `SELECT p.id,p.source,p.account_handle,p.text,p.published_at,p.url,p.media_url,p.media_alt,COALESCE(s.status,'never'),s.route,s.last_attempt_at,s.last_processed_at,s.last_error,s.last_extraction_id,COALESCE(s.reprocess_requested,0) FROM posts p LEFT JOIN post_llm_states s ON s.post_id=p.id WHERE ` + where + ` ORDER BY p.published_at DESC,p.id ASC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, append(args, pageSize, offset)...)
	if err != nil {
		return AdminPostsPage{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminPost{}
	for rows.Next() {
		var p AdminPost
		var account, mediaURL, mediaAlt, route, lastAttempt, lastProcessed, lastError sql.NullString
		var extraction sql.NullInt64
		var requested int
		if err := rows.Scan(&p.ID, &p.Source, &account, &p.Text, &p.PublishedAt, &p.URL, &mediaURL, &mediaAlt, &p.LLMStatus, &route, &lastAttempt, &lastProcessed, &lastError, &extraction, &requested); err != nil {
			return AdminPostsPage{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
		}
		p.AccountHandle = nullString(account)
		p.MediaURL = nullString(mediaURL)
		p.MediaAlt = nullString(mediaAlt)
		p.LLMRoute = nullString(route)
		p.LLMLastAttemptAt = nullString(lastAttempt)
		p.LLMLastProcessedAt = nullString(lastProcessed)
		p.LLMLastError = sanitizeLLMError(lastError)
		p.LLMLastExtractionID = nullInt(extraction)
		p.LLMReprocessRequested = requested != 0
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return AdminPostsPage{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	return AdminPostsPage{Posts: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasPrevious: page > 1, HasNext: page < totalPages}, nil
}

func (s *Store) ListAdminVideos(ctx context.Context, page, pageSize int, status, kind, featured, thumbnail, eventID string) (AdminVideosPage, error) {
	page, pageSize = boundPage(page, pageSize, 200, 100)
	clauses := []string{"1=1"}
	args := []any{}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "published":
		clauses = append(clauses, "v.published_at IS NOT NULL AND v.scheduled_at IS NULL")
	case "scheduled", "upcoming":
		clauses = append(clauses, "v.scheduled_at IS NOT NULL")
	case "live":
		clauses = append(clauses, "lower(COALESCE(v.kind,'')) = 'live'")
	}
	if strings.TrimSpace(kind) != "" {
		clauses = append(clauses, "v.kind = ?")
		args = append(args, strings.TrimSpace(kind))
	}
	switch strings.ToLower(strings.TrimSpace(thumbnail)) {
	case "1", "true", "yes", "ready", "present":
		clauses = append(clauses, "v.thumbnail_url IS NOT NULL AND trim(v.thumbnail_url) <> ''")
	case "missing", "none", "false", "0":
		clauses = append(clauses, "(v.thumbnail_url IS NULL OR trim(v.thumbnail_url) = '')")
	}
	if strings.TrimSpace(eventID) != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM event_sources es WHERE es.source_item_id=v.id AND es.event_id=? AND es.deleted_at IS NULL)")
		args = append(args, strings.TrimSpace(eventID))
	}
	featuredID, _ := s.featuredVideoID(ctx)
	if featured == "1" || strings.EqualFold(featured, "true") {
		if featuredID == nil {
			return AdminVideosPage{Videos: []AdminVideo{}, Page: 1, PageSize: pageSize, Total: 0, TotalPages: 1}, nil
		}
		clauses = append(clauses, "v.id=?")
		args = append(args, *featuredID)
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM videos v WHERE "+where, args...).Scan(&total); err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,v.source,v.title,v.published_at,v.scheduled_at,v.url,v.thumbnail_url,v.kind,COALESCE(v.is_upcoming,0) FROM videos v WHERE `+where+` ORDER BY julianday(COALESCE(v.scheduled_at,v.published_at)) DESC,v.id ASC LIMIT ? OFFSET ?`, append(args, pageSize, offset)...)
	if err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminVideo{}
	for rows.Next() {
		var v AdminVideo
		var published, scheduled, thumbnail, kind sql.NullString
		var upcoming int
		if err := rows.Scan(&v.ID, &v.Source, &v.Title, &published, &scheduled, &v.URL, &thumbnail, &kind, &upcoming); err != nil {
			return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
		}
		v.PublishedAt = nullString(published)
		v.ScheduledAt = nullString(scheduled)
		v.ThumbnailURL = nullString(thumbnail)
		v.Kind = nullString(kind)
		v.IsUpcoming = upcoming != 0
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	if featuredID != nil {
		found := false
		for _, item := range items {
			if item.ID == *featuredID {
				found = true
				break
			}
		}
		if !found {
			var item AdminVideo
			var published, scheduled, thumbnail, itemKind sql.NullString
			var upcoming int
			err := s.db.QueryRowContext(ctx, `SELECT id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,COALESCE(is_upcoming,0) FROM videos WHERE id=?`, *featuredID).Scan(&item.ID, &item.Source, &item.Title, &published, &scheduled, &item.URL, &thumbnail, &itemKind, &upcoming)
			if err == nil {
				item.PublishedAt = nullString(published)
				item.ScheduledAt = nullString(scheduled)
				item.ThumbnailURL = nullString(thumbnail)
				item.Kind = nullString(itemKind)
				item.IsUpcoming = upcoming != 0
				items = append(items, item)
			}
		}
	}
	return AdminVideosPage{Videos: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasPrevious: page > 1, HasNext: page < totalPages, FeaturedVideoID: featuredID}, nil
}

func (s *Store) featuredVideoID(ctx context.Context) (*string, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key='featured_video_id'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		var legacy sql.NullString
		if legacyErr := s.db.QueryRowContext(ctx, "SELECT video_id FROM focus ORDER BY id LIMIT 1").Scan(&legacy); legacyErr != nil || !legacy.Valid || strings.TrimSpace(legacy.String) == "" {
			return nil, nil
		}
		var exists int
		if legacyErr := s.db.QueryRowContext(ctx, "SELECT 1 FROM videos WHERE id=?", legacy.String).Scan(&exists); legacyErr != nil {
			return nil, nil
		}
		v := legacy.String
		return &v, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: featured video unavailable", ErrUnavailable)
	}
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM videos WHERE id=?", value.String).Scan(&exists); err != nil {
		return nil, nil
	}
	v := value.String
	return &v, nil
}

func (s *Store) ListAdminEventsPage(ctx context.Context, page, pageSize int, ascending, includeDeleted bool, search, provenance, source, from, to string) (AdminEventsPage, error) {
	page, pageSize = boundPage(page, pageSize, 100, 25)
	clauses := []string{"1=1"}
	args := []any{}
	if !includeDeleted {
		clauses = append(clauses, "e.deleted_at IS NULL")
	}
	if strings.TrimSpace(search) != "" {
		p := likePattern(strings.TrimSpace(search))
		clauses = append(clauses, "(e.title LIKE ? ESCAPE '\\' OR e.detail LIKE ? ESCAPE '\\' OR e.status LIKE ? ESCAPE '\\')")
		args = append(args, p, p, p)
	}
	if provenance == "automatic" || provenance == "manual" {
		clauses = append(clauses, "e.provenance=?")
		args = append(args, provenance)
	}
	if strings.TrimSpace(source) != "" {
		clauses = append(clauses, "e.source=?")
		args = append(args, strings.TrimSpace(source))
	}
	if adminDateRE.MatchString(from) {
		clauses = append(clauses, "e.starts_on >= ?")
		args = append(args, from)
	}
	if adminDateRE.MatchString(to) {
		clauses = append(clauses, "e.starts_on <= ?")
		args = append(args, to)
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events e WHERE "+where, args...).Scan(&total); err != nil {
		return AdminEventsPage{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	direction := "DESC"
	if ascending {
		direction = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.source,e.source_item_id,e.source_key,e.title,e.detail,e.starts_on,e.starts_at,e.ends_at,e.timezone,e.time_precision,e.status,e.event_type,e.url,e.provenance,e.manual_locked,e.confidence,e.extraction_id,e.cancellation_status,e.cancellation_source,e.cancellation_reason,e.cancellation_evidence,e.cancellation_source_item_id,e.cancellation_confidence,e.cancellation_at,e.deleted_at,e.created_at,e.updated_at FROM events e WHERE `+where+` ORDER BY COALESCE(julianday(e.starts_at),julianday(e.starts_on || 'T23:59:59.999+09:00')) `+direction+`,e.id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return AdminEventsPage{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminEvent{}
	for rows.Next() {
		var e AdminEvent
		var sourceItem, sourceKey, detail, startsAt, endsAt, status, eventType, url, cancelSource, cancelReason, cancelEvidence, cancelSourceItem, cancelAt, deletedAt sql.NullString
		var manual int
		var confidence, cancelConfidence sql.NullFloat64
		var extraction sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Source, &sourceItem, &sourceKey, &e.Title, &detail, &e.StartsOn, &startsAt, &endsAt, &e.Timezone, &e.TimePrecision, &status, &eventType, &url, &e.Provenance, &manual, &confidence, &extraction, &e.CancellationStatus, &cancelSource, &cancelReason, &cancelEvidence, &cancelSourceItem, &cancelConfidence, &cancelAt, &deletedAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return AdminEventsPage{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
		}
		e.SourceItemID = nullString(sourceItem)
		e.SourceKey = nullString(sourceKey)
		e.Detail = nullString(detail)
		e.StartsAt = nullString(startsAt)
		e.EndsAt = nullString(endsAt)
		e.Status = nullString(status)
		e.EventType = nullString(eventType)
		e.URL = nullString(url)
		e.ManualLocked = manual != 0
		e.Confidence = nullFloat(confidence)
		e.ExtractionID = nullInt(extraction)
		e.CancellationSource = nullString(cancelSource)
		e.CancellationReason = nullString(cancelReason)
		e.CancellationEvidence = nullString(cancelEvidence)
		e.CancellationSourceItemID = nullString(cancelSourceItem)
		e.CancellationConfidence = nullFloat(cancelConfidence)
		e.CancellationAt = nullString(cancelAt)
		e.DeletedAt = nullString(deletedAt)
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return AdminEventsPage{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
	}
	return AdminEventsPage{Items: items, Events: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasPrevious: page > 1, HasNext: page < totalPages}, nil
}

func (s *Store) ListAdminRunsPage(ctx context.Context, page, pageSize int, jobID, status string) (AdminRunsPage, error) {
	page, pageSize = boundPage(page, pageSize, 100, 20)
	clauses := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(jobID) != "" {
		clauses = append(clauses, "job_id=?")
		args = append(args, strings.TrimSpace(jobID))
	}
	if strings.TrimSpace(status) != "" {
		clauses = append(clauses, "status=?")
		args = append(args, strings.TrimSpace(status))
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sync_runs WHERE "+where, args...).Scan(&total); err != nil {
		return AdminRunsPage{}, fmt.Errorf("%w: admin runs unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,started_at,finished_at,status,message,counts_json,triggered_by,job_id FROM sync_runs WHERE `+where+` ORDER BY COALESCE(finished_at,started_at) DESC,id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return AdminRunsPage{}, fmt.Errorf("%w: admin runs unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminRun{}
	for rows.Next() {
		var run AdminRun
		var finished, message, counts, triggered, job sql.NullString
		if err := rows.Scan(&run.ID, &run.Source, &run.StartedAt, &finished, &run.Status, &message, &counts, &triggered, &job); err != nil {
			return AdminRunsPage{}, fmt.Errorf("%w: admin runs unavailable", ErrUnavailable)
		}
		run.FinishedAt = nullString(finished)
		run.Message = sanitizeRunMessage(message)
		run.TriggeredBy = nullString(triggered)
		run.JobID = nullString(job)
		run.Counts = sanitizeCounts(counts.String)
		items = append(items, run)
	}
	if err := rows.Err(); err != nil {
		return AdminRunsPage{}, fmt.Errorf("%w: admin runs unavailable", ErrUnavailable)
	}
	return AdminRunsPage{Runs: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, HasPrevious: page > 1, HasNext: page < totalPages}, nil
}

func sanitizeCounts(raw string) map[string]any {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return map[string]any{}
	}
	clean, ok := sanitizeDiagnostic(value).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return clean
}

func sanitizeLLMError(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	lower := strings.ToLower(value.String)
	category := "provider_error"
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		category = "timeout"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "credential") || strings.Contains(lower, "401") || strings.Contains(lower, "403"):
		category = "authentication"
	case strings.Contains(lower, "rate") || strings.Contains(lower, "429"):
		category = "rate_limited"
	case strings.Contains(lower, "json") || strings.Contains(lower, "parse") || strings.Contains(lower, "decode"):
		category = "invalid_response"
	case strings.Contains(lower, "network") || strings.Contains(lower, "connect"):
		category = "network"
	}
	return &category
}

func sanitizeRunMessage(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	text := value.String
	lower := strings.ToLower(text)
	if strings.Contains(text, "\n") || strings.Contains(lower, "stack") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "cipher") || strings.Contains(lower, "bearer ") || strings.HasPrefix(text, "/") || strings.HasPrefix(text, `\\`) || regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(text) {
		return nil
	}
	return &text
}
func sanitizeDiagnostic(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			lower := strings.ToLower(k)
			if lower == "stack" || lower == "trace" || lower == "traceback" || lower == "path" || lower == "filepath" || lower == "database" || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "cipher") || strings.Contains(lower, "raw") {
				continue
			}
			out[k] = sanitizeDiagnostic(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sanitizeDiagnostic(item)
		}
		return out
	case string:
		lower := strings.ToLower(v)
		if strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\\`) || regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(v) || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "cipher") || strings.Contains(lower, "bearer ") {
			return "[redacted]"
		}
		return v
	default:
		return value
	}
}
