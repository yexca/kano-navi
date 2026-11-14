package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// The X status pool (x_statuses) holds statuses that are not Kano's posts:
// every status an own account quotes or reposts, whatever its author, and
// every status of a related account (see x_related.go). No public reader
// reads or joins it: a pooled status reaches the board only as the quote
// projection stored on the quoting post (posts.quote_json) and the
// translations keyed by its status ID. Both the pool row and the projection
// come from the same xsource.Quote, so the pool text equals the quote text
// the board shows. x_reposts records the own accounts' reposts. See the
// related-account rules in the source architecture documentation.

// poolRaw is the sanitized raw_json of a pooled status: what a later
// promotion needs without a new request, including its images.
func poolRaw(q *xsource.Quote) string {
	media, kinds := q.MediaURLs, q.MediaKinds
	if media == nil {
		media = []string{}
	}
	if len(kinds) != len(media) {
		kinds = make([]string, len(media))
		for i := range kinds {
			kinds[i] = kindAt(q.MediaKinds, i)
			if kinds[i] == "" {
				kinds[i] = mediaKindPhoto
			}
		}
	}
	body, _ := json.Marshal(map[string]any{
		"id": q.ID, "text": q.Text, "created_at": q.PublishedAt, "url": q.URL,
		"author":    map[string]any{"screen_name": q.AuthorHandle, "name": q.AuthorName},
		"mediaURLs": media, "mediaKinds": kinds,
	})
	return string(body)
}

// writeQuotedStatus stores the status an own post quotes in the pool, in the
// quoting post's transaction, and records quotedAt, the quoting post's
// publication time, when it is newer than the one stored. An unavailable
// quote carries no status and stores nothing. A status its related author's
// timeline listed keeps the wording and payload that listing stored.
func writeQuotedStatus(ctx context.Context, tx *sql.Tx, q *xsource.Quote, quotedAt, now string) error {
	if q == nil || q.Unavailable || q.ID == "" || q.AuthorHandle == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO x_statuses(id,author_handle,author_name,text,published_at,url,raw_json,quoted_at,first_seen_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET author_handle=excluded.author_handle,
			author_name=CASE WHEN x_statuses.listed_at IS NULL THEN excluded.author_name ELSE x_statuses.author_name END,
			text=CASE WHEN x_statuses.listed_at IS NULL THEN excluded.text ELSE x_statuses.text END,
			published_at=COALESCE(x_statuses.published_at,excluded.published_at),url=excluded.url,
			raw_json=CASE WHEN x_statuses.listed_at IS NULL THEN excluded.raw_json ELSE x_statuses.raw_json END,
			quoted_at=CASE WHEN x_statuses.quoted_at IS NULL OR julianday(excluded.quoted_at)>julianday(x_statuses.quoted_at) THEN excluded.quoted_at ELSE x_statuses.quoted_at END,
			updated_at=excluded.updated_at`,
		q.ID, q.AuthorHandle, nullable(q.AuthorName), q.Text, nullable(q.PublishedAt), q.URL, poolRaw(q), quotedAt, now, now)
	return err
}

// writeXReposts records an own account's reposts. The first sighting keeps
// its time; a later listing only fills in an author that was unknown, and
// the repost's own status ID that a detail answer named. Every reposted
// status the listing carries enters the pool with reposted_at, whatever its
// author; one it does not carry only marks a pooled row reposted. The
// reasons of a related account's status are evaluated again; another
// author's is stored until an operator promotes it. A post stored under the
// repost's own status ID (from an import, say) is soft-deleted with
// everything keyed to it kept (see removeRepostPost). The board shows a
// reposted status only once it is promoted. Every distinct own ID stays in x_repost_ids, even
// when the account reposts the same original again. It reports whether a
// post changed, a new account reposting a promoted post included.
func writeXReposts(ctx context.Context, tx *sql.Tx, account string, reposts []xsource.Repost, now string) (bool, error) {
	if len(reposts) == 0 {
		return false, nil
	}
	accounts, err := readXRelatedAccounts(ctx, tx)
	if err != nil {
		return false, err
	}
	changed := false
	pooled := []string{}
	for _, r := range reposts {
		if !quoteIDPattern.MatchString(r.ID) {
			continue
		}
		retweet := ""
		if quoteIDPattern.MatchString(r.RetweetID) && r.RetweetID != r.ID {
			retweet = r.RetweetID
		}
		// A new reposting account of a promoted post changes its public
		// repostedBy.
		linked, linkErr := rowExists(ctx, tx, `SELECT 1 FROM x_reposts WHERE status_id=? AND account_handle=?`, r.ID, strings.ToLower(account))
		if linkErr != nil {
			return false, linkErr
		}
		if !linked {
			shown, shownErr := rowExists(ctx, tx, `SELECT 1 FROM posts WHERE id=? AND promoted_at IS NOT NULL AND deleted_at IS NULL`, r.ID)
			if shownErr != nil {
				return false, shownErr
			}
			changed = changed || shown
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO x_reposts(status_id,account_handle,author_handle,retweet_id,first_seen_at) VALUES(?,?,?,?,?)
			ON CONFLICT(status_id,account_handle) DO UPDATE SET author_handle=COALESCE(x_reposts.author_handle,excluded.author_handle),
				retweet_id=COALESCE(x_reposts.retweet_id,excluded.retweet_id)`,
			r.ID, strings.ToLower(account), nullable(r.Author), nullable(retweet), now); err != nil {
			return false, err
		}
		if retweet != "" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO x_repost_ids(retweet_id,status_id,account_handle,first_seen_at) VALUES(?,?,?,?)
				ON CONFLICT(retweet_id) DO NOTHING`, retweet, r.ID, strings.ToLower(account), now); err != nil {
				return false, err
			}
			removed, removeErr := removeRepostPost(ctx, tx, retweet, account, now)
			if removeErr != nil {
				return false, removeErr
			}
			changed = changed || removed
		}
		if q := r.Status; q != nil && !q.Unavailable && q.ID == r.ID && q.AuthorHandle != "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO x_statuses(id,author_handle,author_name,text,published_at,url,raw_json,reposted_at,first_seen_at,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(id) DO UPDATE SET reposted_at=COALESCE(x_statuses.reposted_at,excluded.reposted_at),updated_at=excluded.updated_at`,
				q.ID, q.AuthorHandle, nullable(q.AuthorName), q.Text, nullable(q.PublishedAt), q.URL, poolRaw(q), now, now, now)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE x_statuses SET reposted_at=COALESCE(reposted_at,?),updated_at=? WHERE id=?`, now, now, r.ID)
		}
		if err != nil {
			return false, err
		}
		pooled = append(pooled, r.ID)
	}
	reconciled, err := reconcileXStatuses(ctx, tx, accounts, pooled, now)
	return changed || reconciled, err
}

// isRepostID reports whether id is an own account's repost status ID that a
// detail answer named, which is never stored as a post.
func isRepostID(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	return rowExists(ctx, tx, `SELECT 1 FROM x_repost_ids WHERE retweet_id=?`, id)
}

// backfillRepostIDs preserves the IDs older snapshots keep on repost links.
// Repeated reposts keep every distinct ID in x_repost_ids; x_reposts retains
// its first ID for compatibility. Startup is additive and idempotent.
func backfillRepostIDs(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO x_repost_ids(retweet_id,status_id,account_handle,first_seen_at)
		SELECT retweet_id,status_id,account_handle,first_seen_at FROM x_reposts
		WHERE retweet_id IS NOT NULL AND retweet_id<>'' AND retweet_id<>status_id
		ON CONFLICT(retweet_id) DO NOTHING`)
	return err
}

