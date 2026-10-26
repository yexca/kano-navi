package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	// Media lists the post's images in order. MediaStatus is the first
	// image's status, or "none" for a text-only post.
	Media       []AdminPostImage `json:"media"`
	MediaStatus string           `json:"mediaStatus"`
	// Translation is nil for posts outside the machine scope (non-X).
	Translation *AdminTranslation `json:"translation"`
}

// AdminPostImage is one post image. URL is the local /media URL of a ready
// cached file, never the remote address; Status explains a null URL.
type AdminPostImage struct {
	URL    *string `json:"url"`
	Status string  `json:"status"`
	Alt    *string `json:"alt"`
}

type AdminVideo struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	Title       string  `json:"title"`
	PublishedAt *string `json:"publishedAt"`
	ScheduledAt *string `json:"scheduledAt"`
	URL         string  `json:"url"`
	// ThumbnailURL is the local /media URL of a ready cached file, never the
	// remote CDN address; ThumbnailStatus explains a null URL.
	ThumbnailURL    *string `json:"thumbnailUrl"`
	ThumbnailStatus string  `json:"thumbnailStatus"`
	Kind            *string `json:"kind"`
	IsUpcoming      bool    `json:"isUpcoming"`
	// Title, Kind and ThumbnailURL show an operator's correction when one is
	// set; SourceTitle and SourceKind keep what the platform reported.
	SourceTitle     string             `json:"sourceTitle"`
	SourceKind      *string            `json:"sourceKind"`
	ManualTitle     *string            `json:"manualTitle"`
	ManualKind      *string            `json:"manualKind"`
	ManualThumbnail *AdminThumbnailPin `json:"manualThumbnail"`
	DurationSeconds *int64             `json:"durationSeconds"`
	// IsShort shows an operator's format correction when one is set;
	// SourceIsShort keeps what the feed linked, nil while unknown.
	IsShort       *bool `json:"isShort"`
	SourceIsShort *bool `json:"sourceIsShort"`
	ManualIsShort *bool `json:"manualIsShort"`
	// CheckedAt is the last successful watch-page read; RefreshRequestedAt is
	// an operator request the next YouTube step has not answered yet.
	CheckedAt          *string `json:"checkedAt"`
	RefreshRequestedAt *string `json:"refreshRequestedAt"`
	Revisions          int     `json:"revisions"`
	// Translation is the title's translation state (translations.go).
	Translation *AdminTranslation `json:"translation"`
	// A Bilibili upload merged into a YouTube video names it in MergedInto
	// (video_merge.go). MergeSource manual without a target is an undone
	// merge the rules never redo.
	MergedInto      *string `json:"mergedInto"`
	MergedIntoTitle *string `json:"mergedIntoTitle"`
	MergeSource     *string `json:"mergeSource"`
	MergeReason     *string `json:"mergeReason"`
	MergedAt        *string `json:"mergedAt"`
	// Alternates are the uploads merged into this video.
	Alternates []AdminVideoAlternate `json:"alternates"`
	// sourceThumbnail is the platform URL the board falls back to.
	sourceThumbnail *string
}

// AdminVideoAlternate is an upload merged into a video.
type AdminVideoAlternate struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	MergeSource *string `json:"mergeSource"`
	MergeReason *string `json:"mergeReason"`
	MergedAt    *string `json:"mergedAt"`
}

