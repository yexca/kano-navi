package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/works"
)

// Works upload covers (see docs/architecture/media-cache.md#works-upload-covers).
// An upload's cover is the first of its videos, in index order, with a ready
// image: the stored video's thumbnail, else the image the media step
// downloaded for the works list (media_links owner_type work-video). A video
// that has neither is looked up once: a YouTube thumbnail address is fixed,
// a niconico or Bilibili one comes from its platform. Nothing here writes a
// videos row.

const workVideoOwner = "work-video"

// workCoverRetry is how long a failed niconico or Bilibili lookup waits,
// doubling per attempt up to a day.
const workCoverRetry = 30 * time.Minute

// WorkVideoRef is one video of the works list.
type WorkVideoRef struct{ Platform, ID string }

func (r WorkVideoRef) owner() string { return r.Platform + ":" + r.ID }

// WorkCoverPlan is what one media step does for the works list before
// downloading: the YouTube thumbnails it queued, the lookups it should make
// (newest upload first), and how the uploads stand.
type WorkCoverPlan struct {
	Queued  int
	Lookups []WorkVideoRef
	// Covered uploads have a ready cover, Waiting ones a queued image or a
	// lookup that is not due, and Uncovered ones no video left to try.
	Covered, Waiting, Uncovered int
}

// workCover is the cover state of one video.
type workCover struct {
	ready     *mediaReference
	exhausted bool
	waiting   bool
	lookup    bool
}

type workCoverState struct {
	stored  map[string]storedWorkVideo
	links   map[string]linkedWorkCover
	lookups map[string]workLookup
	now     time.Time
}

type storedWorkVideo struct {
	thumbnail    *string
	pinnedMedia  string
	pinnedHash   string
	hasThumbnail bool
	thumbnailRef *mediaReference
	pinnedRef    *mediaReference
	storedSource string
	merged       sql.NullString
	title        string
	publishedAt  sql.NullString
	scheduledAt  sql.NullString
	url          string
	duration     sql.NullInt64
	availability sql.NullString
}

type linkedWorkCover struct {
	asset   mediaAsset
	retryAt sql.NullString
	err     sql.NullString
}

type workLookup struct {
	status  string
	retryAt sql.NullString
	code    sql.NullString
}

func storedKey(platform, id string) string { return platform + ":" + id }

// storedPlatform maps a works platform to the videos source that stores it.
func storedPlatform(platform string) string {
	switch platform {
	case works.YouTube, works.Bilibili:
		return platform
	}
	return ""
}

