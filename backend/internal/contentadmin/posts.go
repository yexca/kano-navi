package contentadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// postLLMState is the reprocess bookkeeping for one source post.
type postLLMState struct {
	PostID             string  `json:"postId"`
	Status             string  `json:"status"`
	Route              *string `json:"route"`
	LastAttemptAt      *string `json:"lastAttemptAt"`
	LastProcessedAt    *string `json:"lastProcessedAt"`
	LastError          *string `json:"lastError"`
	LastExtractionID   *int64  `json:"lastExtractionId"`
	ReprocessRequested bool    `json:"reprocessRequested"`
	UpdatedAt          string  `json:"updatedAt"`
}

var errPostNotFound = errors.New("post not found")

// maxBatchPosts bounds the posts one bulk reprocess, dismissal or refresh
// names.
const maxBatchPosts = 200

// queuePostReprocess marks one post queued for the next extraction run and
// clears its failure backoff and settled stamp, or answers errPostNotFound.
func queuePostReprocess(ctx context.Context, tx *sql.Tx, postID string, route *string, now string) error {
	var found int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE id = ? AND deleted_at IS NULL`, postID).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return errPostNotFound
	} else if err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO post_llm_states (
	     post_id, status, route, last_attempt_at, last_processed_at,
	     last_error, last_extraction_id, reprocess_requested, updated_at
	   ) VALUES (?, 'queued', ?, NULL, NULL, NULL, NULL, 1, ?)
	   ON CONFLICT(post_id) DO UPDATE SET
	     status=excluded.status, route=COALESCE(excluded.route, post_llm_states.route),
	     last_error=excluded.last_error,
	     reprocess_requested=excluded.reprocess_requested,
	     updated_at=excluded.updated_at,
	     failure_count=0, failure_fingerprint=NULL, next_attempt_at=NULL, settled_stamp=NULL`, postID, nullString(route), now)
	return err
}

