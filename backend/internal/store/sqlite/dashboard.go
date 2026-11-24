package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

// Dashboard and its child records contain only columns needed to construct the
// public projection. Raw JSON, source item identities, provider metadata and
// filesystem paths are deliberately absent from these types.
type Dashboard struct {
	Profile        *Profile
	Summary        DashboardSummary
	Projection     DashboardProjection
	Posts          []Post
	Events         []DashboardEvent
	Videos         []Video
	Focus          *Focus
	Timeline       []TimelineItem
	Resources      []Resource
	Assets         []Asset
	ScheduleImages []Asset
	ProfileMedia   ProfileMediaSlots
	AccountAvatars map[string]*string
	// LiveNow is the account's Bilibili broadcast that no entry matched, or
	// nil.
	LiveNow *LiveNow
	Meta    DashboardMeta
	// ValidUntil is the earliest time after the read at which its projection
	// changes without a revision: the projection's next transition, or the
	// moment a broadcast state the read trusted stops counting. It is zero
	// when neither lies ahead. Sliding windows (posts, the week, the video
	// count) and freshness times are not included.
	ValidUntil time.Time
}

type Profile struct {
	ID              string
	DisplayName     string
	RomanizedName   *string
	Bio             *string
	AvatarURL       *string
	BannerURL       *string
	XURL            *string
	YouTubeURL      *string
	UpdatedAt       *string
	AvatarID        *string
	AvatarStatus    string
	AvatarSourceURL *string
	AvatarSource    *string
	BannerID        *string
	BannerStatus    string
	BannerSourceURL *string
	BannerSource    *string
}

type Post struct {
	ID             string
	Source         string
	AccountHandle  *string
	Type           string
	Label          *string
	Text           string
	PublishedAt    string
	URL            string
	Likes          int64
	Reposts        int64
	Replies        int64
	MediaURL       *string
	MediaAlt       *string
	AccountURL     *string
	Media          []PostMedia
	MediaID        *string
	MediaStatus    string
	MediaSourceURL *string
	// Promoted marks a pooled status (a related account's or one an own
	// account reposted) promoted onto the board, which is not one of Kano's
	// own posts.
	Promoted bool
	// RepostedBy lists the own accounts that reposted a promoted post, first
	// sighting first; it is empty for every other post.
	RepostedBy []string
	// Translations resolves the text's translations by precedence.
	Translations TranslationSet
	// Quote is the status the post quotes, nil when it quotes none.
	Quote *PostQuote
	// Rich is the original wording with its stickers, only for a Bilibili
	// dynamic that has any (see placeStickers).
	Rich []RichSegment
}

// PostQuote is the stored projection of a quoted status. Media lists only
// its ready cached images; an Unavailable quote has just its ID and URL.
type PostQuote struct {
	ID           string `json:"id"`
	AuthorHandle string `json:"authorHandle"`
	AuthorName   string `json:"authorName"`
	Text         string `json:"text"`
	PublishedAt  string `json:"publishedAt"`
	URL          string `json:"url"`
	Unavailable  bool   `json:"unavailable"`
	Media        []PostMedia
	// Translations are the stored post's of the same status ID, so a quote
	// of a tracked account's post carries that post's translations.
	Translations TranslationSet `json:"-"`
	// Rich is the forwarded dynamic's wording with its stickers, as a
	// post's Rich.
	Rich []RichSegment `json:"-"`
}

type PostMedia struct {
	ID        *string
	URL       *string
	Status    string
	SourceURL *string
	Alt       *string
	Position  int64
	Width     *int64
	Height    *int64
	// Kind is what an X image shows: photo, or the thumbnail of a video or
	// gif. Every other image is a photo.
	Kind string
}

