package youtube

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"
)

func TestLocalizedTitlesMapLanguageKeysToLocales(t *testing.T) {
	for key, want := range map[string]string{
		"en": "en", "en-US": "en", "EN-gb": "en", "zh": "zh-CN", "zh-CN": "zh-CN", "zh-Hans": "zh-CN", "zh-Hans-CN": "zh-CN",
		"zh-TW": "", "zh-Hant": "", "zh-HK": "", "ja": "", "eng": "", "": "",
	} {
		if got := titleLocale(key); got != want {
			t.Errorf("titleLocale(%q)=%q want %q", key, got, want)
		}
	}
	items, err := ParseAPIVideos([]byte(apiItems(
		// The key naming the locale wins; otherwise the first in sort order.
		`{"id":"AAAAAAAAAAA","snippet":{"title":"【歌枠】秋の夜"},"localizations":{"en-US":{"title":" US title "},"en":{"title":"Plain title"},"zh-Hant":{"title":"繁體"},"zh-Hans":{"title":"【歌回】秋夜"},"ja":{"title":"【歌枠】秋の夜"}}}`,
		`{"id":"BBBBBBBBBBB","snippet":{"title":"Same title"},"localizations":{"en-US":{"title":"US title"},"en-GB":{"title":"GB title"},"zh":{"title":"Same title"},"zh-CN":{"title":" "}}}`,
		`{"id":"CCCCCCCCCCC","snippet":{"title":"no localizations"}}`,
	)))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]map[string]string{
		"AAAAAAAAAAA": {"en": "Plain title", "zh-CN": "【歌回】秋夜"},
		// A title equal to the original, or blank, is no translation.
		"BBBBBBBBBBB": {"en": "GB title"},
		"CCCCCCCCCCC": {},
	} {
		got := localizedTitles(items[id])
		if got == nil || !maps.Equal(got, want) {
			t.Errorf("%s: %v want %v", id, got, want)
		}
	}
}

func TestRecentReadsLocalizedTitlesOfTheNewestUploadsInTheDetailCall(t *testing.T) {
	var apiIDs, watched []string
	parts := ""
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			return response(200, apiItems(playlistItem("NNNNNNNNNNN", "2026-01-02T06:00:00Z"), playlistItem("SSSSSSSSSSS", "2026-01-02T05:00:00Z"), playlistItem("TTTTTTTTTTT", "2026-01-02T04:00:00Z"))), nil
		case "/youtube/v3/videos":
			apiIDs = append(apiIDs, r.URL.Query().Get("id"))
			parts = r.URL.Query().Get("part")
			return response(200, apiItems(
				`{"id":"NNNNNNNNNNN","snippet":{"title":"upload NNNNNNNNNNN","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT3M"},"localizations":{"en":{"title":"New upload"}}}`,
				`{"id":"SSSSSSSSSSS","snippet":{"title":"upload SSSSSSSSSSS","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT3M"},"localizations":{"en":{"title":"Stored upload"},"zh-Hans":{"title":"已存视频"}}}`,
				`{"id":"TTTTTTTTTTT","snippet":{"title":"upload TTTTTTTTTTT","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT3M"}}`,
			)), nil
		case "/feeds/videos.xml":
			return response(200, syntheticFeed([2]string{"NNNNNNNNNNN", ""})), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/watch":
			watched = append(watched, r.URL.Query().Get("v"))
			return response(200, watchPage(r.URL.Query().Get("v"), `"isLiveContent":false,"lengthSeconds":"60"`, "")), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	// Two checked, ordinary uploads are stored: no detail work is due for them.
	state := State{
		Videos: map[string]Video{
			"SSSSSSSSSSS": {ID: "SSSSSSSSSSS", Title: "upload SSSSSSSSSSS", PublishedAt: "2026-01-02T05:00:00Z", Kind: "VIDEO"},
			"TTTTTTTTTTT": {ID: "TTTTTTTTTTT", Title: "upload TTTTTTTTTTT", PublishedAt: "2026-01-02T04:00:00Z", Kind: "VIDEO"},
		},
		CheckedIDs: []string{"SSSSSSSSSSS", "TTTTTTTTTTT"},
	}
	result, err := a.FetchUploads(context.Background(), state, List{Limit: 50})
	if err != nil || len(result.Errors) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	// One call: the queued new upload first, then the listed stored ones.
	if strings.Join(apiIDs, ";") != "NNNNNNNNNNN,SSSSSSSSSSS,TTTTTTTTTTT" || !strings.Contains(parts, "localizations") || result.APIRequests != 2 || len(watched) != 0 {
		t.Fatalf("api=%v parts=%s requests=%d watched=%v", apiIDs, parts, result.APIRequests, watched)
	}
	// The stored uploads' answers refresh only their localized titles.
	if result.Inspected != 1 || strings.Join(result.InspectedIDs, ",") != "NNNNNNNNNNN" {
		t.Fatalf("inspected=%v", result.InspectedIDs)
	}
	want := map[string]map[string]string{
		"NNNNNNNNNNN": {"en": "New upload"},
		"SSSSSSSSSSS": {"en": "Stored upload", "zh-CN": "已存视频"},
		"TTTTTTTTTTT": {},
	}
	if !maps.EqualFunc(result.Localized, want, maps.Equal) {
		t.Fatalf("localized=%v", result.Localized)
	}
}

func TestLocalizedTitlesNeedTheDataAPI(t *testing.T) {
	c := feedClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, syntheticFeed([2]string{"NNNNNNNNNNN", ""})), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/watch":
			if r.URL.Query().Has("hl") {
				t.Fatalf("no localized page is read: %s", r.URL)
			}
			return response(200, watchPage(r.URL.Query().Get("v"), `"isLiveContent":false,"lengthSeconds":"60"`, "")), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5})
	a.Now = syntheticNow
	result, err := a.FetchUploads(context.Background(), State{}, List{Limit: 50})
	if err != nil || result.Inspected != 1 || len(result.Localized) != 0 || result.Localized == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
