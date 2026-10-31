package x

import (
	"context"
	"encoding/json"
	"errors"

	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	profileHost            = "x.com"
	timelineHost           = "api.fxtwitter.com"
	detailHost             = "api.vxtwitter.com"
	userAgent              = "kano-navi-go/x-recent"
	maxResponseBytes int64 = 4 * 1024 * 1024
	// timelinePageSize is FxTwitter's default page; one page usually reaches a
	// stored post, so later pages are requested only to close a gap.
	timelinePageSize = 20
	maxTimelinePages = 10
	// MaxTimelinePageSize is FxTwitter's largest page, which operator windows
	// request so a long window needs few pages.
	MaxTimelinePageSize = 100
)

type ErrorCode string

const (
	ErrRateLimited     ErrorCode = "x_rate_limited"
	ErrTimeout         ErrorCode = "x_timeout"
	ErrServer          ErrorCode = "x_upstream_5xx"
	ErrInvalidJSON     ErrorCode = "x_invalid_json"
	ErrOversized       ErrorCode = "x_response_oversized"
	ErrRedirect        ErrorCode = "x_redirect_rejected"
	ErrHTTP            ErrorCode = "x_http_error"
	ErrInvalidResponse ErrorCode = "x_invalid_response"
)

type Error struct {
	Code   ErrorCode
	Status int
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Client only permits the public profile, FxTwitter timeline and vxtwitter
// detail hosts.
type Client struct{ HTTP *http.Client }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if httpClient.Timeout <= 0 {
		httpClient.Timeout = 10 * time.Second
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{HTTP: httpClient}
}
func (c *Client) request(ctx context.Context, host, path string, query url.Values) ([]byte, error) {
	if host != profileHost && host != timelineHost && host != detailHost {
		return nil, &Error{Code: ErrHTTP, Err: errors.New("host not allowlisted")}
	}
	u := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Error{Code: ErrHTTP, Err: err}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{Code: ErrTimeout, Err: err}
		}
		return nil, &Error{Code: ErrHTTP, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &Error{Code: ErrRedirect, Status: resp.StatusCode}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &Error{Code: ErrRateLimited, Status: resp.StatusCode}
	}
	if resp.StatusCode >= 500 {
		return nil, &Error{Code: ErrServer, Status: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{Code: ErrHTTP, Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &Error{Code: ErrHTTP, Err: err}
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, &Error{Code: ErrOversized}
	}
	return body, nil
}
func (c *Client) Profile(ctx context.Context, account string) ([]byte, error) {
	return c.request(ctx, profileHost, "/"+url.PathEscape(account), nil)
}

// Timeline reads one newest-first page of up to count of the account's
// statuses. An empty cursor requests the first page.
func (c *Client) Timeline(ctx context.Context, account, cursor string, count int) ([]byte, error) {
	query := url.Values{"count": {strconv.Itoa(min(max(count, 1), MaxTimelinePageSize))}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	return c.request(ctx, timelineHost, "/2/profile/"+url.PathEscape(account)+"/statuses", query)
}
func (c *Client) Detail(ctx context.Context, account, id string) ([]byte, error) {
	return c.request(ctx, detailHost, "/"+url.PathEscape(account)+"/status/"+url.PathEscape(id), nil)
}

type Options struct {
	BootstrapDays     int
	DiscoveryLimit    int
	MaxDetailRequests int
	RefreshKnown      int
	Handles           []string
	ScheduleKeywords  []string
	PendingIDs        []string
	// PendingRetries holds the consecutive failures of pending IDs.
	PendingRetries map[string]PendingRetry
	KnownIDs       map[string]bool
	// CursorID is the account's newest stored status; timeline paging stops
	// once it reaches it.
	CursorID string
	// Gap is the unread stretch an earlier capped run left, read after the
	// newest pages.
	Gap *Gap
	// RefreshIDs are stored statuses an operator asked to read again. Each
	// takes a detail request ahead of the rest, even when a timeline page
	// carries it or a failure backoff holds it.
	RefreshIDs []string
	// RefreshOnly reads RefreshIDs alone: no discovery, pending IDs or
	// known-post refreshes. A count setting, whose timeline read leaves the
	// incremental state as it is, serves operator refreshes this way.
	RefreshOnly bool
	// Chain lists the listing methods a run tries in order (ADR-0004):
	// DiscoveryTimeline, DiscoveryProfile and DiscoveryAPI. Empty tries the
	// timeline, then the profile page.
	Chain []string
	// APIGate opens the API method only once TimelineFailures, counting a
	// timeline failure of this run, reaches it; 0 leaves it open.
	APIGate, TimelineFailures int
	// APIBudget is how many posts API answers may still return today; below
	// one minimal page the chain skips the API.
	APIBudget int
	// GapCeilings holds the oldest stored status above each gap floor, which
	// bounds the API read that closes the gap.
	GapCeilings map[string]string
}

func DefaultOptions() Options {
	return Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 12, RefreshKnown: 1, ScheduleKeywords: []string{"schedule", "スケジュール", "配信予定", "週間予定", "今週の予定"}}
}

// OptionsFromEnv reads the accounts from X_HANDLES. The read bounds are the
// defaults until a run applies the operator's sync settings.
func OptionsFromEnv() Options {
	o := DefaultOptions()
	raw := strings.FieldsFunc(getenv("X_HANDLES", "kano_2525,_Kanotic"), func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\n' || r == '\t' })
	for _, h := range raw {
		h = strings.TrimPrefix(h, "@")
		if regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`).MatchString(h) {
			o.Handles = append(o.Handles, h)
		}
	}
	if len(o.Handles) == 0 {
		o.Handles = []string{"kano_2525", "_Kanotic"}
	}
	return o
}
func getenv(k, d string) string {
	if v := strings.TrimSpace(getenvRaw(k)); v != "" {
		return v
	}
	return d
}

var getenvRaw = func(k string) string { return os.Getenv(k) }

// SetEnvReader is useful for tests and avoids exposing process credentials to logs.
func SetEnvReader(fn func(string) string) {
	if fn != nil {
		getenvRaw = fn
	}
}

var statusIDPattern = regexp.MustCompile(`^\d{10,22}$`)

// DiscoverIDs lists the IDs of the account's own status links on a profile
// page, plain or URL-encoded. Links of other handles, such as quoted
// statuses and reposts, are left out as the timeline leaves them out.
func DiscoverIDs(body []byte, account string, limit int) []string {
	pattern := regexp.MustCompile(`(?i)(?:/|%2F)` + regexp.QuoteMeta(account) + `(?:/|%2F)status(?:/|%2F)(\d{10,})`)
	seen := map[string]bool{}
	out := []string{}
	for _, m := range pattern.FindAllSubmatch(body, -1) {
		id := string(m[1])
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

type Post struct {
	ID, AccountHandle, Type, Label, Text, PublishedAt, URL string
	Likes, Reposts, Replies                                int
	MediaURLs                                              []string
	MediaAlt                                               string
	AuthorScreenName                                       string
	// Quote is the status the post quotes, nil when it quotes none. Its
	// images are never the post's own MediaURLs.
	Quote *Quote
	Raw   map[string]any
}
type ScheduleCandidate struct{ PostID, SourceURL, MediaURL, AccountHandle string }
type Result struct {
	Account                         string
	Posts                           []Post
	DiscoveredCount, RequestedCount int
	Errors                          []string
	NextCursor, NextCursorTime      string
	PendingIDs                      []string
	// PendingRetries is the failure state of the IDs still pending.
	PendingRetries map[string]PendingRetry
	// Held counts pending IDs left for a later run by their backoff; Dropped
	// lists the IDs the run stopped requesting.
	Held               int
	Dropped            []DroppedStatus
	ScheduleCandidates []ScheduleCandidate
	// FromListing marks posts read from a timeline or API page without a
	// detail request. A stored one keeps the values it has.
	FromListing map[string]bool
	// Discovery names the chain method that served the account:
	// DiscoveryTimeline, DiscoveryProfile or DiscoveryAPI; empty when none did.
	Discovery string
	// Tried lists the chain methods the run tried, in order, with their
	// controlled codes; the one that served has none.
	Tried []Attempt
	// TimelineFailed reports a timeline that was tried and did not serve.
	TimelineFailed bool
	// APIRead counts the posts API answers returned.
	APIRead int
	// Gap is the account's unread timeline stretch after the run, nil when
	// there is none; Notes are controlled codes about it.
	Gap   *Gap
	Notes []string
	// Refreshed lists the requested refreshes whose detail request was
	// made, whatever it answered.
	Refreshed []string
	// RefreshOnly marks a run of Options.RefreshOnly; its write leaves the
	// account's incremental state as it is.
	RefreshOnly bool
}

type Adapter struct {
	Client  *Client
	Options Options
	Now     func() time.Time
	// API serves the chain's API method; nil leaves it unavailable.
	API APIReader
}

func NewAdapter(c *Client, options Options) *Adapter {
	if c == nil {
		c = NewClient(nil)
	}
	d := DefaultOptions()
	if options.DiscoveryLimit == 0 {
		options.DiscoveryLimit = d.DiscoveryLimit
	}
	if options.BootstrapDays == 0 {
		options.BootstrapDays = d.BootstrapDays
	}
	if options.MaxDetailRequests < 0 {
		options.MaxDetailRequests = d.MaxDetailRequests
	}
	if len(options.ScheduleKeywords) == 0 {
		options.ScheduleKeywords = d.ScheduleKeywords
	}
	if len(options.Handles) == 0 {
		options.Handles = d.Handles
	}
	return &Adapter{Client: c, Options: options, Now: time.Now}
}
func (a *Adapter) FetchRecent(ctx context.Context, account string, opts Options) (Result, error) {
	account = strings.TrimPrefix(strings.TrimSpace(account), "@")
	result := Result{Account: account}
	if !regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`).MatchString(account) {
		return result, &Error{Code: ErrInvalidResponse, Err: errors.New("invalid account")}
	}
	// The bootstrap age limit bounds only an account without a cursor; once
	// one is stored, reading goes back to it, through a gap when need be.
	var cutoff time.Time
	if opts.BootstrapDays > 0 && opts.CursorID == "" {
		now := time.Now()
		if a.Now != nil {
			now = a.Now()
		}
		cutoff = now.Add(-time.Duration(opts.BootstrapDays) * 24 * time.Hour)
	}
	var found discovery
	var err error
	if opts.RefreshOnly {
		if len(opts.RefreshIDs) == 0 {
			return result, &Error{Code: ErrInvalidResponse, Err: errors.New("no status ids")}
		}
		opts.PendingIDs, opts.PendingRetries, opts.RefreshKnown = nil, nil, 0
		result.RefreshOnly = true
	} else if found, err = a.discover(ctx, account, opts, cutoff, &result); err != nil {
		return result, err
	}
	ids := found.ids
	if !cutoff.IsZero() {
		filtered := make([]string, 0, len(ids))
		for _, id := range ids {
			// Synthetic/fake IDs used by local fixtures are intentionally left
			// eligible; real X Snowflakes are 15+ digits.
			if len(id) < 15 || snowflakeTime(id).IsZero() || !snowflakeTime(id).Before(cutoff) {
				filtered = append(filtered, id)
			}
		}
		ids = filtered
	}
	result.DiscoveredCount = len(ids)
	// An API read newer than the cursor may rightly find nothing.
	if len(ids) == 0 && len(opts.PendingIDs) == 0 && len(opts.RefreshIDs) == 0 && result.Discovery != DiscoveryAPI {
		return result, &Error{Code: ErrInvalidResponse, Err: errors.New("no status ids")}
	}
	queue := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if statusIDPattern.MatchString(id) && !seen[id] {
			queue = append(queue, id)
			seen[id] = true
		}
	}
	// An operator's refresh goes first and always takes a detail request.
	forced := map[string]bool{}
	for _, id := range opts.RefreshIDs {
		if add(id); seen[id] {
			forced[id] = true
		}
	}
	for _, id := range opts.PendingIDs {
		add(id)
	}
	for _, id := range ids {
		if !opts.KnownIDs[id] {
			add(id)
		}
	}
	// Known posts are refreshed through the detail request even when the
	// timeline lists them, so a stored post is rewritten from one shape only.
	refreshing := map[string]bool{}
	for id := range forced {
		refreshing[id] = true
	}
	refresh := opts.RefreshKnown
	for _, id := range ids {
		if opts.KnownIDs[id] && refresh > 0 {
			add(id)
			refreshing[id] = true
			refresh--
		}
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	result.FromListing = map[string]bool{}
	result.PendingRetries = map[string]PendingRetry{}
	successful := map[string]bool{}
	accept := func(post *Post) bool {
		if post.AuthorScreenName != "" && !strings.EqualFold(strings.TrimPrefix(post.AuthorScreenName, "@"), account) {
			return false
		}
		result.Posts = append(result.Posts, *post)
		successful[post.ID] = true
		if len(post.MediaURLs) > 0 && isSchedule(post.Text, opts.ScheduleKeywords) {
			result.ScheduleCandidates = append(result.ScheduleCandidates, ScheduleCandidate{PostID: post.ID, SourceURL: post.URL, MediaURL: post.MediaURLs[0], AccountHandle: account})
		}
		return true
	}
	// A status a timeline or API page carried is stored from the page,
	// pending or not; the rest wait for their backoff or take a detail request.
	detail := []string{}
	for _, id := range queue {
		if post := found.posts[id]; post != nil && !refreshing[id] && accept(post) {
			result.FromListing[id] = true
			continue
		}
		if retry, ok := opts.PendingRetries[id]; ok && retry.waiting(now) && !forced[id] {
			result.Held++
			continue
		}
		detail = append(detail, id)
	}
	detail = detail[:min(max(opts.MaxDetailRequests, 0), len(detail))]
	attempted := map[string]bool{}
	failed := []string{}
	foreign := 0
	var firstDetailErr error
	fail := func(id, code string, err error, gone bool) {
		if firstDetailErr == nil && err != nil {
			firstDetailErr = err
		}
		result.Errors = append(result.Errors, id+": "+code)
		retry, dropped := opts.PendingRetries[id].failed(gone, now)
		if dropped != "" {
			result.Dropped = append(result.Dropped, DroppedStatus{ID: id, Code: dropped})
			return
		}
		result.PendingRetries[id] = retry
		failed = append(failed, id)
	}
	for _, id := range detail {
		if ctx.Err() != nil {
			break
		}
		attempted[id] = true
		result.RequestedCount++
		body, e := a.Client.Detail(ctx, account, id)
		// A cancelled run is not a failure of the status: it stays pending
		// in its place.
		if e != nil && ctx.Err() != nil {
			delete(attempted, id)
			if firstDetailErr == nil {
				firstDetailErr = e
			}
			break
		}
		if e != nil {
			fail(id, safeCode(e), e, isGone(e))
			continue
		}
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			fail(id, string(ErrInvalidJSON), &Error{Code: ErrInvalidJSON}, false)
			continue
		}
		post := Normalize(payload, id, account)
		if post == nil || post.ID != id {
			fail(id, string(ErrInvalidResponse), &Error{Code: ErrInvalidResponse}, false)
			continue
		}
		// Another account's status never becomes this account's post. It is
		// no failure either: the ID leaves the queue at once.
		if !accept(post) {
			result.Dropped = append(result.Dropped, DroppedStatus{ID: id, Code: DroppedForeign})
			foreign++
		}
	}
	for _, id := range detail {
		if forced[id] && attempted[id] {
			result.Refreshed = append(result.Refreshed, id)
		}
	}
	// Unattempted work keeps its order and failed work moves behind it, so a
	// failing ID cannot keep blocking the rest.
	for _, id := range queue {
		if !successful[id] && !attempted[id] {
			result.PendingIDs = append(result.PendingIDs, id)
			if retry, ok := opts.PendingRetries[id]; ok {
				result.PendingRetries[id] = retry
			}
		}
	}
	result.PendingIDs = append(result.PendingIDs, failed...)
	if len(result.Posts) > 0 {
		newest := result.Posts[0]
		for _, p := range result.Posts[1:] {
			pt, _ := time.Parse(time.RFC3339Nano, p.PublishedAt)
			nt, _ := time.Parse(time.RFC3339Nano, newest.PublishedAt)
			if pt.After(nt) {
				newest = p
			}
		}
		result.NextCursor = result.NewestID(newest)
		result.NextCursorTime = newest.PublishedAt
	}
	// Requests answered only with other accounts' statuses fail nothing.
	if result.RequestedCount > foreign && len(result.Posts) == 0 {
		if firstDetailErr != nil {
			return result, firstDetailErr
		}
		return result, &Error{Code: ErrInvalidResponse, Err: errors.New("no usable posts")}
	}
	return result, nil
}
func (r Result) NewestID(p Post) string { return p.ID }
func safeCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return string(e.Code)
	}
	return "x_error"
}
func isSchedule(text string, keys []string) bool {
	for _, k := range keys {
		if k != "" && strings.Contains(strings.ToLower(text), strings.ToLower(k)) {
			return true
		}
	}
	return false
}
func parseDate(v any) string {
	switch x := v.(type) {
	case float64:
		if x < 1e12 {
			x *= 1000
		}
		return time.UnixMilli(int64(x)).UTC().Format(time.RFC3339Nano)
	case string:
		if x == "" {
			return ""
		}
		if t, e := time.Parse(time.RFC3339, x); e == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
		if n, e := strconv.ParseInt(x, 10, 64); e == nil {
			if n < 1e12 {
				n *= 1000
			}
			return time.UnixMilli(n).UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func snowflakeTime(id string) time.Time {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	ms := (n >> 22) + 1288834974657
	return time.UnixMilli(ms).UTC()
}
