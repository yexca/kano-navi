package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Dashboard and its child records contain only columns needed to construct the
// public projection. Raw JSON, source item identities, provider metadata and
// filesystem paths are deliberately absent from these types.
type Dashboard struct {
	Profile        *Profile
	Summary        DashboardSummary
	Posts          []Post
	Events         []DashboardEvent
	Videos         []Video
	Focus          *Focus
	Timeline       []TimelineItem
	Resources      []Resource
	Assets         []Asset
	ScheduleImages []Asset
	ProfileMedia   ProfileMediaSlots
	Meta           DashboardMeta
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
	// Translations resolves the text's translations by precedence.
	Translations TranslationSet
	// Quote is the status the post quotes, nil when it quotes none.
	Quote *PostQuote
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
}

type DashboardEvent struct {
	ID                   string
	Source               string
	Title                string
	Detail               *string
	StartsOn             string
	StartsAt             *string
	EndsAt               *string
	Timezone             string
	TimePrecision        string
	Status               *string
	EventType            *string
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
	// LiveStatus is the linked video's trusted broadcast state, or nil.
	LiveStatus *string
	// Translations are the linked video's title translations while the
	// event shows that video's title.
	Translations TranslationSet
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
	// Translations resolves the title's translations by precedence.
	Translations TranslationSet
	// Alternates are the other platforms' uploads merged into this video.
	Alternates []VideoAlternate
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
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}
	windowStart := now.Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	postRows, err := s.queryPosts(ctx, windowStart)
	if err != nil {
		return nil, err
	}
	latestPost, err := s.queryLatestPost(ctx)
	if err != nil {
		return nil, err
	}
	posts := make([]Post, 0, len(postRows))
	for _, row := range postRows {
		mapped, mapErr := s.mapPost(ctx, row)
		if mapErr != nil {
			return nil, mapErr
		}
		posts = append(posts, mapped)
	}
	var latest *Post
	if latestPost != nil {
		mapped, mapErr := s.mapPost(ctx, *latestPost)
		if mapErr != nil {
			return nil, mapErr
		}
		latest = &mapped
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
	probe, err := s.LiveProbeSettings(ctx)
	if err != nil {
		return nil, err
	}
	liveCutoff := now.Add(-probe.Freshness())
	videos, err := s.queryVideos(ctx, now, liveCutoff)
	if err != nil {
		return nil, err
	}
	for i := range videos {
		linked, linkErr := s.queryLinkedEvent(ctx, videos[i], now)
		if linkErr != nil {
			return nil, linkErr
		}
		if linked != nil {
			videos[i].EndsAt = linked.EndsAt
			if !containsEvent(events, linked.ID) {
				events = append(events, *linked)
			}
		}
		ref, refErr := s.resolveMedia(ctx, videos[i].ThumbnailURL)
		if refErr != nil {
			return nil, refErr
		}
		videos[i].ThumbnailID, videos[i].ThumbnailStatus, videos[i].ThumbnailSourceURL = ref.ID, ref.Status, ref.SourceURL
		videos[i].ThumbnailURL = ref.PublicURL
	}
	if featuredID != nil && !containsVideo(videos, *featuredID) {
		featured, getErr := s.getVideo(ctx, *featuredID, liveCutoff)
		if getErr != nil {
			return nil, getErr
		}
		if featured != nil {
			ref, refErr := s.resolveMedia(ctx, featured.ThumbnailURL)
			if refErr != nil {
				return nil, refErr
			}
			featured.ThumbnailID, featured.ThumbnailStatus, featured.ThumbnailSourceURL = ref.ID, ref.Status, ref.SourceURL
			featured.ThumbnailURL = ref.PublicURL
			videos = append(videos, *featured)
		}
	}
	if err = s.pinVideoThumbnails(ctx, videos); err != nil {
		return nil, err
	}
	if featuredID != nil {
		for i := range videos {
			if videos[i].ID != *featuredID {
				continue
			}
			if linked, linkErr := s.queryLinkedEvent(ctx, videos[i], now); linkErr != nil {
				return nil, linkErr
			} else if linked != nil {
				videos[i].EndsAt = linked.EndsAt
				if !containsEvent(events, linked.ID) {
					events = append(events, *linked)
				}
			}
		}
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
	xAccounts, err := s.getXAccounts(ctx)
	if err != nil {
		return nil, err
	}

	for i := range videos {
		videos[i].IsCancelled, err = s.videoCancelled(ctx, videos[i])
		if err != nil {
			return nil, err
		}
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
	// operator featured that Short. An unknown format stays eligible.
	var latestVideo *Video
	for i := range videos {
		short := videos[i].IsShort != nil && *videos[i].IsShort
		featured := featuredID != nil && videos[i].ID == *featuredID
		if !videos[i].IsUpcoming && (!short || featured) {
			latestVideo = &videos[i]
			break
		}
	}
	var nextEvent *DashboardEvent
	if len(upcoming) > 0 {
		nextEvent = &upcoming[0]
	}
	var nextStream *Video
	if len(upcomingVideos) > 0 {
		nextStream = &upcomingVideos[0]
	}
	if latest == nil && len(posts) > 0 {
		latest = &posts[0]
	}
	return &Dashboard{
		Profile: profile,
		Summary: DashboardSummary{NextEvent: nextEvent, NextStream: nextStream, LatestVideo: latestVideo, LatestPost: latest, Counts: DashboardCounts{Posts: postCount, Events: totalEvents, UpcomingEvents: upcomingEvents, Videos: int64(len(videos)), Resources: int64(len(resources))}},
		Posts:   posts, Events: events, Videos: videos, Focus: focus, Timeline: timeline, Resources: resources, Assets: assets, ScheduleImages: filterReadyAssets(calendarAssets), ProfileMedia: profileMedia,
		Meta: DashboardMeta{FetchedAt: fetchedAt, SourceFetchedAt: sourceFetchedAt, LastSync: lastSync, PostWindowDays: days, EventWindow: EventWindow{From: from, To: to, Paginated: true}, GeneratedAt: now.UTC().Format("2006-01-02T15:04:05.000Z"), MediaCache: mediaCache, Revision: revision, FeaturedVideoID: featuredID, XAccounts: xAccounts},
	}, nil
}

type postRow struct {
	id, source, accountHandle, typ, label, text, publishedAt, url, mediaURL, mediaAlt string
	likes, reposts, replies                                                           int64
	hasAccount, hasLabel, hasMediaURL, hasMediaAlt                                    bool
	// quoteJSON is NULL in public reads when an operator hid the quote.
	quoteJSON sql.NullString
}

// dashboardMinPosts is how many of the newest posts the board always lists,
// however old, so a quiet window still fills the wall's whole rows (two rows
// of up to four columns; see src/dashboard/post-rows.ts).
const dashboardMinPosts = 8

// queryPosts lists the posts published since the window start, and at least
// the newest dashboardMinPosts of them.
func (s *Store) queryPosts(ctx context.Context, since string) ([]postRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, source, account_handle, type, label, text, published_at, url, likes, reposts, replies, media_url, media_alt, CASE WHEN quote_hidden_at IS NULL THEN quote_json END FROM posts WHERE julianday(published_at) >= julianday(?) OR id IN (SELECT id FROM posts ORDER BY julianday(published_at) DESC, id ASC LIMIT ?) ORDER BY julianday(published_at) DESC, id ASC LIMIT 100`, since, dashboardMinPosts)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := make([]postRow, 0, 100)
	for rows.Next() {
		var r postRow
		var account, label, mu, ma sql.NullString
		if err := rows.Scan(&r.id, &r.source, &account, &r.typ, &label, &r.text, &r.publishedAt, &r.url, &r.likes, &r.reposts, &r.replies, &mu, &ma, &r.quoteJSON); err != nil {
			return nil, dbErr(err)
		}
		r.accountHandle, r.hasAccount = account.String, account.Valid
		r.label, r.hasLabel = label.String, label.Valid
		r.mediaURL, r.hasMediaURL = mu.String, mu.Valid
		r.mediaAlt, r.hasMediaAlt = ma.String, ma.Valid
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	return out, nil
}
func (s *Store) queryLatestPost(ctx context.Context) (*postRow, error) {
	var r postRow
	var account, label, mu, ma sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt,CASE WHEN quote_hidden_at IS NULL THEN quote_json END FROM posts ORDER BY julianday(published_at) DESC,id ASC LIMIT 1`).Scan(&r.id, &r.source, &account, &r.typ, &label, &r.text, &r.publishedAt, &r.url, &r.likes, &r.reposts, &r.replies, &mu, &ma, &r.quoteJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	r.accountHandle, r.hasAccount = account.String, account.Valid
	r.label, r.hasLabel = label.String, label.Valid
	r.mediaURL, r.hasMediaURL = mu.String, mu.Valid
	r.mediaAlt, r.hasMediaAlt = ma.String, ma.Valid
	return &r, nil
}
func (s *Store) queryPostCount(ctx context.Context, since string) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE julianday(published_at) >= julianday(?)`, since).Scan(&count); err != nil {
		return 0, dbErr(err)
	}
	return count, nil
}
func (s *Store) mapPost(ctx context.Context, r postRow) (Post, error) {
	p := Post{ID: r.id, Source: r.source, Type: r.typ, Text: r.text, PublishedAt: r.publishedAt, URL: r.url, Likes: r.likes, Reposts: r.reposts, Replies: r.replies}
	if r.hasAccount {
		p.AccountHandle = strPtr(r.accountHandle)
	} else if h := xHandleFromURL(r.url); h != "" {
		p.AccountHandle = &h
	}
	if r.hasLabel {
		p.Label = strPtr(r.label)
	}
	if r.hasMediaURL {
		p.MediaURL = strPtr(r.mediaURL)
	}
	if r.hasMediaAlt {
		p.MediaAlt = strPtr(r.mediaAlt)
	}
	if p.AccountHandle != nil && r.source == "x" {
		u := "https://x.com/" + *p.AccountHandle
		p.AccountURL = &u
	}
	ref, err := s.resolveMedia(ctx, p.MediaURL)
	if err != nil {
		return p, err
	}
	p.MediaID, p.MediaStatus, p.MediaSourceURL = ref.ID, ref.Status, ref.SourceURL
	if p.MediaURL != nil {
		p.MediaURL = ref.PublicURL
		p.Media = []PostMedia{{ID: ref.ID, URL: ref.PublicURL, Status: ref.Status, SourceURL: ref.SourceURL, Alt: p.MediaAlt, Position: 0, Width: ref.Width, Height: ref.Height}}
	}
	links, err := s.queryPostMedia(ctx, r.id, "post-image")
	if err != nil {
		return p, err
	}
	if len(links) > 0 {
		p.Media = links
		p.MediaID, p.MediaURL, p.MediaStatus, p.MediaSourceURL = links[0].ID, links[0].URL, links[0].Status, links[0].SourceURL
	}
	if p.Quote, err = s.mapQuote(ctx, r.id, r.quoteJSON); err != nil {
		return p, err
	}
	return p, nil
}

var (
	quoteIDPattern   = regexp.MustCompile(`^\d{10,22}$`)
	quotePathPattern = regexp.MustCompile(`^/[A-Za-z0-9_]{1,30}/status/(\d{10,22})$`)
)

// quoteLinksTo reports whether a stored quote URL is exactly the x.com page
// of the status id.
func quoteLinksTo(raw, id string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "x.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	m := quotePathPattern.FindStringSubmatch(u.Path)
	return m != nil && m[1] == id
}

// mapQuote reads a post's stored quote. A projection that is not a valid
// quote of an x.com status is left out; its images are the post's
// quote-image links whose cached files are ready.
func (s *Store) mapQuote(ctx context.Context, postID string, stored sql.NullString) (*PostQuote, error) {
	if !stored.Valid || stored.String == "" {
		return nil, nil
	}
	var q PostQuote
	if json.Unmarshal([]byte(stored.String), &q) != nil || !quoteIDPattern.MatchString(q.ID) || !quoteLinksTo(q.URL, q.ID) {
		return nil, nil
	}
	q.Media = []PostMedia{}
	if q.Unavailable {
		q.AuthorHandle, q.AuthorName, q.Text, q.PublishedAt = "", "", "", ""
		return &q, nil
	}
	links, err := s.queryPostMedia(ctx, postID, roleQuoteImage)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if link.Status == "ready" && link.ID != nil && link.URL != nil {
			q.Media = append(q.Media, link)
		}
	}
	return &q, nil
}

func (s *Store) queryPostMedia(ctx context.Context, id, role string) ([]PostMedia, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ml.media_id,ml.position,ml.alt,ma.source_url,ma.cache_path,ma.mime_type,ma.byte_size,ma.sha256,NULLIF(ma.width,0),NULLIF(ma.height,0),ma.status FROM media_links ml LEFT JOIN media_assets ma ON ma.id=ml.media_id WHERE ml.owner_type='post' AND ml.owner_id=? AND ml.role=? ORDER BY ml.position ASC`, id, role)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []PostMedia{}
	for rows.Next() {
		var mid string
		var pos int64
		var alt, src, cp, mime, sha sql.NullString
		var bs, w, h sql.NullInt64
		var status sql.NullString
		if err := rows.Scan(&mid, &pos, &alt, &src, &cp, &mime, &bs, &sha, &w, &h, &status); err != nil {
			return nil, dbErr(err)
		}
		if !src.Valid || !status.Valid {
			continue
		}
		asset := mediaAsset{ID: mid, SourceURL: src.String, Status: status.String}
		if src.Valid {
			asset.SourceURL = src.String
		}
		if cp.Valid {
			asset.CachePath = &cp.String
		}
		if mime.Valid {
			asset.MIMEType = &mime.String
		}
		if bs.Valid {
			asset.ByteSize = &bs.Int64
		}
		if sha.Valid {
			asset.SHA256 = &sha.String
		}
		if w.Valid {
			asset.Width = &w.Int64
		}
		if h.Valid {
			asset.Height = &h.Int64
		}
		ref := s.mediaReference(asset)
		if ref == nil {
			continue
		}
		var ap *string
		if alt.Valid {
			ap = &alt.String
		}
		out = append(out, PostMedia{ID: ref.ID, URL: ref.PublicURL, Status: ref.Status, SourceURL: ref.SourceURL, Alt: ap, Position: pos, Width: ref.Width, Height: ref.Height})
	}
	return out, rows.Err()
}

