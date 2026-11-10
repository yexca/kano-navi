package publicview

import (
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/works"
)

func value(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// publicTranslations keeps the resolved translation of each locale; machine
// provenance (provider, model, hashes) stays in the console.
func publicTranslations(set sqlite.TranslationSet) map[string]any {
	out := map[string]any{}
	for locale, t := range set.Locales {
		out[locale] = map[string]any{"text": t.Text, "origin": t.Origin, "platform": value(t.Platform)}
	}
	return out
}

// Dashboard returns a sanitized JSON-ready map. The store types intentionally
// omit sensitive columns; this second boundary also controls the exact public
// field set.
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
		posts = append(posts, publicPost(p))
	}
	events := make([]any, 0, len(d.Events))
	for _, e := range d.Events {
		events = append(events, PublicEvent(e, true))
	}
	videos := make([]any, 0, len(d.Videos))
	for _, v := range d.Videos {
		videos = append(videos, publicVideo(v))
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
	return map[string]any{"profile": profile, "summary": map[string]any{"nextEvent": nextEvent, "nextStream": nextStream, "latestVideo": latestVideo, "latestPost": latestPost, "counts": map[string]any{"posts": d.Summary.Counts.Posts, "events": d.Summary.Counts.Events, "upcomingEvents": d.Summary.Counts.UpcomingEvents, "videos": d.Summary.Counts.Videos, "resources": d.Summary.Counts.Resources}}, "projection": publicDashboardProjection(d.Projection), "posts": posts, "events": events, "videos": videos, "focus": focus, "timeline": timeline, "resources": resources, "assets": assets, "scheduleImages": schedule, "profileMedia": map[string]any{"avatar": publicProfileMedia(d.ProfileMedia.Avatar), "banner": publicProfileMedia(d.ProfileMedia.Banner)}, "liveNow": publicLiveNow(d.LiveNow), "meta": map[string]any{"fetchedAt": value(d.Meta.FetchedAt), "sourceFetchedAt": d.Meta.SourceFetchedAt, "lastSync": PublicSyncRun(d.Meta.LastSync), "postWindowDays": d.Meta.PostWindowDays, "eventWindow": map[string]any{"from": d.Meta.EventWindow.From, "to": d.Meta.EventWindow.To, "paginated": d.Meta.EventWindow.Paginated}, "generatedAt": d.Meta.GeneratedAt, "mediaCache": d.Meta.MediaCache, "revision": d.Meta.Revision, "featuredVideoId": value(d.Meta.FeaturedVideoID), "xAccounts": d.Meta.XAccounts, "bilibiliAccounts": d.Meta.BilibiliAccounts}}
}

func valueInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func valueBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

// publicLiveNow is the account's unannounced Bilibili broadcast, or nil.
func publicLiveNow(l *sqlite.LiveNow) any {
	if l == nil {
		return nil
	}
	return map[string]any{"platform": l.Platform, "url": l.URL, "title": l.Title, "startedAt": l.StartedAt}
}

func PublicEvent(e sqlite.DashboardEvent, extras bool) map[string]any {
	out := map[string]any{"id": e.ID, "source": e.Source, "platform": value(e.Platform), "platforms": publicPlatforms(e.Platforms), "title": e.Title, "detail": value(e.Detail), "startsOn": e.StartsOn, "startsAt": value(e.StartsAt), "endsAt": value(e.EndsAt), "endsOn": value(e.EndsOn), "timezone": e.Timezone, "timePrecision": e.TimePrecision, "status": value(e.Status), "eventType": value(e.EventType), "tag": value(e.Tag), "url": value(e.URL), "provenance": e.Provenance, "cancellationStatus": e.CancellationStatus, "cancellationSource": value(e.CancellationSource), "cancellationReason": value(e.CancellationReason), "cancellationEvidence": value(e.CancellationEvidence), "cancellationAt": value(e.CancellationAt), "view": publicEventView(e.View), "translations": publicTranslations(e.Translations), "sourceLanguage": value(e.Translations.SourceLanguage)}
	if extras {
		out["manualLocked"] = e.ManualLocked
		out["isUpcoming"] = e.IsUpcoming
		out["liveStatus"] = value(e.LiveStatus)
		out["liveUrl"] = value(e.LiveURL)
		out["links"] = PublicEventLinks(e.Links)
	}
	return out
}

// publicPlatforms is an entry's shown platforms, never null.
func publicPlatforms(platforms []string) []string {
	if platforms == nil {
		return []string{}
	}
	return platforms
}

