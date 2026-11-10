package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// AdminPostSchedule is a post's schedule image record: which of the post's
// own images it shows (nil when none of them), its model and operator
// labels, and whether an operator picked the image.
type AdminPostSchedule struct {
	ID           string `json:"id"`
	Index        *int   `json:"index"`
	LLMStatus    string `json:"llmStatus"`
	ManualStatus string `json:"manualStatus"`
	Picked       bool   `json:"picked"`
}

// SchedulePick is an operator's choice of one of an X post's own images as
// its schedule image, with the label reason and, optionally, the Japan dates
// it covers (both or neither).
type SchedulePick struct {
	PostID     string
	Index      int
	Reason     string
	Start, End string
}

var (
	ErrSchedulePickPost  = errors.New("x post not found")
	ErrSchedulePickImage = errors.New("post image not found")
)

type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// scheduleAssetID is the schedule asset of one X post, as synchronization
// names it.
func scheduleAssetID(postID string) string { return "schedule-x-" + postID }

// postImageSources lists a post's own image URLs in the order the console
// shows them: its linked post images, or else its stored media URL.
func postImageSources(ctx context.Context, q rowQueryer, postID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT ma.source_url FROM media_links ml JOIN media_assets ma ON ma.id=ml.media_id
	 WHERE ml.owner_type='post' AND ml.owner_id=? AND ml.role='post-image' AND ma.source_url IS NOT NULL AND ma.status IS NOT NULL
	 ORDER BY ml.position ASC`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []string{}
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sources) > 0 {
		return sources, nil
	}
	var mediaURL sql.NullString
	err = q.QueryRowContext(ctx, `SELECT media_url FROM posts WHERE id=?`, postID).Scan(&mediaURL)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if mediaURL.Valid && strings.TrimSpace(mediaURL.String) != "" {
		sources = append(sources, mediaURL.String)
	}
	return sources, nil
}

// attachAdminPostSchedules fills each post's schedule image record, if any.
// The records of the page are read in batches; only a post that has one reads
// its image sources to find which image it shows.
func (s *Store) attachAdminPostSchedules(ctx context.Context, posts []AdminPost) error {
	byAsset := make(map[string]int, len(posts))
	ids := make([]string, 0, len(posts))
	for i := range posts {
		id := scheduleAssetID(posts[i].ID)
		if _, seen := byAsset[id]; !seen {
			byAsset[id] = i
			ids = append(ids, id)
		}
	}
	type record struct {
		item AdminPostSchedule
		url  string
	}
	found := map[int]record{}
	for _, chunk := range translationChunks(ids) {
		rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.url, a.manual_image_at, COALESCE(r.llm_status,'pending'), COALESCE(r.manual_status,'unreviewed')
	 FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id=a.id WHERE a.kind='schedule' AND a.id IN (`+placeholders(len(chunk))+`)`, idArgs(chunk)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r record
			var picked sql.NullString
			if err = rows.Scan(&r.item.ID, &r.url, &picked, &r.item.LLMStatus, &r.item.ManualStatus); err != nil {
				rows.Close()
				return err
			}
			r.item.Picked = picked.Valid
			found[byAsset[r.item.ID]] = r
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	for i := range posts {
		r, ok := found[i]
		if !ok {
			continue
		}
		item := r.item
		if shown, _, identityErr := mediaIdentity(r.url); identityErr == nil {
			sources, err := postImageSources(ctx, s.db, posts[i].ID)
			if err != nil {
				return err
			}
			for index, source := range sources {
				if id, _, sourceErr := mediaIdentity(source); sourceErr == nil && id == shown {
					item.Index = &index
					break
				}
			}
		}
		posts[i].ScheduleImage = &item
	}
	return nil
}

// PickPostScheduleImage makes the chosen image the post's schedule asset,
// labelled `schedule` by the operator with the reason, and sets its manual
// period when one is given. manual_image_at keeps synchronization from
// replacing the image; a different image than the asset showed returns its
// model verdict to `pending`, as a new URL from synchronization does. It
// returns the asset ID.
func (s *Store) PickPostScheduleImage(ctx context.Context, pick SchedulePick, now string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", dbErr(err)
	}
	defer tx.Rollback()
	var source, postURL string
	var account sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT source, url, account_handle FROM posts WHERE id=?`, pick.PostID).Scan(&source, &postURL, &account)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && source != "x") {
		return "", ErrSchedulePickPost
	}
	if err != nil {
		return "", dbErr(err)
	}
	sources, err := postImageSources(ctx, tx, pick.PostID)
	if err != nil {
		return "", dbErr(err)
	}
	if pick.Index < 0 || pick.Index >= len(sources) {
		return "", ErrSchedulePickImage
	}
	mediaURL := sources[pick.Index]
	if _, _, err = mediaIdentity(mediaURL); err != nil {
		return "", ErrSchedulePickImage
	}
	id := scheduleAssetID(pick.PostID)
	var oldURL string
	if err = tx.QueryRowContext(ctx, `SELECT url FROM assets WHERE id=?`, id).Scan(&oldURL); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", dbErr(err)
	}
	if oldURL != "" && oldURL != mediaURL {
		if _, err = tx.ExecContext(ctx, `UPDATE schedule_asset_reviews SET llm_status='pending',updated_at=? WHERE asset_id=?`, now, id); err != nil {
			return "", dbErr(err)
		}
	}
	var start, end, reason any
	if pick.Start != "" {
		start, end, reason = pick.Start, pick.End, pick.Reason
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO assets(id,kind,url,source_url,source_account,updated_at,raw_json,manual_image_at,manual_period_start,manual_period_end,manual_period_reason)
	 VALUES(?, 'schedule', ?, ?, ?, ?, NULL, ?, ?, ?, ?)
	 ON CONFLICT(id) DO UPDATE SET asset_version=assets.asset_version+CASE WHEN assets.url!=excluded.url THEN 1 ELSE 0 END,
	   url=excluded.url, source_url=excluded.source_url, source_account=excluded.source_account,
	   updated_at=excluded.updated_at, manual_image_at=excluded.manual_image_at,
	   manual_period_start=CASE WHEN excluded.manual_period_start IS NULL THEN assets.manual_period_start ELSE excluded.manual_period_start END,
	   manual_period_end=CASE WHEN excluded.manual_period_start IS NULL THEN assets.manual_period_end ELSE excluded.manual_period_end END,
	   manual_period_reason=CASE WHEN excluded.manual_period_start IS NULL THEN assets.manual_period_reason ELSE excluded.manual_period_reason END`,
		id, mediaURL, postURL, account, now, now, start, end, reason); err != nil {
		return "", dbErr(err)
	}
	if err = replaceMediaLink(ctx, tx, "x", mediaURL, "asset", id, "schedule-image", 0, now); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_asset_reviews(asset_id,llm_status,manual_status,manual_reason,manual_checked_at,updated_at)
	 VALUES(?, 'pending', 'schedule', ?, ?, ?)
	 ON CONFLICT(asset_id) DO UPDATE SET manual_status=excluded.manual_status, manual_reason=excluded.manual_reason,
	   manual_checked_at=excluded.manual_checked_at, updated_at=excluded.updated_at`, id, pick.Reason, now, now); err != nil {
		return "", dbErr(err)
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", dbErr(err)
	}
	return id, nil
}