func (s *Store) queryDashboardEvents(ctx context.Context, from, to string, now time.Time) ([]DashboardEvent, error) {
	q := `SELECT id,source,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_at,updated_at FROM events WHERE deleted_at IS NULL AND merged_into IS NULL AND id IN (SELECT id FROM (SELECT id FROM events WHERE deleted_at IS NULL AND merged_into IS NULL AND starts_on BETWEEN ? AND ? ORDER BY julianday(COALESCE(starts_at,starts_on)) ASC,id ASC LIMIT 100) UNION SELECT id FROM (SELECT id FROM events WHERE ` + eligibleEventSQL + ` AND (julianday(starts_at)>=julianday(?) OR (starts_at IS NULL AND starts_on>=?)) ORDER BY COALESCE(julianday(starts_at),julianday(starts_on||'T23:59:59.999+09:00')) ASC,id ASC LIMIT 30) UNION SELECT id FROM (SELECT id FROM events WHERE ` + eligibleEventSQL + ` AND (julianday(starts_at)<=julianday(?) AND (julianday(ends_at)>julianday(?) OR (ends_at IS NULL AND event_type IN ('stream','member') AND julianday(starts_at)>julianday(?,'-3 hours')))) AND (event_type IN ('stream','member') OR source='youtube') ORDER BY julianday(starts_at) ASC,id ASC LIMIT 30)) ORDER BY COALESCE(julianday(starts_at),julianday(starts_on||'T23:59:59.999+09:00')) ASC,id ASC`
	args := []any{from, to, now.UTC().Format(time.RFC3339Nano), dateInJapan(now), now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)}
	return s.scanEvents(ctx, q, args, now)
}

