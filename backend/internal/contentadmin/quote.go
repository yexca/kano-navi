package contentadmin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// postQuoteState is the operator's standing decisions for one post: when the
// post or its quote was hidden from the public pages, and a refresh from the
// source that waits for the next X synchronization.
type postQuoteState struct {
	PostID             string  `json:"postId"`
	HiddenAt           *string `json:"hiddenAt"`
	QuoteHiddenAt      *string `json:"quoteHiddenAt"`
	RefreshRequestedAt *string `json:"refreshRequestedAt"`
}

var (
	errQuoteNotFound   = errors.New("quote not found")
	errNotRefreshable  = errors.New("post cannot be refreshed")
	statusIDPattern    = regexp.MustCompile(`^[0-9]{10,22}$`)
	refreshHandleRegex = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)
)

func readQuoteState(ctx context.Context, q queryer, postID string) (*postQuoteState, error) {
	state := postQuoteState{PostID: postID}
	var postHidden, hidden, refresh sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT hidden_at, quote_hidden_at, refresh_requested_at FROM posts WHERE id = ?`, postID).Scan(&postHidden, &hidden, &refresh); err != nil {
		return nil, err
	}
	state.HiddenAt, state.QuoteHiddenAt, state.RefreshRequestedAt = textPointer(postHidden), textPointer(hidden), textPointer(refresh)
	return &state, nil
}

// setQuoteHidden records or withdraws the operator's decision to keep a
// post's quote off the public pages. Synchronization never writes this
// column, so the decision outlives every rewrite of the stored quote.
func (m *Module) setQuoteHidden(ctx context.Context, postID string, hidden bool) (*postQuoteState, error) {
	var state *postQuoteState
	now := m.timestamp()
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		var quote sql.NullString
		var was sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT quote_json, quote_hidden_at FROM posts WHERE id = ?`, postID).Scan(&quote, &was); errors.Is(err, sql.ErrNoRows) {
			return errPostNotFound
		} else if err != nil {
			return err
		}
		if !quote.Valid || quote.String == "" {
			return errQuoteNotFound
		}
		// Hiding again keeps the first decision's time.
		if hidden != was.Valid {
			value := any(nil)
			if hidden {
				value = now
			}
			if _, err := tx.ExecContext(ctx, `UPDATE posts SET quote_hidden_at = ? WHERE id = ?`, value, postID); err != nil {
				return err
			}
			if err := sqlite.BumpRevisionTx(ctx, tx, now); err != nil {
				return err
			}
		}
		var err error
		state, err = readQuoteState(ctx, tx, postID)
		return err
	})
	return state, err
}

// setPostHidden records or withdraws the operator's decision to keep a whole
// post off the public pages. Synchronization never writes this column, so a
// later read of the same post does not show it again.
func (m *Module) setPostHidden(ctx context.Context, postID string, hidden bool) (*postQuoteState, error) {
	var state *postQuoteState
	now := m.timestamp()
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		var was sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT hidden_at FROM posts WHERE id = ?`, postID).Scan(&was); errors.Is(err, sql.ErrNoRows) {
			return errPostNotFound
		} else if err != nil {
			return err
		}
		// Hiding again keeps the first decision's time.
		if hidden != was.Valid {
			value := any(nil)
			if hidden {
				value = now
			}
			if _, err := tx.ExecContext(ctx, `UPDATE posts SET hidden_at = ? WHERE id = ?`, value, postID); err != nil {
				return err
			}
			if err := sqlite.BumpRevisionTx(ctx, tx, now); err != nil {
				return err
			}
		}
		var err error
		state, err = readQuoteState(ctx, tx, postID)
		return err
	})
	return state, err
}

// requestPostRefresh queues an X post for a detail request in the next X
// synchronization and clears the failure backoff of its pending status. No
// source is contacted here; the workflow reads the request.
func (m *Module) requestPostRefresh(ctx context.Context, postID string) (*postQuoteState, error) {
	var state *postQuoteState
	now := m.timestamp()
	err := m.inTx(ctx, func(tx *sql.Tx) error {
		var source string
		var account sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT source, account_handle FROM posts WHERE id = ?`, postID).Scan(&source, &account); errors.Is(err, sql.ErrNoRows) {
			return errPostNotFound
		} else if err != nil {
			return err
		}
		if source != "x" || !statusIDPattern.MatchString(postID) || !refreshHandleRegex.MatchString(account.String) {
			return errNotRefreshable
		}
		if _, err := tx.ExecContext(ctx, `UPDATE posts SET refresh_requested_at = COALESCE(refresh_requested_at, ?) WHERE id = ?`, now, postID); err != nil {
			return err
		}
		// The status ID is digits only, so it is a safe JSON path key.
		if _, err := tx.ExecContext(ctx, `UPDATE sync_state SET metadata_json = json_remove(metadata_json, '$.pendingRetries."' || ? || '"')
		  WHERE source = 'x' AND lower(account_id) = lower(?) AND json_valid(metadata_json)`, postID, account.String); err != nil {
			return err
		}
		var err error
		state, err = readQuoteState(ctx, tx, postID)
		return err
	})
	return state, err
}

func (m *Module) writeQuoteState(w http.ResponseWriter, state *postQuoteState, err error) {
	switch {
	case errors.Is(err, errPostNotFound):
		httpx.WriteCode(w, http.StatusNotFound, "post_not_found")
	case errors.Is(err, errQuoteNotFound):
		httpx.WriteCode(w, http.StatusNotFound, "quote_not_found")
	case errors.Is(err, errNotRefreshable):
		httpx.WriteError(w, httpx.Invalid("only stored X posts can be refreshed from the source"))
	case err != nil:
		writeFailure(w, err)
	default:
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"state": state})
	}
}

// hiddenField reads the required boolean `hidden` of a visibility request.
func hiddenField(w http.ResponseWriter, r *http.Request) (hidden, ok bool) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return false, false
	}
	switch raw := string(bytes.TrimSpace(body["hidden"])); raw {
	case "true", "false":
		return raw == "true", true
	default:
		httpx.WriteError(w, httpx.Invalid("hidden must be true or false"))
		return false, false
	}
}

func (m *Module) quoteHiddenRoute(w http.ResponseWriter, r *http.Request, postID string) {
	hidden, ok := hiddenField(w, r)
	if !ok {
		return
	}
	state, err := m.setQuoteHidden(r.Context(), postID, hidden)
	m.writeQuoteState(w, state, err)
}

func (m *Module) postHiddenRoute(w http.ResponseWriter, r *http.Request, postID string) {
	hidden, ok := hiddenField(w, r)
	if !ok {
		return
	}
	state, err := m.setPostHidden(r.Context(), postID, hidden)
	m.writeQuoteState(w, state, err)
}

func (m *Module) refreshPostRoute(w http.ResponseWriter, r *http.Request, postID string) {
	state, err := m.requestPostRefresh(r.Context(), postID)
	m.writeQuoteState(w, state, err)
}