func publicEventView(view sqlite.EventView) map[string]any {
	entries := make([]any, 0, len(view.CalendarEntries))
	for _, entry := range view.CalendarEntries {
		entries = append(entries, map[string]any{
			"saleMark": value(entry.SaleMark), "startsOn": entry.StartsOn,
			"startsAt": value(entry.StartsAt), "timePrecision": entry.TimePrecision,
		})
	}
	return map[string]any{
		"state": view.State, "typeCode": value(view.TypeCode),
		"activityCode": value(view.ActivityCode), "activityContext": view.ActivityContext,
		"isWork": view.IsWork, "calendarEntries": entries,
	}
}

// PublicEventView exposes the shared event view to bounded readers whose
// query path does not build a full dashboard event.
func PublicEventView(view sqlite.EventView) map[string]any { return publicEventView(view) }

func publicVideoView(view sqlite.VideoView) map[string]any {
	return map[string]any{
		"phase": view.Phase, "formCode": value(view.FormCode), "shelfGroup": view.ShelfGroup,
		"latestEligible": view.LatestEligible, "focusEligible": view.FocusEligible,
		"tagContext": view.TagContext,
	}
}

func publicDashboardProjection(projection sqlite.DashboardProjection) map[string]any {
	focus := map[string]any{
		"eventIds": projection.Focus.EventIDs,
		"videoIds": projection.Focus.VideoIDs,
	}
	spotlight := any(nil)
	if value := projection.Spotlight; value != nil {
		spotlight = map[string]any{
			"mode": value.Mode, "eventId": valueOrString(value.EventID), "videoId": valueOrString(value.VideoID),
			"liveNow": value.LiveNow, "isLive": value.IsLive, "membersChip": value.MembersChip,
			"companionVideoId": valueOrString(value.CompanionVideoID),
		}
	}
	shelf := projection.Shelf
	return map[string]any{
		"version": projection.Version, "nextTransitionAt": value(projection.NextTransitionAt),
		"focus": focus, "spotlight": spotlight, "sales": projection.Sales,
		"shelf": map[string]any{
			"leadId": valueOrString(shelf.LeadID), "leadIsFeatured": shelf.LeadIsFeatured,
			"reservations": shelf.Reservations, "works": shelf.Works,
			"archives": shelf.Archives, "shorts": shelf.Shorts,
		},
	}
}

func valueOrString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// PublicEventLinks is one {platform, url, liveStatus} per shown platform.
func PublicEventLinks(links []sqlite.EventLink) []any {
	out := make([]any, 0, len(links))
	for _, link := range links {
		out = append(out, map[string]any{"platform": link.Platform, "url": value(link.URL), "liveStatus": value(link.LiveStatus)})
	}
	return out
}
func publicPost(p sqlite.Post) map[string]any {
	return map[string]any{"id": p.ID, "source": p.Source, "accountHandle": value(p.AccountHandle), "type": p.Type, "label": value(p.Label), "text": p.Text, "publishedAt": p.PublishedAt, "url": p.URL, "likes": p.Likes, "reposts": p.Reposts, "replies": p.Replies, "mediaUrl": value(p.MediaURL), "mediaAlt": value(p.MediaAlt), "accountUrl": value(p.AccountURL), "media": publicMedia(p.Media), "mediaId": value(p.MediaID), "mediaStatus": p.MediaStatus, "mediaSourceUrl": value(p.MediaSourceURL), "translations": publicTranslations(p.Translations), "sourceLanguage": value(p.Translations.SourceLanguage), "quote": publicQuote(p.Quote), "promoted": p.Promoted}
}

func publicMedia(items []sqlite.PostMedia) []any {
	media := make([]any, 0, len(items))
	for _, m := range items {
		media = append(media, map[string]any{"id": value(m.ID), "url": value(m.URL), "status": m.Status, "sourceUrl": value(m.SourceURL), "alt": value(m.Alt), "position": m.Position, "width": valueInt(m.Width), "height": valueInt(m.Height)})
	}
	return media
}

// publicQuote is the quoted status a post shows: its author, wording, time,
// link (an x.com status, or a Bilibili dynamic or upload page for a forward)
// and ready images. Empty fields are null; only an unavailable forward of a
// dynamic whose ID is unknown has a null id.
func publicQuote(q *sqlite.PostQuote) any {
	if q == nil {
		return nil
	}
	text := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	return map[string]any{"id": text(q.ID), "authorHandle": text(q.AuthorHandle), "authorName": text(q.AuthorName), "text": text(q.Text), "publishedAt": text(q.PublishedAt), "url": q.URL, "unavailable": q.Unavailable, "media": publicMedia(q.Media), "translations": publicTranslations(q.Translations), "sourceLanguage": value(q.Translations.SourceLanguage)}
}
func publicVideo(v sqlite.Video) map[string]any {
	return map[string]any{"id": v.ID, "source": v.Source, "title": v.Title, "publishedAt": value(v.PublishedAt), "scheduledAt": value(v.ScheduledAt), "url": v.URL, "thumbnailUrl": value(v.ThumbnailURL), "kind": value(v.Kind), "isUpcoming": v.IsUpcoming, "endsAt": value(v.EndsAt), "thumbnailId": value(v.ThumbnailID), "thumbnailStatus": v.ThumbnailStatus, "thumbnailSourceUrl": value(v.ThumbnailSourceURL), "isCancelled": v.IsCancelled, "liveStatus": value(v.LiveStatus), "durationSeconds": valueInt(v.DurationSeconds), "isShort": valueBool(v.IsShort), "tag": value(v.Tag), "availability": value(v.Availability), "view": publicVideoView(v.View), "translations": publicTranslations(v.Translations), "sourceLanguage": value(v.Translations.SourceLanguage), "alternates": publicAlternates(v.Alternates)}
}