// removeRepostPost soft-deletes the account's stored post id, which a detail
// answer showed to be its repost of another status, in the caller's
// transaction: posts.deleted_at is stamped and its refresh request cleared.
// The row and everything keyed to it (media links, schedule-image candidate
// and review, extraction state and history, translations) stay in place;
// every reader skips a deleted post and its schedule-image candidate (see
// livePostSQL and LiveScheduleAssetSQL). Its event evidence is retired as an
// extraction without events retires it (see retirePostEvidence), and a
// suspected cancellation it raised is withdrawn. A promoted
// post, another account's post or one already deleted is left alone. It
// reports whether a post was deleted.
func removeRepostPost(ctx context.Context, tx *sql.Tx, id, account, now string) (bool, error) {
	var handle sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT account_handle FROM posts WHERE id=? AND source='x' AND promoted_at IS NULL AND deleted_at IS NULL`, id).Scan(&handle)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if handle.String != "" && !strings.EqualFold(handle.String, account) {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE posts SET deleted_at=?,refresh_requested_at=NULL WHERE id=?`, now, id); err != nil {
		return false, err
	}
	// The model's cancellation warning the repost raised has no post behind
	// it; an operator's confirmed cancellation stays.
	if _, err = tx.ExecContext(ctx, `UPDATE events SET cancellation_status='none',cancellation_source=NULL,cancellation_reason=NULL,cancellation_evidence=NULL,
		cancellation_source_item_id=NULL,cancellation_confidence=NULL,cancellation_at=NULL,updated_at=?
		WHERE cancellation_status='llm_suspected' AND cancellation_source_item_id=?`, now, id); err != nil {
		return false, err
	}
	return true, retirePostEvidence(ctx, tx, "x", id, now)
}