// attachAdminVideoAlternates lists the uploads merged into each video.
func (s *Store) attachAdminVideoAlternates(ctx context.Context, videos []AdminVideo) error {
	index := map[string][]int{}
	ids := []string{}
	for i := range videos {
		videos[i].Alternates = []AdminVideoAlternate{}
		if index[videos[i].ID] == nil {
			ids = append(ids, videos[i].ID)
		}
		index[videos[i].ID] = append(index[videos[i].ID], i)
	}
	for _, chunk := range translationChunks(ids) {
		rows, err := s.db.QueryContext(ctx, `SELECT merged_into,id,source,COALESCE(manual_title,title),url,merge_source,merge_reason,merged_at FROM videos
			WHERE merged_into IN (`+placeholders(len(chunk))+`) ORDER BY julianday(published_at),id`, idArgs(chunk)...)
		if err != nil {
			return dbErr(err)
		}
		for rows.Next() {
			var target string
			var a AdminVideoAlternate
			var source, reason, at sql.NullString
			if err = rows.Scan(&target, &a.ID, &a.Source, &a.Title, &a.URL, &source, &reason, &at); err != nil {
				rows.Close()
				return dbErr(err)
			}
			a.MergeSource, a.MergeReason, a.MergedAt = nullString(source), nullString(reason), nullString(at)
			for _, i := range index[target] {
				videos[i].Alternates = append(videos[i].Alternates, a)
			}
		}
		if err = rows.Close(); err != nil {
			return dbErr(err)
		}
	}
	return nil
}

// attachAdminVideoTranslations fills each YouTube video's title translation
// state; other sources are outside the translate step and stay nil.
func (s *Store) attachAdminVideoTranslations(ctx context.Context, videos []AdminVideo) error {
	ids := []string{}
	for i := range videos {
		if videos[i].Source == "youtube" {
			ids = append(ids, videos[i].ID)
		}
	}
	states, err := s.AdminTranslations(ctx, TranslationOwnerVideo, ids)
	if err != nil {
		return err
	}
	for i := range videos {
		if state, ok := states[videos[i].ID]; ok {
			videos[i].Translation = &state
		}
	}
	return nil
}