type DashboardEvent struct {
	ID       string
	Source   string
	Title    string
	Detail   *string
	StartsOn string
	StartsAt *string
	EndsAt   *string
	// EndsOn is the Japan close date of a sale period, nil for every other
	// entry and for a sale whose close is not known.
	EndsOn        *string
	Timezone      string
	TimePrecision string
	Status        *string
	EventType     *string
	// Tag is the calendar label of this entry ("live", "game", "talk",
	// "song" or "video"), decided by an operator or the model, or nil.
	Tag                  *string
	URL                  *string
	Provenance           string
	CancellationStatus   string
	CancellationSource   *string
	CancellationReason   *string
	CancellationEvidence *string
	CancellationAt       *string
	ManualLocked         bool
	IsUpcoming           bool
	// UpdatedAt is the stored ISO time of the event's last write, which
	// feeds use to tell calendar clients that an entry changed.
	UpdatedAt string
	// Platform is where the entry happens (youtube, bilibili, other or
	// unknown), or nil for an entry stored before platforms were recorded.
	Platform *string
	// LiveStatus is the linked video's trusted broadcast state, or the
	// Bilibili room's for a Bilibili stream the room's broadcast matched, or
	// nil.
	LiveStatus *string
	// LiveURL is the account's Bilibili live room for a stream or
	// members-only stream shown on Bilibili, once a live check learned the
	// room; nil otherwise.
	LiveURL *string
	// Platforms are the entry's shown platforms, its own platform first, and
	// Links one link per shown platform (see attachEventPlatforms).
	Platforms []string
	Links     []EventLink
	// announced are the stored platforms as readers see them, and roomURL a
	// live-room link stored as the entry's url, which reads as no identity
	// link (see readPlatforms).
	announced []string
	roomURL   string
	// Translations are the linked video's title translations while the
	// event shows that video's title.
	Translations TranslationSet
	// View is the locale-neutral, time-dependent public event projection. It
	// is computed on read and is never stored in SQLite.
	View EventView
}

// EventView contains the event semantics shared by the dashboard and bounded
// calendar reads. Codes are translated by the frontend.
type EventView struct {
	State           string
	TypeCode        *string
	ActivityCode    *string
	ActivityContext string
	IsWork          bool
	CalendarEntries []CalendarEntry
}

// CalendarEntry is one display occurrence of an event. Sale periods have an
// opening and/or closing occurrence; other events have one occurrence.
type CalendarEntry struct {
	SaleMark      *string
	StartsOn      string
	StartsAt      *string
	TimePrecision string
}

// EventLink is one shown platform of an entry, the link to follow there
// (nil when there is none) and that platform's broadcast state.
type EventLink struct {
	Platform   string
	URL        *string
	LiveStatus *string
}

type Video struct {
	ID                 string
	Source             string
	Title              string
	PublishedAt        *string
	ScheduledAt        *string
	URL                string
	ThumbnailURL       *string
	Kind               *string
	IsUpcoming         bool
	EndsAt             *string
	ThumbnailID        *string
	ThumbnailStatus    string
	ThumbnailSourceURL *string
	IsCancelled        bool
	// LiveStatus is the broadcast state a watch page reported: an end is
	// final, while live and upcoming count only while fresh. Nil is unknown.
	LiveStatus *string
	// DurationSeconds is the length a watch page reported; nil while unknown,
	// as for a feed entry, a waiting room or a broadcast still on air.
	DurationSeconds *int64
	// IsShort is an operator's format correction, else the feed's: true for
	// a Short, false for any other upload, nil while unknown, as for a row
	// the feed never listed with a format link.
	IsShort *bool
	// Tag is the content tag (game, talk or song) the operator, the schedule
	// entry or the title inference gave the video; nil when none did.
	Tag *string
	// Availability is who can watch the video (public, unlisted, members,
	// private or removed) as the last check could tell; nil until one did.
	Availability *string
	// Translations resolves the title's translations by precedence.
	Translations TranslationSet
	// Alternates are the other platforms' uploads merged into this video.
	Alternates []VideoAlternate
	// View is the locale-neutral, time-dependent public video projection. It
	// is computed on read and is never stored in SQLite.
	View VideoView
}

type VideoView struct {
	Phase          string
	FormCode       *string
	ShelfGroup     string
	LatestEligible bool
	FocusEligible  bool
	TagContext     string
}

// VideoAlternate is the public link of an upload merged into a video.
type VideoAlternate struct {
	Source string
	URL    string
}

