package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
)

// SyncState reads one cursor row. A missing row is an empty state.
func (s *Store) SyncState(ctx context.Context, source, account string) (history.State, error) {
	state := history.State{Metadata: map[string]any{}}
	if s == nil || s.db == nil {
		return state, ErrUnavailable
	}
	var cursorID, cursorTime, metadata sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT cursor_id,cursor_time,metadata_json FROM sync_state WHERE source=? AND account_id=?`, source, account).Scan(&cursorID, &cursorTime, &metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, ErrUnavailable
	}
	state.Found, state.CursorID, state.CursorTime = true, cursorID.String, cursorTime.String
	if metadata.Valid {
		_ = json.Unmarshal([]byte(metadata.String), &state.Metadata)
		if state.Metadata == nil {
			state.Metadata = map[string]any{}
		}
	}
	return state, nil
}

// WriteSyncMetadata replaces a bookkeeping row's metadata without recording a
// source success, such as the account rotation of a request budget.
func (s *Store) WriteSyncMetadata(ctx context.Context, source, account string, metadata map[string]any) error {
	if s == nil || s.db == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES(?,?,NULL,NULL,NULL,?) ON CONFLICT(source,account_id) DO UPDATE SET metadata_json=excluded.metadata_json`, source, account, string(encoded)); err != nil || tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

