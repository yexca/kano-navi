package schedule

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// A twin runs one scenario on two snapshots: fast counts settled posts as
// cache hits without loading them, as every run does; full judges every
// candidate. Each scan compares what the two would do and what they wrote.
type twin struct {
	t          *testing.T
	fast, full *fixture
	clock      time.Time
	// failing names the posts whose model calls fail.
	failing map[string]bool
	// fastCounts are the counts of fast's last scan.
	fastCounts map[string]any
}

const (
	twinImageURL = "https://pbs.twimg.com/media/synthetic-board.png"
	twinQuote    = `{"id":"quoted-1","url":"https://x.com/synthetic_other/status/quoted-1","authorHandle":"synthetic_other","authorName":"Synthetic Other","publishedAt":"2026-04-27T00:00:00.000Z","text":"明日 21時から歌枠"}`
)

var sourceURLPattern = regexp.MustCompile(`Source URL: ([^\\\s"]+)`)

func newTwin(t *testing.T) *twin {
	t.Helper()
	tw := &twin{t: t, clock: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), failing: map[string]bool{}}
	tw.fast, tw.full = newFixture(t), newFixture(t)
	tw.full.run.ignoreSettled = true
	tw.do(func(f *fixture) {
		f.addProvider("p-main", protocolChat, "https://provider.example.test/v1", "model-a", `["text","image"]`)
		f.addProvider("p-alt", protocolChat, "https://alternate.example.test/v1", "model-b", `["text","image"]`)
		f.queue("p-main")
		f.queueVerify("p-main")
		f.addPost("text-1", "", "明日 21時から配信", "2026-04-30T01:00:00.000Z", "")
		f.addPost("text-2", "", "今日 20時から雑談配信", "2026-04-30T02:00:00.000Z", "")
		f.addPost("board-1", "", "今週のスケジュール", "2026-04-30T03:00:00.000Z", stringify(obj("media_urls", []any{twinImageURL})))
		f.addCachedImage(twinImageURL, []byte(pngHeader+"board"))
		f.exec(`INSERT INTO media_links (media_id, owner_type, owner_id, role, position, created_at, updated_at)
			VALUES (?, 'post', 'board-1', 'post-image', 0, ?, ?)`, mediaIDForSourceURL(twinImageURL), testTimestamp, testTimestamp)
		f.addPost("quote-1", "", "告知 これ見て", "2026-04-30T04:00:00.000Z", "")
		f.exec(`UPDATE posts SET quote_json = ? WHERE id = 'quote-1'`, twinQuote)
		f.addBilibiliPost("900000000000000020", "今天北京时间20点开播", "2026-04-30T05:00:00.000Z")
		// Not a candidate unless every post is.
		f.addPost("noise-1", "", "おはよう", "2026-04-30T06:00:00.000Z", "")
		f.provider.respond = tw.respond
	})
	return tw
}

// settledTwin is a twin whose candidates all reached a settled state: a
// first scan extracts them, a second records the cache hits and their
// stamps, and a third judges none of them.
func settledTwin(t *testing.T) *twin {
	t.Helper()
	tw := newTwin(t)
	tw.scan("first")
	tw.scan("second")
	tw.scan("third")
	tw.expect("third", 0, 5)
	return tw
}

func (tw *twin) respond(request recordedRequest) (int, string) {
	match := sourceURLPattern.FindStringSubmatch(request.body)
	if match == nil {
		return http.StatusBadRequest, `{}`
	}
	url := match[1]
	for id := range tw.failing {
		if strings.HasSuffix(url, "/"+id) {
			return http.StatusServiceUnavailable, `{}`
		}
	}
	switch {
	case strings.Contains(url, "bilibili.com"):
		return http.StatusOK, chatReply(stringify(obj("classification", "uncertain", "events", []any{},
			"confidence", 0.4, "evidence", "synthetic evidence", "reason", "synthetic reason", "action", "add")))
	case strings.HasSuffix(url, "/noise-1"):
		return http.StatusOK, chatReply(stringify(obj("classification", "not_schedule", "events", []any{},
			"confidence", 0.9, "evidence", "synthetic evidence", "reason", nil, "action", "add")))
	}
	return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
}

// do applies one change to both snapshots.
func (tw *twin) do(change func(f *fixture)) {
	change(tw.fast)
	change(tw.full)
}

// exec runs one statement on both snapshots.
func (tw *twin) exec(query string, args ...any) {
	tw.do(func(f *fixture) { f.exec(query, args...) })
}