const eligibleEventSQL = `deleted_at IS NULL AND merged_into IS NULL AND lower(trim(COALESCE(status,''))) NOT LIKE 'cancel%' AND trim(COALESCE(status,'')) NOT IN ('取消','已取消','中止','キャンセル') AND cancellation_status NOT IN ('llm_suspected','manual_confirmed')`

func (s *Store) scanEvents(ctx context.Context, q string, args []any, now time.Time) ([]DashboardEvent, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []DashboardEvent{}
	for rows.Next() {
		e, err := scanEvent(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanEvent(row scanner, now time.Time) (DashboardEvent, error) {
	var e DashboardEvent
	var detail, sa, ea, status, et, urlv, cs, cr, ce, cat sql.NullString
	var locked int64
	if err := row.Scan(&e.ID, &e.Source, &e.Title, &detail, &e.StartsOn, &sa, &ea, &e.Timezone, &e.TimePrecision, &status, &et, &urlv, &e.Provenance, &locked, &e.CancellationStatus, &cs, &cr, &ce, &cat, &e.UpdatedAt); err != nil {
		return e, dbErr(err)
	}
	if detail.Valid {
		e.Detail = &detail.String
	}
	if sa.Valid {
		e.StartsAt = &sa.String
	}
	if ea.Valid {
		e.EndsAt = &ea.String
	}
	if status.Valid {
		e.Status = &status.String
	}
	if et.Valid {
		e.EventType = &et.String
	}
	if urlv.Valid {
		e.URL = &urlv.String
	}
	if cs.Valid {
		e.CancellationSource = &cs.String
	}
	if cr.Valid {
		e.CancellationReason = &cr.String
	}
	if ce.Valid {
		e.CancellationEvidence = &ce.String
	}
	if cat.Valid {
		e.CancellationAt = &cat.String
	}
	e.ManualLocked = locked != 0
	e.IsUpcoming = eventUpcoming(e, now)
	if e.CancellationStatus == "" {
		e.CancellationStatus = "none"
	}
	return e, nil
}
func (s *Store) queryEventCounts(ctx context.Context, today string, now time.Time) (int64, int64, error) {
	var total, up int64
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events WHERE deleted_at IS NULL AND merged_into IS NULL),(SELECT COUNT(*) FROM events WHERE `+eligibleEventSQL+` AND (julianday(starts_at)>=julianday(?) OR (starts_at IS NULL AND starts_on>=?)))`, now.UTC().Format(time.RFC3339Nano), today).Scan(&total, &up)
	if err != nil {
		return 0, 0, dbErr(err)
	}
	return total, up, nil
}

func (s *Store) queryVideos(ctx context.Context, now, liveCutoff time.Time) ([]Video, error) {
	n := now.UTC().Format(time.RFC3339Nano)
	// A confirmed live broadcast stays listed past the start-time grace.
	q := `SELECT id,source,COALESCE(manual_title,title),published_at,scheduled_at,url,thumbnail_url,COALESCE(manual_kind,kind),is_upcoming,live_status,live_checked_at,duration_seconds,COALESCE(manual_is_short,is_short) FROM videos WHERE id IN (SELECT id FROM (SELECT id FROM videos WHERE ` + publicVideoScope + ` ORDER BY julianday(COALESCE(scheduled_at,published_at)) DESC,id ASC LIMIT 30) UNION SELECT id FROM (SELECT v.id FROM videos v WHERE (julianday(v.scheduled_at)>julianday(?,'-3 hours') OR (v.scheduled_at IS NULL AND v.is_upcoming=1) OR (v.live_status='live' AND julianday(v.live_checked_at)>julianday(?)) OR EXISTS (SELECT 1 FROM events e WHERE e.deleted_at IS NULL AND julianday(e.ends_at)>julianday(?) AND (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.id) OR (e.url IS NOT NULL AND stream_identity(e.url)=stream_identity(v.url))))) AND NOT EXISTS (SELECT 1 FROM events e WHERE e.cancellation_status='manual_confirmed' AND (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.id))) AND NOT EXISTS (SELECT 1 FROM events e WHERE e.deleted_at IS NULL AND julianday(e.ends_at)<=julianday(?) AND (e.id='youtube-'||v.id OR (e.source='youtube' AND e.source_item_id=v.id) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=v.id) OR (e.url IS NOT NULL AND stream_identity(e.url)=stream_identity(v.url)))) ORDER BY julianday(v.scheduled_at) ASC,v.id ASC LIMIT 30) UNION SELECT id FROM (SELECT id FROM videos WHERE ` + publicVideoScope + ` AND (scheduled_at IS NULL OR julianday(scheduled_at)<julianday(?)) ORDER BY julianday(COALESCE(scheduled_at,published_at)) DESC,id ASC LIMIT 1)) ORDER BY julianday(COALESCE(scheduled_at,published_at)) DESC,id ASC`
	rows, err := s.db.QueryContext(ctx, q, n, liveCutoff.UTC().Format(time.RFC3339Nano), n, n, n)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []Video{}
	for rows.Next() {
		var v Video
		var pub, sch, thumb, kind, liveStatus, liveChecked sql.NullString
		var upcoming int64
		var duration sql.NullInt64
		var short sql.NullBool
		if err := rows.Scan(&v.ID, &v.Source, &v.Title, &pub, &sch, &v.URL, &thumb, &kind, &upcoming, &liveStatus, &liveChecked, &duration, &short); err != nil {
			return nil, dbErr(err)
		}
		v.LiveStatus = liveView(liveStatus, liveChecked, liveCutoff)
		v.DurationSeconds = nullInt(duration)
		v.IsShort = nullBool(short)
		if pub.Valid {
			v.PublishedAt = &pub.String
		}
		if sch.Valid {
			v.ScheduledAt = &sch.String
		}
		if thumb.Valid {
			v.ThumbnailURL = &thumb.String
		}
		if kind.Valid {
			v.Kind = &kind.String
		}
		v.IsUpcoming = upcoming != 0
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr(err)
	}
	return out, nil
}
func (s *Store) getVideo(ctx context.Context, id string, liveCutoff time.Time) (*Video, error) {
	var v Video
	var pub, sch, thumb, kind, liveStatus, liveChecked sql.NullString
	var up int64
	var duration sql.NullInt64
	var short sql.NullBool
	err := s.db.QueryRowContext(ctx, `SELECT id,source,COALESCE(manual_title,title),published_at,scheduled_at,url,thumbnail_url,COALESCE(manual_kind,kind),is_upcoming,live_status,live_checked_at,duration_seconds,COALESCE(manual_is_short,is_short) FROM videos WHERE id=? AND `+publicVideoScope, id).Scan(&v.ID, &v.Source, &v.Title, &pub, &sch, &v.URL, &thumb, &kind, &up, &liveStatus, &liveChecked, &duration, &short)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if pub.Valid {
		v.PublishedAt = &pub.String
	}
	if sch.Valid {
		v.ScheduledAt = &sch.String
	}
	if thumb.Valid {
		v.ThumbnailURL = &thumb.String
	}
	if kind.Valid {
		v.Kind = &kind.String
	}
	v.IsUpcoming = up != 0
	v.LiveStatus = liveView(liveStatus, liveChecked, liveCutoff)
	v.DurationSeconds = nullInt(duration)
	v.IsShort = nullBool(short)
	return &v, nil
}
func (s *Store) queryLinkedEvent(ctx context.Context, v Video, now time.Time) (*DashboardEvent, error) {
	q := `SELECT id,source,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_at,updated_at FROM events e WHERE e.deleted_at IS NULL AND (e.id='youtube-'||? OR (e.source='youtube' AND e.source_item_id=?) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=?) OR (e.url IS NOT NULL AND stream_identity(e.url)=stream_identity(?))) ORDER BY ABS(julianday(e.starts_at)-julianday(?)),e.id ASC LIMIT 1`
	rows, err := s.db.QueryContext(ctx, q, v.ID, v.ID, v.ID, v.URL, stringValue(v.ScheduledAt))
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	e, err := scanEvent(rows, now)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) videoCancelled(ctx context.Context, video Video) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events e WHERE e.cancellation_status='manual_confirmed' AND (e.id='youtube-'||? OR (e.source='youtube' AND e.source_item_id=?) OR EXISTS (SELECT 1 FROM event_sources es WHERE es.event_id=e.id AND es.source='youtube' AND es.source_item_id=?) OR (e.url IS NOT NULL AND stream_identity(e.url)=stream_identity(?)))`, video.ID, video.ID, video.ID, video.URL).Scan(&n)
	if err != nil {
		return false, dbErr(err)
	}
	return n > 0, nil
}

