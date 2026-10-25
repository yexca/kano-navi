package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// Source freshness records successful recent reads, rather than the time a
// historical page or unrelated workflow last updated the snapshot.
func (s *Store) sourceFetchedAt(ctx context.Context) (map[string]*string, error) {
	out := map[string]*string{"x": nil, "youtube": nil}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sync_state'`).Scan(&exists); err != nil {
		return nil, dbErr(err)
	}
	if exists == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source,last_success_at,metadata_json FROM sync_state WHERE source IN ('x','youtube','x-window','youtube-window','x-timeline-window') AND last_success_at IS NOT NULL`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	latest := map[string]time.Time{}
	for rows.Next() {
		var source, success string
		var metadata sql.NullString
		if err = rows.Scan(&source, &success, &metadata); err != nil {
			return nil, dbErr(err)
		}
		at, parseErr := time.Parse(time.RFC3339Nano, success)
		if parseErr != nil {
			continue
		}
		if strings.HasSuffix(source, "-window") {
			var input struct {
				Window        struct{ Mode, EndTime string }
				HeadFetchedAt string
			}
			if json.Unmarshal([]byte(metadata.String), &input) != nil {
				continue
			}
			if source == "x-timeline-window" {
				head, e := time.Parse(time.RFC3339Nano, input.HeadFetchedAt)
				if e != nil {
					continue
				}
				if head.Before(at) {
					at = head
				}
			}
			switch input.Window.Mode {
			case "recent":
				end, e := time.Parse(time.RFC3339Nano, input.Window.EndTime)
				if e != nil {
					continue
				}
				// Resuming a page does not move its original recent window forward.
				if end.Before(at) {
					at = end
				}
			case "count":
				// A resumed count fetch retains its original head-page read time.
				if source != "x-timeline-window" {
					continue
				}
			default:
				continue
			}
			if source == "youtube-window" {
				source = "youtube"
			} else {
				source = "x"
			}
		}
		if at.After(latest[source]) {
			latest[source] = at
			out[source] = strPtr(ISOTime(at))
		}
	}
	return out, dbErr(rows.Err())
}