// livePostSQL is the condition that the posts row aliased alias is not
// soft-deleted. Every reader of posts applies it, except the write paths'
// existence checks, which keep a deleted ID from being stored again.
func livePostSQL(alias string) string {
	if alias == "" {
		return "deleted_at IS NULL"
	}
	return alias + ".deleted_at IS NULL"
}

// LiveScheduleAssetSQL is the condition that the assets row aliased a is not
// the schedule-image candidate of a soft-deleted post. The candidate's ID is
// scheduleAssetID of its post.
func LiveScheduleAssetSQL(a string) string {
	return a + `.id NOT IN (SELECT 'schedule-x-'||id FROM posts WHERE deleted_at IS NOT NULL)`
}

// retirePostEvidence retires every active event evidence of a removed post
// as the extractor retires evidence a new result no longer names: an
// automatic, unlocked event left without active evidence is retired and the
// rule merges into it are released, while a manual or locked event keeps
// its row and state.
func retirePostEvidence(ctx context.Context, tx *sql.Tx, source, id, now string) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT event_id FROM event_sources WHERE source=? AND source_item_id=? AND deleted_at IS NULL`, source, id)
	if err != nil {
		return err
	}
	events := []string{}
	for rows.Next() {
		var event string
		if err = rows.Scan(&event); err != nil {
			rows.Close()
			return err
		}
		events = append(events, event)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE event_sources SET deleted_at=?,updated_at=? WHERE source=? AND source_item_id=? AND deleted_at IS NULL`, now, now, source, id); err != nil {
		return err
	}
	for _, event := range events {
		var remaining int
		// An upload a release was paired with keeps no retired entry alive.
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_sources WHERE event_id=? AND deleted_at IS NULL AND source<>'bilibili-video'`, event).Scan(&remaining); err != nil {
			return err
		}
		if remaining > 0 {
			continue
		}
		retired, retireErr := tx.ExecContext(ctx, `UPDATE events SET deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL AND provenance='automatic' AND manual_locked=0`, now, now, event)
		if retireErr != nil {
			return retireErr
		}
		if count, _ := retired.RowsAffected(); count > 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE events SET merged_into=NULL,merge_source=NULL,merge_reason=NULL,merged_at=NULL WHERE merged_into=? AND merge_source='rule'`, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillRepostedStatuses marks every pooled status an own account
// reposted, from the x_reposts links, with the first sighting as
// reposted_at. A link whose status the pool does not hold has no wording to
// store, so it waits for a listing that carries the status. It runs on
// every startup, makes no request and is idempotent.
func backfillRepostedStatuses(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE x_statuses SET reposted_at=(SELECT MIN(r.first_seen_at) FROM x_reposts r WHERE r.status_id=x_statuses.id)
		WHERE reposted_at IS NULL AND EXISTS (SELECT 1 FROM x_reposts r WHERE r.status_id=x_statuses.id)`)
	return err
}

// quotedStatusesSQL selects the own X posts (not promoted ones) with an
// available quote that the pool does not hold as quoted yet. A time either
// side cannot read counts as held, so such a post is not written again on
// every startup.
const quotedStatusesSQL = `SELECT p.raw_json,p.published_at FROM posts p
 WHERE p.source='x' AND p.promoted_at IS NULL AND p.deleted_at IS NULL AND json_valid(p.quote_json) AND json_type(p.quote_json,'$.unavailable') IS NOT 'true'
  AND json_valid(p.raw_json)
  AND NOT EXISTS (SELECT 1 FROM x_statuses s WHERE s.id=json_extract(p.quote_json,'$.id') AND s.quoted_at IS NOT NULL
   AND (julianday(s.quoted_at)>=julianday(p.published_at) OR julianday(s.quoted_at) IS NULL OR julianday(p.published_at) IS NULL))`

// backfillQuotedStatuses fills the pool from the quotes stored posts already
// hold, mapping each post's raw_json as the source writers do, with no
// request. It runs on every startup and reads only the posts whose quote the
// pool lacks, so it is idempotent.
func backfillQuotedStatuses(ctx context.Context, tx *sql.Tx, now string) error {
	rows, err := tx.QueryContext(ctx, quotedStatusesSQL)
	if err != nil {
		return err
	}
	type row struct{ raw, publishedAt string }
	found := []row{}
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.raw, &r.publishedAt); err != nil {
			rows.Close()
			return err
		}
		found = append(found, r)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, r := range found {
		var payload map[string]any
		if json.Unmarshal([]byte(r.raw), &payload) != nil {
			continue
		}
		if err = writeQuotedStatus(ctx, tx, xsource.QuoteOf(payload), r.publishedAt, now); err != nil {
			return err
		}
	}
	return nil
}