// publicAlternates lists the other platforms' copies of a video by their
// public links only.
func publicAlternates(alternates []sqlite.VideoAlternate) []any {
	out := make([]any, 0, len(alternates))
	for _, a := range alternates {
		out = append(out, map[string]any{"source": a.Source, "url": a.URL})
	}
	return out
}
func publicAsset(a sqlite.Asset) map[string]any {
	return map[string]any{"id": a.ID, "kind": a.Kind, "alt": value(a.Alt), "weekStart": value(a.WeekStart), "periodStart": value(a.PeriodStart), "periodEnd": value(a.PeriodEnd), "periodBasis": value(a.PeriodBasis), "sourceAccount": value(a.SourceAccount), "updatedAt": value(a.UpdatedAt), "url": value(a.URL), "sourceUrl": value(a.SourceURL), "mediaId": value(a.MediaID), "mediaStatus": a.MediaStatus, "mediaSourceUrl": value(a.MediaSourceURL)}
}

// PublicAsset is the calendar-facing form of a schedule asset. Keep the
// projection exported so calendar and dashboard use the same field boundary.
func PublicAsset(a sqlite.Asset) map[string]any { return publicAsset(a) }

// PublicPost and PublicVideo are the archive-facing forms of a post and a
// video, sharing the dashboard's field boundary.
func PublicPost(p sqlite.Post) map[string]any   { return publicPost(p) }
func PublicVideo(v sqlite.Video) map[string]any { return publicVideo(v) }
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
		"success": "Sync completed",
		"partial": "Some data sources are unavailable or reported warnings; the existing snapshot remains",
		"failed":  "Sync failed; the existing snapshot remains",
		"running": "Sync in progress",
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

// worksCover is a ready cover's local URL and size, or nil.
func worksCover(cover *sqlite.WorksCover) any {
	if cover == nil {
		return nil
	}
	return map[string]any{"url": cover.URL, "width": valueInt(cover.Width), "height": valueInt(cover.Height)}
}

// WorksUploads is the Works page's view of stored data: for each upload with
// a ready cover or a stored video, the cover's local URL and size and the
// videos' public fields, and the recent uploads dated after the list. Uploads
// without either are left out.
func WorksUploads(index works.Index, overlay *sqlite.WorksOverlay) map[string]any {
	uploads := map[string]any{}
	for _, upload := range overlay.Uploads {
		cover := worksCover(upload.Cover)
		videos := make([]any, 0, len(upload.Videos))
		for _, v := range upload.Videos {
			videos = append(videos, map[string]any{"source": v.Source, "id": v.ID, "title": v.Title, "url": v.URL, "publishedAt": value(v.PublishedAt), "scheduledAt": value(v.ScheduledAt), "durationSeconds": valueInt(v.DurationSeconds), "availability": value(v.Availability), "translations": publicTranslations(v.Translations), "sourceLanguage": value(v.Translations.SourceLanguage)})
		}
		uploads[upload.ID] = map[string]any{"cover": cover, "videos": videos}
	}
	recent := make([]any, 0, len(overlay.Recent))
	for _, v := range overlay.Recent {
		recent = append(recent, map[string]any{
			"id": v.ID, "source": v.Source, "title": v.Title, "url": v.URL, "publishedAt": worksRecentDate(v),
			"cover": worksCover(v.Cover), "origin": v.Origin, "translations": publicTranslations(v.Translations),
			"sourceLanguage": value(v.Translations.SourceLanguage), "alternates": publicAlternates(v.Alternates),
		})
	}
	return map[string]any{"updatedAt": index.UpdatedAt, "revision": overlay.Revision, "uploads": uploads, "recent": recent, "recentTotal": overlay.RecentTotal}
}

// worksRecentDate is when a recent upload went up: a premiere's scheduled
// time, else its published time.
func worksRecentDate(v sqlite.WorksRecentUpload) any {
	if v.ScheduledAt != nil {
		return *v.ScheduledAt
	}
	return value(v.PublishedAt)
}
