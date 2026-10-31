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
	"unicode/utf8"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

const (
	SourceX       = "x"
	SourceYoutube = "youtube"

	xSearchURL = "https://api.x.com/2/tweets/search/all"
	// xArchiveStart begins a count search. Without a start_time the search
	// covers only 30 days; status IDs, and so every post this board reads,
	// begin with the Snowflake epoch in November 2010.
	xArchiveStart = "2010-11-04T00:00:00.000Z"
	// At most 100 X posts per page, including expansions.
	maxResponseBytes = 4 * 1024 * 1024
	// MaxItems bounds the new records one result names.
	MaxItems = 20
	// itemTitleRunes bounds an X item's title, the start of its text.
	itemTitleRunes = 80
)

// Error is a controlled failure code. Response bodies and transport messages
// can echo credentials, so only these codes reach run history and logs.
type Error struct {
	Code string
	// Requested is how many pages the failed fetch consumed.
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
}

// Page is one listing page and the checkpoint that must commit with it.
type Page struct {
	Source, Account string
	// CheckpointKey names the continuation; empty writes none, for a listing
	// read in one request.
	CheckpointKey string
	// Checkpoint names the sync_state source of the continuation; empty means
	// Source+"-window". Timeline fetches keep their own, because a timeline
	// cursor is not an API next_token.
	Checkpoint string
	// HeadFetchedAt records a successful read of the newest timeline page.
	// Resumed pages retain it; a checkpoint without it leaves it unknown.
	HeadFetchedAt string
	// CheckpointOnly clears a stale continuation without a successful read.
	CheckpointOnly     bool
	Window             Window
	Next               string
	Discovered         int
	Posts              []xsource.Post
	ScheduleCandidates []xsource.ScheduleCandidate
	Videos             []ytsource.Video
	// Described are the listed videos the Data API answered for, written
	// through the recent step's upsert, and Localized their localized titles.
	Described []ytsource.Video
	Localized map[string]map[string]string
	Backfill  *Backfill
	// KeepStored leaves records that are already stored untouched, together
	// with their media links and schedule candidates. X pages always keep
	// them; YouTube pages unless the fetch refreshes.
	KeepStored bool
}

// Written is what one committed page stored.
type Written struct {
	// New counts the records stored for the first time and Stored the ones
	// that already were; ScheduleAssets the schedule candidates registered.
	New, Stored, ScheduleAssets int
	// NewIDs are the new records, in page order.
	NewIDs []string
}

// Store persists pages atomically with their continuation.
type Store interface {
	SyncState(ctx context.Context, source, account string) (State, error)
	OldestSourceTimestamp(ctx context.Context, source, account string) (*string, error)
	WriteHistoryPage(ctx context.Context, page Page) (Written, error)
	// StoredYoutubeVideos reads the stored rows of the given video IDs.
	StoredYoutubeVideos(ctx context.Context, ids []string) (map[string]ytsource.Video, error)
}

type Options struct {
	Timeout      time.Duration
	RequestDelay time.Duration
	// MaxPages bounds one fetch; a repeated fetch continues from the checkpoint.
	MaxPages         int
	ScheduleKeywords []string
}

// DefaultOptions are the bounds before the operator's sync settings apply
// (see SetLimits).
func DefaultOptions() Options {
	return Options{
		Timeout:          7 * time.Second,
		MaxPages:         4,
		ScheduleKeywords: xsource.DefaultOptions().ScheduleKeywords,
	}
}