// loadWorkCoverState reads the stored videos, works images and lookups the
// index names, in batched queries.
func (s *Store) loadWorkCoverState(ctx context.Context, index works.Index, now time.Time) (*workCoverState, error) {
	state := &workCoverState{stored: map[string]storedWorkVideo{}, links: map[string]linkedWorkCover{}, lookups: map[string]workLookup{}, now: now}
	ids := []string{}
	for _, upload := range index.Uploads {
		for _, video := range upload.Videos {
			if storedPlatform(video.Platform) != "" {
				ids = append(ids, video.ID)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for start := 0; start < len(ids); start += memoChunk {
		chunk := ids[start:min(len(ids), start+memoChunk)]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id,source,COALESCE(manual_title,title),published_at,scheduled_at,url,thumbnail_url,COALESCE(manual_thumbnail_media_id,''),COALESCE(manual_thumbnail_sha256,''),duration_seconds,COALESCE(manual_availability,availability),merged_into
 FROM videos WHERE source IN('youtube','bilibili') AND id IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var id string
			var v storedWorkVideo
			var thumb sql.NullString
			if err = rows.Scan(&id, &v.storedSource, &v.title, &v.publishedAt, &v.scheduledAt, &v.url, &thumb, &v.pinnedMedia, &v.pinnedHash, &v.duration, &v.availability, &v.merged); err != nil {
				_ = rows.Close()
				return nil, dbErr(err)
			}
			if thumb.Valid && strings.TrimSpace(thumb.String) != "" {
				v.thumbnail, v.hasThumbnail = &thumb.String, true
			}
			state.stored[storedKey(v.storedSource, id)] = v
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, dbErr(err)
		}
	}
	thumbnails := []*string{}
	for _, v := range state.stored {
		thumbnails = append(thumbnails, v.thumbnail)
	}
	if err := s.prefetchMedia(ctx, thumbnails...); err != nil {
		return nil, err
	}
	for key, v := range state.stored {
		if v.hasThumbnail {
			ref, err := s.resolveMedia(ctx, v.thumbnail)
			if err != nil {
				return nil, err
			}
			v.thumbnailRef = ref
		}
		if v.pinnedMedia != "" && safeHash(v.pinnedMedia) && safeHash(v.pinnedHash) {
			if url, servable := s.mediaVersionURL(ctx, v.pinnedMedia, v.pinnedHash); servable {
				media := v.pinnedMedia
				v.pinnedRef = &mediaReference{ID: &media, Status: "ready", PublicURL: &url}
			}
		}
		state.stored[key] = v
	}
	rows, err := s.db.QueryContext(ctx, `SELECT l.owner_id,a.id,a.source_url,a.cache_path,a.mime_type,a.byte_size,a.sha256,NULLIF(a.width,0),NULLIF(a.height,0),a.status,a.next_retry_at,a.last_error
 FROM media_links l JOIN media_assets a ON a.id=l.media_id WHERE l.owner_type=? AND l.role='thumbnail' AND l.position=0`, workVideoOwner)
	if err != nil {
		return nil, dbErr(err)
	}
	for rows.Next() {
		var owner string
		var link linkedWorkCover
		var cp, mime, sha sql.NullString
		var bs, w, h sql.NullInt64
		if err = rows.Scan(&owner, &link.asset.ID, &link.asset.SourceURL, &cp, &mime, &bs, &sha, &w, &h, &link.asset.Status, &link.retryAt, &link.err); err != nil {
			_ = rows.Close()
			return nil, dbErr(err)
		}
		link.asset.CachePath, link.asset.MIMEType, link.asset.SHA256 = nullString(cp), nullString(mime), nullString(sha)
		link.asset.ByteSize, link.asset.Width, link.asset.Height = nullInt(bs), nullInt(w), nullInt(h)
		state.links[owner] = link
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, dbErr(err)
	}
	rows, err = s.db.QueryContext(ctx, `SELECT platform,video_id,status,next_retry_at,last_error FROM work_cover_lookups`)
	if err != nil {
		return nil, dbErr(err)
	}
	for rows.Next() {
		var platform, id string
		var lookup workLookup
		if err = rows.Scan(&platform, &id, &lookup.status, &lookup.retryAt, &lookup.code); err != nil {
			_ = rows.Close()
			return nil, dbErr(err)
		}
		state.lookups[storedKey(platform, id)] = lookup
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, dbErr(err)
	}
	return state, nil
}

// due reports whether a stored retry time has passed (or none is set).
func (state *workCoverState) due(retryAt sql.NullString) bool {
	if !retryAt.Valid {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, retryAt.String)
	return err != nil || !at.After(state.now)
}

// cover reports the cover state of one video.
func (s *Store) workVideoCover(ctx context.Context, state *workCoverState, video works.Video) workCover {
	storedWaiting := false
	if platform := storedPlatform(video.Platform); platform != "" {
		if v, ok := state.stored[storedKey(platform, video.ID)]; ok {
			switch {
			case v.pinnedRef != nil:
				return workCover{ready: v.pinnedRef}
			case v.thumbnailRef != nil && v.thumbnailRef.Status == "ready":
				return workCover{ready: v.thumbnailRef}
			case v.thumbnailRef != nil && v.thumbnailRef.Status != "missing":
				// The stored video's own image is still on its way.
				storedWaiting = true
			}
		}
	}
	if link, ok := state.links[storedKey(video.Platform, video.ID)]; ok {
		ref := s.mediaReference(ctx, link.asset)
		switch {
		case ref != nil && ref.Status == "ready":
			return workCover{ready: ref}
		case link.asset.Status == "missing", ref != nil && ref.Status == "missing":
			// The image is gone at its source, or it was ready and its cached
			// file is gone; its row stays and the upload's next video is tried.
			return workCover{exhausted: !storedWaiting, waiting: storedWaiting}
		case link.asset.Status == "failed" && !link.retryAt.Valid && link.err.Valid && mediaRetryDelay(link.err.String, 1) == 0:
			// A rejected destination, an invalid image or an oversized body is
			// not retried.
			return workCover{exhausted: !storedWaiting, waiting: storedWaiting}
		}
		return workCover{waiting: true}
	}
	if storedWaiting {
		return workCover{waiting: true}
	}
	if video.Platform == works.YouTube {
		return workCover{lookup: true}
	}
	lookup, ok := state.lookups[storedKey(video.Platform, video.ID)]
	switch {
	case !ok, lookup.status == "resolved":
		return workCover{lookup: true}
	case lookup.status == "missing":
		return workCover{exhausted: true}
	case state.due(lookup.retryAt):
		return workCover{lookup: true}
	}
	return workCover{waiting: true}
}

// uploadCover walks an upload's videos in order: the first ready image is its
// cover, and the first video still to try decides what the step does next.
func (s *Store) uploadCover(ctx context.Context, state *workCoverState, upload works.Upload) (*mediaReference, *works.Video, bool) {
	var next *works.Video
	waiting := false
	for i := range upload.Videos {
		cover := s.workVideoCover(ctx, state, upload.Videos[i])
		if cover.ready != nil {
			return cover.ready, nil, false
		}
		if cover.exhausted || next != nil || waiting {
			continue
		}
		if cover.waiting {
			waiting = true
			continue
		}
		next = &upload.Videos[i]
	}
	return nil, next, waiting
}

// youtubeWorkThumbnail is the fixed address of a YouTube video's largest
// thumbnail; the downloader steps down to hqdefault when YouTube made none.
func youtubeWorkThumbnail(id string) string {
	return "https://i.ytimg.com/vi/" + id + "/maxresdefault.jpg"
}

// PlanWorkCovers queues the YouTube thumbnails the works list needs and
// returns the niconico and Bilibili lookups that are due, newest upload first.
func (s *Store) PlanWorkCovers(ctx context.Context, index works.Index, now time.Time) (WorkCoverPlan, error) {
	out := WorkCoverPlan{Lookups: []WorkVideoRef{}}
	if s == nil || !s.workflowWritable {
		return out, ErrSourceWrite
	}
	ctx = withReadMemo(ctx)
	state, err := s.loadWorkCoverState(ctx, index, now)
	if err != nil {
		return out, err
	}
	queue := []WorkVideoRef{}
	uploads := slices.Clone(index.Uploads)
	slices.SortStableFunc(uploads, func(a, b works.Upload) int { return strings.Compare(b.Date, a.Date) })
	for _, upload := range uploads {
		ready, next, waiting := s.uploadCover(ctx, state, upload)
		switch {
		case ready != nil:
			out.Covered++
		case next == nil && waiting:
			out.Waiting++
		case next == nil:
			out.Uncovered++
		case next.Platform == works.YouTube:
			queue = append(queue, WorkVideoRef{next.Platform, next.ID})
			out.Waiting++
		default:
			out.Lookups = append(out.Lookups, WorkVideoRef{next.Platform, next.ID})
			out.Waiting++
		}
	}
	if len(queue) == 0 {
		return out, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return out, err
	}
	stamp := workflowTime(now)
	for _, ref := range queue {
		if err = replaceMediaLink(ctx, tx, "youtube", youtubeWorkThumbnail(ref.ID), workVideoOwner, ref.owner(), "thumbnail", 0, stamp); err != nil {
			return out, ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return out, ErrSourceWrite
	}
	out.Queued = len(queue)
	return out, nil
}

// Upload and video states of a WorkCoverReport.
const (
	WorkCoverDue       = "due"
	WorkCoverWaiting   = "waiting"
	WorkCoverUncovered = "uncovered"
	WorkCoverGone      = "gone"
)

// WorkCoverReport is how the Works list's upload covers stand: the counts
// and, newest first, every upload without a ready cover. Due uploads are
// those the next media step acts on; Due and Waiting together are the
// media step's waiting count.
type WorkCoverReport struct {
	Total, Covered, Due, Waiting, Uncovered int
	Uploads                                 []WorkCoverUpload
}

// WorkCoverUpload is one upload without a ready cover and its videos in
// cover order.
type WorkCoverUpload struct {
	ID, Date, State string
	Videos          []WorkCoverVideo
}

// WorkCoverVideo is one video's cover state: due (the media step would look
// it up or queue its image), waiting (an image or lookup is on its way or
// waits to retry) or gone (deleted, missing or rejected). Code is the last
// lookup or download error code and RetryAt when a failure is tried again.
type WorkCoverVideo struct {
	Platform, ID, State string
	Members             bool
	Code, RetryAt       *string
}

// WorkCoverReport reads how the Works list's covers stand. It never writes
// and makes no lookup, so it answers the same before and after a deployment
// has run a media step.
func (s *Store) WorkCoverReport(ctx context.Context, index works.Index, now time.Time) (WorkCoverReport, error) {
	out := WorkCoverReport{Uploads: []WorkCoverUpload{}}
	if s == nil || s.db == nil {
		return out, ErrUnavailable
	}
	ctx = withReadMemo(ctx)
	state, err := s.loadWorkCoverState(ctx, index, now)
	if err != nil {
		return out, err
	}
	uploads := slices.Clone(index.Uploads)
	slices.SortStableFunc(uploads, func(a, b works.Upload) int { return strings.Compare(b.Date, a.Date) })
	for _, upload := range uploads {
		out.Total++
		ready, next, waiting := s.uploadCover(ctx, state, upload)
		item := WorkCoverUpload{ID: upload.ID, Date: upload.Date, Videos: []WorkCoverVideo{}}
		switch {
		case ready != nil:
			out.Covered++
			continue
		case next != nil:
			out.Due++
			item.State = WorkCoverDue
		case waiting:
			out.Waiting++
			item.State = WorkCoverWaiting
		default:
			out.Uncovered++
			item.State = WorkCoverUncovered
		}
		for _, video := range upload.Videos {
			item.Videos = append(item.Videos, s.workVideoReport(ctx, state, video))
		}
		out.Uploads = append(out.Uploads, item)
	}
	return out, nil
}

// workVideoReport describes one video of an upload without a cover, with the
// error and retry time of the works image or lookup that decided its state.
func (s *Store) workVideoReport(ctx context.Context, state *workCoverState, video works.Video) WorkCoverVideo {
	out := WorkCoverVideo{Platform: video.Platform, ID: video.ID, Members: video.Members}
	cover := s.workVideoCover(ctx, state, video)
	switch {
	case cover.exhausted:
		out.State = WorkCoverGone
	case cover.waiting:
		out.State = WorkCoverWaiting
	default:
		out.State = WorkCoverDue
	}
	key := storedKey(video.Platform, video.ID)
	if link, ok := state.links[key]; ok {
		out.Code, out.RetryAt = nullString(link.err), nullString(link.retryAt)
	} else if lookup, ok := state.lookups[key]; ok {
		out.Code, out.RetryAt = nullString(lookup.code), nullString(lookup.retryAt)
	}
	if out.State == WorkCoverDue {
		out.RetryAt = nil
	}
	return out
}

// Lookup outcomes recorded by SaveWorkCoverLookup.
const (
	WorkLookupResolved = "resolved"
	WorkLookupMissing  = "missing"
	WorkLookupFailed   = "failed"
)

// SaveWorkCoverLookup records one niconico or Bilibili lookup. A resolved one
// queues its image for the media download; a missing video is not asked
// again; a failure waits from 30 minutes, doubling to a day.
func (s *Store) SaveWorkCoverLookup(ctx context.Context, ref WorkVideoRef, status, imageURL, code string, now time.Time) error {
	if s == nil || !s.workflowWritable {
		return ErrSourceWrite
	}
	if (ref.Platform != works.Niconico && ref.Platform != works.Bilibili) || !works.ValidVideo(ref.Platform, ref.ID) {
		return ErrSourceWrite
	}
	switch status {
	case WorkLookupResolved:
		if imageURL == "" {
			return ErrSourceWrite
		}
	case WorkLookupMissing, WorkLookupFailed:
		imageURL = ""
	default:
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
	stamp := workflowTime(now)
	attempts := 0
	if status == WorkLookupFailed {
		if err = tx.QueryRowContext(ctx, `SELECT retry_attempts FROM work_cover_lookups WHERE platform=? AND video_id=?`, ref.Platform, ref.ID).Scan(&attempts); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ErrSourceWrite
		}
		attempts = min(max(attempts, 0), 11) + 1
	}
	var retryAt any
	if status == WorkLookupFailed {
		retryAt = workflowTime(now.Add(min(workCoverRetry*time.Duration(1<<min(attempts-1, 6)), 24*time.Hour)))
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO work_cover_lookups(platform,video_id,status,source_url,last_error,retry_attempts,next_retry_at,checked_at) VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(platform,video_id) DO UPDATE SET status=excluded.status,source_url=excluded.source_url,last_error=excluded.last_error,retry_attempts=excluded.retry_attempts,next_retry_at=excluded.next_retry_at,checked_at=excluded.checked_at`,
		ref.Platform, ref.ID, status, nullable(imageURL), nullable(code), attempts, retryAt, stamp); err != nil {
		return ErrSourceWrite
	}
	if status == WorkLookupResolved {
		if err = replaceMediaLink(ctx, tx, ref.Platform, imageURL, workVideoOwner, ref.owner(), "thumbnail", 0, stamp); err != nil {
			return ErrSourceWrite
		}
	}
	if tx.Commit() != nil {
		return ErrSourceWrite
	}
	return nil
}

// WorksCover is an upload's ready cover image.
type WorksCover struct {
	URL           string
	Width, Height *int64
}

// WorksVideo is a stored video of the works list as readers see it.
type WorksVideo struct {
	Source, ID, Title, URL   string
	PublishedAt, ScheduledAt *string
	DurationSeconds          *int64
	Availability             *string
	Translations             TranslationSet
}

// WorksUpload is what the store knows about one upload of the works list.
type WorksUpload struct {
	ID     string
	Cover  *WorksCover
	Videos []WorksVideo
}

// WorksOverlay is the store's side of the works list: for each upload that
// has one, its cover and its stored videos, and the recent uploads dated
// after the list (works_recent.go), newest first.
type WorksOverlay struct {
	Uploads []WorksUpload
	Recent  []WorksRecentUpload
	// RecentTotal counts the recent uploads beyond Recent's bound.
	RecentTotal int
	Revision    int64
}

// GetWorksOverlay joins the works list with stored videos and images by
// video ID and derives the recent uploads, on each read. It never writes.
func (s *Store) GetWorksOverlay(ctx context.Context, index works.Index) (*WorksOverlay, error) {
	if s != nil && s.readDB != nil {
		return s.publicReader().GetWorksOverlay(ctx, index)
	}
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	ctx = withReadMemo(ctx)
	state, err := s.loadWorkCoverState(ctx, index, time.Now())
	if err != nil {
		return nil, err
	}
	out := &WorksOverlay{Uploads: []WorksUpload{}}
	for _, upload := range index.Uploads {
		item := WorksUpload{ID: upload.ID, Videos: []WorksVideo{}}
		if ready, _, _ := s.uploadCover(ctx, state, upload); ready != nil && ready.PublicURL != nil {
			item.Cover = &WorksCover{URL: *ready.PublicURL, Width: ready.Width, Height: ready.Height}
		}
		for _, video := range upload.Videos {
			platform := storedPlatform(video.Platform)
			stored, ok := state.stored[storedKey(platform, video.ID)]
			if platform == "" || !ok {
				continue
			}
			item.Videos = append(item.Videos, WorksVideo{
				Source: platform, ID: video.ID, Title: stored.title, URL: stored.url,
				PublishedAt: nullString(stored.publishedAt), ScheduledAt: nullString(stored.scheduledAt),
				DurationSeconds: nullInt(stored.duration), Availability: nullString(stored.availability),
			})
		}
		if item.Cover != nil || len(item.Videos) > 0 {
			out.Uploads = append(out.Uploads, item)
		}
	}
	refs := []*Video{}
	type slot struct{ upload, video int }
	slots := []slot{}
	for i := range out.Uploads {
		for j, video := range out.Uploads[i].Videos {
			refs = append(refs, &Video{ID: video.ID, Source: video.Source, Title: video.Title})
			slots = append(slots, slot{i, j})
		}
	}
	if err = s.attachVideoTranslations(ctx, refs...); err != nil {
		return nil, err
	}
	for k, ref := range refs {
		out.Uploads[slots[k].upload].Videos[slots[k].video].Translations = ref.Translations
	}
	if out.Recent, out.RecentTotal, err = s.worksRecentUploads(ctx, index, state.now); err != nil {
		return nil, err
	}
	if out.Revision, err = s.GetDashboardRevision(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// WorksMergeConflict is a stored Bilibili upload whose merge disagrees with
// the works list: the list pairs it with other YouTube videos than the one it
// is merged into, or with stored ones while it is not merged.
type WorksMergeConflict struct {
	Upload       string   `json:"upload"`
	Bilibili     string   `json:"bilibili"`
	MergedInto   *string  `json:"mergedInto"`
	WorksYoutube []string `json:"worksYoutube"`
}

// WorksMergeConflicts lists the disagreements for the operator. The list
// never merges or releases an upload.
func (s *Store) WorksMergeConflicts(ctx context.Context, index works.Index) ([]WorksMergeConflict, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	ctx = withReadMemo(ctx)
	state, err := s.loadWorkCoverState(ctx, index, time.Now())
	if err != nil {
		return nil, err
	}
	out := []WorksMergeConflict{}
	for _, upload := range index.Uploads {
		youtube, stored := []string{}, false
		for _, video := range upload.Videos {
			if video.Platform == works.YouTube {
				youtube = append(youtube, video.ID)
				_, ok := state.stored[storedKey(works.YouTube, video.ID)]
				stored = stored || ok
			}
		}
		for _, video := range upload.Videos {
			if video.Platform != works.Bilibili {
				continue
			}
			row, ok := state.stored[storedKey(works.Bilibili, video.ID)]
			if !ok {
				continue
			}
			merged := nullString(row.merged)
			if merged != nil && slices.Contains(youtube, *merged) || merged == nil && !stored {
				continue
			}
			out = append(out, WorksMergeConflict{Upload: upload.ID, Bilibili: video.ID, MergedInto: merged, WorksYoutube: youtube})
		}
	}
	return out, nil
}
