package youtube

import (
	"context"
	"strings"
	"testing"
)

func TestYTDLPListUploadsParsesFlatPlaylist(t *testing.T) {
	y := NewYTDLP("synthetic-yt-dlp")
	var args []string
	y.runFunc = func(_ context.Context, got ...string) ([]byte, []byte, error) {
		args = got
		return []byte(`{"entries":[{"id":"AAAAAAAAAAA","title":"new song","upload_date":"20261030","thumbnail":"https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg","webpage_url":"https://www.youtube.com/watch?v=AAAAAAAAAAA"},{"id":"BBBBBBBBBBB","title":"scheduled","release_timestamp":1793350800,"live_status":"is_upcoming"}]}`), nil, nil
	}
	videos, err := y.ListUploads(context.Background(), "UCAAAAAAAAAAAAAAAAAAAAAA", 2)
	if err != nil || len(videos) != 2 {
		t.Fatalf("videos=%+v err=%v", videos, err)
	}
	if videos[0].ID != "AAAAAAAAAAA" || videos[0].PublishedAt != "2026-10-30T00:00:00Z" || videos[0].ThumbnailURL == "" {
		t.Fatalf("first=%+v", videos[0])
	}
	if !videos[1].IsUpcoming || videos[1].ScheduledAt == "" || videos[1].LiveStatus != LiveStatusUpcoming {
		t.Fatalf("second=%+v", videos[1])
	}
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--flat-playlist", "--dump-single-json", "--skip-download", "--playlist-end", "2"} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("args=%v missing %q", args, flag)
		}
	}
}

func TestYTDLPInspectKeepsRawObjectPrivateToCaller(t *testing.T) {
	y := NewYTDLP("synthetic-yt-dlp")
	y.runFunc = func(_ context.Context, _ ...string) ([]byte, []byte, error) {
		return []byte(`{"id":"CCCCCCCCCCC","title":"full info","timestamp":1793361600,"duration":61,"description":"private source text","webpage_url":"https://www.youtube.com/watch?v=CCCCCCCCCCC"}`), nil, nil
	}
	video, raw, err := y.Inspect(context.Background(), "https://www.youtube.com/watch?v=CCCCCCCCCCC")
	if err != nil || video.ID != "CCCCCCCCCCC" || raw["description"] != "private source text" {
		t.Fatalf("video=%+v raw=%v err=%v", video, raw, err)
	}
	if video.DurationSeconds != 61 || video.Title != "full info" {
		t.Fatalf("video=%+v", video)
	}
}
