package sqlite

import (
	"context"
	"fmt"
	"time"
)

// AdminBacklog counts the operator requests that wait for a run. The step
// that does the work clears each one: schedule extraction a re-extraction,
// the X step a post refresh, the YouTube step a video refresh and the
// translate step a machine translation.
type AdminBacklog struct {
	Extraction       int `json:"extraction"`
	PostRefresh      int `json:"postRefresh"`
	PostTranslation  int `json:"postTranslation"`
	VideoRefresh     int `json:"videoRefresh"`
	VideoTranslation int `json:"videoTranslation"`
}

// postQueuedSQL is the condition of a post list's queued filter on posts p
// joined with post_llm_states s: "extraction", "refresh", "translation" or
// "any" of them. Anything else adds no condition.
func postQueuedSQL(queue string) string {
	// A hidden post is not extracted, so its request waits until it is shown.
	extraction := "(COALESCE(s.reprocess_requested, 0) = 1 AND p.hidden_at IS NULL AND p.deleted_at IS NULL)"
	refresh := "(p.refresh_requested_at IS NOT NULL AND p.deleted_at IS NULL)"
	translation := "(" + TranslationRequestedSQL(TranslationOwnerPost, "p") + " AND p.deleted_at IS NULL)"
	switch queue {
	case "extraction":
		return extraction
	case "refresh":
		return refresh
	case "translation":
		return translation
	case "any":
		return "(" + extraction + " OR " + refresh + " OR " + translation + ")"
	}
	return ""
}

// videoQueuedSQL is the condition of a video list's queued filter on videos
// v: "refresh" (a YouTube refresh request still inside refreshRequestTTL at
// now), "translation" or "any" of them. Anything else adds no condition.
func videoQueuedSQL(queue string, now time.Time) (string, []any) {
	refresh := "(v.source = 'youtube' AND julianday(v.refresh_requested_at) >= julianday(?))"
	since := workflowTime(now.Add(-refreshRequestTTL))
	translation := TranslationRequestedSQL(TranslationOwnerVideo, "v")
	switch queue {
	case "refresh":
		return refresh, []any{since}
	case "translation":
		return translation, nil
	case "any":
		return "(" + refresh + " OR " + translation + ")", []any{since}
	}
	return "", nil
}

// AdminBacklog reads the open operator requests at now.
func (s *Store) AdminBacklog(ctx context.Context, now time.Time) (AdminBacklog, error) {
	var b AdminBacklog
	videoRefresh, args := videoQueuedSQL("refresh", now)
	err := s.db.QueryRowContext(ctx, `SELECT
	  (SELECT COUNT(*) FROM posts p LEFT JOIN post_llm_states s ON s.post_id=p.id WHERE `+postQueuedSQL("extraction")+`),
	  (SELECT COUNT(*) FROM posts p WHERE `+postQueuedSQL("refresh")+`),
	  (SELECT COUNT(*) FROM posts p WHERE `+postQueuedSQL("translation")+`),
	  (SELECT COUNT(*) FROM videos v WHERE `+videoRefresh+`),
	  (SELECT COUNT(*) FROM videos v WHERE `+TranslationRequestedSQL(TranslationOwnerVideo, "v")+`)`, args...).Scan(
		&b.Extraction, &b.PostRefresh, &b.PostTranslation, &b.VideoRefresh, &b.VideoTranslation)
	if err != nil {
		return AdminBacklog{}, fmt.Errorf("%w: admin backlog unavailable", ErrUnavailable)
	}
	return b, nil
}

// XRefreshPerAccount is how many refresh requests of one account a run
// reads; the rest wait for a later run.
const XRefreshPerAccount = maxRefreshIDs

// RefreshRequest is a post an operator asked the X step to read again.
type RefreshRequest struct {
	PostID        string `json:"postId"`
	Source        string `json:"source"`
	AccountHandle string `json:"accountHandle"`
	Text          string `json:"text"`
	URL           string `json:"url"`
	PublishedAt   string `json:"publishedAt"`
	RequestedAt   string `json:"requestedAt"`
}

// RefreshRequests lists up to limit post refresh requests oldest first, the
// order the X step reads each account's, with the total count.
func (s *Store) RefreshRequests(ctx context.Context, limit int) ([]RefreshRequest, int, error) {
	out := []RefreshRequest{}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts p WHERE `+postQueuedSQL("refresh")).Scan(&total); err != nil {
		return nil, 0, dbErr(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.source, COALESCE(p.account_handle, ''), COALESCE(p.text, ''), COALESCE(p.url, ''),
		COALESCE(p.published_at, ''), p.refresh_requested_at
		FROM posts p WHERE `+postQueuedSQL("refresh")+` ORDER BY p.refresh_requested_at, p.id LIMIT ?`, limit)
	if err != nil {
		return nil, 0, dbErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item RefreshRequest
		if err = rows.Scan(&item.PostID, &item.Source, &item.AccountHandle, &item.Text, &item.URL, &item.PublishedAt, &item.RequestedAt); err != nil {
			return nil, 0, dbErr(err)
		}
		out = append(out, item)
	}
	return out, total, dbErr(rows.Err())
}
