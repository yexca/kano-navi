package contentadmin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
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

// requestPostReprocess queues a post for the next extraction run and clears
// its failure backoff. Only the two schedule routes may be pinned; anything
// else keeps the stored route.
func (m *Module) requestPostReprocess(ctx context.Context, postID string, route *string) (*postLLMState, error) {
	var state postLLMState
	now := m.timestamp()
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		var found int
		if lookupErr := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE id = ?`, postID).Scan(&found); errors.Is(lookupErr, sql.ErrNoRows) {
			return errPostNotFound
		} else if lookupErr != nil {
			return lookupErr
		}
		if _, execErr := tx.ExecContext(ctx, `INSERT INTO post_llm_states (
		     post_id, status, route, last_attempt_at, last_processed_at,
		     last_error, last_extraction_id, reprocess_requested, updated_at
		   ) VALUES (?, 'queued', ?, NULL, NULL, NULL, NULL, 1, ?)
		   ON CONFLICT(post_id) DO UPDATE SET
		     status=excluded.status, route=COALESCE(excluded.route, post_llm_states.route),
		     last_error=excluded.last_error,
		     reprocess_requested=excluded.reprocess_requested,
		     updated_at=excluded.updated_at,
		     failure_count=0, failure_fingerprint=NULL, next_attempt_at=NULL`, postID, nullString(route), now); execErr != nil {
			return execErr
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
