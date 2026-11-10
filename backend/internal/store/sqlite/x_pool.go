package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// The X status pool (x_statuses) holds statuses that are not Kano's posts:
// every status an own account quotes, whatever its author, every status of a
// related account (see x_related.go) and a related account's status an own
// account reposted. No public reader
// reads or joins it: a pooled status reaches the board only as the quote
// projection stored on the quoting post (posts.quote_json) and the
// translations keyed by its status ID. Both the pool row and the projection
// come from the same xsource.Quote, so the pool text equals the quote text
// the board shows. x_reposts records the own accounts' reposts. See the
// related-account rules in the source architecture documentation.

// poolRaw is the sanitized raw_json of a pooled status: what a later
// promotion needs without a new request, including its images.
func poolRaw(q *xsource.Quote) string {
	media := q.MediaURLs
	if media == nil {
		media = []string{}
	}
	body, _ := json.Marshal(map[string]any{
		"id": q.ID, "text": q.Text, "created_at": q.PublishedAt, "url": q.URL,
		"author":    map[string]any{"screen_name": q.AuthorHandle, "name": q.AuthorName},
		"mediaURLs": media,
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
// its time; a later listing only fills in an author that was unknown. The
// board shows no reposts. A reposted status of a related account also enters
// the pool with reposted_at, from the repost item when its own timeline has
// not stored it yet, and its reasons are evaluated again; any other author's
// repost records the link alone. It reports whether a post changed.
func writeXReposts(ctx context.Context, tx *sql.Tx, account string, reposts []xsource.Repost, now string) (bool, error) {
	if len(reposts) == 0 {
		return false, nil
	}
	accounts, err := readXRelatedAccounts(ctx, tx)
	if err != nil {
		return false, err
	}
	rules := relatedKeywords(accounts)
	pooled := []string{}
	for _, r := range reposts {
		if !quoteIDPattern.MatchString(r.ID) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO x_reposts(status_id,account_handle,author_handle,first_seen_at) VALUES(?,?,?,?)
			ON CONFLICT(status_id,account_handle) DO UPDATE SET author_handle=COALESCE(x_reposts.author_handle,excluded.author_handle)`,
			r.ID, strings.ToLower(account), nullable(r.Author), now); err != nil {
			return false, err
		}
		if _, related := rules[strings.ToLower(r.Author)]; !related {
			continue
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
	return reconcileXStatuses(ctx, tx, accounts, pooled, now)
}

// quotedStatusesSQL selects the own X posts (not promoted ones) with an
// available quote that the pool does not hold as quoted yet. A time either
// side cannot read counts as held, so such a post is not written again on
// every startup.
const quotedStatusesSQL = `SELECT p.raw_json,p.published_at FROM posts p
 WHERE p.source='x' AND p.promoted_at IS NULL AND json_valid(p.quote_json) AND json_type(p.quote_json,'$.unavailable') IS NOT 'true'
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
