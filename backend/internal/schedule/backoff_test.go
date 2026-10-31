package schedule

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

var backoffStart = time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)

// at fixes the extractor clock.
func (f *fixture) at(instant time.Time) {
	f.run.now = func() time.Time { return instant }
}

func (f *fixture) failing() {
	f.provider.respond = func(recordedRequest) (int, string) { return http.StatusServiceUnavailable, `{}` }
}

// backoffState is failure_count|next_attempt_at ("-" for NULL).
func (f *fixture) backoffState(postID string) string {
	f.t.Helper()
	return f.text(`SELECT failure_count || '|' || COALESCE(next_attempt_at, '-') FROM post_llm_states WHERE post_id=?`, postID)
}

func TestFailedPostBacksOffAndStopsRetrying(t *testing.T) {
	f := messageFixture(t)
	f.failing()
	perFailure := defaultAttempts
	steps := []struct {
		offset   time.Duration
		requests int
		count    string
		state    string
	}{
		{0, perFailure, "failed", "1|2026-04-28T01:00:00.000Z"},
		{59 * time.Minute, perFailure, "backoff", "1|2026-04-28T01:00:00.000Z"},
		{time.Hour, 2 * perFailure, "failed", "2|2026-04-28T07:00:00.000Z"},
		{7*time.Hour - time.Second, 2 * perFailure, "backoff", "2|2026-04-28T07:00:00.000Z"},
		{7 * time.Hour, 3 * perFailure, "failed", "3|2026-04-29T07:00:00.000Z"},
		{31 * time.Hour, 4 * perFailure, "failed", "4|-"},
		// After the fourth failure the scan stops retrying this input.
		{31*time.Hour + 1000*time.Hour, 4 * perFailure, "retryStopped", "4|-"},
	}
	for _, step := range steps {
		f.at(backoffStart.Add(step.offset))
		result := f.scan()
		if got := f.provider.count(); got != step.requests {
			t.Fatalf("+%s: provider requests = %d, want %d", step.offset, got, step.requests)
		}
		if countOf(result.Counts, step.count) != 1 {
			t.Fatalf("+%s: counts = %v, want one %s", step.offset, result.Counts, step.count)
		}
		if got := f.backoffState("post-1"); got != step.state {
			t.Fatalf("+%s: backoff = %q, want %q", step.offset, got, step.state)
		}
		if step.count != "failed" {
			// A held post is neither attempted nor a failure of this run, and
			// keeps its visible failure.
			if result.Status != "success" || countOf(result.Counts, "attempted") != 0 || countOf(result.Counts, "failed") != 0 {
				t.Fatalf("+%s: held post result = %q %v", step.offset, result.Status, result.Counts)
			}
			if got := f.text(`SELECT status || '|' || last_error FROM post_llm_states`); got != "failed|LLM request failed (HTTP 503)" {
				t.Fatalf("+%s: held post state = %q", step.offset, got)
			}
		}
	}
}

func TestBackoffStartsOverForNewInputAndClearsOnSuccess(t *testing.T) {
	f := messageFixture(t)
	f.failing()
	f.at(backoffStart)
	f.scan()
	f.at(backoffStart.Add(time.Hour))
	f.scan()
	if got := f.backoffState("post-1"); got != "2|2026-04-28T07:00:00.000Z" {
		t.Fatalf("backoff = %q", got)
	}
	perFailure := defaultAttempts

	// Edited content is new input: it is tried at once and counts from one.
	f.at(backoffStart.Add(2 * time.Hour))
	f.exec(`UPDATE posts SET text = text || ' 追記' WHERE id='post-1'`)
	f.scan()
	if f.provider.count() != 3*perFailure || f.backoffState("post-1") != "1|2026-04-28T03:00:00.000Z" {
		t.Fatalf("edited post: %d requests, backoff %q", f.provider.count(), f.backoffState("post-1"))
	}

	// So is a changed model queue.
	f.addProvider("p-next", protocolChat, "https://next.example.test/v1", "model-n", `["text"]`)
	f.queue("p-next")
	f.at(backoffStart.Add(2*time.Hour + time.Minute))
	f.scan()
	if f.provider.count() != 5*perFailure || f.backoffState("post-1") != "1|2026-04-28T03:01:00.000Z" {
		t.Fatalf("changed queue: %d requests, backoff %q", f.provider.count(), f.backoffState("post-1"))
	}

	// A success after the wait clears the backoff.
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
	}
	f.at(backoffStart.Add(4 * time.Hour))
	if result := f.scan(); countOf(result.Counts, "success") != 1 {
		t.Fatalf("recovery = %v", result.Counts)
	}
	if got := f.text(`SELECT failure_count || '|' || COALESCE(failure_fingerprint, '-') || '|' || COALESCE(next_attempt_at, '-') FROM post_llm_states`); got != "0|-|-" {
		t.Errorf("backoff after success = %q", got)
	}
}