func (s *Store) queryFocus(ctx context.Context) (*Focus, error) {
	var f Focus
	var vid, dl, desc, img, u, su, up sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,video_id,date_label,title,description,image_url,url,source_url,updated_at FROM focus ORDER BY id LIMIT 1`).Scan(&f.ID, &vid, &dl, &f.Title, &desc, &img, &u, &su, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if vid.Valid {
		f.VideoID = &vid.String
	}
	if dl.Valid {
		f.DateLabel = &dl.String
	}
	if desc.Valid {
		f.Description = &desc.String
	}
	if img.Valid {
		f.ImageURL = &img.String
	}
	if u.Valid {
		f.URL = &u.String
	}
	if su.Valid {
		f.SourceURL = &su.String
	}
	if up.Valid {
		f.UpdatedAt = &up.String
	}
	return &f, nil
}
func (s *Store) queryTimeline(ctx context.Context) ([]TimelineItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,year,title,detail,sort_order FROM timeline ORDER BY sort_order ASC,id ASC`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []TimelineItem{}
	for rows.Next() {
		var x TimelineItem
		var d sql.NullString
		if err := rows.Scan(&x.ID, &x.Year, &x.Title, &d, &x.SortOrder); err != nil {
			return nil, dbErr(err)
		}
		if d.Valid {
			x.Detail = &d.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) queryResources(ctx context.Context) ([]Resource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,detail,icon,tone,url,sort_order FROM resources ORDER BY sort_order ASC,id ASC`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		var x Resource
		var d, i, t sql.NullString
		if err := rows.Scan(&x.ID, &x.Title, &d, &i, &t, &x.URL, &x.SortOrder); err != nil {
			return nil, dbErr(err)
		}
		if d.Valid {
			x.Detail = &d.String
		}
		if i.Valid {
			x.Icon = &i.String
		}
		if t.Valid {
			x.Tone = &t.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) queryScheduleAssets(ctx context.Context, from, to string) ([]Asset, error) {
	q := `SELECT a.id,a.kind,a.alt,COALESCE(date(a.manual_period_start,'weekday 0','-6 days'),a.week_start),COALESCE(a.manual_period_start,a.period_start),COALESCE(a.manual_period_end,a.period_end),CASE WHEN a.manual_period_start IS NOT NULL THEN 'manual' ELSE a.period_basis END,a.source_account,a.updated_at,a.url,a.source_url,COALESCE(r.manual_status,'unreviewed'),COALESCE(r.llm_status,'pending'),(SELECT COALESCE(json_extract(p.raw_json,'$.search_text'),p.text) FROM posts p WHERE p.url=a.source_url ORDER BY p.published_at DESC LIMIT 1) FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id=a.id WHERE a.kind='schedule' AND COALESCE(a.manual_period_start,a.period_start)<=? AND COALESCE(a.manual_period_end,a.period_end)>=? AND (r.manual_status='schedule' OR (COALESCE(r.manual_status,'unreviewed')='unreviewed' AND r.llm_status='schedule')) ORDER BY a.updated_at DESC,a.id ASC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, q, to, from)
	if err != nil {
		return nil, dbErr(err)
	}
	assets := []Asset{}
	for rows.Next() {
		a, manualStatus, llmStatus, sourceText, err := scanScheduleAsset(rows)
		if err != nil {
			return nil, err
		}
		if manualStatus.String == "unreviewed" && llmStatus.String == "schedule" && sourceText.Valid && !scheduleBoardText.MatchString(sourceText.String) {
			continue
		}
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err)
	}
	_ = rows.Close()
	out := []Asset{}
	for _, a := range assets {
		ref, e := s.resolveMedia(ctx, a.URL)
		if e != nil {
			return nil, e
		}
		a.MediaID, a.MediaStatus, a.MediaSourceURL, a.URL = ref.ID, ref.Status, ref.SourceURL, ref.PublicURL
		if a.SourceURL == nil {
			a.SourceURL = ref.SourceURL
		}
		out = append(out, a)
	}
	return out, nil
}

