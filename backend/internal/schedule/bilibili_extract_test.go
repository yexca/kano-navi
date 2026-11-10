package schedule

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// bilibiliAnnouncement is the acceptance example: a Chinese dynamic posted at
// 2026-11-07 12:04 Beijing time announcing an upload at 17:00 and a talk
// stream from 20:00, both Beijing time.
const bilibiliAnnouncement = "大家好！虽然花了很长时间，但是终于可以在今天北京时间17点上传很久以前就说过的200万粉B站限定歌曲了。我还打算从20点开始播杂谈，欢迎大家来玩"

func (f *fixture) addBilibiliPost(id, text, publishedAt string) string {
	f.t.Helper()
	url := "https://www.bilibili.com/opus/" + id
	f.exec(`INSERT INTO posts (id, source, account_handle, type, label, text, published_at, url)
		VALUES (?, 'bilibili', '316381099', 'daily', 'DAILY / 近况', ?, ?, ?)`, "bilibili-"+id, text, publishedAt, url)
	return url
}

func bilibiliEntry(title, date, clock, eventType string, url any, zone any) *object {
	return obj("title", title, "detail", nil, "date", date, "time", clock, "endTime", nil, "endDate", nil,
		"timePrecision", "exact", "status", "scheduled", "eventType", eventType, "tag", nil, "url", url,
		"platform", "unknown", "timeZone", zone, "confidence", 0.9, "evidence", "synthetic evidence")
}

func TestBilibiliHeuristicReadsChineseWording(t *testing.T) {
	for text, want := range map[string]bool{
		bilibiliAnnouncement: true,
		"明天开播":               true,
		"晚上8点见":              true,
		"谢谢大家的礼物":            false,
		"[鹿乃_比心]":            false,
	} {
		if got := bilibiliMessagePattern.MatchString(text) || scheduleMessagePattern.MatchString(text); got != want {
			t.Errorf("%q => %v, want %v", text, got, want)
		}
	}
	// The Japanese heuristic alone misses the example, so X posts keep it.
	if scheduleMessagePattern.MatchString(bilibiliAnnouncement) {
		t.Fatal("the X heuristic matched the Chinese example")
	}
}

func TestBilibiliExtractionConvertsBeijingTimeAndRecordsThePlatform(t *testing.T) {
	f := newFixture(t)
	f.addProvider("p-main", protocolChat, "https://provider.example.test/v1", "model-a", `["text"]`)
	f.queue("p-main")
	opus := f.addBilibiliPost("900000000000000010", bilibiliAnnouncement, "2026-11-07T04:04:48Z")
	// A Chinese dynamic the heuristic leaves out is not sent.
	f.addBilibiliPost("900000000000000011", "谢谢大家", "2026-11-07T05:00:00Z")
	var body string
	f.provider.respond = func(request recordedRequest) (int, string) {
		body = request.body
		// The model gives the times as written; the stream carries no URL.
		return http.StatusOK, chatReply(stringify(obj(
			"classification", "schedule",
			"events", []any{
				bilibiliEntry("200万粉B站限定歌曲", "2026-11-07", "17:00", "release", nil, "Asia/Shanghai"),
				bilibiliEntry("杂谈", "2026-11-07", "20:00", "stream", nil, nil),
			},
			"confidence", 0.9, "evidence", "synthetic evidence", "reason", nil, "action", "add")))
	}
	f.scan()
	if f.provider.count() != 1 {
		t.Fatalf("requests=%d", f.provider.count())
	}
	for _, want := range []string{"Chinese Bilibili dynamic", "2026-11-07T04:04:48Z (2026-11-07 Sat 12:04 Asia/Shanghai)", `"timeZone"`, `"platforms"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %q", want)
		}
	}
	rows, err := f.db.Query(`SELECT e.id, e.source, e.event_type, e.starts_on, e.starts_at, e.url, COALESCE(e.platform, ''), s.source, s.url FROM events e
		JOIN event_sources s ON s.event_id = e.id WHERE e.deleted_at IS NULL ORDER BY e.starts_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct{ id, source, kind, on, at, url, platform, evidenceSource, evidenceURL string }
	got := []row{}
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.source, &r.kind, &r.on, &r.at, &r.url, &r.platform, &r.evidenceSource, &r.evidenceURL); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("events=%#v", got)
	}
	release, stream := got[0], got[1]
	at := func(value string) string {
		parsed, _ := time.Parse(time.RFC3339Nano, value)
		return parsed.In(jst).Format("2006-01-02 15:04")
	}
	if !strings.HasPrefix(release.id, "bilibili-schedule-20261107-") || release.source != "bilibili" || release.kind != "release" ||
		at(release.at) != "2026-11-07 18:00" || release.url != opus || release.platform != "bilibili" || release.evidenceSource != "bilibili" || release.evidenceURL != opus {
		t.Fatalf("release=%#v", release)
	}
	// The stream keeps the dynamic's link and is on Bilibili; reads add the
	// live room.
	if stream.kind != "stream" || at(stream.at) != "2026-11-07 21:00" || stream.url != opus || stream.platform != "bilibili" || stream.evidenceURL != opus {
		t.Fatalf("stream=%#v", stream)
	}
	if got := f.text(`SELECT source FROM schedule_extractions WHERE source_item_id = 'bilibili-900000000000000010'`); got != "bilibili" {
		t.Fatalf("extraction source=%q", got)
	}
}

