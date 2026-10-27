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
	"slices"
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
	KnownIDs          map[string]bool
	// CursorID is the account's newest stored status; timeline paging stops
	// once it reaches it.
	CursorID string
	// Gap is the unread stretch an earlier capped run left, read after the
	// newest pages.
	Gap *Gap
}

func DefaultOptions() Options {
	return Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 12, RefreshKnown: 1, ScheduleKeywords: []string{"schedule", "スケジュール", "配信予定", "週間予定", "今週の予定"}}
}
func OptionsFromEnv() Options {
	o := DefaultOptions()
	o.BootstrapDays = envInt("X_BOOTSTRAP_DAYS", o.BootstrapDays, 1, 30)
	o.DiscoveryLimit = envInt("X_DISCOVERY_LIMIT", o.DiscoveryLimit, 1, 200)
	o.MaxDetailRequests = envInt("X_MAX_STATUS_REQUESTS", o.MaxDetailRequests, 0, 50)
	o.RefreshKnown = envInt("X_REFRESH_KNOWN", o.RefreshKnown, 0, 5)
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

func envInt(k string, d, min, max int) int {
	n, e := strconv.Atoi(getenvRaw(k))
	if e != nil || n < min || n > max {
		return d
	}
	return n
}

// SetEnvReader is useful for tests and avoids exposing process credentials to logs.
func SetEnvReader(fn func(string) string) {
	if fn != nil {
		getenvRaw = fn
	}
}

var (
	statusPattern   = regexp.MustCompile(`(?i)(?:status%2F|status/)(\d{10,})`)
	statusIDPattern = regexp.MustCompile(`^\d{10,22}$`)
)

func DiscoverIDs(body []byte, limit int) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range statusPattern.FindAllSubmatch(body, -1) {
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
	Raw                                                    map[string]any
}
type ScheduleCandidate struct{ PostID, SourceURL, MediaURL, AccountHandle string }
type Result struct {
	Account                         string
	Posts                           []Post
	DiscoveredCount, RequestedCount int
	Errors                          []string
	NextCursor, NextCursorTime      string
	PendingIDs                      []string
	ScheduleCandidates              []ScheduleCandidate
	// Discovery names where the status IDs came from: DiscoveryTimeline, or
	// DiscoveryProfile after a timeline fallback.
	Discovery string
	// Gap is the account's unread timeline stretch after the run, nil when
	// there is none; Notes are controlled codes about it.
	Gap   *Gap
	Notes []string
}

type Adapter struct {
	Client  *Client
	Options Options
	Now     func() time.Time
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
	// The timeline pages back to stored posts; the logged-out profile page
	// lists only the newest few statuses and is the fallback.
	found, err := a.discoverTimeline(ctx, account, opts, cutoff)
	ids := found.ids
	result.Discovery, result.Gap, result.Notes = DiscoveryTimeline, found.gap, found.notes
	if err != nil || len(ids) == 0 {
		result.Discovery, result.Gap, result.Notes = DiscoveryProfile, opts.Gap, nil
		profile, profileErr := a.Client.Profile(ctx, account)
		if profileErr != nil {
			return result, profileErr
		}
		ids = DiscoverIDs(profile, opts.DiscoveryLimit)
		// The profile lists only the newest few statuses; when none of them is
		// stored, the timeline reads the rest later.
		if opts.CursorID != "" && !slices.ContainsFunc(ids, func(id string) bool { return reachesStored(id, opts, cutoff) }) {
			result.Gap = withGap(opts.Gap, Gap{Floor: opts.CursorID})
		}
	}
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
	if len(ids) == 0 && len(opts.PendingIDs) == 0 {
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
	for _, id := range opts.PendingIDs {
		add(id)
	}
	for _, id := range ids {
		if !opts.KnownIDs[id] {
			add(id)
		}
	}
	refresh := opts.RefreshKnown
	for _, id := range ids {
		if opts.KnownIDs[id] && refresh > 0 {
			add(id)
			refresh--
		}
	}
	ids = queue
	if opts.MaxDetailRequests <= 0 {
		result.PendingIDs = ids
		return result, nil
	}
	if len(ids) > opts.MaxDetailRequests {
		result.PendingIDs = append(result.PendingIDs, ids[opts.MaxDetailRequests:]...)
		ids = ids[:opts.MaxDetailRequests]
	}
	successful := map[string]bool{}
	var firstDetailErr error
	for _, id := range ids {
		result.RequestedCount++
		body, e := a.Client.Detail(ctx, account, id)
		if e != nil {
			if firstDetailErr == nil {
				firstDetailErr = e
			}
			result.Errors = append(result.Errors, id+": "+safeCode(e))
			continue
		}
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			if firstDetailErr == nil {
				firstDetailErr = &Error{Code: ErrInvalidJSON}
			}
			result.Errors = append(result.Errors, id+": "+string(ErrInvalidJSON))
			continue
		}
		post := Normalize(payload, id, account)
		if post == nil || post.ID != id {
			if firstDetailErr == nil {
				firstDetailErr = &Error{Code: ErrInvalidResponse}
			}
			result.Errors = append(result.Errors, id+": "+string(ErrInvalidResponse))
			continue
		}
		if post.AuthorScreenName != "" && !strings.EqualFold(strings.TrimPrefix(post.AuthorScreenName, "@"), account) {
			result.Errors = append(result.Errors, id+": account_mismatch")
			continue
		}
		result.Posts = append(result.Posts, *post)
		successful[id] = true
		if len(post.MediaURLs) > 0 && isSchedule(post.Text, opts.ScheduleKeywords) {
			result.ScheduleCandidates = append(result.ScheduleCandidates, ScheduleCandidate{PostID: post.ID, SourceURL: post.URL, MediaURL: post.MediaURLs[0], AccountHandle: account})
		}
	}
	for _, id := range ids {
		if !successful[id] {
			result.PendingIDs = append(result.PendingIDs, id)
		}
	}
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
	if result.RequestedCount > 0 && len(result.Posts) == 0 {
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