// Fetcher serves each method: X full-archive search over its one fixed
// endpoint, with redirects rejected so the credential header never follows
// one, and the timeline and uploads listings through the source adapters.
type Fetcher struct {
	HTTP    *http.Client
	Store   Store
	Options Options
	Now     func() time.Time
	// Timeline serves the X timeline method; nil leaves it unavailable.
	Timeline Timeline
	// Uploads serves the YouTube API and RSS methods; nil leaves them
	// unavailable.
	Uploads Uploads
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

// SetLimits applies the operator's page cap, request timeout and request
// spacing. The workflow sets them before each source step, never while a
// fetch is reading.
func (f *Fetcher) SetLimits(maxPages int, timeout, delay time.Duration) {
	if maxPages > 0 {
		f.Options.MaxPages = maxPages
	}
	if timeout > 0 {
		f.Options.Timeout = timeout
		if f.HTTP != nil {
			f.HTTP.Timeout = timeout
		}
	}
	if delay >= 0 {
		f.Options.RequestDelay = delay
	}
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Request selects one account's fetch. Credential is the X API credential and
// is sent only as a request header.
type Request struct {
	Source, Account, Credential string
	Window                      Window
	// PageLimit optionally lowers Options.MaxPages for this fetch.
	PageLimit int
}

// Item is a record a fetch stored for the first time, as the console lists
// it: an X post's text start or a video's title.
type Item struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Account string `json:"account,omitempty"`
}

// Result counts one fetch. Listed records are the ones the selection took
// from the listing: New, Stored (already stored, refreshed or not) and Failed
// (listed without usable content) split them. Requested counts pages. A
// YouTube API fetch also counts its Data API calls (quota units) in
// APIRequests and the videos their details answered for in Inspected.
type Result struct {
	Listed, New, Stored, Failed, Requested, ScheduleAssets int
	APIRequests, Inspected                                 int
	// PostsRead counts the posts X API answers returned, which X bills and
	// the day's X API usage counts.
	PostsRead int
	// Items names the first MaxItems new records.
	Items   []Item
	Window  Window
	HasMore bool
	Errors  []string
}

// add folds a committed page into the result. titles maps the page's record
// IDs to their items.
func (r *Result) add(written Written, titles map[string]Item) {
	r.New += written.New
	r.Stored += written.Stored
	r.ScheduleAssets += written.ScheduleAssets
	for _, id := range written.NewIDs {
		if item, ok := titles[id]; ok && len(r.Items) < MaxItems {
			r.Items = append(r.Items, item)
		}
	}
}

func postItems(posts []xsource.Post, account string) map[string]Item {
	out := map[string]Item{}
	for _, post := range posts {
		out[post.ID] = Item{ID: post.ID, Title: postTitle(post.Text), Account: account}
	}
	return out
}

// postTitle is the first line of a post's text, cut to itemTitleRunes.
func postTitle(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > itemTitleRunes {
		line = string([]rune(line)[:itemTitleRunes-1]) + "…"
	}
	return line
}

func (f *Fetcher) pageLimit(request Request) int {
	if request.PageLimit > 0 {
		return request.PageLimit
	}
	return f.Options.MaxPages
}

// wait spaces requests by Options.RequestDelay.
func (f *Fetcher) wait(ctx context.Context) error {
	if f.Options.RequestDelay <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(f.Options.RequestDelay):
		return nil
	}
}

const budgetMessage = "history_page_budget_reached; repeat this fetch to continue"

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

