package history

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

const (
	SourceX       = "x"
	SourceYoutube = "youtube"

	xSearchURL       = "https://api.x.com/2/tweets/search/all"
	youtubeSearchURL = "https://www.googleapis.com/youtube/v3/search"
	// At most 100 X posts or 50 YouTube entries per page, including expansions.
	maxResponseBytes = 4 * 1024 * 1024
	// YouTube search stops returning results after this many entries.
	youtubeSearchLimit = 500
)

// Error is a controlled failure code. Response bodies and transport messages
// can echo credentials, so only these codes reach run history and logs.
type Error struct {
	Code string
	// Requested is how many API pages the failed fetch consumed.
	Requested int
}

func (e *Error) Error() string { return e.Code }

// State is one sync_state row.
type State struct {
	Found      bool
	CursorID   string
	CursorTime string
	Metadata   map[string]any
}

// Backfill describes how a "before" page advances the backfill cursor.
type Backfill struct {
	// Pending keeps the interval open because more pages remain.
	Pending bool
	// SearchLimitReached restarts the next backfill from the oldest stored
	// record because the API cannot page any further into this interval.
	SearchLimitReached bool
}

// Page is one API page and the checkpoint that must commit with it.
type Page struct {
	Source, Account, CheckpointKey string
	// Checkpoint names the sync_state source of the continuation; empty means
	// Source+"-window". Timeline windows keep their own, because a timeline
	// cursor is not an API next_token.
	Checkpoint string
	// HeadFetchedAt records a successful read of the newest timeline page.
	// Resumed pages retain it; checkpoints from older releases leave it unknown.
	HeadFetchedAt string
	// CheckpointOnly clears a stale timeline cursor without a successful read.
	CheckpointOnly     bool
	Window             Window
	Next               string
	Discovered         int
	Posts              []xsource.Post
	ScheduleCandidates []xsource.ScheduleCandidate
	Videos             []ytsource.Video
	Backfill           *Backfill
	// KeepStored leaves posts that are already stored untouched, together with
	// their media links and schedule candidates.
	KeepStored bool
}

// Written is what one committed page stored.
type Written struct {
	// Posts counts the X posts written; ScheduleAssets the schedule candidates
	// registered with them.
	Posts, ScheduleAssets int
}

// Store persists pages atomically with their continuation.
type Store interface {
	SyncState(ctx context.Context, source, account string) (State, error)
	OldestSourceTimestamp(ctx context.Context, source, account string) (*string, error)
	WriteHistoryPage(ctx context.Context, page Page) (Written, error)
}

type Options struct {
	Timeout      time.Duration
	RequestDelay time.Duration
	// MaxPages bounds one fetch; a repeated fetch continues from the checkpoint.
	MaxPages         int
	ScheduleKeywords []string
}

// OptionsFromEnv reads the same bounds the recent synchronization uses.
func OptionsFromEnv(getenv func(string) string) Options {
	bounded := func(key string, fallback, maximum int) int {
		value, err := strconv.Atoi(strings.TrimSpace(getenv(key)))
		if err != nil || value < 1 {
			return fallback
		}
		return min(value, maximum)
	}
	delay, err := strconv.Atoi(strings.TrimSpace(getenv("SYNC_REQUEST_DELAY_MS")))
	if err != nil || delay < 0 {
		delay = 0
	}
	return Options{
		Timeout:          time.Duration(bounded("SYNC_TIMEOUT_MS", 7000, 60_000)) * time.Millisecond,
		RequestDelay:     time.Duration(min(delay, 10_000)) * time.Millisecond,
		MaxPages:         bounded("SOURCE_HISTORY_MAX_PAGES", 4, 20),
		ScheduleKeywords: xsource.DefaultOptions().ScheduleKeywords,
	}
}

// Fetcher calls only the two fixed API endpoints. Redirects are rejected so a
// credential header never follows one to another host.
type Fetcher struct {
	HTTP    *http.Client
	Store   Store
	Options Options
	Now     func() time.Time
	// Timeline serves X windows from the public FxTwitter timeline; nil
	// leaves X windows to the official API alone.
	Timeline Timeline
}