type Focus struct {
	ID             int64
	VideoID        *string
	DateLabel      *string
	Title          string
	Description    *string
	ImageURL       *string
	URL            *string
	SourceURL      *string
	UpdatedAt      *string
	ImageID        *string
	ImageStatus    string
	ImageSourceURL *string
}

type TimelineItem struct {
	ID        int64
	Year      string
	Title     string
	Detail    *string
	SortOrder int64
}

type Resource struct {
	ID        int64
	Title     string
	Detail    *string
	Icon      *string
	Tone      *string
	URL       string
	SortOrder int64
}

type Asset struct {
	ID             string
	Kind           string
	Alt            *string
	WeekStart      *string
	PeriodStart    *string
	PeriodEnd      *string
	PeriodBasis    *string
	SourceAccount  *string
	UpdatedAt      *string
	URL            *string
	SourceURL      *string
	MediaID        *string
	MediaStatus    string
	MediaSourceURL *string
}

type ProfileMedia struct {
	ID        string
	Slot      string
	Source    string
	SourceURL *string
	Status    string
	IsActive  bool
	PublicURL *string
}

type ProfileMediaSlots struct {
	Avatar *ProfileMedia
	Banner *ProfileMedia
}

type DashboardSummary struct {
	NextEvent   *DashboardEvent
	NextStream  *Video
	LatestVideo *Video
	LatestPost  *Post
	Counts      DashboardCounts
}

type DashboardProjection struct {
	Version          int
	NextTransitionAt *string
	Focus            DashboardFocusProjection
	Spotlight        *DashboardSpotlight
	Sales            []string
	Shelf            DashboardShelf
}

type DashboardFocusProjection struct {
	EventIDs []string
	VideoIDs []string
}

type DashboardSpotlight struct {
	Mode             string
	EventID          *string
	VideoID          *string
	LiveNow          bool
	IsLive           bool
	MembersChip      bool
	CompanionVideoID *string
}

type DashboardShelf struct {
	LeadID         *string
	LeadIsFeatured bool
	Reservations   []string
	Works          []string
	Archives       []string
	Shorts         []string
}

type DashboardCounts struct {
	Posts          int64
	Events         int64
	UpcomingEvents int64
	Videos         int64
	Resources      int64
}

type EventWindow struct {
	From      string
	To        string
	Paginated bool
}

type DashboardMeta struct {
	FetchedAt       *string
	SourceFetchedAt map[string]*string
	LastSync        *SyncRun
	PostWindowDays  int
	EventWindow     EventWindow
	GeneratedAt     string
	MediaCache      map[string]int64
	Revision        int64
	FeaturedVideoID *string
	XAccounts       []string
	// BilibiliAccounts are the mids of the stored Bilibili posts, for the
	// post wall's account filter.
	BilibiliAccounts []string
}

type mediaAsset struct {
	ID        string
	SourceURL string
	CachePath *string
	MIMEType  *string
	ByteSize  *int64
	SHA256    *string
	Width     *int64
	Height    *int64
	Status    string
}

type mediaReference struct {
	ID        *string
	Status    string
	SourceURL *string
	PublicURL *string
	MIMEType  *string
	ByteSize  *int64
	Width     *int64
	Height    *int64
}

