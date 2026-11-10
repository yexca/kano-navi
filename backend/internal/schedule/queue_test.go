package schedule

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

var judgeNow = time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

func judgedPost() post {
	return post{source: "x", id: "post-j", text: "明日 21時から配信", publishedAt: "2026-04-28T03:00:00.000Z", url: "https://x.com/synthetic_handle/status/post-j"}
}

func judgeTargets(providers ...provider) (targetLookup, *int) {
	asked := 0
	return func(string) ([]provider, string, error) {
		asked++
		if len(providers) == 0 {
			return []provider{}, "no_route_target", nil
		}
		return providers, "", nil
	}, &asked
}

var (
	judgeTarget = provider{id: "p-a", protocol: protocolChat, baseURL: "https://a.example.test/v1", model: "model-a"}
	otherTarget = provider{id: "p-b", protocol: protocolChat, baseURL: "https://b.example.test/v1", model: "model-b"}
)

// failureKeyFor is the failure key a call of the judged post would record.
func failureKeyFor(t *testing.T, input candidateInput, targets ...provider) string {
	t.Helper()
	input.state = nil
	lookup, _ := judgeTargets(targets...)
	decided, err := judge(input, "message", false, lookup, judgeNow)
	if err != nil || decided.disposition != dispositionCall {
		t.Fatalf("baseline = %+v, %v", decided, err)
	}
	return decided.failureKey
}

func TestJudgeDispositions(t *testing.T) {
	text := candidateInput{post: judgedPost(), media: postImages{images: []cachedImage{}}}
	key := failureKeyFor(t, text, judgeTarget)
	later := sqlite.ISOTime(judgeNow.Add(time.Hour))
	earlier := sqlite.ISOTime(judgeNow.Add(-time.Minute))
	attempted := "2026-04-27T00:00:00.000Z"
	withState := func(state postState) candidateInput {
		input := text
		input.state = &state
		return input
	}
	success := &extractionRow{id: 7, status: "success"}
	cases := []struct {
		name    string
		input   candidateInput
		force   bool
		targets []provider
		want    string
		reason  string
		cause   string
		// asks reports whether the queue targets are read.
		asks bool
	}{
		{name: "no content", input: candidateInput{post: post{source: "x", id: "post-e"}}, want: dispositionSkip, reason: "no_content"},
		{name: "images still downloading", input: candidateInput{post: judgedPost(), media: postImages{hasImages: true}}, want: dispositionSkip, reason: "media_pending"},
		{name: "stored success", input: candidateInput{post: judgedPost(), latest: success}, targets: []provider{judgeTarget}, want: dispositionCached},
		{name: "stored uncertain", input: candidateInput{post: judgedPost(), latest: &extractionRow{status: "uncertain"}}, want: dispositionCached},
		{name: "stored failure", input: candidateInput{post: judgedPost(), latest: &extractionRow{status: "failed"}}, targets: []provider{judgeTarget}, want: dispositionCall, cause: "new", asks: true},
		{name: "forced past a stored success", input: candidateInput{post: judgedPost(), latest: success}, force: true, targets: []provider{judgeTarget}, want: dispositionCall, cause: "requested", asks: true},
		{name: "stored request", input: withState(postState{reprocessRequested: true, lastAttemptAt: attempted}), targets: []provider{judgeTarget}, want: dispositionCall, cause: "requested", asks: true},
		{name: "no queue target", input: text, want: dispositionSkip, reason: "no_route_target", asks: true},
		{name: "never attempted", input: text, targets: []provider{judgeTarget}, want: dispositionCall, cause: "new", asks: true},
		{name: "attempted without a result", input: withState(postState{lastAttemptAt: attempted}), targets: []provider{judgeTarget}, want: dispositionCall, cause: "changed", asks: true},
		{name: "in backoff", input: withState(postState{lastAttemptAt: attempted, failureCount: 1, failureFingerprint: key, nextAttemptAt: later}), targets: []provider{judgeTarget}, want: dispositionBackoff, asks: true},
		{name: "backoff over", input: withState(postState{lastAttemptAt: attempted, failureCount: 2, failureFingerprint: key, nextAttemptAt: earlier}), targets: []provider{judgeTarget}, want: dispositionCall, cause: "backoffDue", asks: true},
		{name: "retries stopped", input: withState(postState{lastAttemptAt: attempted, failureCount: 4, failureFingerprint: key}), targets: []provider{judgeTarget}, want: dispositionRetryStopped, asks: true},
		{name: "queue changed since the failures", input: withState(postState{lastAttemptAt: attempted, failureCount: 4, failureFingerprint: key}), targets: []provider{otherTarget}, want: dispositionCall, cause: "changed", asks: true},
		{name: "request past stopped retries", input: withState(postState{lastAttemptAt: attempted, failureCount: 4, failureFingerprint: key}), force: true, targets: []provider{judgeTarget}, want: dispositionCall, cause: "requested", asks: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup, asked := judgeTargets(tc.targets...)
			decided, err := judge(tc.input, "message", tc.force, lookup, judgeNow)
			if err != nil {
				t.Fatalf("judge: %v", err)
			}
			if decided.disposition != tc.want || decided.reason != tc.reason {
				t.Fatalf("decision = %s/%q, want %s/%q", decided.disposition, decided.reason, tc.want, tc.reason)
			}
			if (*asked > 0) != tc.asks {
				t.Errorf("queue targets read %d times", *asked)
			}
			if tc.want == dispositionCall {
				if got := callReason(tc.input, decided); got != tc.cause {
					t.Errorf("call reason = %q, want %q", got, tc.cause)
				}
				if decided.failureKey == "" || len(decided.providers) != len(tc.targets) {
					t.Errorf("call without its failure key or targets: %+v", decided)
				}
			}
			if tc.want == dispositionBackoff && !decided.retryAt.Equal(judgeNow.Add(time.Hour)) {
				t.Errorf("retry at %s", decided.retryAt)
			}
			if tc.want == dispositionCached && decided.cached != tc.input.latest {
				t.Errorf("cached row = %+v", decided.cached)
			}
		})
	}
}