var scheduleBoardText = regexp.MustCompile(`(?i)(\b(?:weekly[\s_-]+)?schedule(?:[\s_-]+(?:board|table))?\b|\bthis\s+week(?:'s)?\s+schedule\b|スケジュール|予定表|(?:今週|来週|今月|週間).{0,4}(?:予定|スケジュール))`)

func (s *Store) queryOtherAssets(ctx context.Context) ([]Asset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,alt,NULL,NULL,NULL,NULL,source_account,updated_at,url,source_url FROM assets WHERE kind!='schedule' ORDER BY id LIMIT 100`)
	if err != nil {
		return nil, dbErr(err)
	}
	assets := []Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, dbErr(err)
	}
	_ = rows.Close()
	out := []Asset{}
	for _, a := range assets {
		ref, e := s.resolveMedia(ctx, a.URL)
		if e != nil {
			return nil, e
		}
		a.MediaID, a.MediaStatus, a.MediaSourceURL, a.URL = ref.ID, ref.Status, ref.SourceURL, ref.PublicURL
		out = append(out, a)
	}
	return out, nil
}
func scanAsset(row scanner) (Asset, error) {
	var a Asset
	var alt, ws, ps, pe, pb, sa, ua, u, su sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &alt, &ws, &ps, &pe, &pb, &sa, &ua, &u, &su); err != nil {
		return a, dbErr(err)
	}
	if alt.Valid {
		a.Alt = &alt.String
	}
	if ws.Valid {
		a.WeekStart = &ws.String
	}
	if ps.Valid {
		a.PeriodStart = &ps.String
	}
	if pe.Valid {
		a.PeriodEnd = &pe.String
	}
	if pb.Valid {
		a.PeriodBasis = &pb.String
	}
	if sa.Valid {
		a.SourceAccount = &sa.String
	}
	if ua.Valid {
		a.UpdatedAt = &ua.String
	}
	if u.Valid {
		a.URL = &u.String
	}
	if su.Valid {
		a.SourceURL = &su.String
	}
	return a, nil
}

func scanScheduleAsset(row scanner) (Asset, sql.NullString, sql.NullString, sql.NullString, error) {
	var a Asset
	var alt, ws, ps, pe, pb, sa, ua, u, su, manual, llm, sourceText sql.NullString
	if err := row.Scan(&a.ID, &a.Kind, &alt, &ws, &ps, &pe, &pb, &sa, &ua, &u, &su, &manual, &llm, &sourceText); err != nil {
		return a, manual, llm, sourceText, dbErr(err)
	}
	if alt.Valid {
		a.Alt = &alt.String
	}
	if ws.Valid {
		a.WeekStart = &ws.String
	}
	if ps.Valid {
		a.PeriodStart = &ps.String
	}
	if pe.Valid {
		a.PeriodEnd = &pe.String
	}
	if pb.Valid {
		a.PeriodBasis = &pb.String
	}
	if sa.Valid {
		a.SourceAccount = &sa.String
	}
	if ua.Valid {
		a.UpdatedAt = &ua.String
	}
	if u.Valid {
		a.URL = &u.String
	}
	if su.Valid {
		a.SourceURL = &su.String
	}
	return a, manual, llm, sourceText, nil
}
func filterReadyAssets(in []Asset) []Asset {
	out := []Asset{}
	for _, a := range in {
		if a.URL != nil {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) queryProfile(ctx context.Context) (*Profile, error) {
	var p Profile
	var rn, bio, av, bn, x, y, up sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,display_name,romanized_name,bio,avatar_url,banner_url,x_url,youtube_url,updated_at FROM profiles ORDER BY id LIMIT 1`).Scan(&p.ID, &p.DisplayName, &rn, &bio, &av, &bn, &x, &y, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if rn.Valid {
		p.RomanizedName = &rn.String
	}
	if bio.Valid {
		p.Bio = &bio.String
	}
	if av.Valid {
		p.AvatarURL = &av.String
	}
	if bn.Valid {
		p.BannerURL = &bn.String
	}
	if x.Valid {
		p.XURL = &x.String
	}
	if y.Valid {
		p.YouTubeURL = &y.String
	}
	if up.Valid {
		p.UpdatedAt = &up.String
	}
	return &p, nil
}
func (s *Store) queryProfileMedia(ctx context.Context) (ProfileMediaSlots, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,slot,source,source_url,active_cache_path,active_sha256,active_mime_type,status,is_active FROM profile_media WHERE is_active=1 AND slot IN ('avatar','banner') ORDER BY slot,id`)
	if err != nil {
		return ProfileMediaSlots{}, dbErr(err)
	}
	defer rows.Close()
	var out ProfileMediaSlots
	for rows.Next() {
		var id, slot, source string
		var su, cp, sha, mime, status sql.NullString
		var active int64
		if err := rows.Scan(&id, &slot, &source, &su, &cp, &sha, &mime, &status, &active); err != nil {
			return out, dbErr(err)
		}
		pm := s.profileMedia(id, slot, source, su, cp, sha, mime, status.String, active != 0)
		switch slot {
		case "avatar":
			out.Avatar = pm
		case "banner":
			out.Banner = pm
		}
	}
	return out, rows.Err()
}
func (s *Store) profileMedia(id, slot, source string, su, cp, sha, mime sql.NullString, status string, active bool) *ProfileMedia {
	var sourceURL *string
	if su.Valid {
		sourceURL = &su.String
	}
	pm := &ProfileMedia{ID: id, Slot: slot, Source: source, SourceURL: sourceURL, Status: status, IsActive: active}
	if active && cp.Valid && safeHash(sha.String) && s.cacheFileExists(cp.String) && sha.Valid {
		v := "/media/profile/" + slot + "?v=" + sha.String
		pm.PublicURL = &v
		pm.Status = "ready"
	}
	return pm
}

func (s *Store) getFeaturedVideoID(ctx context.Context) (*string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key='featured_video_id'`).Scan(&v)
	// An absent setting falls back to the focus row's video. An explicit
	// empty (or stale) setting is an operator decision and must not fall back.
	if errors.Is(err, sql.ErrNoRows) {
		var legacy sql.NullString
		err = s.db.QueryRowContext(ctx, `SELECT video_id FROM focus ORDER BY id LIMIT 1`).Scan(&legacy)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		v = legacy.String
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if v == "" {
		return nil, nil
	}
	exists, err := s.getVideo(ctx, v, time.Time{})
	if err != nil {
		return nil, err
	}
	if exists == nil {
		return nil, nil
	}
	return &v, nil
}
func (s *Store) getFetchedAt(ctx context.Context) (*string, error) {
	var v sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT finished_at FROM sync_runs WHERE status IN ('success','partial') AND finished_at IS NOT NULL ORDER BY finished_at DESC,id DESC LIMIT 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || !v.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return &v.String, nil
}
func (s *Store) getMediaCache(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM media_assets GROUP BY status`)
	if err != nil {
		return nil, dbErr(err)
	}
	defer rows.Close()
	out := map[string]int64{"total": 0}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, dbErr(err)
		}
		out[status] = n
		out["total"] += n
	}
	return out, rows.Err()
}
func (s *Store) getXAccounts(ctx context.Context) ([]string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key='x_accounts'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{"kano_2525", "_Kanotic"}, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return []string{"kano_2525", "_Kanotic"}, nil
	}
	v = strings.Trim(v, "[]\"")
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '"' })
	out := []string{}
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimPrefix(strings.TrimSpace(p), "@")
		if p != "" && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"kano_2525", "_Kanotic"}, nil
	}
	return out, nil
}

func (s *Store) resolveMedia(ctx context.Context, source *string) (*mediaReference, error) {
	if source == nil || strings.TrimSpace(*source) == "" {
		return &mediaReference{Status: "missing"}, nil
	}
	v := strings.TrimSpace(*source)
	if strings.HasPrefix(v, "/assets/") {
		if s.publicAssetExists(v) {
			return &mediaReference{Status: "ready", PublicURL: strPtr(v)}, nil
		}
		return &mediaReference{Status: "missing"}, nil
	}
	norm := normalizeSourceURL(v)
	if norm == "" {
		return &mediaReference{Status: "missing"}, nil
	}
	var a mediaAsset
	var cp, mime, sha sql.NullString
	var bs, w, h sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,source_url,cache_path,mime_type,byte_size,sha256,NULLIF(width,0),NULLIF(height,0),status FROM media_assets WHERE source_url=?`, norm).Scan(&a.ID, &a.SourceURL, &cp, &mime, &bs, &sha, &w, &h, &a.Status)
	if errors.Is(err, sql.ErrNoRows) {
		id := sha256Hex(norm)
		return &mediaReference{ID: &id, Status: "pending", SourceURL: &norm}, nil
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if cp.Valid {
		a.CachePath = &cp.String
	}
	if mime.Valid {
		a.MIMEType = &mime.String
	}
	if bs.Valid {
		a.ByteSize = &bs.Int64
	}
	if sha.Valid {
		a.SHA256 = &sha.String
	}
	if w.Valid {
		a.Width = &w.Int64
	}
	if h.Valid {
		a.Height = &h.Int64
	}
	return s.mediaReference(a), nil
}
func (s *Store) mediaReference(a mediaAsset) *mediaReference {
	if a.ID == "" {
		return nil
	}
	r := &mediaReference{Status: a.Status, SourceURL: strPtr(a.SourceURL), MIMEType: a.MIMEType, ByteSize: a.ByteSize, Width: a.Width, Height: a.Height}
	if a.Status == "ready" && a.CachePath != nil && a.SHA256 != nil && safeHash(*a.SHA256) && s.safeDataPath(*a.CachePath) && s.cacheFileExists(*a.CachePath) {
		r.Status = "ready"
		r.ID = &a.ID
		v := "/media/" + a.ID + "?v=" + *a.SHA256
		r.PublicURL = &v
	} else if a.Status == "ready" {
		r.Status = "missing"
	}
	r.ID = &a.ID
	return r
}