var twinTables = map[string]string{
	"postStates": `SELECT post_id, status, route, last_attempt_at, last_processed_at, last_error, last_extraction_id,
		reprocess_requested, failure_count, failure_fingerprint, next_attempt_at, settled_stamp FROM post_llm_states ORDER BY post_id`,
	"extractions":  `SELECT id, source, source_item_id, content_fingerprint, extractor_version, model, status, result_json, error, created_at, updated_at FROM schedule_extractions ORDER BY id`,
	"events":       parityQueries["events"],
	"eventSources": parityQueries["eventSources"],
	"providers":    `SELECT id, last_status, last_error, last_checked_at FROM llm_providers ORDER BY id`,
	"assets":       `SELECT id, kind, url, source_url, week_start, period_start, period_end FROM assets ORDER BY id`,
	"reviews":      `SELECT asset_id, llm_status, manual_status, updated_at FROM schedule_asset_reviews ORDER BY asset_id`,
}

// comparablePreview leaves out the times a preview measures.
func comparablePreview(preview QueuePreview) QueuePreview {
	preview.CandidateMs, preview.ElapsedMs = 0, 0
	return preview
}

// comparableCounts leaves out the counts that measure the candidate phase.
func comparableCounts(t *testing.T, counts map[string]any) any {
	t.Helper()
	shape := jsonShape(t, counts).(map[string]any)
	for _, key := range []string{"candidateMs", "judgeMs", "judged", "settled"} {
		delete(shape, key)
	}
	return shape
}

// calls are the posts a scan sent to a model, in order, with the host.
func calls(f *fixture, from int) []string {
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	out := []string{}
	for _, request := range f.provider.requests[from:] {
		url := ""
		if match := sourceURLPattern.FindStringSubmatch(request.body); match != nil {
			url = match[1]
		}
		out = append(out, strings.SplitN(request.url, "/", 4)[2]+" "+url)
	}
	return out
}

// scan advances both clocks two hours, previews and runs both snapshots,
// and fails on any difference in what they would do or what they wrote.
func (tw *twin) scan(label string) {
	tw.t.Helper()
	tw.clock = tw.clock.Add(2 * time.Hour)
	tw.do(func(f *fixture) { f.at(tw.clock) })
	if fast, full := comparablePreview(tw.fast.preview()), comparablePreview(tw.full.preview()); !reflect.DeepEqual(fast, full) {
		tw.t.Fatalf("%s: previews differ\nfast %+v\nfull %+v", label, fast, full)
	}
	fastFrom, fullFrom := tw.fast.provider.count(), tw.full.provider.count()
	fast, full := tw.fast.scan(), tw.full.scan()
	if fast.Status != full.Status {
		tw.t.Fatalf("%s: status %s, judged %s", label, fast.Status, full.Status)
	}
	if a, b := comparableCounts(tw.t, fast.Counts), comparableCounts(tw.t, full.Counts); !reflect.DeepEqual(a, b) {
		tw.t.Fatalf("%s: counts differ\nfast %v\nfull %v", label, a, b)
	}
	if a, b := calls(tw.fast, fastFrom), calls(tw.full, fullFrom); !reflect.DeepEqual(a, b) {
		tw.t.Fatalf("%s: model calls differ\nfast %v\nfull %v", label, a, b)
	}
	for name, query := range twinTables {
		if a, b := jsonShape(tw.t, tw.fast.table(query)), jsonShape(tw.t, tw.full.table(query)); !reflect.DeepEqual(a, b) {
			tw.t.Fatalf("%s: %s differ\nfast %v\nfull %v", label, name, a, b)
		}
	}
	if countOf(full.Counts, "settled") != 0 {
		tw.t.Fatalf("%s: the judging twin skipped posts: %v", label, full.Counts)
	}
	tw.fastCounts = fast.Counts
}

// expect checks how many candidates fast's last scan judged and how many it
// took as settled, so each scenario shows the filter at work.
func (tw *twin) expect(label string, judged, settled int) {
	tw.t.Helper()
	if got, left := countOf(tw.fastCounts, "judged"), countOf(tw.fastCounts, "settled"); got != judged || left != settled {
		tw.t.Fatalf("%s: judged %d and settled %d, want %d and %d (%v)", label, got, left, judged, settled, tw.fastCounts)
	}
}

// stampAs stores, for every settled post, the stamp an extractor of version
// would have recorded.
func (tw *twin) stampAs(version string) {
	tw.exec(`UPDATE post_llm_states SET settled_stamp = (SELECT `+sqlite.ScheduleInputStampSQL("p")+`
		FROM posts p WHERE p.id = post_llm_states.post_id) WHERE settled_stamp IS NOT NULL`, "message", version)
	tw.exec(`UPDATE post_llm_states SET settled_stamp = (SELECT `+sqlite.ScheduleInputStampSQL("p")+`
		FROM posts p WHERE p.id = post_llm_states.post_id) WHERE settled_stamp IS NOT NULL AND route = ?`, "board", version, routeBoard)
}

