package x

import (
	"context"
	"strings"
	"testing"
	"time"
)

// An operator's refresh of a stored status takes exactly one detail request,
// even though the timeline page carries the status and a failure backoff
// still holds it, and the detail answer's quote replaces the stored one.
func TestRefreshForcesOneDetailRequest(t *testing.T) {
	at := func(h int) time.Time { return timelineNow.Add(-time.Duration(h) * time.Hour).Truncate(time.Second) }
	stale := &statusFixture{id: statusAt(at(50)), text: "old quoted wording", at: at(50), author: "other"}
	fresh := &statusFixture{id: stale.id, text: "new quoted wording", at: at(50), author: "other", media: []mediaFixture{{kind: "photo", file: "https://pbs.twimg.com/media/fresh.jpg"}}}
	listed := statusFixture{id: statusAt(at(2)), text: "look", at: at(2), quote: stale}
	known := statusFixture{id: statusAt(at(30)), text: "stored", at: at(30)}
	s := &timelineServer{bodies: map[string]string{"": fxPage(t, "", listed.timeline(), known.timeline())}}
	detailed := listed
	detailed.quote = fresh
	s.detail = detailFor(t, map[string]statusFixture{listed.id: detailed})
	opts := Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 5, CursorID: known.id,
		KnownIDs: map[string]bool{known.id: true, listed.id: true}, RefreshIDs: []string{listed.id, "not-a-status"},
		PendingIDs:     []string{listed.id},
		PendingRetries: map[string]PendingRetry{listed.id: {Failures: 2, RetryAt: timelineNow.Add(time.Hour).Format(time.RFC3339Nano)}}}
	got, err := s.adapter(opts).FetchRecent(context.Background(), "kano", opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.details, ",") != listed.id || got.Held != 0 || strings.Join(got.Refreshed, ",") != listed.id || got.FromTimeline[listed.id] {
		t.Fatalf("details=%v held=%d refreshed=%v fromTimeline=%v", s.details, got.Held, got.Refreshed, got.FromTimeline)
	}
	var refreshed *Post
	for i := range got.Posts {
		if got.Posts[i].ID == listed.id {
			refreshed = &got.Posts[i]
		}
	}
	if refreshed == nil || refreshed.Quote == nil || refreshed.Quote.Text != "new quoted wording" || strings.Join(refreshed.Quote.MediaURLs, ",") != "https://pbs.twimg.com/media/fresh.jpg" {
		t.Fatalf("refreshed=%+v", refreshed)
	}

	// Without a detail budget the refresh waits and is not reported done.
	s.details = nil
	opts.MaxDetailRequests = 0
	got, err = s.adapter(opts).FetchRecent(context.Background(), "kano", opts)
	if err != nil || len(s.details) != 0 || len(got.Refreshed) != 0 {
		t.Fatalf("details=%v refreshed=%v err=%v", s.details, got.Refreshed, err)
	}
}
