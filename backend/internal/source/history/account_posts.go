package history

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// xUsersURL is the root of the user lookup and the user posts timeline
// (https://docs.x.com/x-api/users/get-posts), which the timed X chain's API
// method reads (ADR-0004).
const xUsersURL = "https://api.x.com/2/users/"

var xUserIDPattern = regexp.MustCompile(`^\d{1,19}$`)

// XAPI serves the timed X chain's API method through the user posts
// timeline: an account's own posts newest first, without reposts or replies,
// newer than since_id and older than until_id. It shares the fetcher's
// client, so the credential travels only in the Authorization header with
// the same timeout, response limit and rejected redirects as the manual API
// method. Each account's numeric user ID is looked up once per process.
type XAPI struct {
	Fetcher    *Fetcher
	Credential string

	mu  sync.Mutex
	ids map[string]string
}

// AccountPosts reads one page of the account's own posts. Failures carry
// the manual API method's controlled source_api_* codes.
func (x *XAPI) AccountPosts(ctx context.Context, query xsource.APIQuery) (xsource.APIPage, error) {
	id, err := x.userID(ctx, query.Account)
	if err != nil {
		return xsource.APIPage{}, xCode(err)
	}
	values := url.Values{}
	values.Set("max_results", strconv.Itoa(min(max(query.Max, 5), 100)))
	values.Set("exclude", "retweets,replies")
	setPostFields(values)
	for key, value := range map[string]string{"since_id": query.SinceID, "until_id": query.UntilID, "start_time": query.StartTime, "pagination_token": query.Token} {
		if value != "" {
			values.Set(key, value)
		}
	}
	payload, err := x.Fetcher.apiJSON(ctx, xUsersURL+id+"/tweets", values, "Authorization", "Bearer "+x.Credential)
	if err != nil {
		return xsource.APIPage{}, xCode(err)
	}
	if !validPostsAnswer(payload) {
		return xsource.APIPage{}, xCode(&Error{Code: "source_api_invalid_response"})
	}
	posts, read, next, err := xsource.APIPosts(payload, query.Account)
	if err != nil {
		return xsource.APIPage{Read: read}, xCode(&Error{Code: "source_api_invalid_records"})
	}
	return xsource.APIPage{Posts: posts, Next: next, Read: read}, nil
}

// userID resolves an account's numeric ID through the user lookup, once.
func (x *XAPI) userID(ctx context.Context, account string) (string, error) {
	key := strings.ToLower(account)
	x.mu.Lock()
	id, known := x.ids[key]
	x.mu.Unlock()
	if known {
		return id, nil
	}
	payload, err := x.Fetcher.apiJSON(ctx, xUsersURL+"by/username/"+url.PathEscape(account), url.Values{"user.fields": {"username"}}, "Authorization", "Bearer "+x.Credential)
	if err != nil {
		return "", err
	}
	user := object(payload["data"])
	id = text(user["id"])
	if !xUserIDPattern.MatchString(id) || !strings.EqualFold(text(user["username"]), account) {
		return "", &Error{Code: "source_api_invalid_response"}
	}
	x.mu.Lock()
	if x.ids == nil {
		x.ids = map[string]string{}
	}
	x.ids[key] = id
	x.mu.Unlock()
	return id, nil
}

// xCode carries a controlled code into the X adapter's error type.
func xCode(err error) error {
	var controlled *Error
	if errors.As(err, &controlled) {
		return &xsource.Error{Code: xsource.ErrorCode(controlled.Code)}
	}
	return &xsource.Error{Code: "source_api_request_failed"}
}