func (s *Store) GetDashboard(ctx context.Context, days int, now time.Time) (*Dashboard, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if s.readDB != nil {
		return s.publicReader().GetDashboard(ctx, days, now)
	}
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}
	ctx = withReadMemo(ctx)
	windowStart := now.Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	postRows, err := s.queryPosts(ctx, windowStart)
	if err != nil {
		return nil, err
	}
	if err = s.prefetchPostRows(ctx, postRows); err != nil {
		return nil, err
	}
	posts := make([]Post, 0, len(postRows))
	var latest *Post
	for _, row := range postRows {
		mapped, mapErr := s.mapPost(ctx, row)
		if mapErr != nil {
			return nil, mapErr
		}
		posts = append(posts, mapped)
		// The latest post is an own account's: a promoted post never takes
		// it. queryPosts lists every account's newest posts newest first, so
		// the first own post is the newest of them all.
		if latest == nil && !row.promoted {
			latestCopy := mapped
			latest = &latestCopy
		}
	}
	postCount, err := s.queryPostCount(ctx, windowStart)
	if err != nil {
		return nil, err
	}

	today := dateInJapan(now)
	from, to := mondayWindow(today)
	events, err := s.queryDashboardEvents(ctx, from, to, now)
	if err != nil {
		return nil, err
	}
	totalEvents, upcomingEvents, err := s.queryEventCounts(ctx, today, now)
	if err != nil {
		return nil, err
	}

	featuredID, err := s.getFeaturedVideoID(ctx)
	if err != nil {
		return nil, err
	}
	videoCount, err := s.queryRecentVideoCount(ctx, now)
	if err != nil {
		return nil, err
	}
	probe, err := s.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	liveCutoff := now.Add(-probe.Freshness())
	videos, err := s.queryVideos(ctx, now, liveCutoff)
	if err != nil {
		return nil, err
	}
	if featuredID != nil && !containsVideo(videos, *featuredID) {
		featured, getErr := s.getVideo(ctx, *featuredID, liveCutoff)
		if getErr != nil {
			return nil, getErr
		}
		if featured != nil {
			videos = append(videos, *featured)
		}
	}
	// Each returned video adopts its linked event's end, and that event joins
	// the board's list.
	linkedEvents, err := s.queryLinkedEvents(ctx, videos, now)
	if err != nil {
		return nil, err
	}
	if err = s.resolveVideoThumbnails(ctx, videos); err != nil {
		return nil, err
	}
	for i := range videos {
		if linked := linkedEvents[videos[i].ID]; linked != nil {
			videos[i].EndsAt = linked.EndsAt
			if !containsEvent(events, linked.ID) {
				events = append(events, *linked)
			}
		}
	}
	if err = s.pinVideoThumbnails(ctx, videos); err != nil {
		return nil, err
	}
	live := map[string]*string{}
	for i := range videos {
		videos[i].IsUpcoming = videoUpcoming(videos[i], now)
		if videos[i].LiveStatus != nil {
			live[streamIdentity(videos[i].URL)] = videos[i].LiveStatus
		}
	}
	for i := range events {
		if events[i].URL != nil {
			events[i].LiveStatus = live[streamIdentity(*events[i].URL)]
		}
	}
	liveNow, err := s.attachEventPlatforms(ctx, events, now)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(events, func(i, j int) bool {
		return eventSortKey(events[i]) < eventSortKey(events[j]) || (eventSortKey(events[i]) == eventSortKey(events[j]) && events[i].ID < events[j].ID)
	})

	focus, err := s.queryFocus(ctx)
	if err != nil {
		return nil, err
	}
	if focus != nil {
		ref, e := s.resolveMedia(ctx, focus.ImageURL)
		if e != nil {
			return nil, e
		}
		focus.ImageID, focus.ImageStatus, focus.ImageSourceURL, focus.ImageURL = ref.ID, ref.Status, ref.SourceURL, ref.PublicURL
	}
	timeline, err := s.queryTimeline(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := s.queryResources(ctx)
	if err != nil {
		return nil, err
	}
	calendarAssets, err := s.queryScheduleAssets(ctx, from, to)
	if err != nil {
		return nil, err
	}
	otherAssets, err := s.queryOtherAssets(ctx)
	if err != nil {
		return nil, err
	}
	assets := append(otherAssets, calendarAssets...)

	profile, err := s.queryProfile(ctx)
	if err != nil {
		return nil, err
	}
	profileMedia, err := s.queryProfileMedia(ctx)
	if err != nil {
		return nil, err
	}
	if profile != nil {
		avatarRef, e := s.resolveMedia(ctx, profile.AvatarURL)
		if e != nil {
			return nil, e
		}
		bannerRef, e := s.resolveMedia(ctx, profile.BannerURL)
		if e != nil {
			return nil, e
		}
		if profileMedia.Avatar != nil {
			profile.AvatarURL, profile.AvatarID, profile.AvatarStatus = firstString(profileMedia.Avatar.PublicURL, avatarRef.PublicURL), strPtr(profileMedia.Avatar.ID), profileMedia.Avatar.Status
			profile.AvatarSourceURL, profile.AvatarSource = firstString(profileMedia.Avatar.SourceURL, avatarRef.SourceURL), strPtr(profileMedia.Avatar.Source)
		} else {
			profile.AvatarURL, profile.AvatarID, profile.AvatarStatus, profile.AvatarSourceURL = avatarRef.PublicURL, avatarRef.ID, avatarRef.Status, avatarRef.SourceURL
		}
		if profileMedia.Banner != nil {
			profile.BannerURL, profile.BannerID, profile.BannerStatus = firstString(profileMedia.Banner.PublicURL, bannerRef.PublicURL), strPtr(profileMedia.Banner.ID), profileMedia.Banner.Status
			profile.BannerSourceURL, profile.BannerSource = firstString(profileMedia.Banner.SourceURL, bannerRef.SourceURL), strPtr(profileMedia.Banner.Source)
		} else {
			profile.BannerURL, profile.BannerID, profile.BannerStatus, profile.BannerSourceURL = bannerRef.PublicURL, bannerRef.ID, bannerRef.Status, bannerRef.SourceURL
		}
	}
	lastSync, err := s.GetLatestSync(ctx)
	if err != nil {
		return nil, err
	}
	fetchedAt, err := s.getFetchedAt(ctx)
	if err != nil {
		return nil, err
	}
	sourceFetchedAt, err := s.sourceFetchedAt(ctx)
	if err != nil {
		return nil, err
	}
	mediaCache, err := s.getMediaCache(ctx)
	if err != nil {
		return nil, err
	}
	revision, err := s.GetDashboardRevision(ctx)
	if err != nil {
		return nil, err
	}
	bilibiliAccounts, err := s.getBilibiliAccounts(ctx)
	if err != nil {
		return nil, err
	}
	xAccounts, err := s.getXAccounts(ctx)
	if err != nil {
		return nil, err
	}

	cancelled, err := s.cancelledVideos(ctx, videos)
	if err != nil {
		return nil, err
	}
	for i := range videos {
		videos[i].IsCancelled = cancelled[videos[i].ID]
	}
	postRefs := []*Post{latest}
	for i := range posts {
		postRefs = append(postRefs, &posts[i])
	}
	if err = s.attachPostTranslations(ctx, postRefs...); err != nil {
		return nil, err
	}
	videoRefs := []*Video{}
	for i := range videos {
		videoRefs = append(videoRefs, &videos[i])
	}
	if err = s.attachVideoTranslations(ctx, videoRefs...); err != nil {
		return nil, err
	}
	if err = s.attachVideoAlternates(ctx, videoRefs...); err != nil {
		return nil, err
	}
	if err = s.attachEventTranslations(ctx, events); err != nil {
		return nil, err
	}
	upcoming := make([]DashboardEvent, 0)
	for i := range events {
		if events[i].IsUpcoming && !cancelledStatus(events[i].Status) && events[i].CancellationStatus != "llm_suspected" && events[i].CancellationStatus != "manual_confirmed" {
			upcoming = append(upcoming, events[i])
		}
	}
	upcomingVideos := make([]Video, 0)
	for _, video := range videos {
		if video.IsUpcoming && !video.IsCancelled {
			upcomingVideos = append(upcomingVideos, video)
		}
	}
	sort.SliceStable(upcomingVideos, func(i, j int) bool {
		a, b := timeKey(upcomingVideos[i].ScheduledAt), timeKey(upcomingVideos[j].ScheduledAt)
		return a < b || (a == b && upcomingVideos[i].ID < upcomingVideos[j].ID)
	})
	// The stage's fallback: the newest upload that is not a Short, unless the
	// operator featured that Short, and that visitors can still watch. An
	// unknown format or availability stays eligible.
	var latestVideo *Video
	for i := range videos {
		short := videos[i].IsShort != nil && *videos[i].IsShort
		featured := featuredID != nil && videos[i].ID == *featuredID
		if !videos[i].IsUpcoming && (!short || featured) && !gone(videos[i].Availability) {
			latestVideo = &videos[i]
			break
		}
	}
	// A sale period is never the next event: the stage shows activities, and
	// a month-long order window would hold it.
	var nextEvent *DashboardEvent
	for i := range upcoming {
		if !SaleEvent(upcoming[i].EventType) {
			nextEvent = &upcoming[i]
			break
		}
	}
	var nextStream *Video
	if len(upcomingVideos) > 0 {
		nextStream = &upcomingVideos[0]
	}
	if latest == nil && len(posts) > 0 {
		latest = &posts[0]
	}
	accountAvatars, err := s.AccountAvatars(ctx)
	if err != nil {
		return nil, err
	}
	dashboard := &Dashboard{
		Profile: profile,
		Summary: DashboardSummary{NextEvent: nextEvent, NextStream: nextStream, LatestVideo: latestVideo, LatestPost: latest, Counts: DashboardCounts{Posts: postCount, Events: totalEvents, UpcomingEvents: upcomingEvents, Videos: videoCount, Resources: int64(len(resources))}},
		Posts:   posts, Events: events, Videos: videos, Focus: focus, Timeline: timeline, Resources: resources, Assets: assets, ScheduleImages: filterReadyAssets(calendarAssets), ProfileMedia: profileMedia, AccountAvatars: accountAvatars, LiveNow: liveNow,
		Meta: DashboardMeta{FetchedAt: fetchedAt, SourceFetchedAt: sourceFetchedAt, LastSync: lastSync, PostWindowDays: days, EventWindow: EventWindow{From: from, To: to, Paginated: true}, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05.000Z"), MediaCache: mediaCache, Revision: revision, FeaturedVideoID: featuredID, XAccounts: xAccounts, BilibiliAccounts: bilibiliAccounts},
	}
	applyDashboardProjection(dashboard, now)
	liveExpiry, err := s.liveStatusExpiry(ctx, now, probe)
	if err != nil {
		return nil, err
	}
	dashboard.ValidUntil = liveExpiry
	if next := dashboard.Projection.NextTransitionAt; next != nil {
		if at, parseErr := time.Parse(time.RFC3339Nano, *next); parseErr == nil && at.After(now) && (dashboard.ValidUntil.IsZero() || at.Before(dashboard.ValidUntil)) {
			dashboard.ValidUntil = at
		}
	}
	return dashboard, nil
}

// liveStatusExpiry is the earliest time after now at which a broadcast state
// that a read at now trusts stops counting: a YouTube video's live or
// upcoming state (see liveView) or the Bilibili room's on-air state (see
// projectRoom). It covers every stored video, not only the read's, so it is
// never later than the read's own; it is zero when no state would expire.
func (s *Store) liveStatusExpiry(ctx context.Context, now time.Time, probe LiveProbeSettings) (time.Time, error) {
	var expiry time.Time
	consider := func(at time.Time) {
		if at.After(now) && (expiry.IsZero() || at.Before(expiry)) {
			expiry = at
		}
	}
	cutoff := now.Add(-probe.Freshness())
	rows, err := s.db.QueryContext(ctx, `SELECT live_status,live_checked_at FROM videos WHERE live_status IN (?,?) AND julianday(live_checked_at) > julianday(?)`,
		ytsource.LiveStatusLive, ytsource.LiveStatusUpcoming, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return time.Time{}, dbErr(err)
	}
	for rows.Next() {
		var status, checked sql.NullString
		if err = rows.Scan(&status, &checked); err != nil {
			_ = rows.Close()
			return time.Time{}, dbErr(err)
		}
		// Only a state liveView trusts can stop counting.
		if liveView(status, checked, cutoff) != nil {
			at, _ := time.Parse(time.RFC3339Nano, checked.String)
			consider(at.Add(probe.Freshness()))
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return time.Time{}, dbErr(err)
	}
	if exists, tableErr := s.hasTable(ctx, "sync_state"); tableErr != nil || !exists {
		return expiry, tableErr
	}
	live, err := readBilibiliLive(ctx, s.db, bilisource.OptionsFromEnv().Account)
	if err != nil {
		return time.Time{}, dbErr(err)
	}
	if live.State.Room.RoomID != "" && live.State.Room.LiveStatus == bilisource.RoomLive && !live.LastBroadcast.IsZero() {
		consider(live.State.CheckedAt.Add(probe.BilibiliFreshness()))
	}
	return expiry, nil
}
