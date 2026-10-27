// Package bilibili reads the public uploads of one Bilibili account.
//
// Bilibili's anonymous web API answers a few requests and then puts the
// caller's address behind a captcha (HTTP 412 or an HTML page, sometimes the
// JSON codes -352 and -412) for hours. The client therefore makes one request
// per call, never retries, reports that state as bilibili_risk_control, and
// leaves the waiting to the adapter's persisted backoff. It reads only
// endpoints that need no signature and no login.
package bilibili

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

// MaxResponseBytes bounds one API response; a page of 20 uploads is far
// smaller.
const MaxResponseBytes = 2 * 1024 * 1024

const apiHost = "api.bilibili.com"

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

// Session is the anonymous device identity Bilibili's own pages request
// before calling the API, plus the operator's optional login cookie. Only
// the device identity is persisted; Login comes from the environment on every
// request and is never stored, logged or returned.
type Session struct {
	Buvid3 string `json:"buvid3"`
	Buvid4 string `json:"buvid4"`
	Login  string `json:"-"`
}

func (s Session) valid() bool {
	return buvid3.MatchString(s.Buvid3) && buvid4.MatchString(s.Buvid4)
}

var (
	buvid3 = regexp.MustCompile(`^[A-Fa-f0-9-]{20,64}infoc$`)
	buvid4 = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{10,200}$`)
	mid    = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
	// sessdata is the login cookie as browsers store it (URL-encoded, so no
	// separators that could add another cookie or header).
	sessdata = regexp.MustCompile(`^[A-Za-z0-9%*,._-]{16,512}$`)
)

type Client struct{ http *http.Client }

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.Timeout = 10 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copy}
}

// get requests one api.bilibili.com path and returns the envelope's data.
func (c *Client) get(ctx context.Context, path string, query url.Values, referer string, session *Session) (json.RawMessage, error) {
	u := url.URL{Scheme: "https", Host: apiHost, Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Error{"bilibili_invalid_request"}
	}
	req.Header.Set("User-Agent", "kano-navi-go/source-sync")
	req.Header.Set("Accept", "application/json")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if session != nil {
		cookie := "buvid3=" + session.Buvid3 + "; buvid4=" + session.Buvid4
		if sessdata.MatchString(session.Login) {
			cookie += "; SESSDATA=" + session.Login
		}
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{"bilibili_timeout"}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{"bilibili_unavailable"}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, &Error{"bilibili_redirect_rejected"}
	case resp.StatusCode == http.StatusPreconditionFailed:
		return nil, &Error{"bilibili_risk_control"}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &Error{"bilibili_rate_limited"}
	case resp.StatusCode >= 500:
		return nil, &Error{"bilibili_upstream_5xx"}
	case resp.StatusCode != http.StatusOK:
		return nil, &Error{"bilibili_http_error"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, &Error{"bilibili_body_unavailable"}
	}
	if len(body) > MaxResponseBytes {
		return nil, &Error{"bilibili_response_oversized"}
	}
	return envelope(body)
}

// envelope reads Bilibili's {code, message, data}. An HTML answer is the
// captcha page.
func envelope(body []byte) (json.RawMessage, error) {
	var out struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &out) != nil || out.Code == nil {
		if strings.Contains(strings.ToLower(string(body[:min(len(body), 512)])), "<!doctype html") {
			return nil, &Error{"bilibili_risk_control"}
		}
		return nil, &Error{"bilibili_invalid_response"}
	}
	switch *out.Code {
	case 0:
		return out.Data, nil
	case -352, -412:
		return nil, &Error{"bilibili_risk_control"}
	case -799, -509:
		return nil, &Error{"bilibili_rate_limited"}
	case -404, 62002, 62004, 62012:
		return nil, &Error{"bilibili_not_found"}
	}
	return nil, &Error{"bilibili_api_error"}
}

// NewSession requests the anonymous device identity.
func (c *Client) NewSession(ctx context.Context) (Session, error) {
	data, err := c.get(ctx, "/x/frontend/finger/spi", nil, "https://www.bilibili.com/", nil)
	if err != nil {
		return Session{}, err
	}
	var out struct {
		B3 string `json:"b_3"`
		B4 string `json:"b_4"`
	}
	session := Session{}
	if json.Unmarshal(data, &out) == nil {
		session = Session{Buvid3: out.B3, Buvid4: out.B4}
	}
	if !session.valid() {
		return Session{}, &Error{"bilibili_invalid_response"}
	}
	return session, nil
}

// Uploads lists an account's newest uploads, newest first.
func (c *Client) Uploads(ctx context.Context, account string, pageSize int, session Session) ([]Video, error) {
	if !mid.MatchString(account) {
		return nil, &Error{"bilibili_invalid_account"}
	}
	if !session.valid() {
		return nil, &Error{"bilibili_invalid_session"}
	}
	query := url.Values{"mid": {account}, "keywords": {""}, "ps": {strconv.Itoa(pageSize)}, "pn": {"1"}, "orderby": {"pubdate"}}
	data, err := c.get(ctx, "/x/series/recArchivesByKeywords", query, "https://space.bilibili.com/"+account, &session)
	if err != nil {
		return nil, err
	}
	return ParseUploads(data, account)
}

// Video reads one upload by its BV ID.
func (c *Client) Video(ctx context.Context, bvid string, session Session) (Video, error) {
	if !bvidPattern.MatchString(bvid) {
		return Video{}, &Error{"bilibili_invalid_video"}
	}
	if !session.valid() {
		return Video{}, &Error{"bilibili_invalid_session"}
	}
	data, err := c.get(ctx, "/x/web-interface/view", url.Values{"bvid": {bvid}}, "https://www.bilibili.com/video/"+bvid, &session)
	if err != nil {
		return Video{}, err
	}
	return ParseView(data, bvid)
}

type Options struct {
	// Account is the uploader's numeric mid.
	Account string
	// MinInterval spaces list reads, whatever the source schedule.
	MinInterval time.Duration
	PageSize    int
	// SESSDATA is an operator's Bilibili login cookie from BILIBILI_SESSDATA.
	// Signed-in reads meet the captcha far less often. It is a credential of
	// that account: it stays in memory and goes only to api.bilibili.com.
	SESSDATA string
}

// SignedIn reports whether a valid login cookie is configured.
func (o Options) SignedIn() bool { return sessdata.MatchString(o.SESSDATA) }

// DefaultAccount is Kano's public space, linked from the resource directory.
const DefaultAccount = "316381099"

func OptionsFromEnv() Options {
	account := strings.TrimSpace(os.Getenv("BILIBILI_UID"))
	if !mid.MatchString(account) {
		account = DefaultAccount
	}
	minutes, err := strconv.Atoi(os.Getenv("BILIBILI_MIN_INTERVAL_MINUTES"))
	if err != nil || minutes < 60 || minutes > 1440 {
		minutes = 180
	}
	login := strings.TrimSpace(os.Getenv("BILIBILI_SESSDATA"))
	if !sessdata.MatchString(login) {
		login = ""
	}
	return Options{Account: account, MinInterval: time.Duration(minutes) * time.Minute, PageSize: 20, SESSDATA: login}
}
