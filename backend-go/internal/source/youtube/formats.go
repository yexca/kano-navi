package youtube

import "context"

// FormatSets holds the whole live and long-form playlists of the channel for
// an operator's whole-playlist fetch, whose old ended broadcasts are far below
// the newest page the recent step reads. Both lists are paged to their end,
// at most Pages pages each, once per run and only when a described ended
// broadcast first needs a verdict; the sets stay in memory for the rest of
// the run.
type FormatSets struct {
	// Pages bounds the pages read of each list.
	Pages   int
	read    bool
	members [2]map[string]bool
}

// NewFormatSets prepares sets that read at most pages pages of each list.
func NewFormatSets(pages int) *FormatSets { return &FormatSets{Pages: pages} }

// listed reads both lists on first use and names the list each video is on,
// as the newest pages do in the recent step: a video seen on both gets no
// verdict. A list cut short names only what it read; absence proves nothing.
func (s *FormatSets) listed(ctx context.Context, a *Adapter, requests *int, errs *[]string) map[string]formatList {
	if !s.read {
		s.read = true
		for i, live := range []bool{true, false} {
			s.members[i] = a.readFormatList(ctx, live, s.Pages, requests, errs)
		}
	}
	out := map[string]formatList{}
	for id := range s.members[0] {
		if !s.members[1][id] {
			out[id] = listedLive
		}
	}
	for id := range s.members[1] {
		if !s.members[0][id] {
			out[id] = listedVideos
		}
	}
	return out
}

// readFormatList pages one format playlist, newest first, within pages
// pages. A list the budget or a failed page cuts short keeps what it read and
// records its code.
func (a *Adapter) readFormatList(ctx context.Context, live bool, pages int, requests *int, errs *[]string) map[string]bool {
	members := map[string]bool{}
	cursor := ""
	for range max(pages, 0) {
		*requests++
		body, err := a.Client.FormatPlaylistPage(ctx, a.Options.APIKey, a.Options.ChannelID, live, cursor)
		var videos []Video
		next := ""
		if err == nil {
			videos, next, err = ParseUploadsPage(body)
		}
		if err != nil {
			if ctx.Err() == nil {
				*errs = append(*errs, safeError(err))
			}
			return members
		}
		for _, v := range videos {
			members[v.ID] = true
		}
		if next == "" {
			return members
		}
		if next == cursor {
			*errs = append(*errs, "youtube_api_repeated_page")
			return members
		}
		cursor = next
	}
	*errs = append(*errs, "youtube_format_list_incomplete")
	return members
}