// dismissFailedPost turns one failed extraction into `dismissed`, keeping its
// failure keyed to the current input with no retry time and no settled
// stamp. It reports whether the post was failed.
func dismissFailedPost(ctx context.Context, tx *sql.Tx, postID, now string) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE post_llm_states SET status='dismissed',
	     reprocess_requested=0, failure_count=MAX(failure_count, 1), next_attempt_at=NULL, settled_stamp=NULL, updated_at=?
	   WHERE post_id=? AND status='failed'`, now, postID)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

// batchPostIDs reads a bulk request's `ids`: 1 to maxBatchPosts post IDs,
// with repeats dropped. No other field is accepted.
func batchPostIDs(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return nil, false
	}
	for key := range body {
		if key != "ids" {
			httpx.WriteError(w, httpx.Invalid("%s is not supported", key))
			return nil, false
		}
	}
	var raw []string
	if json.Unmarshal(body["ids"], &raw) != nil || len(raw) == 0 || len(raw) > maxBatchPosts {
		httpx.WriteError(w, httpx.Invalid("ids must list 1 to %d post IDs", maxBatchPosts))
		return nil, false
	}
	seen := make(map[string]bool, len(raw))
	ids := make([]string, 0, len(raw))
	for _, id := range raw {
		if id == "" {
			httpx.WriteError(w, httpx.Invalid("ids must not be empty"))
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, true
}

// reprocessPosts queues every listed post that exists for the next
// extraction run in one transaction; unknown IDs are skipped.
func (m *Module) reprocessPosts(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchPostIDs(w, r)
	if !ok {
		return
	}
	ctx, now, queued := r.Context(), m.timestamp(), 0
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			switch err := queuePostReprocess(ctx, tx, id, nil, now); {
			case errors.Is(err, errPostNotFound):
			case err != nil:
				return err
			default:
				queued++
			}
		}
		return nil
	})
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"queued": queued})
}

// dismissPosts dismisses every listed post whose extraction failed in one
// transaction; other and unknown IDs are skipped.
func (m *Module) dismissPosts(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchPostIDs(w, r)
	if !ok {
		return
	}
	ctx, now, dismissed := r.Context(), m.timestamp(), 0
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			changed, err := dismissFailedPost(ctx, tx, id, now)
			if err != nil {
				return err
			}
			if changed {
				dismissed++
			}
		}
		return nil
	})
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"dismissed": dismissed})
}

// refreshPosts queues every listed own X post for a refresh from the source
// in one transaction (see queuePostRefresh); unknown IDs and posts that
// cannot be refreshed are skipped. It answers how many are queued, a post
// already queued included.
func (m *Module) refreshPosts(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchPostIDs(w, r)
	if !ok {
		return
	}
	ctx, now, queued := r.Context(), m.timestamp(), 0
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			switch err := queuePostRefresh(ctx, tx, id, now); {
			case errors.Is(err, errPostNotFound), errors.Is(err, errNotRefreshable):
			case err != nil:
				return err
			default:
				queued++
			}
		}
		return nil
	})
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"queued": queued})
}

// requestPostReprocess queues a post for the next extraction run and clears
// its failure backoff. Only the two schedule routes may be pinned; anything
// else keeps the stored route.
func (m *Module) requestPostReprocess(ctx context.Context, postID string, route *string) (*postLLMState, error) {
	var state postLLMState
	now := m.timestamp()
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		if queueErr := queuePostReprocess(ctx, tx, postID, route, now); queueErr != nil {
			return queueErr
		}
		var storedRoute, lastAttempt, lastProcessed, lastError, updatedAt sql.NullString
		var extraction, requested sql.NullInt64
		if scanErr := tx.QueryRowContext(ctx, `SELECT post_id, status, route, last_attempt_at, last_processed_at, last_error,
		     last_extraction_id, reprocess_requested, updated_at FROM post_llm_states WHERE post_id = ?`, postID).
			Scan(&state.PostID, &state.Status, &storedRoute, &lastAttempt, &lastProcessed, &lastError, &extraction, &requested, &updatedAt); scanErr != nil {
			return scanErr
		}
		state.Route, state.LastAttemptAt, state.LastProcessedAt, state.LastError = textPointer(storedRoute), textPointer(lastAttempt), textPointer(lastProcessed), textPointer(lastError)
		state.LastExtractionID = intPointer(extraction)
		state.ReprocessRequested = requested.Valid && requested.Int64 != 0
		state.UpdatedAt = updatedAt.String
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Module) reprocessPost(w http.ResponseWriter, r *http.Request, postID string) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var route *string
	if value, ok := stringField(body, "route"); ok && (value == "schedule_message" || value == "schedule_board") {
		route = &value
	}
	state, err := m.requestPostReprocess(r.Context(), postID, route)
	if errors.Is(err, errPostNotFound) {
		httpx.WriteCode(w, http.StatusNotFound, "post_not_found")
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"state": state})
}

var errPostNotFailed = errors.New("post extraction has not failed")

// dismissPost stops the automatic scan from retrying a failed extraction:
// the post leaves the failed list as `dismissed`, and its failure stays keyed
// to the current input and targets with no retry time, so the scan holds it
// back until the wording, images or model queue change or an operator asks
// for a reprocess.
func (m *Module) dismissPost(w http.ResponseWriter, r *http.Request, postID string) {
	ctx, now := r.Context(), m.timestamp()
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		if changed, dismissErr := dismissFailedPost(ctx, tx, postID, now); dismissErr != nil || changed {
			return dismissErr
		}
		var found int
		if lookupErr := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE id = ? AND deleted_at IS NULL`, postID).Scan(&found); errors.Is(lookupErr, sql.ErrNoRows) {
			return errPostNotFound
		} else if lookupErr != nil {
			return lookupErr
		}
		return errPostNotFailed
	})
	switch {
	case errors.Is(err, errPostNotFound):
		httpx.WriteCode(w, http.StatusNotFound, "post_not_found")
	case errors.Is(err, errPostNotFailed):
		httpx.WriteCode(w, http.StatusConflict, "post_not_failed")
	case err != nil:
		writeFailure(w, err)
	default:
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"state": map[string]any{"postId": postID, "status": "dismissed"}})
	}
}

// pickScheduleImageRoute makes one of an X post's own images its schedule
// image, labelled `schedule` with the required reason. A period is optional;
// without one the asset keeps its stored manual period.
func (m *Module) pickScheduleImageRoute(w http.ResponseWriter, r *http.Request, postID string) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var index int
	if raw, ok := body["index"]; !ok || json.Unmarshal(raw, &index) != nil || index < 0 || index > 99 {
		httpx.WriteError(w, httpx.Invalid("index is invalid"))
		return
	}
	reason, err := textField(body, "reason", true, 500)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	pick := sqlite.SchedulePick{PostID: postID, Index: index, Reason: *reason}
	start, _ := stringField(body, "start")
	end, _ := stringField(body, "end")
	if start != "" || end != "" {
		startDate, startOK := calendarDate(start)
		endDate, endOK := calendarDate(end)
		if !startOK || !endOK || end < start || endDate.Sub(startDate).Hours() > 366*24 {
			httpx.WriteError(w, httpx.Invalid("period is invalid"))
			return
		}
		pick.Start, pick.End = start, end
	}
	id, err := m.store.PickPostScheduleImage(r.Context(), pick, m.timestamp())
	switch {
	case errors.Is(err, sqlite.ErrSchedulePickPost):
		httpx.WriteCode(w, http.StatusNotFound, "post_not_found")
	case errors.Is(err, sqlite.ErrSchedulePickImage):
		httpx.WriteError(w, httpx.Invalid("index is invalid"))
	case err != nil:
		writeFailure(w, err)
	default:
		m.writeScheduleAsset(w, r, id)
	}
}