func TestCachedResultClearsBackoff(t *testing.T) {
	f := messageFixture(t)
	f.at(backoffStart)
	f.scan()
	// A failure recorded for other input does not hold back a cached result.
	f.exec(`UPDATE post_llm_states SET status='failed', failure_count=2, failure_fingerprint='synthetic-other-input', next_attempt_at='2099-01-01T00:00:00.000Z'`)
	if result := f.scan(); countOf(result.Counts, "cached") != 1 || f.provider.count() != 1 {
		t.Fatalf("cached scan = %v after %d requests", result.Counts, f.provider.count())
	}
	if got := f.backoffState("post-1"); got != "0|-" {
		t.Errorf("backoff after cache hit = %q", got)
	}
}

func TestReprocessRequestBypassesBackoff(t *testing.T) {
	f := messageFixture(t)
	f.failing()
	for _, offset := range []time.Duration{0, time.Hour, 7 * time.Hour, 31 * time.Hour} {
		f.at(backoffStart.Add(offset))
		f.scan()
	}
	if got := f.backoffState("post-1"); got != "4|-" {
		t.Fatalf("backoff = %q", got)
	}
	perFailure := defaultAttempts
	// The extractor honours a queued request even when the flag alone was
	// set, and the forced failure starts the count over.
	f.requestReprocess("post-1")
	f.at(backoffStart.Add(32 * time.Hour))
	if result := f.scan(); countOf(result.Counts, "failed") != 1 || f.provider.count() != 5*perFailure {
		t.Fatalf("requested scan = %v after %d requests", result.Counts, f.provider.count())
	}
	if got := f.backoffState("post-1"); got != "1|2026-04-29T09:00:00.000Z" {
		t.Errorf("backoff after forced failure = %q", got)
	}
	// It also runs while a backoff is pending.
	f.requestReprocess("post-1")
	f.at(backoffStart.Add(32*time.Hour + time.Minute))
	if result := f.scan(); countOf(result.Counts, "failed") != 1 || f.provider.count() != 6*perFailure {
		t.Fatalf("requested scan during backoff = %v after %d requests", result.Counts, f.provider.count())
	}
}

func TestBackoffPostsLeaveTheScanLimitToOtherWork(t *testing.T) {
	f := messageFixture(t)
	f.exec(`DELETE FROM posts`)
	f.failing()
	for i := range scanLimit {
		f.addPost(fmt.Sprintf("held-%02d", i), "", "明日 21時から歌枠配信します", "2026-04-28T03:00:00.000Z", "")
	}
	f.at(backoffStart)
	if result := f.scan(); countOf(result.Counts, "failed") != scanLimit {
		t.Fatalf("first scan = %v", result.Counts)
	}
	// A post tried more recently sorts behind every held post.
	f.addPost("fresh", "", "明後日 20時から雑談配信", "2026-04-27T03:00:00.000Z", "")
	f.exec(`INSERT INTO post_llm_states (post_id, status, last_attempt_at, last_error, updated_at) VALUES ('fresh', 'skipped', ?, 'media_pending', ?)`,
		sqlite.ISOTime(backoffStart.Add(time.Second)), testTimestamp)
	before := f.provider.count()
	f.at(backoffStart.Add(time.Minute))
	result := f.scan()
	if countOf(result.Counts, "backoff") != scanLimit || countOf(result.Counts, "attempted") != 1 || countOf(result.Counts, "failed") != 1 {
		t.Fatalf("second scan = %v", result.Counts)
	}
	if got := f.provider.count() - before; got != defaultAttempts {
		t.Errorf("provider requests = %d, want only the fresh post's", got)
	}
	if got := f.backoffState("fresh"); got != "1|2026-04-28T01:01:00.000Z" {
		t.Errorf("fresh backoff = %q", got)
	}
}

func TestCancelledRunIsNotCountedAsFailure(t *testing.T) {
	f := messageFixture(t)
	f.at(backoffStart)
	ctx, cancel := context.WithCancel(context.Background())
	f.provider.respond = func(recordedRequest) (int, string) {
		cancel()
		return http.StatusInternalServerError, `{}`
	}
	if _, err := f.run.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if got := f.backoffState("post-1"); got != "0|-" {
		t.Fatalf("cancelled run counted: %q", got)
	}
	// The next scan tries the post at once.
	f.failing()
	f.scan()
	if got := f.backoffState("post-1"); got != "1|2026-04-28T01:00:00.000Z" {
		t.Errorf("backoff after the first real failure = %q", got)
	}
}

func TestSkippedPostsAreNotFailures(t *testing.T) {
	f := messageFixture(t)
	f.exec(`DELETE FROM llm_route_targets`)
	f.at(backoffStart)
	for range 2 {
		result := f.scan()
		if countOf(result.Counts, "skipped") != 1 || countOf(result.Counts, "attempted") != 1 || countOf(result.Counts, "backoff") != 0 {
			t.Fatalf("skipped scan = %v", result.Counts)
		}
	}
	if got := f.text(`SELECT status || '|' || last_error || '|' || failure_count FROM post_llm_states`); got != "skipped|no_route_target|0" || f.provider.count() != 0 {
		t.Errorf("state = %q after %d requests", got, f.provider.count())
	}
}