// FetchSearch reads up to the page budget of one X account's selection through
// full-archive search: its newest Count posts, a date range, or a backfill
// before the oldest stored post. A failure on the first page returns an
// *Error; later failures stop paging and are reported in Result.Errors with
// everything already committed. Stored posts keep what they have.
func (f *Fetcher) FetchSearch(ctx context.Context, request Request) (Result, error) {
	if request.Source != SourceX {
		return Result{}, invalid("unsupported source")
	}
	if strings.TrimSpace(request.Credential) == "" {
		return Result{}, invalid("X_API_BEARER_TOKEN is required for the X API method")
	}
	input, err := request.Window.Normalize(SourceX)
	if err != nil {
		return Result{}, err
	}
	if input.Method != MethodAPI {
		return Result{}, ErrRetired
	}
	now := f.now()
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
	window, err := input.Resolve(oldest)
	if err != nil {
		return Result{}, err
	}
	if input.Mode == ModeCount {
		window.StartTime, window.EndTime = xArchiveStart, formatTime(now)
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
	previous := 0
	if next != "" {
		if saved, ok := windowFromMetadata(checkpoint.Metadata["window"]); ok {
			window = saved
		}
		if count, ok := checkpoint.Metadata["discovered"].(float64); ok && count > 0 {
			previous = int(count)
		}
	}
	// X requires an end time at least ten seconds behind the present. Keep it
	// stable over every page, including a resumed page.
	if next == "" {
		if end, _ := parseTime(window.EndTime); end.After(now.Add(-10 * time.Second)) {
			window.EndTime = formatTime(now.Add(-10 * time.Second))
		}
	}
	start, _ := parseTime(window.StartTime)
	end, _ := parseTime(window.EndTime)
	if !start.Before(end) {
		return Result{}, invalid("fetch window must begin in the past")
	}
	maxPages := f.pageLimit(request)
	result := Result{Window: window, Errors: []string{}, Items: []Item{}}
	for {
		size := 100
		if input.Mode == ModeCount {
			// The endpoint answers 10 to 100 posts a page.
			size = max(10, min(size, window.Count-previous-result.Listed))
		}
		query := url.Values{}
		query.Set("query", "from:"+request.Account)
		query.Set("start_time", window.StartTime)
		query.Set("end_time", window.EndTime)
		query.Set("max_results", strconv.Itoa(size))
		query.Set("sort_order", "recency")
		setPostFields(query)
		if next != "" {
			query.Set("next_token", next)
		}
		result.Requested++
		page := Page{Source: request.Source, Account: request.Account, CheckpointKey: key, Window: window, KeepStored: true}
		payload, fetchErr := f.apiJSON(ctx, xSearchURL, query, "Authorization", "Bearer "+request.Credential)
		if fetchErr == nil {
			var read int
			read, fetchErr = f.mapSearchPage(request, payload, &page)
			result.PostsRead += read
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
		following := text(object(payload["meta"])["next_token"])
		if input.Mode == ModeCount {
			if left := window.Count - previous - result.Listed; len(page.Posts) >= left {
				page.Posts, following = page.Posts[:left], ""
				kept := map[string]bool{}
				for _, post := range page.Posts {
					kept[post.ID] = true
				}
				candidates := page.ScheduleCandidates[:0]
				for _, candidate := range page.ScheduleCandidates {
					if kept[candidate.PostID] {
						candidates = append(candidates, candidate)
					}
				}
				page.ScheduleCandidates = candidates
			}
		}
		if following != "" && following == next {
			result.Errors = append(result.Errors, "source_api_repeated_page")
			break
		}
		result.Listed += len(page.Posts)
		page.Next = following
		page.Discovered = previous + result.Listed
		if input.Mode == ModeBefore {
			page.Backfill = &Backfill{Pending: following != ""}
		}
		// Persist the page and continuation together, so retries cannot skip it.
		written, writeErr := f.Store.WriteHistoryPage(ctx, page)
		if writeErr != nil {
			return result, writeErr
		}
		result.add(written, postItems(page.Posts, request.Account))
		next = following
		if next == "" || result.Requested >= maxPages {
			break
		}
		if err := f.wait(ctx); err != nil {
			return result, err
		}
	}
	if next != "" && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, budgetMessage)
	}
	result.HasMore = next != ""
	return result, nil
}

// mapSearchPage validates one search payload and keeps the posts inside the
// window, mapped as the timed chain's API method maps them. It reports how
// many posts the answer returned.
func (f *Fetcher) mapSearchPage(request Request, payload map[string]any, page *Page) (int, error) {
	// A referenced status X cannot return is reported in errors beside data.
	if !validPostsAnswer(payload) {
		return 0, &Error{Code: "source_api_invalid_response"}
	}
	posts, read, _, err := xsource.APIPosts(payload, request.Account)
	if err != nil {
		return read, &Error{Code: "source_api_invalid_records"}
	}
	for _, post := range posts {
		if !page.Window.Contains(post.PublishedAt) {
			continue
		}
		page.Posts = append(page.Posts, post)
		if len(post.MediaURLs) > 0 && matchesKeyword(post.Text, f.Options.ScheduleKeywords) {
			page.ScheduleCandidates = append(page.ScheduleCandidates, xsource.ScheduleCandidate{PostID: post.ID, SourceURL: post.URL, MediaURL: post.MediaURLs[0], AccountHandle: request.Account})
		}
	}
	return read, nil
}

// validPostsAnswer reports a posts answer's shape: a data list (absent when
// empty) and meta. A referenced status X cannot return is reported in errors
// beside data, so errors alone fail only an answer without data.
func validPostsAnswer(payload map[string]any) bool {
	_, isList := payload["data"].([]any)
	return payload["error"] == nil && object(payload["meta"]) != nil &&
		(isList || (payload["data"] == nil && len(list(payload["errors"])) == 0))
}

// setPostFields asks for what the shared mapping reads: long text, links,
// counters, the post's media and the quoted status with its author and
// media.
func setPostFields(query url.Values) {
	query.Set("tweet.fields", "created_at,public_metrics,attachments,author_id,entities,note_tweet,referenced_tweets")
	query.Set("expansions", "attachments.media_keys,author_id,referenced_tweets.id,referenced_tweets.id.author_id,referenced_tweets.id.attachments.media_keys")
	query.Set("media.fields", "type,url,preview_image_url")
	query.Set("user.fields", "username,name")
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