func upsertSyncState(ctx context.Context, tx *sql.Tx, source, account, cursorID, cursorTime string, metadata map[string]any, now string) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES(?,?,?,?,?,?) ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=excluded.cursor_id,cursor_time=excluded.cursor_time,last_success_at=COALESCE(excluded.last_success_at,sync_state.last_success_at),metadata_json=excluded.metadata_json`, source, account, nullable(cursorID), nullable(cursorTime), nullable(now), string(encoded))
	return err
}

// WriteHistoryPage commits one fetch page together with its continuation, so
// a retry can neither skip nor repeat a page. It writes only the fetch and
// backfill checkpoints, none for a page without a CheckpointKey, and never
// touches the incremental cursors. It counts the page's records that are new
// and the ones already stored; a KeepStored page leaves the stored ones, with
// their media links and schedule candidates, as they are.
func (s *Store) WriteHistoryPage(ctx context.Context, page history.Page) (history.Written, error) {
	if s == nil || s.db == nil || !s.workflowWritable {
		return history.Written{}, ErrSourceWrite
	}
	if page.Source != history.SourceX && page.Source != history.SourceYoutube {
		return history.Written{}, ErrSourceWrite
	}
	checkpoint := page.Source + "-window"
	if page.Checkpoint != "" {
		if page.Source != history.SourceX || page.Checkpoint != history.TimelineCheckpoint {
			return history.Written{}, ErrSourceWrite
		}
		checkpoint = page.Checkpoint
	}
	if page.CheckpointKey == "" && (page.CheckpointOnly || page.Backfill != nil) {
		return history.Written{}, ErrSourceWrite
	}
	if page.CheckpointOnly && (checkpoint != history.TimelineCheckpoint || len(page.Posts) > 0 || len(page.ScheduleCandidates) > 0 || len(page.Videos) > 0 || page.Backfill != nil) {
		return history.Written{}, ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return history.Written{}, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return history.Written{}, err
	}
	now := workflowTime(time.Now())
	out := history.Written{NewIDs: []string{}}
	changed := false
	if page.Source == history.SourceX {
		kept := map[string]bool{}
		for _, post := range page.Posts {
			existed, lookupErr := rowExists(ctx, tx, `SELECT 1 FROM posts WHERE id=?`, post.ID)
			if lookupErr != nil {
				return history.Written{}, ErrSourceWrite
			}
			if existed && page.KeepStored {
				kept[post.ID] = true
				out.Stored++
				continue
			}
			// A post already owned by another account keeps its owner.
			stored, writeErr := writeXPost(ctx, tx, page.Account, post, now)
			if writeErr != nil {
				return history.Written{}, ErrSourceWrite
			}
			changed = changed || stored
			if existed || !stored {
				out.Stored++
				continue
			}
			out.New++
			out.NewIDs = append(out.NewIDs, post.ID)
		}
		for _, candidate := range page.ScheduleCandidates {
			if candidate.MediaURL == "" || candidate.SourceURL == "" || kept[candidate.PostID] {
				continue
			}
			if err = writeScheduleCandidate(ctx, tx, candidate, now); err != nil {
				return history.Written{}, ErrSourceWrite
			}
			out.ScheduleAssets++
		}
	} else {
		for _, video := range page.Videos {
			// Stored reservation state (kind, upcoming flag, scheduled time) is
			// owned by the recent synchronization and survives a history page.
			// The API names thumbnails differently from the watch page, so only
			// a new title is kept as history here, and a stored thumbnail stays:
			// replacing it with the API's form of the same image would make the
			// next detail check read as a replacement.
			prior, existed, priorErr := readVideoSource(ctx, tx, video.ID)
			if priorErr != nil {
				return history.Written{}, ErrSourceWrite
			}
			if existed {
				out.Stored++
				if page.KeepStored {
					continue
				}
			} else {
				out.New++
				out.NewIDs = append(out.NewIDs, video.ID)
			}
			if existed && prior.Title != video.Title {
				if err = recordVideoRevision(ctx, tx, video.ID, prior, revisionSync, now); err != nil {
					return history.Written{}, ErrSourceWrite
				}
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,raw_json) VALUES(?,'youtube',?,?,NULL,?,?,?,?,'{}') ON CONFLICT(id) DO UPDATE SET title=excluded.title,published_at=COALESCE(excluded.published_at,videos.published_at),url=excluded.url,thumbnail_url=COALESCE(videos.thumbnail_url,excluded.thumbnail_url)`, video.ID, video.Title, nullable(video.PublishedAt), video.URL, nullable(video.ThumbnailURL), video.Kind, video.IsUpcoming); err != nil {
				return history.Written{}, ErrSourceWrite
			}
			if video.ThumbnailURL != "" && prior.ThumbnailURL == "" {
				if err = replaceMediaLink(ctx, tx, "youtube", video.ThumbnailURL, "video", video.ID, "thumbnail", 0, now); err != nil {
					return history.Written{}, ErrSourceWrite
				}
			}
			changed = true
		}
		// The Data API answers for listed videos go through the recent step's
		// upsert, so manual locks and deletion tombstones hold, and their
		// localized titles become platform translations.
		described, describeErr := writeYoutubeVideos(ctx, tx, page.Described, now)
		if describeErr != nil {
			return history.Written{}, describeErr
		}
		translated, translateErr := writeYoutubeLocalizations(ctx, tx, page.Localized, now)
		if translateErr != nil {
			return history.Written{}, translateErr
		}
		changed = changed || described || translated
	}
	metadata := map[string]any{"window": page.Window, "discovered": page.Discovered}
	if page.HeadFetchedAt != "" {
		metadata["headFetchedAt"] = page.HeadFetchedAt
	}
	successAt := now
	if page.CheckpointOnly {
		successAt = ""
	}
	if page.CheckpointKey != "" {
		if err = upsertSyncState(ctx, tx, checkpoint, page.CheckpointKey, page.Next, "", metadata, successAt); err != nil {
			return history.Written{}, ErrSourceWrite
		}
	}
	if page.Backfill != nil {
		cursor := page.Window.StartTime
		var pending any
		if page.Backfill.Pending {
			cursor, pending = page.Window.EndTime, page.Window
		}
		if err = upsertSyncState(ctx, tx, page.Source+"-backfill", page.Account, "", cursor, map[string]any{"pendingWindow": pending}, now); err != nil {
			return history.Written{}, ErrSourceWrite
		}
	}
	if changed {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return history.Written{}, ErrSourceWrite
		}
	}
	if err = tx.Commit(); err != nil {
		return history.Written{}, ErrSourceWrite
	}
	return out, nil
}

// rowExists runs a single-row lookup and reports whether it found the row.
func rowExists(ctx context.Context, tx *sql.Tx, query string, args ...any) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, query, args...).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
