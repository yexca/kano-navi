package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// bilibiliReleaseWindow is how far an upload's publication may be from the
// release time a Bilibili dynamic announced for it to be that release.
const bilibiliReleaseWindow = 30 * time.Minute

// bilibiliVideoSource names the event_sources row that pairs a release with
// its upload. It is evidence of the work, not an announcement, so it keeps no
// retired entry alive.
const bilibiliVideoSource = "bilibili-video"

type pairingRelease struct {
	id, url, announcement string
	start                 time.Time
	// paired is the upload its active bilibili-video row names, or "".
	paired string
}

type pairingUpload struct {
	id, url   string
	published time.Time
}

// pairBilibiliReleases recomputes the pairing of the automatic, unlocked
// releases that Bilibili dynamics announced in the window with the account's
// uploads, as the merge rules recompute theirs. A release and an upload pair
// when each is the other's only match within bilibiliReleaseWindow and no
// other entry holds the upload: the release's link becomes the upload's page
// and an active bilibili-video event source names the upload. A pairing that
// no longer holds is released: its source row is retired and the link goes
// back to the announcement's. Locked and manual events and their source rows
// are never touched, and an unchanged run writes nothing. It reports how many
// releases changed.
func pairBilibiliReleases(ctx context.Context, tx *sql.Tx, from, to, now string) (int, error) {
	releases, err := loadPairingReleases(ctx, tx, from, to)
	if err != nil {
		return 0, err
	}
	uploads, err := loadPairingUploads(ctx, tx, releases)
	if err != nil {
		return 0, err
	}
	matches := func(r pairingRelease, u pairingUpload) bool {
		gap := u.published.Sub(r.start)
		return gap <= bilibiliReleaseWindow && gap >= -bilibiliReleaseWindow
	}
	byRelease, byUpload := map[string][]string{}, map[string][]string{}
	for _, r := range releases {
		for _, u := range uploads {
			if matches(r, u) {
				byRelease[r.id] = append(byRelease[r.id], u.id)
				byUpload[u.id] = append(byUpload[u.id], r.id)
			}
		}
	}
	uploadURL := map[string]string{}
	for _, u := range uploads {
		uploadURL[u.id] = u.url
	}
	targets := make([]string, len(releases))
	for i, r := range releases {
		if ids := byRelease[r.id]; len(ids) == 1 && len(byUpload[ids[0]]) == 1 {
			targets[i] = ids[0]
		}
	}
	// Released pairings are retired first, so an upload one release lets go
	// is free for another in the same run.
	for i, r := range releases {
		if r.paired != "" && r.paired != targets[i] {
			if _, err = tx.ExecContext(ctx, `UPDATE event_sources SET deleted_at=?,updated_at=? WHERE event_id=? AND source=? AND source_item_id=? AND deleted_at IS NULL`,
				now, now, r.id, bilibiliVideoSource, r.paired); err != nil {
				return 0, err
			}
		}
	}
	changed := 0
	for i, r := range releases {
		target := targets[i]
		if target != "" && target != r.paired {
			held, heldErr := uploadHeld(ctx, tx, r.id, target)
			if heldErr != nil {
				return 0, heldErr
			}
			if held {
				target = ""
			}
		}
		if target == r.paired && (target == "" || r.url == uploadURL[target]) {
			continue
		}
		link := r.announcement
		if target != "" {
			link = uploadURL[target]
			if _, err = tx.ExecContext(ctx, `INSERT INTO event_sources(event_id,source,source_item_id,source_key,url,raw_json,deleted_at,created_at,updated_at)
 VALUES(?,?,?,'upload',?,NULL,NULL,?,?)
 ON CONFLICT(source,source_item_id,source_key) DO UPDATE SET event_id=excluded.event_id,url=excluded.url,deleted_at=NULL,updated_at=excluded.updated_at`,
				r.id, bilibiliVideoSource, target, link, now, now); err != nil {
				return 0, err
			}
		}
		if link != "" && link != r.url {
			if _, err = tx.ExecContext(ctx, `UPDATE events SET url=?,updated_at=? WHERE id=? AND manual_locked=0 AND provenance='automatic'`, link, now, r.id); err != nil {
				return 0, err
			}
		}
		changed++
	}
	return changed, nil
}

// uploadHeld reports whether an entry other than release holds the upload's
// bilibili-video row: a locked or manual entry whatever the row's state, or
// a live entry whose row is active. The row of a retired automatic entry, or
// one it released, is free.
func uploadHeld(ctx context.Context, tx *sql.Tx, release, upload string) (bool, error) {
	var held int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_sources s JOIN events e ON e.id=s.event_id
 WHERE s.source=? AND s.source_item_id=? AND s.source_key='upload' AND s.event_id<>?
 AND (e.manual_locked<>0 OR e.provenance<>'automatic' OR s.deleted_at IS NULL AND e.deleted_at IS NULL)`,
		bilibiliVideoSource, upload, release).Scan(&held)
	return held > 0, err
}

// loadPairingReleases reads the releases the pairing owns, with the link of
// the dynamic that announced each and the upload it is paired with now.
func loadPairingReleases(ctx context.Context, tx *sql.Tx, from, to string) ([]pairingRelease, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.starts_at,COALESCE(e.url,''),
 COALESCE((SELECT s.url FROM event_sources s WHERE s.event_id=e.id AND s.source='bilibili' AND s.deleted_at IS NULL ORDER BY s.id LIMIT 1),''),
 COALESCE((SELECT s.source_item_id FROM event_sources s WHERE s.event_id=e.id AND s.source=? AND s.deleted_at IS NULL ORDER BY s.id LIMIT 1),'')
 FROM events e
 WHERE e.source='bilibili' AND e.event_type='release' AND e.starts_at IS NOT NULL AND e.deleted_at IS NULL
 AND e.provenance='automatic' AND e.manual_locked=0 AND e.starts_on BETWEEN ? AND ? ORDER BY e.id`, bilibiliVideoSource, from, to)
	if err != nil {
		return nil, err
	}
	out := []pairingRelease{}
	for rows.Next() {
		var r pairingRelease
		var start string
		if err = rows.Scan(&r.id, &start, &r.url, &r.announcement, &r.paired); err != nil {
			rows.Close()
			return nil, err
		}
		if r.start = parseInstant(start); !r.start.IsZero() {
			out = append(out, r)
		}
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// loadPairingUploads reads the Bilibili uploads published near any of the
// releases.
func loadPairingUploads(ctx context.Context, tx *sql.Tx, releases []pairingRelease) ([]pairingUpload, error) {
	if len(releases) == 0 {
		return nil, nil
	}
	first, last := releases[0].start, releases[0].start
	for _, r := range releases[1:] {
		first, last = minTime(first, r.start), maxTime(last, r.start)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,url,published_at FROM videos WHERE source='bilibili' AND published_at IS NOT NULL
 AND julianday(published_at)>=julianday(?) AND julianday(published_at)<=julianday(?) ORDER BY id`,
		first.Add(-bilibiliReleaseWindow).UTC().Format(time.RFC3339Nano), last.Add(bilibiliReleaseWindow).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	out := []pairingUpload{}
	for rows.Next() {
		var u pairingUpload
		var published string
		if err = rows.Scan(&u.id, &u.url, &published); err != nil {
			rows.Close()
			return nil, err
		}
		if u.published = parseInstant(published); !u.published.IsZero() {
			out = append(out, u)
		}
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
