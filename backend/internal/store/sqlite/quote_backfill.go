package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// quotedPostsSQL selects stored X posts whose raw_json names a quoted status
// but that have no quote projection yet. Every vxtwitter answer carries
// "qrt": null, so only an object or a quoted URL counts.
const quotedPostsSQL = `SELECT id,raw_json,COALESCE(media_url,'') FROM posts
 WHERE source='x' AND quote_json IS NULL AND json_valid(raw_json) AND (
  json_type(raw_json,'$.qrt')='object' OR json_type(raw_json,'$.quote')='object'
  OR json_type(raw_json,'$.quotedTweet')='object' OR json_type(raw_json,'$.quoted_tweet')='object'
  OR json_type(raw_json,'$.qrtURL')='text')`

// backfillQuotes gives posts without a stored quote their quote from the
// raw_json each kept, and moves quoted images linked as the post's own images
// to quote-image links.
//
// Such rows list the post's own images first and the quoted status's after
// them, both as post-image. A post-image link is moved when its asset
// is one of the quote's images and not one the post's raw_json declares as
// its own, so the remaining own links keep their positions. An image both
// declare stays the post's own. media_url, the first own image, follows, and
// a schedule candidate made from a quoted image is removed. Only rows without quote_json are read, so the step is idempotent.
func backfillQuotes(ctx context.Context, tx *sql.Tx) error {
	type row struct{ id, raw, mediaURL string }
	rows, err := tx.QueryContext(ctx, quotedPostsSQL)
	if err != nil {
		return ErrSchema
	}
	found := []row{}
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.raw, &r.mediaURL); err != nil {
			rows.Close()
			return ErrSchema
		}
		found = append(found, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrSchema
	}
	fail := func() error { return fmt.Errorf("%w: quote backfill failed", ErrSchema) }
	now := workflowTime(time.Now())
	changed := false
	for _, r := range found {
		var payload map[string]any
		if json.Unmarshal([]byte(r.raw), &payload) != nil {
			continue
		}
		quote := xsource.QuoteOf(payload)
		if quote == nil {
			continue
		}
		own := map[string]bool{}
		ownURLs := xsource.OwnMediaURLs(payload)
		for _, u := range ownURLs {
			if id, _, e := mediaIdentity(u); e == nil {
				own[id] = true
			}
		}
		quoted := map[string]bool{}
		for _, u := range quote.MediaURLs {
			if id, _, e := mediaIdentity(u); e == nil && !own[id] {
				quoted[id] = true
			}
		}
		for id := range quoted {
			if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role='post-image' AND media_id=?`, r.id, id); err != nil {
				return fail()
			}
		}
		if id, _, e := mediaIdentity(r.mediaURL); e == nil && quoted[id] {
			first := ""
			if len(ownURLs) > 0 {
				first = ownURLs[0]
			}
			if _, err = tx.ExecContext(ctx, `UPDATE posts SET media_url=? WHERE id=?`, nullable(first), r.id); err != nil {
				return fail()
			}
		}
		if err = dropQuotedScheduleCandidate(ctx, tx, r.id, quoted); err != nil {
			return fail()
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM media_links WHERE owner_type='post' AND owner_id=? AND role=?`, r.id, roleQuoteImage); err != nil {
			return fail()
		}
		if err = writeQuoteMedia(ctx, tx, r.id, quote.MediaURLs, now); err != nil {
			return fail()
		}
		if _, err = tx.ExecContext(ctx, `UPDATE posts SET quote_json=? WHERE id=?`, quoteJSON(quote), r.id); err != nil {
			return fail()
		}
		changed = true
	}
	if changed {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return fail()
		}
	}
	return nil
}

// dropQuotedScheduleCandidate removes a post's schedule candidate whose image
// is one of its quote's: a post without images of its own took the quote's
// first image as its candidate. A candidate an operator labelled or gave a
// period keeps that decision.
func dropQuotedScheduleCandidate(ctx context.Context, tx *sql.Tx, postID string, quoted map[string]bool) error {
	id := "schedule-x-" + postID
	var url string
	err := tx.QueryRowContext(ctx, `SELECT a.url FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id=a.id
	 WHERE a.id=? AND a.kind='schedule' AND COALESCE(r.manual_status,'unreviewed')='unreviewed' AND a.manual_period_start IS NULL`, id).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if media, _, e := mediaIdentity(url); e != nil || !quoted[media] {
		return nil
	}
	for _, statement := range []string{
		`DELETE FROM media_links WHERE owner_type='asset' AND owner_id=?`,
		`DELETE FROM schedule_asset_reviews WHERE asset_id=?`,
		`DELETE FROM assets WHERE id=?`,
	} {
		if _, err = tx.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	return nil
}