func NewFetcher(store Store, options Options, transport http.RoundTripper) *Fetcher {
	if options.Timeout <= 0 {
		options.Timeout = 7 * time.Second
	}
	if options.MaxPages < 1 {
		options.MaxPages = 4
	}
	client := &http.Client{
		Timeout:       options.Timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Fetcher{HTTP: client, Store: store, Options: options, Now: time.Now}
}

// Request selects one account's window. Credential is the source API
// credential and is sent only as a request header.
type Request struct {
	Source, Account, Credential string
	Window                      Window
	// PageLimit optionally lowers Options.MaxPages for this fetch.
	PageLimit int
}

type Result struct {
	Count, Discovered, Requested, ScheduleAssets int
	Window                                       Window
	HasMore                                      bool
	Errors                                       []string
}

func (f *Fetcher) apiJSON(ctx context.Context, endpoint string, query url.Values, header, credential string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, &Error{Code: "source_api_request_failed"}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set(header, credential)
	response, err := f.HTTP.Do(request)
	if err != nil {
		return nil, &Error{Code: "source_api_request_failed"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &Error{Code: "source_api_http_" + strconv.Itoa(response.StatusCode)}
	}
	if response.ContentLength > maxResponseBytes {
		return nil, &Error{Code: "source_api_response_too_large"}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &Error{Code: "source_api_request_failed"}
	}
	if len(body) > maxResponseBytes {
		return nil, &Error{Code: "source_api_response_too_large"}
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return nil, &Error{Code: "source_api_request_failed"}
	}
	return payload, nil
}

func object(value any) map[string]any { out, _ := value.(map[string]any); return out }
func list(value any) []any            { out, _ := value.([]any); return out }
func text(value any) string           { out, _ := value.(string); return out }

// Fetch reads up to the page budget of one account's window. A failure on the
// first page returns an *Error; later failures stop paging and are reported in
// Result.Errors with everything already committed.
func (f *Fetcher) Fetch(ctx context.Context, request Request) (Result, error) {
	if request.Source != SourceX && request.Source != SourceYoutube {
		return Result{}, invalid("unsupported source")
	}
	if strings.TrimSpace(request.Credential) == "" {
		if request.Source == SourceX {
			return Result{}, invalid("X_API_BEARER_TOKEN is required for history search")
		}
		return Result{}, invalid("YOUTUBE_API_KEY is required for history search")
	}
	input, err := request.Window.Normalize()
	if err != nil {
		return Result{}, err
	}
	if input.Mode == ModeCount {
		return Result{}, invalid("count windows read the public X timeline")
	}
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
	backfillState, err := f.Store.SyncState(ctx, request.Source+"-backfill", request.Account)
	if err != nil {
		return Result{}, err
	}
	oldest := ""
	if stored, lookupErr := f.Store.OldestSourceTimestamp(ctx, request.Source, request.Account); lookupErr != nil {
		return Result{}, lookupErr
	} else if stored != nil {
		oldest = *stored
	}
	if cursor, ok := parseTime(backfillState.CursorTime); ok && oldest != "" {
		if stored, storedOK := parseTime(oldest); storedOK && cursor.Before(stored) {
			oldest = backfillState.CursorTime
		}
	}
	window, err := input.Resolve(oldest, now)
	if err != nil {
		return Result{}, err
	}
	// Finish an interrupted backfill interval before opening another interval.
	if input.Mode == ModeBefore {
		if pending, ok := windowFromMetadata(backfillState.Metadata["pendingWindow"]); ok {
			window = pending
		}
	}
	key := input.checkpointKey(request.Account)
	checkpoint, err := f.Store.SyncState(ctx, request.Source+"-window", key)
	if err != nil {
		return Result{}, err
	}
	next := checkpoint.CursorID
	previousDiscovered := 0
	if next != "" {
		if saved, ok := windowFromMetadata(checkpoint.Metadata["window"]); ok {
			savedEnd, _ := parseTime(saved.EndTime)
			start, _ := parseTime(window.StartTime)
			if input.Mode != ModeRecent || savedEnd.After(start) {
				window = saved
			} else {
				next = ""
			}
		}
	}
	if next != "" {
		if count, ok := checkpoint.Metadata["discovered"].(float64); ok && count > 0 {
			previousDiscovered = int(count)
		}
	}
	// X requires an end time at least ten seconds behind the present. Keep it
	// stable over every page, including a resumed page.
	if request.Source == SourceX && next == "" {
		if end, _ := parseTime(window.EndTime); end.After(now.Add(-10 * time.Second)) {
			window.EndTime = formatTime(now.Add(-10 * time.Second))
		}
	}
	start, _ := parseTime(window.StartTime)
	end, _ := parseTime(window.EndTime)
	if !start.Before(end) {
		return Result{}, invalid("fetch window must begin in the past")
	}
	maxPages := f.Options.MaxPages
	if request.PageLimit > 0 {
		maxPages = request.PageLimit
	}
	result := Result{Window: window, Errors: []string{}}
	for {
		query := url.Values{}
		endpoint, header, credential := xSearchURL, "Authorization", "Bearer "+request.Credential
		if request.Source == SourceX {
			query.Set("query", "from:"+request.Account)
			query.Set("start_time", window.StartTime)
			query.Set("end_time", window.EndTime)
			query.Set("max_results", "100")
			query.Set("sort_order", "recency")
			query.Set("tweet.fields", "created_at,public_metrics,attachments,author_id")
			query.Set("expansions", "attachments.media_keys,author_id")
			query.Set("media.fields", "url,preview_image_url,alt_text")
			query.Set("user.fields", "username,name,profile_image_url")
			if next != "" {
				query.Set("next_token", next)
			}
		} else {
			endpoint, header, credential = youtubeSearchURL, "X-Goog-Api-Key", request.Credential
			query.Set("part", "snippet")
			query.Set("channelId", request.Account)
			query.Set("type", "video")
			query.Set("order", "date")
			query.Set("publishedAfter", formatTime(start.Add(-time.Second)))
			query.Set("publishedBefore", window.EndTime)
			query.Set("maxResults", "50")
			if next != "" {
				query.Set("pageToken", next)
			}
		}
		result.Requested++
		page := Page{Source: request.Source, Account: request.Account, CheckpointKey: key, Window: window}
		payload, fetchErr := f.apiJSON(ctx, endpoint, query, header, credential)
		entries := 0
		if fetchErr == nil {
			entries, fetchErr = f.mapPage(request, payload, &page)
		}
		if fetchErr != nil {
			var controlled *Error
			if !errors.As(fetchErr, &controlled) {
				controlled = &Error{Code: "source_api_request_failed"}
			}
			if result.Requested == 1 {
				return result, &Error{Code: controlled.Code, Requested: result.Requested}
			}
			result.Errors = append(result.Errors, controlled.Code)
			break
		}
		result.Discovered += entries
		following := text(object(payload["meta"])["next_token"])
		if request.Source == SourceYoutube {
			following = text(payload["nextPageToken"])
		}
		if following != "" && following == next {
			result.Errors = append(result.Errors, "source_api_repeated_page")
			break
		}
		page.Next = following
		page.Discovered = previousDiscovered + result.Discovered
		if input.Mode == ModeBefore {
			page.Backfill = &Backfill{
				Pending:            following != "",
				SearchLimitReached: request.Source == SourceYoutube && page.Discovered >= youtubeSearchLimit,
			}
		}
		// Persist the page and continuation together, so retries cannot skip it.
		written, writeErr := f.Store.WriteHistoryPage(ctx, page)
		if writeErr != nil {
			return result, writeErr
		}
		if request.Source == SourceX {
			result.Count += len(page.Posts)
			result.ScheduleAssets += written.ScheduleAssets
		} else {
			result.Count += len(page.Videos)
		}
		next = following
		if next == "" || result.Requested >= maxPages {
			break
		}
		if f.Options.RequestDelay > 0 {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(f.Options.RequestDelay):
			}
		}
	}
	if next != "" && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, "history_page_budget_reached; repeat this fetch to continue")
	}
	if request.Source == SourceYoutube && previousDiscovered+result.Discovered >= youtubeSearchLimit {
		result.Errors = append(result.Errors, "youtube_search_limit; use a narrower date range")
	}
	result.HasMore = next != ""
	return result, nil
}