func TestBilibiliTimesRollOverToTheJapanDate(t *testing.T) {
	published, _ := time.Parse(time.RFC3339, "2026-11-07T04:04:48Z")
	result := obj("classification", "schedule", "events", []any{
		bilibiliEntry("深夜杂谈", "2026-11-07", "23:30", "stream", "https://www.youtube.com/watch?v=AAAAAAAAAAA", nil),
		bilibiliEntry("日本时间", "2026-11-08", "20:00", "event", nil, "Asia/Tokyo"),
	}, "confidence", 0.9, "evidence", "e", "reason", nil, "action", "add")
	normalized, err := bilibiliProfile.normalizeResult(result, "bilibili-1", "https://www.bilibili.com/opus/1", "", published, published)
	if err != nil || len(normalized.events) != 2 {
		t.Fatalf("normalized=%#v err=%v", normalized, err)
	}
	late, japan := normalized.events[0], normalized.events[1]
	// Beijing 23:30 is 00:30 the next day in Japan; a YouTube link makes the
	// stream a YouTube one.
	if late.startsOn != "2026-11-08" || *late.startsAt != "2026-11-07T15:30:00.000Z" || late.platform != "youtube" {
		t.Fatalf("late=%#v", late)
	}
	if japan.startsOn != "2026-11-08" || *japan.startsAt != "2026-11-08T11:00:00.000Z" || japan.url != "https://www.bilibili.com/opus/1" {
		t.Fatalf("japan=%#v", japan)
	}
	// Aliases name the two zones; anything else falls back to Beijing time
	// rather than failing the post. X entries ignore a zone.
	zoned := obj("classification", "schedule", "events", []any{
		bilibiliEntry("a", "2026-11-07", "20:00", "stream", nil, "UTC+9"),
		bilibiliEntry("b", "2026-11-07", "20:00", "stream", nil, "北京时间"),
		bilibiliEntry("c", "2026-11-07", "20:00", "stream", nil, "Europe/Paris"),
	}, "confidence", 0.9, "evidence", "e", "reason", nil, "action", "add")
	got, err := bilibiliProfile.normalizeResult(zoned, "bilibili-1", "https://www.bilibili.com/opus/1", "", published, published)
	if err != nil || *got.events[0].startsAt != "2026-11-07T11:00:00.000Z" || *got.events[1].startsAt != "2026-11-07T12:00:00.000Z" || *got.events[2].startsAt != "2026-11-07T12:00:00.000Z" {
		t.Fatalf("zones=%#v err=%v", got.events, err)
	}
	x, err := xProfile.normalizeResult(zoned, "1", "https://x.com/synthetic_handle/status/1", "", published, published)
	if err != nil || *x.events[1].startsAt != "2026-11-07T11:00:00.000Z" {
		t.Fatalf("x zones=%#v err=%v", x.events, err)
	}
}