func TestSettledPostsMatchJudgedPosts(t *testing.T) {
	cases := []struct {
		name string
		run  func(tw *twin)
	}{
		{"text changes", func(tw *twin) {
			tw.exec(`UPDATE posts SET text = '明日 22時から配信に変更' WHERE id = 'text-1'`)
			tw.scan("changed")
			tw.expect("changed", 1, 4)
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"image hash changes", func(tw *twin) {
			tw.exec(`UPDATE media_assets SET sha256 = ?`, sha256Hex("replaced"))
			tw.scan("changed")
			tw.expect("changed", 1, 4)
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"image pending and ready again", func(tw *twin) {
			tw.exec(`UPDATE media_assets SET status = 'pending'`)
			tw.scan("pending")
			tw.expect("pending", 1, 4)
			tw.exec(`UPDATE media_assets SET status = 'ready'`)
			tw.scan("ready")
			tw.expect("ready", 1, 4)
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"detection stage changes", func(tw *twin) {
			// The board post leaves the board stage for the message stage.
			tw.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_keywords', '["unmatched-keyword"]', ?)`, testTimestamp)
			tw.scan("message route")
			tw.expect("message route", 1, 4)
			tw.scan("message recorded")
			tw.scan("message settled")
			tw.expect("message settled", 0, 5)
			// And returns to its stored board result.
			tw.exec(`DELETE FROM app_settings WHERE key = 'schedule_keywords'`)
			tw.scan("board route")
			tw.expect("board route", 1, 4)
			tw.scan("board settled")
			tw.expect("board settled", 0, 5)
			// A stage switched off and on again keeps its settled posts.
			tw.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_message_enabled', '0', ?)`, testTimestamp)
			tw.scan("message off")
			tw.expect("message off", 0, 1)
			tw.exec(`DELETE FROM app_settings WHERE key = 'schedule_message_enabled'`)
			tw.scan("message on")
			tw.expect("message on", 0, 5)
		}},
		{"every post mode", func(tw *twin) {
			tw.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_message_all_posts', '1', ?)`, testTimestamp)
			tw.scan("all posts")
			tw.expect("all posts", 1, 5)
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 6)
		}},
		{"extractor version changes", func(tw *twin) {
			// The results and stamps of an earlier extractor version.
			tw.exec(`UPDATE schedule_extractions SET extractor_version = replace(extractor_version, ?, 'synthetic-schedule-v0')`, extractorVersion)
			tw.stampAs("synthetic-schedule-v0")
			tw.scan("upgraded")
			tw.expect("upgraded", 5, 0)
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"quote changes, hides and shows again", func(tw *twin) {
			tw.exec(`UPDATE posts SET quote_json = replace(quote_json, '21時', '22時') WHERE id = 'quote-1'`)
			tw.scan("quote text")
			tw.expect("quote text", 1, 4)
			tw.scan("recorded")
			tw.exec(`UPDATE posts SET quote_hidden_at = ? WHERE id = 'quote-1'`, testTimestamp)
			tw.scan("quote hidden")
			tw.expect("quote hidden", 1, 4)
			tw.scan("hidden recorded")
			tw.exec(`UPDATE posts SET quote_hidden_at = NULL WHERE id = 'quote-1'`)
			tw.scan("quote shown")
			tw.expect("quote shown", 1, 4)
			tw.scan("settled")
			tw.expect("settled", 0, 5)
			// A quote the extractor reads the same way changes nothing.
			tw.exec(`UPDATE posts SET quote_json = json_set(quote_json, '$.publishedAt', '2026-04-27T01:00:00.000Z') WHERE id = 'quote-1'`)
			tw.scan("unread quote field")
			tw.expect("unread quote field", 0, 5)
		}},
		{"queue changes", func(tw *twin) {
			tw.failing["text-2"] = true
			tw.exec(`UPDATE posts SET text = '今日 21時から雑談配信' WHERE id = 'text-2'`)
			tw.scan("failed")
			tw.expect("failed", 1, 4)
			tw.scan("backoff over, failed again")
			tw.exec(`DELETE FROM llm_route_targets WHERE route = ?`, routeQueue)
			tw.do(func(f *fixture) {
				f.queue("p-alt")
				f.queueVerify("p-alt")
			})
			// Stored results stand; the failure starts over on the new queue.
			tw.scan("new queue")
			tw.expect("new queue", 1, 4)
			delete(tw.failing, "text-2")
			tw.scan("backoff over")
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"operator requests", func(tw *twin) {
			tw.exec(`UPDATE post_llm_states SET reprocess_requested = 1 WHERE post_id = 'text-2'`)
			tw.do(func(f *fixture) { f.requestReprocess("quote-1") })
			tw.scan("requested")
			tw.expect("requested", 2, 3)
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"failure then success", func(tw *twin) {
			tw.failing["text-1"] = true
			tw.exec(`UPDATE posts SET text = '明日 23時から配信' WHERE id = 'text-1'`)
			for i := 1; i <= 4; i++ {
				tw.scan(fmt.Sprintf("failing %d", i))
			}
			delete(tw.failing, "text-1")
			for i := 1; i <= 4; i++ {
				tw.scan(fmt.Sprintf("recovering %d", i))
			}
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"dismissed failure", func(tw *twin) {
			tw.failing["text-2"] = true
			tw.exec(`UPDATE posts SET text = '今日 22時から雑談配信' WHERE id = 'text-2'`)
			tw.scan("failed")
			// As the console dismisses it.
			tw.exec(`UPDATE post_llm_states SET status = 'dismissed', reprocess_requested = 0, failure_count = MAX(failure_count, 1),
				next_attempt_at = NULL, settled_stamp = NULL WHERE post_id = 'text-2' AND status = 'failed'`)
			tw.scan("dismissed")
			tw.expect("dismissed", 1, 4)
			tw.scan("still dismissed")
			delete(tw.failing, "text-2")
			tw.do(func(f *fixture) { f.requestReprocess("text-2") })
			tw.scan("requested")
			tw.scan("recorded")
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
		{"hidden and shown again", func(tw *twin) {
			tw.exec(`UPDATE posts SET hidden_at = ? WHERE id = 'text-1'`, testTimestamp)
			tw.scan("hidden")
			tw.expect("hidden", 0, 4)
			tw.exec(`UPDATE posts SET hidden_at = NULL WHERE id = 'text-1'`)
			tw.scan("shown")
			tw.expect("shown", 0, 5)
		}},
		{"state written outside a cache hit", func(tw *twin) {
			// A stray failure count or error is not a settled state, whatever
			// the stamp says.
			tw.exec(`UPDATE post_llm_states SET failure_count = 1, failure_fingerprint = 'synthetic' WHERE post_id = 'text-1'`)
			tw.exec(`UPDATE post_llm_states SET last_error = 'synthetic' WHERE post_id = 'text-2'`)
			tw.exec(`UPDATE post_llm_states SET route = ? WHERE post_id = 'quote-1'`, routeBoard)
			tw.scan("rewritten")
			tw.expect("rewritten", 3, 2)
			tw.scan("settled")
			tw.expect("settled", 0, 5)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(settledTwin(t))
		})
	}
}

// A settled post whose cached image file is gone keeps its stored result: it
// is not loaded, so the missing file is not seen. A judged post with a
// missing file waits as media_pending.
func TestSettledPostKeepsItsResultWithoutItsCacheFile(t *testing.T) {
	tw := settledTwin(t)
	tw.do(func(f *fixture) {
		path := f.text(`SELECT cache_path FROM media_assets`)
		if err := os.Remove(filepath.Join(f.store.DataRoot(), filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	})
	tw.do(func(f *fixture) { f.at(tw.clock.Add(time.Hour)) })
	fast, full := tw.fast.scan(), tw.full.scan()
	if countOf(fast.Counts, "settled") != 5 || countOf(fast.Counts, "skipped") != 0 {
		t.Fatalf("fast counts %v", fast.Counts)
	}
	if countOf(full.Counts, "skipped") != 1 {
		t.Fatalf("full counts %v", full.Counts)
	}
	state := `SELECT status || '|' || COALESCE(last_error, '-') FROM post_llm_states WHERE post_id = 'board-1'`
	if got := tw.fast.text(state); got != "success|-" {
		t.Errorf("settled post = %s", got)
	}
	if got := tw.full.text(state); got != "skipped|media_pending" {
		t.Errorf("judged post = %s", got)
	}
}

// ---- cache hits that change nothing ----

func TestCacheHitLeavesAnUnchangedStateUnwritten(t *testing.T) {
	f := messageFixture(t)
	f.run.ignoreSettled = true
	row := `SELECT status || '|' || route || '|' || last_processed_at || '|' || last_extraction_id || '|' ||
		COALESCE(settled_stamp, '-') || '|' || updated_at FROM post_llm_states WHERE post_id = 'post-1'`
	f.at(backoffStart)
	f.scan()
	f.at(backoffStart.Add(time.Hour))
	f.scan()
	recorded := f.text(row)
	if !strings.Contains(recorded, sqlite.ISOTime(backoffStart.Add(time.Hour))) || strings.Contains(recorded, "|-|") {
		t.Fatalf("first cache hit = %s", recorded)
	}
	f.at(backoffStart.Add(2 * time.Hour))
	result := f.scan()
	if countOf(result.Counts, "cached") != 1 || countOf(result.Counts, "judged") != 1 {
		t.Fatalf("counts %v", result.Counts)
	}
	if got := f.text(row); got != recorded {
		t.Fatalf("second cache hit rewrote %s as %s", recorded, got)
	}
}

func TestCacheHitWritesAChangedState(t *testing.T) {
	for name, change := range map[string]string{
		"status":             `UPDATE post_llm_states SET status = 'uncertain'`,
		"route":              `UPDATE post_llm_states SET route = 'schedule_board'`,
		"processed at":       `UPDATE post_llm_states SET last_processed_at = '2026-01-01T00:00:00.000Z'`,
		"error":              `UPDATE post_llm_states SET last_error = 'synthetic'`,
		"empty error":        `UPDATE post_llm_states SET last_error = ''`,
		"extraction":         `UPDATE post_llm_states SET last_extraction_id = last_extraction_id + 100`,
		"failure count":      `UPDATE post_llm_states SET failure_count = 2`,
		"failure key":        `UPDATE post_llm_states SET failure_fingerprint = 'synthetic'`,
		"next attempt":       `UPDATE post_llm_states SET next_attempt_at = '2026-01-01T00:00:00.000Z'`,
		"missing stamp":      `UPDATE post_llm_states SET settled_stamp = NULL`,
		"another stamp":      `UPDATE post_llm_states SET settled_stamp = 'synthetic'`,
		"missing extraction": `UPDATE post_llm_states SET last_extraction_id = NULL`,
	} {
		t.Run(name, func(t *testing.T) {
			f := messageFixture(t)
			f.run.ignoreSettled = true
			f.at(backoffStart)
			f.scan()
			f.at(backoffStart.Add(time.Hour))
			f.scan()
			want := f.text(`SELECT status || '|' || route || '|' || last_processed_at || '|' || COALESCE(last_error, '-') || '|' ||
				last_extraction_id || '|' || failure_count || '|' || COALESCE(failure_fingerprint, '-') || '|' ||
				COALESCE(next_attempt_at, '-') || '|' || settled_stamp FROM post_llm_states`)
			f.exec(change)
			f.at(backoffStart.Add(2 * time.Hour))
			if result := f.scan(); countOf(result.Counts, "cached") != 1 || f.provider.count() != 1 {
				t.Fatalf("counts %v, requests %d", result.Counts, f.provider.count())
			}
			got := f.text(`SELECT status || '|' || route || '|' || last_processed_at || '|' || COALESCE(last_error, '-') || '|' ||
				last_extraction_id || '|' || failure_count || '|' || COALESCE(failure_fingerprint, '-') || '|' ||
				COALESCE(next_attempt_at, '-') || '|' || settled_stamp FROM post_llm_states`)
			if got != want {
				t.Fatalf("state = %s, want %s", got, want)
			}
			if updated := f.text(`SELECT updated_at FROM post_llm_states`); updated != sqlite.ISOTime(backoffStart.Add(2*time.Hour)) {
				t.Errorf("updated_at = %s", updated)
			}
		})
	}
}

// A fresh result records no stamp: the next scan's cache hit writes the
// state every later cache hit keeps, and only then is the post settled.
func TestFreshResultSettlesAfterItsFirstCacheHit(t *testing.T) {
	f := messageFixture(t)
	stamp := `SELECT COALESCE(settled_stamp, '-') FROM post_llm_states WHERE post_id = 'post-1'`
	for i, want := range []struct {
		judged, settled int
		stamped         bool
	}{{1, 0, false}, {1, 0, true}, {0, 1, true}} {
		f.at(backoffStart.Add(time.Duration(i) * time.Hour))
		result := f.scan()
		if countOf(result.Counts, "judged") != want.judged || countOf(result.Counts, "settled") != want.settled {
			t.Fatalf("scan %d counts %v", i+1, result.Counts)
		}
		if got := f.text(stamp); (got != "-") != want.stamped {
			t.Fatalf("scan %d stamp %s", i+1, got)
		}
	}
	if f.provider.count() != 1 {
		t.Fatalf("requests %d", f.provider.count())
	}
}
