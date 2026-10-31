package history

import (
	"context"
	"errors"
	"sort"
	"time"

	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

// Uploads lists the YouTube channel's uploads; the YouTube adapter implements
// it over the Data API's uploads playlist and the public feed, with their
// timeouts, response limits, rejected redirects and controlled codes.
type Uploads interface {
	// UploadsPage reads one uploads-playlist page, newest first, and the
	// token of the next, older page.
	UploadsPage(ctx context.Context, token string) ([]ytsource.Video, string, error)
	// FeedUploads reads the feed's newest uploads.
	FeedUploads(ctx context.Context) ([]ytsource.Video, error)
	// DescribeVideos asks the Data API about listed videos under the recent
	// step's rules; stored holds their stored rows, and formats, when given,
	// the run's whole format playlists.
	DescribeVideos(ctx context.Context, listed []ytsource.Video, stored map[string]ytsource.Video, formats *ytsource.FormatSets) ytsource.Described
}

// YTDLPUploads is implemented by the YouTube adapter when a local yt-dlp
// executable is configured. It is optional so existing test and sidecar
// adapters can continue to implement the API/RSS interface only.
type YTDLPUploads interface {
	YTDLPUploads(ctx context.Context, limit int) ([]ytsource.Video, error)
}

// formatListPages bounds the pages an all fetch reads of each format
// playlist (UULV, UULF) to classify old ended broadcasts: 1,000 entries each.
const formatListPages = 20

// uploadsCode keeps the YouTube adapter's controlled codes.
func uploadsCode(err error) string {
	var source *ytsource.Error
	if errors.As(err, &source) {
		return source.Code
	}
	return "youtube_unavailable"
}

func videoItems(videos []ytsource.Video) map[string]Item {
	out := map[string]Item{}
	for _, video := range videos {
		out[video.ID] = Item{ID: video.ID, Title: video.Title}
	}
	return out
}

// FetchUploads reads the channel's selection through its method: the RSS
// method takes the feed's newest Count entries in one request; the API method
// pages the uploads playlist (50 entries and one quota unit a page) for the
// newest Count uploads or to the playlist's end, within the page budget and
// resuming from its checkpoint. Stored videos keep their row unless the fetch
// refreshes them, and their reservation state always stays with the recent
// synchronization.
func (f *Fetcher) FetchUploads(ctx context.Context, request Request) (Result, error) {
	if request.Source != SourceYoutube || f.Uploads == nil {
		return Result{}, invalid("unsupported source")
	}
	window, err := request.Window.Normalize(SourceYoutube)
	if err != nil {
		return Result{}, err
	}
	result := Result{Window: window, Errors: []string{}, Items: []Item{}}
	if window.Method == MethodYTDLP {
		reader, ok := f.Uploads.(YTDLPUploads)
		if !ok {
			return result, &Error{Code: "youtube_ytdlp_unavailable", Requested: 1}
		}
		limit := window.Count
		if window.Mode == ModeAll {
			limit = MaxCount
		}
		result.Requested++
		videos, fetchErr := reader.YTDLPUploads(ctx, limit)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, &Error{Code: uploadsCode(fetchErr), Requested: result.Requested}
		}
		videos = videos[:min(len(videos), limit)]
		written, writeErr := f.Store.WriteHistoryPage(ctx, Page{Source: SourceYoutube, Account: request.Account, Window: window, Videos: videos, KeepStored: !window.Refresh})
		if writeErr != nil {
			return result, writeErr
		}
		result.Listed = len(videos)
		result.HasMore = window.Mode == ModeAll && len(videos) == limit
		result.add(written, videoItems(videos))
		return result, nil
	}
	if window.Method == MethodRSS {
		result.Requested++
		videos, fetchErr := f.Uploads.FeedUploads(ctx)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, &Error{Code: uploadsCode(fetchErr), Requested: 1}
		}
		newest := append([]ytsource.Video(nil), videos...)
		sort.SliceStable(newest, func(i, j int) bool {
			ti, _ := time.Parse(time.RFC3339Nano, newest[i].PublishedAt)
			tj, _ := time.Parse(time.RFC3339Nano, newest[j].PublishedAt)
			return ti.After(tj)
		})
		newest = newest[:min(len(newest), window.Count)]
		written, writeErr := f.Store.WriteHistoryPage(ctx, Page{Source: SourceYoutube, Account: request.Account, Window: window, Videos: newest, KeepStored: !window.Refresh})
		if writeErr != nil {
			return result, writeErr
		}
		result.Listed = len(newest)
		result.add(written, videoItems(newest))
		return result, nil
	}
	if window.Method != MethodAPI {
		return Result{}, ErrRetired
	}
	key := window.checkpointKey(request.Account)
	checkpoint, err := f.Store.SyncState(ctx, SourceYoutube+"-window", key)
	if err != nil {
		return Result{}, err
	}
	cursor, previous := checkpoint.CursorID, 0
	if cursor != "" {
		if count, ok := checkpoint.Metadata["discovered"].(float64); ok && count > 0 {
			previous = int(count)
		}
	}
	counted := window.Mode == ModeCount
	maxPages := f.pageLimit(request)
	commit := func(page Page) (Written, error) {
		page.Source, page.Account, page.CheckpointKey, page.Window = SourceYoutube, request.Account, key, window
		page.KeepStored = !window.Refresh
		return f.Store.WriteHistoryPage(ctx, page)
	}
	// An all fetch reaches broadcasts far below the newest page of the format
	// playlists, so it reads them whole, once per run, when first needed.
	var formats *ytsource.FormatSets
	if window.Mode == ModeAll {
		formats = ytsource.NewFormatSets(formatListPages)
	}
	seen := map[string]bool{}
	committed, restarted, failed := 0, false, false
	for {
		result.Requested++
		result.APIRequests++
		videos, next, fetchErr := f.Uploads.UploadsPage(ctx, cursor)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			// A page cursor that no longer answers must not pin the fetch:
			// begin again at the newest page.
			if cursor != "" && committed == 0 && !restarted {
				restarted = true
				cursor, previous = "", 0
				if result.Requested < maxPages {
					continue
				}
			}
			if committed == 0 {
				return result, &Error{Code: uploadsCode(fetchErr), Requested: result.Requested}
			}
			result.Errors = append(result.Errors, uploadsCode(fetchErr))
			failed = true
			break
		}
		// A new upload can shift the playlist while it is paged, so an entry
		// may appear on two pages.
		taken := []ytsource.Video{}
		for _, video := range videos {
			if counted && previous+result.Listed >= window.Count {
				break
			}
			if seen[video.ID] {
				continue
			}
			seen[video.ID] = true
			result.Listed++
			taken = append(taken, video)
		}
		following := next
		switch {
		case counted && previous+result.Listed >= window.Count:
			following = ""
		case following != "" && following == cursor:
			result.Errors = append(result.Errors, "youtube_api_repeated_page")
			following = ""
		}
		described, describeErr := f.describe(ctx, taken, window.Refresh, formats)
		if describeErr != nil {
			return result, describeErr
		}
		result.APIRequests += described.APIRequests
		result.Inspected += len(described.Videos)
		result.Errors = append(result.Errors, described.Errors...)
		written, writeErr := commit(Page{Next: following, Discovered: previous + result.Listed, Videos: taken, Described: described.Videos, Localized: described.Localized})
		if writeErr != nil {
			return result, writeErr
		}
		committed++
		result.add(written, videoItems(taken))
		cursor = following
		if cursor == "" || result.Requested >= maxPages {
			break
		}
		if err := f.wait(ctx); err != nil {
			return result, err
		}
	}
	if cursor != "" && !failed {
		result.Errors = append(result.Errors, budgetMessage)
	}
	result.HasMore = cursor != ""
	return result, nil
}

// describe checks the details of a page's listed videos through the Data API:
// the ones not stored yet, and the stored ones too when the fetch refreshes.
// A failed call is recorded in the answer's errors and leaves its videos as
// listed; only a failed store read fails the fetch.
func (f *Fetcher) describe(ctx context.Context, listed []ytsource.Video, refresh bool, formats *ytsource.FormatSets) (ytsource.Described, error) {
	if len(listed) == 0 {
		return ytsource.Described{}, nil
	}
	ids := make([]string, 0, len(listed))
	for _, video := range listed {
		ids = append(ids, video.ID)
	}
	stored, err := f.Store.StoredYoutubeVideos(ctx, ids)
	if err != nil {
		return ytsource.Described{}, err
	}
	asked := []ytsource.Video{}
	for _, video := range listed {
		if _, found := stored[video.ID]; !found || refresh {
			asked = append(asked, video)
		}
	}
	if len(asked) == 0 {
		return ytsource.Described{}, nil
	}
	described := f.Uploads.DescribeVideos(ctx, asked, stored, formats)
	return described, ctx.Err()
}