// AdminThumbnailPin is the cached thumbnail file an operator chose.
type AdminThumbnailPin struct {
	MediaID string `json:"mediaId"`
	SHA256  string `json:"sha256"`
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
	// A merged event is hidden behind MergedInto on every public path.
	// MergeSource manual without a target is an undone merge.
	MergedInto      *string `json:"mergedInto"`
	MergedIntoTitle *string `json:"mergedIntoTitle"`
	MergeSource     *string `json:"mergeSource"`
	MergeReason     *string `json:"mergeReason"`
	MergedAt        *string `json:"mergedAt"`
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

// AdminPage is one bounded page of an operator list. Page is clamped to the
// last page.
type AdminPage[T any] struct {
	Items      []T `json:"items"`
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

type AdminVideosPage struct {
	AdminPage[AdminVideo]
	FeaturedVideoID *string `json:"featuredVideoId"`
	// FeaturedVideo is the pinned video itself, whatever page or filter is
	// listed, so the console can show and clear it.
	FeaturedVideo *AdminVideo `json:"featuredVideo"`
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

// AdminPostFilter narrows the post list. Media is "present" or "missing";
// LLM is a post_llm_states status or "never".
type AdminPostFilter struct {
	Page, PageSize                                int
	Source, Account, From, To, Search, Media, LLM string
	Reprocess                                     bool
	// Translation is "incomplete", "complete" or "requested".
	Translation string
}

// AdminVideoFilter narrows the video list. Status is "published",
// "upcoming" or "live"; Thumbnail is "ready", "present" or "missing";
// Source is "youtube" or "bilibili".
type AdminVideoFilter struct {
	Page, PageSize                  int
	Status, Kind, Thumbnail, Source string
}

// AdminEventFilter narrows the event list. Cancellation is "none",
// "llm_suspected" or "manual_confirmed".
type AdminEventFilter struct {
	Page, PageSize                                     int
	Ascending, IncludeDeleted                          bool
	Search, Provenance, Source, Cancellation, From, To string
	// Merge is "merged" (only merged duplicates), "visible" (excluding them,
	// as every public reader does) or empty for both.
	Merge string
}

// AdminCounts are the bounded aggregates behind the console overview.
type AdminCounts struct {
	UpcomingEvents         int `json:"upcomingEvents"`
	Events                 int `json:"events"`
	Posts                  int `json:"posts"`
	Videos                 int `json:"videos"`
	PendingImages          int `json:"pendingImages"`
	SuspectedCancellations int `json:"suspectedCancellations"`
	FailedPosts            int `json:"failedPosts"`
	ReprocessRequested     int `json:"reprocessRequested"`
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
	n := total / size
	if total%size != 0 {
		n++
	}
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
func nullBool(v sql.NullBool) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Bool
	return &b
}
func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	n := v.Float64
	return &n
}

func (s *Store) ListAdminPosts(ctx context.Context, f AdminPostFilter) (AdminPage[AdminPost], error) {
	page, pageSize := boundPage(f.Page, f.PageSize, 200, 50)
	clauses := []string{"1=1"}
	args := []any{}
	if source := strings.TrimSpace(f.Source); source != "" {
		clauses = append(clauses, "p.source = ?")
		args = append(args, source)
	}
	if account := strings.TrimPrefix(strings.TrimSpace(f.Account), "@"); account != "" {
		clauses = append(clauses, "lower(p.account_handle) = lower(?)")
		args = append(args, account)
	}
	if adminDateRE.MatchString(f.From) {
		clauses = append(clauses, "date(p.published_at,'+9 hours') >= ?")
		args = append(args, f.From)
	}
	if adminDateRE.MatchString(f.To) {
		clauses = append(clauses, "date(p.published_at,'+9 hours') <= ?")
		args = append(args, f.To)
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		pattern := likePattern(search)
		clauses = append(clauses, "(p.text LIKE ? ESCAPE '\\' OR p.url LIKE ? ESCAPE '\\' OR p.media_alt LIKE ? ESCAPE '\\' OR p.id LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern, pattern, pattern)
	}
	switch f.Media {
	case "present":
		clauses = append(clauses, "p.media_url IS NOT NULL AND trim(p.media_url) <> ''")
	case "missing":
		clauses = append(clauses, "(p.media_url IS NULL OR trim(p.media_url) = '')")
	}
	switch f.LLM {
	case "never", "pending", "processed", "failed", "skipped", "uncertain":
		clauses = append(clauses, "COALESCE(s.status, 'never') = ?")
		args = append(args, f.LLM)
	}
	if f.Reprocess {
		clauses = append(clauses, "COALESCE(s.reprocess_requested, 0) = 1")
	}
	switch f.Translation {
	case "incomplete":
		clauses = append(clauses, "p.source = 'x' AND "+TranslationIncompleteSQL(TranslationOwnerPost, "p"))
	case "complete":
		clauses = append(clauses, "p.source = 'x' AND NOT "+TranslationIncompleteSQL(TranslationOwnerPost, "p"))
	case "requested":
		clauses = append(clauses, TranslationRequestedSQL(TranslationOwnerPost, "p"))
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM posts p LEFT JOIN post_llm_states s ON s.post_id=p.id WHERE "+where, args...).Scan(&total); err != nil {
		return AdminPage[AdminPost]{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	offset := (page - 1) * pageSize
	query := `SELECT p.id,p.source,p.account_handle,p.text,p.published_at,p.url,p.media_url,p.media_alt,COALESCE(s.status,'never'),s.route,s.last_attempt_at,s.last_processed_at,s.last_error,s.last_extraction_id,COALESCE(s.reprocess_requested,0) FROM posts p LEFT JOIN post_llm_states s ON s.post_id=p.id WHERE ` + where + ` ORDER BY julianday(p.published_at) DESC,p.id ASC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, append(args, pageSize, offset)...)
	if err != nil {
		return AdminPage[AdminPost]{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminPost{}
	for rows.Next() {
		var p AdminPost
		var account, mediaURL, mediaAlt, route, lastAttempt, lastProcessed, lastError sql.NullString
		var extraction sql.NullInt64
		var requested int
		if err := rows.Scan(&p.ID, &p.Source, &account, &p.Text, &p.PublishedAt, &p.URL, &mediaURL, &mediaAlt, &p.LLMStatus, &route, &lastAttempt, &lastProcessed, &lastError, &extraction, &requested); err != nil {
			return AdminPage[AdminPost]{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
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
		return AdminPage[AdminPost]{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	// Release the single connection before resolving each cached image.
	rows.Close()
	for i := range items {
		if err := s.resolveAdminPostMedia(ctx, &items[i]); err != nil {
			return AdminPage[AdminPost]{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
		}
	}
	ids := []string{}
	for _, item := range items {
		if item.Source == "x" {
			ids = append(ids, item.ID)
		}
	}
	translations, err := s.AdminTranslations(ctx, TranslationOwnerPost, ids)
	if err != nil {
		return AdminPage[AdminPost]{}, fmt.Errorf("%w: admin posts unavailable", ErrUnavailable)
	}
	for i := range items {
		if translation, ok := translations[items[i].ID]; ok {
			items[i].Translation = &translation
		}
	}
	return AdminPage[AdminPost]{Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}, nil
}

// resolveAdminPostMedia lists a post's images as the board does: linked post
// images, or else the stored media URL.
func (s *Store) resolveAdminPostMedia(ctx context.Context, p *AdminPost) error {
	p.Media, p.MediaStatus = []AdminPostImage{}, "none"
	links, err := s.queryPostMedia(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, link := range links {
		p.Media = append(p.Media, AdminPostImage{URL: link.URL, Status: link.Status, Alt: link.Alt})
	}
	if len(p.Media) == 0 && p.MediaURL != nil && strings.TrimSpace(*p.MediaURL) != "" {
		ref, err := s.resolveMedia(ctx, p.MediaURL)
		if err != nil {
			return err
		}
		p.Media = append(p.Media, AdminPostImage{URL: ref.PublicURL, Status: ref.Status, Alt: p.MediaAlt})
	}
	if len(p.Media) > 0 {
		p.MediaStatus = p.Media[0].Status
	}
	return nil
}

func (s *Store) ListAdminVideos(ctx context.Context, f AdminVideoFilter) (AdminVideosPage, error) {
	page, pageSize := boundPage(f.Page, f.PageSize, 200, 50)
	clauses := []string{"1=1"}
	args := []any{}
	switch f.Status {
	case "published":
		clauses = append(clauses, "v.published_at IS NOT NULL AND COALESCE(v.is_upcoming,0)=0")
	case "upcoming":
		clauses = append(clauses, "v.scheduled_at IS NOT NULL AND COALESCE(v.is_upcoming,0)=1")
	case "live":
		clauses = append(clauses, "lower(COALESCE(v.kind,'')) = 'live'")
	}
	if f.Source == "youtube" || f.Source == "bilibili" {
		clauses = append(clauses, "v.source = ?")
		args = append(args, f.Source)
	}
	if kind := strings.TrimSpace(f.Kind); kind != "" {
		clauses = append(clauses, "COALESCE(v.manual_kind,v.kind) = ?")
		args = append(args, kind)
	}
	switch f.Thumbnail {
	case "ready":
		clauses = append(clauses, "EXISTS (SELECT 1 FROM media_assets m WHERE m.source_url=v.thumbnail_url AND m.status='ready')")
	case "present":
		clauses = append(clauses, "v.thumbnail_url IS NOT NULL AND trim(v.thumbnail_url) <> ''")
	case "missing":
		clauses = append(clauses, "(v.thumbnail_url IS NULL OR trim(v.thumbnail_url) = '')")
	}
	featuredID, err := s.featuredVideoID(ctx)
	if err != nil {
		return AdminVideosPage{}, err
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM videos v WHERE "+where, args...).Scan(&total); err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx, `SELECT `+adminVideoColumns+` FROM videos v WHERE `+where+` ORDER BY julianday(COALESCE(v.scheduled_at,v.published_at)) DESC,v.id ASC LIMIT ? OFFSET ?`, append(args, pageSize, offset)...)
	if err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminVideo{}
	for rows.Next() {
		v, err := scanAdminVideo(rows)
		if err != nil {
			return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	// Release the single connection before resolving each cached thumbnail.
	rows.Close()
	for i := range items {
		if err := s.resolveAdminThumbnail(ctx, &items[i]); err != nil {
			return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
		}
	}
	if err := s.attachAdminVideoTranslations(ctx, items); err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	if err := s.attachAdminVideoAlternates(ctx, items); err != nil {
		return AdminVideosPage{}, fmt.Errorf("%w: admin videos unavailable", ErrUnavailable)
	}
	featured, err := s.adminFeaturedVideo(ctx, featuredID, items)
	if err != nil {
		return AdminVideosPage{}, err
	}
	return AdminVideosPage{
		AdminPage:       AdminPage[AdminVideo]{Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages},
		FeaturedVideoID: featuredID,
		FeaturedVideo:   featured,
	}, nil
}

const adminVideoColumns = `v.id,v.source,v.title,v.published_at,v.scheduled_at,v.url,v.thumbnail_url,v.kind,COALESCE(v.is_upcoming,0),
 v.manual_title,v.manual_kind,v.manual_thumbnail_media_id,v.manual_thumbnail_sha256,v.duration_seconds,v.live_checked_at,v.refresh_requested_at,
 (SELECT COUNT(*) FROM video_revisions r WHERE r.video_id=v.id),
 v.merged_into,(SELECT COALESCE(t.manual_title,t.title) FROM videos t WHERE t.id=v.merged_into),v.merge_source,v.merge_reason,v.merged_at,v.is_short,v.manual_is_short`

func scanAdminVideo(row scanner) (AdminVideo, error) {
	var v AdminVideo
	var published, scheduled, thumbnail, kind, manualTitle, manualKind, pinMedia, pinHash, checked, requested sql.NullString
	var mergedInto, mergedTitle, mergeSource, mergeReason, mergedAt sql.NullString
	var duration sql.NullInt64
	var short, manualShort sql.NullBool
	var upcoming int
	if err := row.Scan(&v.ID, &v.Source, &v.SourceTitle, &published, &scheduled, &v.URL, &thumbnail, &kind, &upcoming,
		&manualTitle, &manualKind, &pinMedia, &pinHash, &duration, &checked, &requested, &v.Revisions,
		&mergedInto, &mergedTitle, &mergeSource, &mergeReason, &mergedAt, &short, &manualShort); err != nil {
		return AdminVideo{}, err
	}
	v.MergedInto, v.MergedIntoTitle, v.MergeSource = nullString(mergedInto), nullString(mergedTitle), nullString(mergeSource)
	v.MergeReason, v.MergedAt = nullString(mergeReason), nullString(mergedAt)
	v.Alternates = []AdminVideoAlternate{}
	v.PublishedAt = nullString(published)
	v.ScheduledAt = nullString(scheduled)
	v.sourceThumbnail = nullString(thumbnail)
	v.ThumbnailURL = v.sourceThumbnail
	v.SourceKind = nullString(kind)
	v.ManualTitle = nullString(manualTitle)
	v.ManualKind = nullString(manualKind)
	v.Title, v.Kind = v.SourceTitle, v.SourceKind
	if v.ManualTitle != nil {
		v.Title = *v.ManualTitle
	}
	if v.ManualKind != nil {
		v.Kind = v.ManualKind
	}
	if pinMedia.Valid && pinHash.Valid {
		v.ManualThumbnail = &AdminThumbnailPin{MediaID: pinMedia.String, SHA256: pinHash.String}
	}
	v.DurationSeconds = nullInt(duration)
	v.SourceIsShort, v.ManualIsShort = nullBool(short), nullBool(manualShort)
	v.IsShort = v.SourceIsShort
	if v.ManualIsShort != nil {
		v.IsShort = v.ManualIsShort
	}
	v.CheckedAt = nullString(checked)
	v.RefreshRequestedAt = nullString(requested)
	v.IsUpcoming = upcoming != 0
	return v, nil
}

// resolveAdminThumbnail replaces the scanned source URL with the local URL of
// the pinned file, or of the platform's cached file when nothing is pinned or
// the pinned file is gone.
func (s *Store) resolveAdminThumbnail(ctx context.Context, v *AdminVideo) error {
	if v.ManualThumbnail != nil {
		if url, ok := s.mediaVersionURL(ctx, v.ManualThumbnail.MediaID, v.ManualThumbnail.SHA256); ok {
			v.ThumbnailURL, v.ThumbnailStatus = &url, "ready"
			return nil
		}
	}
	ref, err := s.resolveMedia(ctx, v.sourceThumbnail)
	if err != nil {
		return err
	}
	v.ThumbnailURL, v.ThumbnailStatus = ref.PublicURL, ref.Status
	return nil
}

// AdminVideoByID reads one video as the console lists it. It returns
// ErrVideoNotFound for an unknown ID.
func (s *Store) AdminVideoByID(ctx context.Context, id string) (AdminVideo, error) {
	v, err := scanAdminVideo(s.db.QueryRowContext(ctx, `SELECT `+adminVideoColumns+` FROM videos v WHERE v.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminVideo{}, ErrVideoNotFound
	}
	if err != nil {
		return AdminVideo{}, fmt.Errorf("%w: video unavailable", ErrUnavailable)
	}
	if err = s.resolveAdminThumbnail(ctx, &v); err != nil {
		return AdminVideo{}, fmt.Errorf("%w: video unavailable", ErrUnavailable)
	}
	one := []AdminVideo{v}
	if err = s.attachAdminVideoTranslations(ctx, one); err != nil {
		return AdminVideo{}, fmt.Errorf("%w: video unavailable", ErrUnavailable)
	}
	if err = s.attachAdminVideoAlternates(ctx, one); err != nil {
		return AdminVideo{}, fmt.Errorf("%w: video unavailable", ErrUnavailable)
	}
	return one[0], nil
}

// adminVideosByID reads several videos as the console lists them; unknown
// IDs are left out.
func (s *Store) adminVideosByID(ctx context.Context, ids []string) (map[string]AdminVideo, error) {
	items := []AdminVideo{}
	seen := map[string]bool{}
	unique := []string{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	for _, chunk := range translationChunks(unique) {
		rows, err := s.db.QueryContext(ctx, `SELECT `+adminVideoColumns+` FROM videos v WHERE v.id IN (`+placeholders(len(chunk))+`)`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("%w: videos unavailable", ErrUnavailable)
		}
		for rows.Next() {
			v, err := scanAdminVideo(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("%w: videos unavailable", ErrUnavailable)
			}
			items = append(items, v)
		}
		if err = rows.Close(); err != nil {
			return nil, fmt.Errorf("%w: videos unavailable", ErrUnavailable)
		}
	}
	for i := range items {
		if err := s.resolveAdminThumbnail(ctx, &items[i]); err != nil {
			return nil, fmt.Errorf("%w: videos unavailable", ErrUnavailable)
		}
	}
	if err := s.attachAdminVideoTranslations(ctx, items); err != nil {
		return nil, fmt.Errorf("%w: videos unavailable", ErrUnavailable)
	}
	if err := s.attachAdminVideoAlternates(ctx, items); err != nil {
		return nil, fmt.Errorf("%w: videos unavailable", ErrUnavailable)
	}
	out := make(map[string]AdminVideo, len(items))
	for _, v := range items {
		out[v.ID] = v
	}
	return out, nil
}

// adminFeaturedVideo reuses the listed copy of the pinned video when the page
// holds it and reads it by ID otherwise.
func (s *Store) adminFeaturedVideo(ctx context.Context, id *string, listed []AdminVideo) (*AdminVideo, error) {
	if id == nil {
		return nil, nil
	}
	for i := range listed {
		if listed[i].ID == *id {
			v := listed[i]
			return &v, nil
		}
	}
	v, err := s.AdminVideoByID(ctx, *id)
	if errors.Is(err, ErrVideoNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: featured video unavailable", ErrUnavailable)
	}
	return &v, nil
}

func (s *Store) featuredVideoID(ctx context.Context) (*string, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key='featured_video_id'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRowContext(ctx, "SELECT video_id FROM focus ORDER BY id LIMIT 1").Scan(&value)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: featured video unavailable", ErrUnavailable)
	}
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	var exists int
	err = s.db.QueryRowContext(ctx, "SELECT 1 FROM videos WHERE id=?", value.String).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: featured video unavailable", ErrUnavailable)
	}
	return &value.String, nil
}

func (s *Store) ListAdminEvents(ctx context.Context, f AdminEventFilter) (AdminPage[AdminEvent], error) {
	page, pageSize := boundPage(f.Page, f.PageSize, 100, 25)
	clauses := []string{"1=1"}
	args := []any{}
	if !f.IncludeDeleted {
		clauses = append(clauses, "e.deleted_at IS NULL")
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		p := likePattern(search)
		clauses = append(clauses, "(e.title LIKE ? ESCAPE '\\' OR e.detail LIKE ? ESCAPE '\\' OR e.status LIKE ? ESCAPE '\\')")
		args = append(args, p, p, p)
	}
	if f.Provenance == "automatic" || f.Provenance == "manual" {
		clauses = append(clauses, "e.provenance=?")
		args = append(args, f.Provenance)
	}
	if source := strings.TrimSpace(f.Source); source != "" {
		clauses = append(clauses, "e.source=?")
		args = append(args, source)
	}
	switch f.Cancellation {
	case "none", "llm_suspected", "manual_confirmed":
		clauses = append(clauses, "e.cancellation_status=?")
		args = append(args, f.Cancellation)
	}
	switch f.Merge {
	case "merged":
		clauses = append(clauses, "e.merged_into IS NOT NULL")
	case "visible":
		clauses = append(clauses, "e.merged_into IS NULL")
	}
	if adminDateRE.MatchString(f.From) {
		clauses = append(clauses, "e.starts_on >= ?")
		args = append(args, f.From)
	}
	if adminDateRE.MatchString(f.To) {
		clauses = append(clauses, "e.starts_on <= ?")
		args = append(args, f.To)
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events e WHERE "+where, args...).Scan(&total); err != nil {
		return AdminPage[AdminEvent]{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	direction := "DESC"
	if f.Ascending {
		direction = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.source,e.source_item_id,e.source_key,e.title,e.detail,e.starts_on,e.starts_at,e.ends_at,e.timezone,e.time_precision,e.status,e.event_type,e.url,e.provenance,e.manual_locked,e.confidence,e.extraction_id,e.cancellation_status,e.cancellation_source,e.cancellation_reason,e.cancellation_evidence,e.cancellation_source_item_id,e.cancellation_confidence,e.cancellation_at,e.deleted_at,e.created_at,e.updated_at,e.merged_into,(SELECT t.title FROM events t WHERE t.id=e.merged_into),e.merge_source,e.merge_reason,e.merged_at FROM events e WHERE `+where+` ORDER BY COALESCE(julianday(e.starts_at),julianday(e.starts_on || 'T23:59:59.999+09:00')) `+direction+`,e.id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return AdminPage[AdminEvent]{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
	}
	defer rows.Close()
	items := []AdminEvent{}
	for rows.Next() {
		var e AdminEvent
		var sourceItem, sourceKey, detail, startsAt, endsAt, status, eventType, url, cancelSource, cancelReason, cancelEvidence, cancelSourceItem, cancelAt, deletedAt sql.NullString
		var mergedInto, mergedIntoTitle, mergeSource, mergeReason, mergedAt sql.NullString
		var manual int
		var confidence, cancelConfidence sql.NullFloat64
		var extraction sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Source, &sourceItem, &sourceKey, &e.Title, &detail, &e.StartsOn, &startsAt, &endsAt, &e.Timezone, &e.TimePrecision, &status, &eventType, &url, &e.Provenance, &manual, &confidence, &extraction, &e.CancellationStatus, &cancelSource, &cancelReason, &cancelEvidence, &cancelSourceItem, &cancelConfidence, &cancelAt, &deletedAt, &e.CreatedAt, &e.UpdatedAt, &mergedInto, &mergedIntoTitle, &mergeSource, &mergeReason, &mergedAt); err != nil {
			return AdminPage[AdminEvent]{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
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
		e.MergedInto = nullString(mergedInto)
		e.MergedIntoTitle = nullString(mergedIntoTitle)
		e.MergeSource = nullString(mergeSource)
		e.MergeReason = nullString(mergeReason)
		e.MergedAt = nullString(mergedAt)
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return AdminPage[AdminEvent]{}, fmt.Errorf("%w: admin events unavailable", ErrUnavailable)
	}
	return AdminPage[AdminEvent]{Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}, nil
}

// AdminCounts reads the overview aggregates. today is the current Japan date;
// upcoming events start on or after it and are not confirmed cancellations.
func (s *Store) AdminCounts(ctx context.Context, today string) (AdminCounts, error) {
	var c AdminCounts
	err := s.db.QueryRowContext(ctx, `SELECT
	  (SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND merged_into IS NULL AND starts_on >= ? AND cancellation_status <> 'manual_confirmed'),
	  (SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND merged_into IS NULL),
	  (SELECT COUNT(*) FROM posts),
	  (SELECT COUNT(*) FROM videos),
	  (SELECT COUNT(*) FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id=a.id
	     WHERE a.kind='schedule' AND TRIM(a.url)<>'' AND COALESCE(r.manual_status,'unreviewed')='unreviewed'
	       AND COALESCE(r.llm_status,'pending') IN ('pending','uncertain','failed')),
	  (SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND cancellation_status='llm_suspected'),
	  (SELECT COUNT(*) FROM post_llm_states WHERE status='failed'),
	  (SELECT COUNT(*) FROM post_llm_states WHERE reprocess_requested=1)`, today).Scan(
		&c.UpcomingEvents, &c.Events, &c.Posts, &c.Videos, &c.PendingImages, &c.SuspectedCancellations, &c.FailedPosts, &c.ReprocessRequested)
	if err != nil {
		return AdminCounts{}, fmt.Errorf("%w: admin counts unavailable", ErrUnavailable)
	}
	return c, nil
}

// SourceLastSuccess reports the newest successful incremental read of each
// source across its accounts, or nil when it never succeeded.
func (s *Store) SourceLastSuccess(ctx context.Context) (map[string]*string, error) {
	out := map[string]*string{"x": nil, "youtube": nil}
	rows, err := s.db.QueryContext(ctx, `SELECT source, MAX(last_success_at) FROM sync_state WHERE source IN ('x','youtube') GROUP BY source`)
	if err != nil {
		return nil, fmt.Errorf("%w: sync state unavailable", ErrUnavailable)
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var at sql.NullString
		if err := rows.Scan(&source, &at); err != nil {
			return nil, fmt.Errorf("%w: sync state unavailable", ErrUnavailable)
		}
		out[source] = nullString(at)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: sync state unavailable", ErrUnavailable)
	}
	return out, nil
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
			// These provider usage fields contain numbers, never credentials.
			// All other token-like fields retain the generic redaction below.
			if lower == "inputtokens" || lower == "outputtokens" || lower == "maxtokens" {
				if n, ok := item.(float64); ok && n >= 0 && n <= 1_000_000_000 && n == math.Trunc(n) {
					out[k] = n
				}
				continue
			}
			if k == "stopReason" && (item == "max_tokens" || item == "max_output_tokens") {
				out[k] = item
				continue
			}
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
