package publicview

import (
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func value(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// Dashboard returns a sanitized JSON-ready map. The store types intentionally
// omit sensitive columns; this second boundary also controls the exact public
// field set used by the TypeScript service.
func Dashboard(d *sqlite.Dashboard) map[string]any {
	if d == nil {
		return nil
	}
	profile := any(nil)
	if p := d.Profile; p != nil {
		profile = map[string]any{
			"id": p.ID, "displayName": p.DisplayName, "romanizedName": value(p.RomanizedName), "bio": value(p.Bio),
			"avatarUrl": value(p.AvatarURL), "bannerUrl": value(p.BannerURL), "xUrl": value(p.XURL), "youtubeUrl": value(p.YouTubeURL), "updatedAt": value(p.UpdatedAt),
			"avatarId": value(p.AvatarID), "avatarStatus": p.AvatarStatus, "avatarSourceUrl": value(p.AvatarSourceURL), "avatarSource": value(p.AvatarSource),
			"bannerId": value(p.BannerID), "bannerStatus": p.BannerStatus, "bannerSourceUrl": value(p.BannerSourceURL), "bannerSource": value(p.BannerSource),
		}
	}
	posts := make([]any, 0, len(d.Posts))
	for _, p := range d.Posts {
		media := make([]any, 0, len(p.Media))
		for _, m := range p.Media {
			media = append(media, map[string]any{"id": value(m.ID), "url": value(m.URL), "status": m.Status, "sourceUrl": value(m.SourceURL), "alt": value(m.Alt), "position": m.Position, "width": valueInt(m.Width), "height": valueInt(m.Height)})
		}
		posts = append(posts, map[string]any{
			"id": p.ID, "source": p.Source, "accountHandle": value(p.AccountHandle), "type": p.Type, "label": value(p.Label), "text": p.Text, "publishedAt": p.PublishedAt, "url": p.URL, "likes": p.Likes, "reposts": p.Reposts, "replies": p.Replies, "mediaUrl": value(p.MediaURL), "mediaAlt": value(p.MediaAlt), "accountUrl": value(p.AccountURL), "media": media, "mediaId": value(p.MediaID), "mediaStatus": p.MediaStatus, "mediaSourceUrl": value(p.MediaSourceURL),
		})
	}
	events := make([]any, 0, len(d.Events))
	for _, e := range d.Events {
		events = append(events, PublicEvent(e, true))
	}
	videos := make([]any, 0, len(d.Videos))
	for _, v := range d.Videos {
		videos = append(videos, map[string]any{"id": v.ID, "source": v.Source, "title": v.Title, "publishedAt": value(v.PublishedAt), "scheduledAt": value(v.ScheduledAt), "url": v.URL, "thumbnailUrl": value(v.ThumbnailURL), "kind": value(v.Kind), "isUpcoming": v.IsUpcoming, "endsAt": value(v.EndsAt), "thumbnailId": value(v.ThumbnailID), "thumbnailStatus": v.ThumbnailStatus, "thumbnailSourceUrl": value(v.ThumbnailSourceURL), "isCancelled": v.IsCancelled})
	}
	var focus any = nil
	if f := d.Focus; f != nil {
		focus = map[string]any{"id": f.ID, "videoId": value(f.VideoID), "dateLabel": value(f.DateLabel), "title": f.Title, "description": value(f.Description), "imageUrl": value(f.ImageURL), "url": value(f.URL), "sourceUrl": value(f.SourceURL), "updatedAt": value(f.UpdatedAt), "imageId": value(f.ImageID), "imageStatus": f.ImageStatus, "imageSourceUrl": value(f.ImageSourceURL)}
	}
	timeline := make([]any, 0, len(d.Timeline))
	for _, x := range d.Timeline {
		timeline = append(timeline, map[string]any{"id": x.ID, "year": x.Year, "title": x.Title, "detail": value(x.Detail), "sortOrder": x.SortOrder})
	}
	resources := make([]any, 0, len(d.Resources))
	for _, x := range d.Resources {
		resources = append(resources, map[string]any{"id": x.ID, "title": x.Title, "detail": value(x.Detail), "icon": value(x.Icon), "tone": value(x.Tone), "url": x.URL, "sortOrder": x.SortOrder})
	}
	assets := make([]any, 0, len(d.Assets))
	for _, a := range d.Assets {
		assets = append(assets, publicAsset(a))
	}
	schedule := make([]any, 0, len(d.ScheduleImages))
	for _, a := range d.ScheduleImages {
		schedule = append(schedule, publicAsset(a))
	}
	var nextEvent, latestPost, latestVideo, nextStream any
	if d.Summary.NextEvent != nil {
		nextEvent = PublicEvent(*d.Summary.NextEvent, true)
	}
	if d.Summary.LatestPost != nil {
		for _, p := range posts {
			if p.(map[string]any)["id"] == d.Summary.LatestPost.ID {
				latestPost = p
				break
			}
		}
		if latestPost == nil {
			latestPost = publicPost(*d.Summary.LatestPost)
		}
	}
	if d.Summary.LatestVideo != nil {
		latestVideo = publicVideo(*d.Summary.LatestVideo)
	}
	if d.Summary.NextStream != nil {
		nextStream = publicVideo(*d.Summary.NextStream)
	}
	return map[string]any{"profile": profile, "summary": map[string]any{"nextEvent": nextEvent, "nextStream": nextStream, "latestVideo": latestVideo, "latestPost": latestPost, "counts": map[string]any{"posts": d.Summary.Counts.Posts, "events": d.Summary.Counts.Events, "upcomingEvents": d.Summary.Counts.UpcomingEvents, "videos": d.Summary.Counts.Videos, "resources": d.Summary.Counts.Resources}}, "posts": posts, "events": events, "videos": videos, "focus": focus, "timeline": timeline, "resources": resources, "assets": assets, "scheduleImages": schedule, "profileMedia": map[string]any{"avatar": publicProfileMedia(d.ProfileMedia.Avatar), "banner": publicProfileMedia(d.ProfileMedia.Banner)}, "meta": map[string]any{"fetchedAt": value(d.Meta.FetchedAt), "lastSync": PublicSyncRun(d.Meta.LastSync), "postWindowDays": d.Meta.PostWindowDays, "eventWindow": map[string]any{"from": d.Meta.EventWindow.From, "to": d.Meta.EventWindow.To, "paginated": d.Meta.EventWindow.Paginated}, "generatedAt": d.Meta.GeneratedAt, "mediaCache": d.Meta.MediaCache, "revision": d.Meta.Revision, "featuredVideoId": value(d.Meta.FeaturedVideoID), "xAccounts": d.Meta.XAccounts}}
}

func valueInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func PublicEvent(e sqlite.DashboardEvent, extras bool) map[string]any {
	out := map[string]any{"id": e.ID, "source": e.Source, "title": e.Title, "detail": value(e.Detail), "startsOn": e.StartsOn, "startsAt": value(e.StartsAt), "endsAt": value(e.EndsAt), "timezone": e.Timezone, "timePrecision": e.TimePrecision, "status": value(e.Status), "eventType": value(e.EventType), "url": value(e.URL), "provenance": e.Provenance, "cancellationStatus": e.CancellationStatus, "cancellationSource": value(e.CancellationSource), "cancellationReason": value(e.CancellationReason), "cancellationEvidence": value(e.CancellationEvidence), "cancellationAt": value(e.CancellationAt)}
	if extras {
		out["manualLocked"] = e.ManualLocked
		out["isUpcoming"] = e.IsUpcoming
	}
	return out
}
func publicPost(p sqlite.Post) map[string]any {
	media := make([]any, 0, len(p.Media))
	for _, m := range p.Media {
		media = append(media, map[string]any{"id": value(m.ID), "url": value(m.URL), "status": m.Status, "sourceUrl": value(m.SourceURL), "alt": value(m.Alt), "position": m.Position, "width": valueInt(m.Width), "height": valueInt(m.Height)})
	}
	return map[string]any{"id": p.ID, "source": p.Source, "accountHandle": value(p.AccountHandle), "type": p.Type, "label": value(p.Label), "text": p.Text, "publishedAt": p.PublishedAt, "url": p.URL, "likes": p.Likes, "reposts": p.Reposts, "replies": p.Replies, "mediaUrl": value(p.MediaURL), "mediaAlt": value(p.MediaAlt), "accountUrl": value(p.AccountURL), "media": media, "mediaId": value(p.MediaID), "mediaStatus": p.MediaStatus, "mediaSourceUrl": value(p.MediaSourceURL)}
}
func publicVideo(v sqlite.Video) map[string]any {
	return map[string]any{"id": v.ID, "source": v.Source, "title": v.Title, "publishedAt": value(v.PublishedAt), "scheduledAt": value(v.ScheduledAt), "url": v.URL, "thumbnailUrl": value(v.ThumbnailURL), "kind": value(v.Kind), "isUpcoming": v.IsUpcoming, "endsAt": value(v.EndsAt), "thumbnailId": value(v.ThumbnailID), "thumbnailStatus": v.ThumbnailStatus, "thumbnailSourceUrl": value(v.ThumbnailSourceURL), "isCancelled": v.IsCancelled}
}
func publicAsset(a sqlite.Asset) map[string]any {
	return map[string]any{"id": a.ID, "kind": a.Kind, "alt": value(a.Alt), "weekStart": value(a.WeekStart), "periodStart": value(a.PeriodStart), "periodEnd": value(a.PeriodEnd), "periodBasis": value(a.PeriodBasis), "sourceAccount": value(a.SourceAccount), "updatedAt": value(a.UpdatedAt), "url": value(a.URL), "sourceUrl": value(a.SourceURL), "mediaId": value(a.MediaID), "mediaStatus": a.MediaStatus, "mediaSourceUrl": value(a.MediaSourceURL)}
}

// PublicAsset is the calendar-facing form of a schedule asset. Keep the
// projection exported so calendar and dashboard use the same field boundary.
func PublicAsset(a sqlite.Asset) map[string]any { return publicAsset(a) }
func publicProfileMedia(m *sqlite.ProfileMedia) any {
	if m == nil {
		return nil
	}
	return map[string]any{"id": m.ID, "slot": m.Slot, "source": m.Source, "sourceUrl": value(m.SourceURL), "status": m.Status, "isActive": m.IsActive, "publicUrl": value(m.PublicURL)}
}

type PublicSyncRunResponse struct {
	ID         int64   `json:"id"`
	Source     string  `json:"source"`
	StartedAt  string  `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
	Status     string  `json:"status"`
	Message    *string `json:"message"`
}

type PublicHealthResponse struct {
	OK       bool                   `json:"ok"`
	Service  string                 `json:"service"`
	Now      string                 `json:"now"`
	LastSync *PublicSyncRunResponse `json:"lastSync"`
}

type PublicRevisionResponse struct {
	Revision int64 `json:"revision"`
	Changed  bool  `json:"changed"`
}

func PublicSyncRun(run *sqlite.SyncRun) *PublicSyncRunResponse {
	if run == nil {
		return nil
	}
	message, ok := map[string]string{
		"success": "同步完成",
		"partial": "部分数据源不可用或存在警告，保留已有快照",
		"failed":  "同步失败，保留已有快照",
		"running": "同步进行中",
	}[run.Status]
	var publicMessage *string
	if ok {
		publicMessage = &message
	}
	return &PublicSyncRunResponse{
		ID:         run.ID,
		Source:     run.Source,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
		Status:     run.Status,
		Message:    publicMessage,
	}
}

func Health(now time.Time, run *sqlite.SyncRun) PublicHealthResponse {
	return PublicHealthResponse{
		OK:       true,
		Service:  "kano-status-board",
		Now:      now.UTC().Format("2006-01-02T15:04:05.000Z"),
		LastSync: PublicSyncRun(run),
	}
}

func Revision(revision int64, since *int64) PublicRevisionResponse {
	changed := true
	if since != nil {
		changed = revision != *since
	}
	return PublicRevisionResponse{Revision: revision, Changed: changed}
}