func TestJudgeKeepsTheExtractionIdentity(t *testing.T) {
	input := candidateInput{post: judgedPost(), media: postImages{hasImages: true, complete: true, images: []cachedImage{{sha256: "hash-1"}}}}
	lookup, _ := judgeTargets(judgeTarget)
	decided, err := judge(input, "board", false, lookup, judgeNow)
	if err != nil {
		t.Fatal(err)
	}
	if decided.inputMode != "text_image" || decided.fingerprint != fingerprintOf(input.post, "board", "text_image", input.media) {
		t.Fatalf("identity = %s %s", decided.inputMode, decided.fingerprint)
	}
	// A quote is part of the input.
	input.post.quote = quotedContext{author: "synthetic_other", text: "引用"}
	quoted, _ := judge(input, "board", false, lookup, judgeNow)
	if quoted.fingerprint == decided.fingerprint || quoted.failureKey == decided.failureKey {
		t.Fatal("a quote does not change the fingerprint")
	}
}

func TestJudgeReportsTargetErrors(t *testing.T) {
	failure := errors.New("synthetic queue failure")
	_, err := judge(candidateInput{post: judgedPost()}, "message", false, func(string) ([]provider, string, error) {
		return nil, "", failure
	}, judgeNow)
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v", err)
	}
}

// tick advances the extractor clock by a millisecond on every read, so the
// run's writes record the order it processed posts in.
func (f *fixture) tick(start time.Time) {
	reads := 0
	f.run.now = func() time.Time {
		reads++
		return start.Add(time.Duration(reads) * time.Millisecond)
	}
}

// snapshotState is every row a preview must leave as it is.
func (f *fixture) snapshotState() string {
	f.t.Helper()
	parts := []string{}
	for _, query := range []string{
		`SELECT COALESCE(group_concat(post_id || status || COALESCE(last_attempt_at,'') || reprocess_requested || COALESCE(failure_count,0) || COALESCE(next_attempt_at,'') || COALESCE(updated_at,''), ';'), '') FROM (SELECT * FROM post_llm_states ORDER BY post_id)`,
		`SELECT COALESCE(group_concat(id || status || updated_at, ';'), '') FROM (SELECT * FROM schedule_extractions ORDER BY id)`,
		`SELECT COUNT(*) || '|' || COALESCE(MAX(updated_at), '') FROM events`,
		`SELECT COALESCE(group_concat(id || COALESCE(last_status,'') || COALESCE(last_checked_at,''), ';'), '') FROM llm_providers`,
	} {
		parts = append(parts, f.text(query))
	}
	return strings.Join(parts, "\n")
}