func (s *Store) cacheFileExists(relative string) bool {
	if !s.safeDataPath(relative) {
		return false
	}
	info, err := os.Stat(filepath.Join(s.dataRoot, filepath.FromSlash(relative)))
	return err == nil && info.Mode().IsRegular()
}
func (s *Store) safeDataPath(relative string) bool {
	if filepath.IsAbs(relative) {
		return false
	}
	base, err := filepath.EvalSymlinks(s.dataRoot)
	if err != nil {
		return false
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(s.dataRoot, filepath.FromSlash(relative)))
	if err != nil {
		return false
	}
	base, _ = filepath.Abs(base)
	candidate, _ = filepath.Abs(candidate)
	return candidate == base || strings.HasPrefix(candidate, base+string(os.PathSeparator))
}
func (s *Store) publicAssetExists(v string) bool {
	rel, err := url.PathUnescape(strings.TrimPrefix(v, "/assets/"))
	if err != nil {
		return false
	}
	if rel == "" || strings.Contains(rel, "..") || strings.ContainsRune(rel, 0) {
		return false
	}
	root := filepath.Join(filepath.Dir(s.dataRoot), "public", "assets")
	candidate := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(candidate)
	return err == nil && info.Mode().IsRegular()
}

func dbErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: query failed", ErrUnavailable)
}
func strPtr(v string) *string { return &v }
func firstString(values ...*string) *string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func safeHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	if strings.ToLower(v) != v {
		return false
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func sha256Hex(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func normalizeSourceURL(v string) string {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}
func xHandleFromURL(v string) string {
	u, err := url.Parse(v)
	if err != nil || u.Host != "x.com" && u.Host != "www.x.com" && u.Host != "twitter.com" && u.Host != "www.twitter.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	h := parts[0]
	switch strings.ToLower(h) {
	case "i", "intent", "share", "search", "hashtag":
		return ""
	}
	if len(h) < 1 || len(h) > 30 {
		return ""
	}
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return ""
		}
	}
	return h
}
func dateInJapan(t time.Time) string {
	return t.In(time.FixedZone("JST", 9*60*60)).Format("2006-01-02")
}
func mondayWindow(day string) (string, string) {
	t, _ := time.Parse("2006-01-02", day)
	delta := (int(t.Weekday()) + 6) % 7
	t = t.AddDate(0, 0, -delta)
	return t.Format("2006-01-02"), t.AddDate(0, 0, 6).Format("2006-01-02")
}
func eventUpcoming(e DashboardEvent, now time.Time) bool {
	if e.StartsAt != nil {
		t, err := time.Parse(time.RFC3339, *e.StartsAt)
		return err == nil && !t.Before(now)
	}
	return e.StartsOn >= dateInJapan(now)
}
func eventSortKey(e DashboardEvent) int64 {
	if e.StartsAt != nil {
		if t, err := time.Parse(time.RFC3339, *e.StartsAt); err == nil {
			return t.UnixNano()
		}
	}
	t, err := time.Parse("2006-01-02T15:04:05.999-07:00", e.StartsOn+"T23:59:59.999+09:00")
	if err != nil {
		return 0
	}
	return t.UnixNano()
}
func timeKey(v *string) int64 {
	if v == nil {
		return 1 << 62
	}
	t, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return 1 << 62
	}
	return t.UnixNano()
}
func videoUpcoming(v Video, now time.Time) bool {
	if v.ScheduledAt != nil {
		t, err := time.Parse(time.RFC3339, *v.ScheduledAt)
		return err == nil && !t.Before(now)
	}
	return v.IsUpcoming
}
func containsEvent(es []DashboardEvent, id string) bool {
	for _, e := range es {
		if e.ID == id {
			return true
		}
	}
	return false
}
func containsVideo(vs []Video, id string) bool {
	for _, v := range vs {
		if v.ID == id {
			return true
		}
	}
	return false
}
func cancelledStatus(v *string) bool {
	if v == nil {
		return false
	}
	x := strings.ToLower(strings.TrimSpace(*v))
	return x == "cancelled" || x == "canceled" || x == "cancel" || x == "取消" || x == "已取消" || x == "中止" || x == "キャンセル" || strings.HasPrefix(x, "cancel")
}

// Cancelled reports a definitive cancellation: a cancelled status or an
// operator's confirmation. A model's suspicion alone is not one.
func (e DashboardEvent) Cancelled() bool {
	return cancelledStatus(e.Status) || e.CancellationStatus == "manual_confirmed"
}