// mapPage validates one API payload and keeps the records inside the window.
// It returns how many entries the API listed.
func (f *Fetcher) mapPage(request Request, payload map[string]any, page *Page) (int, error) {
	if len(list(payload["errors"])) > 0 || payload["error"] != nil {
		return 0, &Error{Code: "source_api_invalid_response"}
	}
	if request.Source == SourceX {
		data, isList := payload["data"].([]any)
		if (payload["data"] != nil && !isList) || object(payload["meta"]) == nil {
			return 0, &Error{Code: "source_api_invalid_response"}
		}
		includes := object(payload["includes"])
		for _, item := range data {
			entry := object(item)
			var author map[string]any
			for _, candidate := range list(includes["users"]) {
				if user := object(candidate); user != nil && text(user["id"]) != "" && text(user["id"]) == text(entry["author_id"]) {
					author = user
					break
				}
			}
			if name := text(author["username"]); name != "" && !strings.EqualFold(name, request.Account) {
				return 0, &Error{Code: "source_api_invalid_records"}
			}
			keys := map[string]bool{}
			for _, mediaKey := range list(object(entry["attachments"])["media_keys"]) {
				keys[text(mediaKey)] = true
			}
			media := []any{}
			for _, candidate := range list(includes["media"]) {
				item := object(candidate)
				if !keys[text(item["media_key"])] {
					continue
				}
				if link := text(item["url"]); link != "" {
					media = append(media, link)
				} else if link = text(item["preview_image_url"]); link != "" {
					media = append(media, link)
				}
			}
			metrics := object(entry["public_metrics"])
			normalized := map[string]any{
				"id": entry["id"], "text": entry["text"], "created_at": entry["created_at"],
				"author":    map[string]any{"screenName": request.Account, "name": author["name"]},
				"mediaURLs": media,
				"likes":     metrics["like_count"], "retweets": metrics["retweet_count"], "replies": metrics["reply_count"],
			}
			post := xsource.Normalize(normalized, text(entry["id"]), request.Account)
			if post == nil {
				return 0, &Error{Code: "source_api_invalid_records"}
			}
			// The API page is read without referenced statuses.
			post.QuoteUnread = true
			if !page.Window.Contains(post.PublishedAt) {
				continue
			}
			page.Posts = append(page.Posts, *post)
			if len(post.MediaURLs) > 0 && matchesKeyword(post.Text, f.Options.ScheduleKeywords) {
				page.ScheduleCandidates = append(page.ScheduleCandidates, xsource.ScheduleCandidate{PostID: post.ID, SourceURL: post.URL, MediaURL: post.MediaURLs[0], AccountHandle: request.Account})
			}
		}
		return len(data), nil
	}
	items, isList := payload["items"].([]any)
	if !isList {
		return 0, &Error{Code: "source_api_invalid_response"}
	}
	for _, item := range items {
		entry := object(item)
		snippet := object(entry["snippet"])
		id := text(object(entry["id"])["videoId"])
		title := text(snippet["title"])
		published, ok := parseTime(text(snippet["publishedAt"]))
		if id == "" || title == "" || text(snippet["channelId"]) != request.Account || !ok {
			return 0, &Error{Code: "source_api_invalid_records"}
		}
		video := ytsource.Video{
			ID: id, Title: title, PublishedAt: formatTime(published),
			URL:        "https://www.youtube.com/watch?v=" + id,
			IsUpcoming: text(snippet["liveBroadcastContent"]) == "upcoming",
		}
		thumbnails := object(snippet["thumbnails"])
		if video.ThumbnailURL = text(object(thumbnails["high"])["url"]); video.ThumbnailURL == "" {
			video.ThumbnailURL = text(object(thumbnails["default"])["url"])
		}
		video.Kind = ytsource.Classify(title)
		if video.IsUpcoming {
			video.Kind = "UPCOMING LIVE"
		}
		if page.Window.Contains(video.PublishedAt) {
			page.Videos = append(page.Videos, video)
		}
	}
	return len(items), nil
}

func matchesKeyword(value string, keywords []string) bool {
	lower := strings.ToLower(value)
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}