func (f *fixture) preview() QueuePreview {
	f.t.Helper()
	out, err := f.run.PreviewQueue(context.Background())
	if err != nil {
		f.t.Fatalf("preview: %v", err)
	}
	return out
}

func TestPreviewMatchesTheRun(t *testing.T) {
	f := newFixture(t)
	f.addProvider("p-main", protocolChat, "https://provider.example.test/v1", "model-a", `["text","image"]`)
	f.queue("p-main")
	f.queueVerify("p-main")
	reply := chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
	f.provider.respond = func(request recordedRequest) (int, string) {
		if strings.Contains(request.body, "失敗") {
			return http.StatusServiceUnavailable, `{}`
		}
		return http.StatusOK, reply
	}
	published := func(minute int) string {
		return sqlite.ISOTime(time.Date(2026, 4, 20, 0, minute, 0, 0, time.UTC))
	}
	// A first run leaves cached results, failures and results whose input
	// then changes.
	for i := 1; i <= 3; i++ {
		f.addPost(fmt.Sprintf("cached-%d", i), "", fmt.Sprintf("明日 21時から配信 cached %d", i), published(i), "")
	}
	for i := 1; i <= 3; i++ {
		f.addPost(fmt.Sprintf("failing-%d", i), "", fmt.Sprintf("明日 21時から配信 失敗 %d", i), published(10+i), "")
	}
	for i := 1; i <= 5; i++ {
		f.addPost(fmt.Sprintf("edited-%d", i), "", fmt.Sprintf("明日 21時から配信 edited %d", i), published(20+i), "")
	}
	// A post with two ready, linked images: its cache hit needs both images'
	// assets in its fingerprint.
	images := []string{"https://pbs.twimg.com/media/synthetic-ready-1.png", "https://pbs.twimg.com/media/synthetic-ready-2.png"}
	f.addPost("ready-images", "", "明日 21時から配信 images", published(4), `{"media_urls":["`+images[0]+`","`+images[1]+`"]}`)
	for position, imageURL := range images {
		f.addCachedImage(imageURL, []byte(pngHeader+imageURL))
		f.exec(`INSERT INTO media_links (media_id, owner_type, owner_id, role, position, created_at, updated_at)
			VALUES (?, 'post', 'ready-images', 'post-image', ?, ?, ?)`, mediaIDForSourceURL(imageURL), position, testTimestamp, testTimestamp)
	}
	f.at(backoffStart)
	if first := f.scan(); countOf(first.Counts, "attempted") != 12 {
		t.Fatalf("first run = %v", first.Counts)
	}
	// failing-3 is out of automatic retries; the others wait until
	// backoffStart+1h.
	f.exec(`UPDATE post_llm_states SET next_attempt_at = NULL WHERE post_id = 'failing-3'`)
	f.exec(`UPDATE posts SET text = text || ' 追記' WHERE id LIKE 'edited-%'`)
	f.exec(`UPDATE post_llm_states SET last_attempt_at = '2026-04-27T00:00:00.000Z' WHERE post_id NOT LIKE 'edited-%'`)
	for i := 1; i <= 5; i++ {
		f.exec(`UPDATE post_llm_states SET last_attempt_at = ? WHERE post_id = ?`, fmt.Sprintf("2026-04-27T12:00:0%d.000Z", i), fmt.Sprintf("edited-%d", i))
	}
	// New posts, one with an image not downloaded yet, and requests; the
	// hidden post's request waits.
	for i := 1; i <= 15; i++ {
		f.addPost(fmt.Sprintf("new-%02d", i), "", fmt.Sprintf("明日 21時から配信 new %d", i), published(30+i), "")
	}
	f.addPost("pending-image", "", "明日 21時から配信 image", published(46), `{"media_urls":["https://pbs.twimg.com/media/synthetic-pending.png"]}`)
	f.addPost("asked-1", "", "告知 asked 1", published(5), "")
	f.addPost("asked-2", "", "告知 asked 2", published(6), "")
	f.addPost("asked-hidden", "", "告知 hidden", published(7), "")
	f.exec(`UPDATE posts SET hidden_at = ? WHERE id = 'asked-hidden'`, testTimestamp)
	for _, id := range []string{"asked-1", "asked-2", "asked-hidden"} {
		f.requestReprocess(id)
	}

	start := backoffStart.Add(30 * time.Minute)
	f.tick(start)
	before, requests := f.snapshotState(), f.provider.count()
	preview := f.preview()
	if f.snapshotState() != before || f.provider.count() != requests {
		t.Fatal("the preview wrote to the snapshot or called a model")
	}

	order := []string{}
	reasons := []string{}
	for _, entry := range preview.Run {
		order = append(order, entry.PostID)
		reason := entry.Reason
		if entry.Outcome == dispositionSkip {
			reason = "skip:" + reason
		}
		reasons = append(reasons, reason)
	}
	wantOrder := []string{"asked-2", "asked-1", "pending-image"}
	wantReasons := []string{"requested", "requested", "skip:media_pending"}
	for i := 15; i >= 1; i-- {
		wantOrder = append(wantOrder, fmt.Sprintf("new-%02d", i))
		wantReasons = append(wantReasons, "new")
	}
	wantOrder = append(wantOrder, "edited-1", "edited-2")
	wantReasons = append(wantReasons, "changed", "changed")
	if !reflect.DeepEqual(order, wantOrder) || !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("preview order:\n got  %v %v\n want %v %v", order, reasons, wantOrder, wantReasons)
	}
	if preview.Later != 3 || preview.Cached != 4 || preview.Backoff.Count != 2 || preview.RetryStopped.Count != 1 {
		t.Fatalf("preview counts = later %d cached %d backoff %+v stopped %+v", preview.Later, preview.Cached, preview.Backoff, preview.RetryStopped)
	}
	if preview.Backoff.NextAt != "2026-04-28T01:00:00.000Z" || preview.RetryStopped.NextAt != "" {
		t.Fatalf("hold times = %+v %+v", preview.Backoff, preview.RetryStopped)
	}
	if preview.Requested != 2 || preview.Candidates != 30 || preview.Limit != scanLimit {
		t.Fatalf("preview totals = requested %d candidates %d limit %d", preview.Requested, preview.Candidates, preview.Limit)
	}
	for _, entry := range preview.Run {
		if entry.Route != "message" || entry.Source != "x" || entry.URL == "" {
			t.Fatalf("entry = %+v", entry)
		}
	}

	// The run processes the same posts in the same order.
	result := f.scan()
	rows, err := f.db.Query(`SELECT post_id FROM post_llm_states WHERE last_attempt_at >= ? ORDER BY last_attempt_at, post_id`, sqlite.ISOTime(start))
	if err != nil {
		t.Fatal(err)
	}
	processed := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		processed = append(processed, id)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(processed, wantOrder) {
		t.Fatalf("run order:\n got  %v\n want %v", processed, wantOrder)
	}
	if got := f.provider.count() - requests; got != len(wantOrder)-1 {
		t.Errorf("run made %d model calls, want %d", got, len(wantOrder)-1)
	}
	for key, want := range map[string]int{"attempted": len(preview.Run), "cached": preview.Cached, "backoff": preview.Backoff.Count, "retryStopped": preview.RetryStopped.Count, "skipped": 1} {
		if got := countOf(result.Counts, key); got != want {
			t.Errorf("run %s = %d, want %d (%v)", key, got, want, result.Counts)
		}
	}
	if countOf(result.Counts, "candidatePosts") != preview.Candidates || countOf(result.Counts, "postsRead") != preview.PostsRead {
		t.Errorf("candidate phase counts = %v, preview %d/%d", result.Counts, preview.Candidates, preview.PostsRead)
	}
	// It judged every post it reached: the places, the cache hits and the
	// held-back posts ahead of the stop.
	if got := countOf(result.Counts, "judged"); got != len(preview.Run)+preview.Cached+preview.Backoff.Count+preview.RetryStopped.Count {
		t.Errorf("judged = %d", got)
	}
	if f.text(`SELECT status FROM post_llm_states WHERE post_id='pending-image'`) != "skipped" {
		t.Error("the pending image was not skipped")
	}
}

func TestPreviewReportsADisabledScan(t *testing.T) {
	f := messageFixture(t)
	for _, key := range []string{"schedule_keyword_enabled", "schedule_vision_enabled", "schedule_message_enabled"} {
		f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES (?, '0', ?)`, key, testTimestamp)
	}
	if preview := f.preview(); !preview.Disabled || len(preview.Run) != 0 || preview.Candidates != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	// An operator request still runs.
	f.requestReprocess("post-1")
	preview := f.preview()
	if preview.Disabled || len(preview.Run) != 1 || preview.Run[0].Reason != "requested" || preview.PostsRead != 0 {
		t.Fatalf("preview = %+v", preview)
	}
}