func TestEntryPlatformsDefaultAndNeverStoreTheRoomLink(t *testing.T) {
	published, _ := time.Parse(time.RFC3339, "2026-11-07T04:04:48Z")
	entry := func(eventType string, url, platform any) *object {
		e := bilibiliEntry("杂谈", "2026-11-07", "20:00", eventType, url, nil)
		e.set("platform", platform)
		return e
	}
	result := func(entries ...any) *object {
		return obj("classification", "schedule", "events", entries, "confidence", 0.9, "evidence", "e", "reason", nil, "action", "add")
	}
	dynamicEntry := func(eventType string, url any, platform string) *object {
		e := bilibiliEntry("杂谈", "2026-11-07", "20:00", eventType, url, nil)
		e.set("platforms", []any{platform})
		return e
	}
	opus := "https://www.bilibili.com/opus/1"
	got, err := bilibiliProfile.normalizeResult(result(
		dynamicEntry("stream", "https://live.bilibili.com/1000001", "bilibili"),
		dynamicEntry("member", nil, "made-up"),
		dynamicEntry("stream", "https://live.bilibili.com/2000002", "other"),
	), "bilibili-1", opus, "", published, published)
	if err != nil || len(got.events) != 3 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	// A room link is dropped for the dynamic's link; an invalid platform is
	// unknown, which a Bilibili stream reads as bilibili; a collaboration
	// in someone else's room keeps that link.
	if got.events[0].url != opus || got.events[0].platform != "bilibili" || got.events[1].platform != "bilibili" ||
		got.events[2].platform != "other" || got.events[2].url != "https://live.bilibili.com/2000002" {
		t.Fatalf("events=%#v", got.events)
	}
	// An X post that names Bilibili makes its stream and release Bilibili
	// entries; another X stream stays unknown.
	xPost := "https://x.com/synthetic_handle/status/1"
	x, err := xProfile.normalizeResult(result(entry("stream", nil, nil), entry("release", nil, "unknown")), "1", xPost, "今夜20時からbilibili限定配信！", published, published)
	if err != nil || x.events[0].platform != "bilibili" || x.events[1].platform != "bilibili" || x.events[0].url != xPost {
		t.Fatalf("x=%#v err=%v", x.events, err)
	}
	plain, err := xProfile.normalizeResult(result(entry("stream", nil, nil)), "1", xPost, "今夜20時から配信", published, published)
	if err != nil || plain.events[0].platform != "unknown" {
		t.Fatalf("plain=%#v err=%v", plain.events, err)
	}
}

// A Bilibili entry's platforms come from its platforms array only: a
// scalar platform, which the Bilibili schema does not ask for, is not read.
func TestBilibiliEntriesReadOnlyThePlatformsArray(t *testing.T) {
	published, _ := time.Parse(time.RFC3339, "2026-11-07T04:04:48Z")
	scalar := bilibiliEntry("合唱", "2026-11-07", "20:00", "release", nil, nil)
	scalar.set("platform", "youtube")
	listed := bilibiliEntry("合唱", "2026-11-08", "20:00", "release", nil, nil)
	listed.set("platforms", []any{"youtube"})
	got, err := bilibiliProfile.normalizeResult(obj("classification", "schedule", "events", []any{scalar, listed}, "confidence", 0.9, "evidence", "e", "reason", nil, "action", "add"),
		"bilibili-1", "https://www.bilibili.com/opus/1", "", published, published)
	if err != nil || len(got.events) != 2 || len(got.events[0].platforms) != 0 || strings.Join(got.events[1].platforms, ",") != "youtube" {
		t.Fatalf("events=%#v err=%v", got.events, err)
	}
}

