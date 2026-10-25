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
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES(?,?,?,?,?,?) ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=excluded.cursor_id,cursor_time=excluded.cursor_time,last_success_at=excluded.last_success_at,metadata_json=excluded.metadata_json`, source, account, nullable(cursorID), nullable(cursorTime), now, string(encoded))
	return err
}

// WriteHistoryPage commits one API page together with its continuation, so a
// retry can neither skip nor repeat a page. It writes only the window and
// backfill checkpoints and never touches the incremental cursors. For X it
// returns how many posts and schedule image candidates the page wrote; a
// KeepStored page skips posts that are already stored.
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return history.Written{}, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return history.Written{}, err
	}
	now := workflowTime(time.Now())
	written, candidates := 0, 0
	if page.Source == history.SourceX {
		kept := map[string]bool{}
		for _, post := range page.Posts {
			if page.KeepStored {
				var exists int
				lookupErr := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE id=?`, post.ID).Scan(&exists)
				if lookupErr == nil {
					kept[post.ID] = true
					continue
				}
				if !errors.Is(lookupErr, sql.ErrNoRows) {
					return history.Written{}, ErrSourceWrite
				}
			}
			// A post already owned by another account keeps its owner.
			stored, writeErr := writeXPost(ctx, tx, page.Account, post, now)
			if writeErr != nil {
				return history.Written{}, ErrSourceWrite
			}
			if stored {
				written++
			}
		}
		for _, candidate := range page.ScheduleCandidates {
			if candidate.MediaURL == "" || candidate.SourceURL == "" || kept[candidate.PostID] {
				continue
			}
			if err = writeScheduleCandidate(ctx, tx, candidate, now); err != nil {
				return history.Written{}, ErrSourceWrite
			}
			candidates++
		}
	} else {
		for _, video := range page.Videos {
			// Stored reservation state (kind, upcoming flag, scheduled time) is
			// owned by the recent synchronization and survives a history page.
			// The API names thumbnails differently from the watch page, so only
			// a new title is kept as history here.
			prior, existed, priorErr := readVideoSource(ctx, tx, video.ID)
			if priorErr != nil {
				return history.Written{}, ErrSourceWrite
			}
			if existed && prior.Title != video.Title {
				if err = recordVideoRevision(ctx, tx, video.ID, prior, revisionSync, now); err != nil {
					return history.Written{}, ErrSourceWrite
				}
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,raw_json) VALUES(?,'youtube',?,?,NULL,?,?,?,?,'{}') ON CONFLICT(id) DO UPDATE SET title=excluded.title,published_at=COALESCE(excluded.published_at,videos.published_at),url=excluded.url,thumbnail_url=COALESCE(excluded.thumbnail_url,videos.thumbnail_url)`, video.ID, video.Title, nullable(video.PublishedAt), video.URL, nullable(video.ThumbnailURL), video.Kind, video.IsUpcoming); err != nil {
				return history.Written{}, ErrSourceWrite
			}
			if video.ThumbnailURL != "" {
				if err = replaceMediaLink(ctx, tx, "youtube", video.ThumbnailURL, "video", video.ID, "thumbnail", 0, now); err != nil {
					return history.Written{}, ErrSourceWrite
				}
			}
			written++
		}
	}
	if err = upsertSyncState(ctx, tx, checkpoint, page.CheckpointKey, page.Next, "", map[string]any{"window": page.Window, "discovered": page.Discovered}, now); err != nil {
		return history.Written{}, ErrSourceWrite
	}
	if page.Backfill != nil {
		cursor := page.Window.StartTime
		var pending any
		switch {
		case page.Backfill.Pending:
			cursor, pending = page.Window.EndTime, page.Window
		case page.Backfill.SearchLimitReached:
			cursor = page.Window.EndTime
			var oldest sql.NullString
			lookupErr := tx.QueryRowContext(ctx, `SELECT published_at FROM videos WHERE source='youtube' AND julianday(published_at) IS NOT NULL ORDER BY julianday(published_at) ASC LIMIT 1`).Scan(&oldest)
			if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
				return history.Written{}, ErrSourceWrite
			}
			if oldest.Valid && oldest.String != "" {
				cursor = oldest.String
			}
		}
		if err = upsertSyncState(ctx, tx, page.Source+"-backfill", page.Account, "", cursor, map[string]any{"pendingWindow": pending}, now); err != nil {
			return history.Written{}, ErrSourceWrite
		}
	}
	if written > 0 {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return history.Written{}, ErrSourceWrite
		}
	}
	if err = tx.Commit(); err != nil {
		return history.Written{}, ErrSourceWrite
	}
	out := history.Written{ScheduleAssets: candidates}
	if page.Source == history.SourceX {
		out.Posts = written
	}
	return out, nil
}
