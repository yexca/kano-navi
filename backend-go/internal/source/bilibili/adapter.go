package bilibili

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// State is the bookkeeping kept between reads of one account. Times are zero
// while unknown.
type State struct {
	Session                     Session
	SessionAt, LastAttemptAt    time.Time
	LastSuccessAt, BlockedUntil time.Time
	// Failures counts consecutive risk-control or rate-limit answers; each
	// doubles the wait before the next request.
	Failures  int
	LastError string
}

// sessionTTL is how long an anonymous device identity is reused.
const sessionTTL = 7 * 24 * time.Hour

// Backoff after a captcha or rate-limit answer: 6 hours, doubled for each
// consecutive one, at most a day.
const (
	firstBlock = 6 * time.Hour
	maxBlock   = 24 * time.Hour
)

// Result is what one list read learned.
type Result struct {
	Videos []Video
	State  State
	// Skipped is "blocked" or "interval" when no request was made.
	Skipped   string
	Requested int
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
	return &Adapter{c, options, time.Now}
}

// FetchRecent reads the account's newest uploads at most once per
// MinInterval and never while a block lasts. A failure is returned together
// with the state to persist, which records the attempt and any backoff.
func (a *Adapter) FetchRecent(ctx context.Context, state State) (Result, error) {
	now := a.Now()
	result := Result{Videos: []Video{}, State: state}
	switch {
	case now.Before(state.BlockedUntil):
		result.Skipped = "blocked"
		return result, nil
	case !state.LastAttemptAt.IsZero() && now.Sub(state.LastAttemptAt) < a.Options.MinInterval:
		result.Skipped = "interval"
		return result, nil
	}
	result.State.LastAttemptAt = now
	session, err := a.session(ctx, &result.State, &result.Requested, now)
	if err == nil {
		result.Requested++
		result.Videos, err = a.Client.Uploads(ctx, a.Options.Account, a.Options.PageSize, session)
	}
	if err != nil {
		result.State = failed(result.State, err, now)
		result.Videos = []Video{}
		return result, err
	}
	result.State = succeeded(result.State, now)
	return result, nil
}

// FetchVideo reads one upload for an operator, whatever the list interval,
// but never while a block lasts. Only uploads of the configured account are
// accepted.
func (a *Adapter) FetchVideo(ctx context.Context, state State, bvid string) (Video, State, error) {
	now := a.Now()
	if now.Before(state.BlockedUntil) {
		return Video{}, state, &Error{"bilibili_risk_control"}
	}
	requests := 0
	session, err := a.session(ctx, &state, &requests, now)
	var v Video
	if err == nil {
		v, err = a.Client.Video(ctx, bvid, session)
	}
	if err != nil {
		return Video{}, failed(state, err, now), err
	}
	if strconv.FormatInt(v.OwnerMid, 10) != a.Options.Account {
		return Video{}, succeeded(state, now), &Error{"bilibili_other_account"}
	}
	return v, succeeded(state, now), nil
}

// session reuses a recent device identity or requests a new one, and adds
// the configured login cookie for this request only.
func (a *Adapter) session(ctx context.Context, state *State, requests *int, now time.Time) (Session, error) {
	if !state.Session.valid() || now.Sub(state.SessionAt) >= sessionTTL {
		*requests++
		session, err := a.Client.NewSession(ctx)
		if err != nil {
			return Session{}, err
		}
		state.Session, state.SessionAt = session, now
	}
	session := state.Session
	if a.Options.SignedIn() {
		session.Login = a.Options.SESSDATA
	}
	return session, nil
}

// ErrorCode is the fixed code of a source error.
func ErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "bilibili_unavailable"
}

func failed(state State, err error, now time.Time) State {
	state.LastError = ErrorCode(err)
	if state.LastError == "bilibili_risk_control" || state.LastError == "bilibili_rate_limited" {
		state.Failures++
		wait := firstBlock << min(state.Failures-1, 4)
		state.BlockedUntil = now.Add(min(wait, maxBlock))
	}
	return state
}

func succeeded(state State, now time.Time) State {
	state.Failures, state.LastError = 0, ""
	state.LastSuccessAt, state.BlockedUntil = now, time.Time{}
	return state
}