func TestRoomLinksAndSimulcastWording(t *testing.T) {
	published, _ := time.Parse(time.RFC3339, "2026-11-07T04:04:48Z")
	entry := func(eventType string, url, platform any) *object {
		e := bilibiliEntry("配信", "2026-11-07", "20:00", eventType, url, nil)
		e.set("platform", platform)
		return e
	}
	result := func(entries ...any) *object {
		return obj("classification", "schedule", "events", entries, "confidence", 0.9, "evidence", "e", "reason", nil, "action", "add")
	}
	xPost := "https://x.com/synthetic_handle/status/1"
	// A room link makes any entry but a collaboration a Bilibili one, and
	// never stays as its link.
	got, err := xProfile.normalizeResult(result(
		entry("stream", "https://live.bilibili.com/1000001", "unknown"),
		entry("stream", "https://live.bilibili.com/1000001", "youtube"),
		entry("stream", "https://live.bilibili.com/2000002", "other"),
	), "1", xPost, "", published, published)
	if err != nil || got.events[0].platform != "bilibili" || got.events[0].url != xPost || got.events[1].platform != "bilibili" || got.events[1].url != xPost ||
		got.events[2].platform != "other" || got.events[2].url != "https://live.bilibili.com/2000002" {
		t.Fatalf("room links=%#v err=%v", got.events, err)
	}
	// A simulcast post names both platforms: none of its entries becomes a
	// Bilibili one; a post that names only bilibili without 限定 neither.
	simulcast, err := xProfile.normalizeResult(result(entry("stream", nil, nil), entry("stream", nil, nil), entry("release", nil, nil), entry("member", nil, nil), entry("stream", nil, nil)),
		"1", xPost, "今夜はYouTubeとbilibili限定の同時配信もあるよ", published, published)
	if err != nil || len(simulcast.events) != 5 {
		t.Fatalf("simulcast err=%v", err)
	}
	for _, e := range simulcast.events {
		if e.platform != "unknown" {
			t.Fatalf("simulcast entry=%#v", e)
		}
	}
	mention, _ := xProfile.normalizeResult(result(entry("stream", nil, nil)), "1", xPost, "bilibiliでも見てね", published, published)
	limited, _ := xProfile.normalizeResult(result(entry("stream", nil, nil)), "1", xPost, "今夜はB限配信", published, published)
	if mention.events[0].platform != "unknown" || limited.events[0].platform != "bilibili" {
		t.Fatalf("mention=%s limited=%s", mention.events[0].platform, limited.events[0].platform)
	}
}

func TestWebOnlyWordingIsNotBilibili(t *testing.T) {
	published, _ := time.Parse(time.RFC3339, "2026-11-07T04:04:48Z")
	xPost := "https://x.com/synthetic_handle/status/1"
	for text, want := range map[string]string{
		"WEB限定グッズ 11/20 21:00〜": "unknown",
		"Web限定":                 "unknown",
		"web限定セット":              "unknown",
		"B限配信 21時〜":             "bilibili",
		"【B限】雑談":                "bilibili",
		"今夜はb限で歌います":            "bilibili",
	} {
		entry := bilibiliEntry("グッズ", "2026-11-20", "21:00", "release", nil, nil)
		got, err := xProfile.normalizeResult(obj("classification", "schedule", "events", []any{entry}, "confidence", 0.9, "evidence", "e", "reason", nil, "action", "add"),
			"1", xPost, text, published, published)
		if err != nil || got.events[0].platform != want {
			t.Fatalf("%q => %#v err=%v, want %s", text, got.events, err, want)
		}
	}
}
