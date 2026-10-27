package x

import (
	"errors"
	"net/http"
	"time"
)

// PendingRetry is a pending status's run of consecutive failed detail
// requests. It is kept beside pendingStatusIds in sync_state.metadata_json.
type PendingRetry struct {
	// Failures counts consecutive failures of any kind.
	Failures int `json:"failures"`
	// Gone counts consecutive answers that the status is not there: 404 or
	// 410, or a status of another account.
	Gone int `json:"gone,omitempty"`
	// RetryAt is when the next detail request may be made.
	RetryAt string `json:"retryAt,omitempty"`
}

// DroppedStatus is a pending status a run stopped requesting, with the code
// that says why.
type DroppedStatus struct{ ID, Code string }

// Codes a run reports for pending statuses it stops requesting.
const (
	// DroppedGone follows a second consecutive gone answer.
	DroppedGone = "x_status_gone"
	// DroppedRetryLimit follows the last allowed consecutive failure.
	DroppedRetryLimit = "x_retry_limit"
)

// pendingBackoff is the wait after the first, second and third consecutive
// failure; the next failure drops the status. A gone status is dropped on
// its second gone answer. The waits follow the extraction failure backoff:
// the first covers a short outage, the last a daily limit.
var pendingBackoff = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}

const pendingGoneLimit = 2

// waiting reports whether the status must not be requested yet.
func (r PendingRetry) waiting(now time.Time) bool {
	at, err := time.Parse(time.RFC3339Nano, r.RetryAt)
	return err == nil && now.Before(at)
}

// failed records one more failed request. It returns the code the status is
// dropped with, or "" with the state to keep.
func (r PendingRetry) failed(gone bool, now time.Time) (PendingRetry, string) {
	r.Failures++
	if gone {
		r.Gone++
	} else {
		r.Gone = 0
	}
	switch {
	case r.Gone >= pendingGoneLimit:
		return r, DroppedGone
	case r.Failures > len(pendingBackoff):
		return r, DroppedRetryLimit
	}
	r.RetryAt = now.Add(pendingBackoff[r.Failures-1]).UTC().Format(time.RFC3339Nano)
	return r, ""
}

// isGone reports a detail answer that the status does not exist (any more).
func isGone(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == ErrHTTP && (e.Status == http.StatusNotFound || e.Status == http.StatusGone)
}
