package schedule

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	songTitle = "歌枠"
	songDate  = "2099-05-03"
	songClock = "21:00"
)

// messageFixture has one text provider on the message route and one post the
// message detector selects.
func messageFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.addProvider("p-main", protocolChat, "https://provider.example.test/v1", "model-a", `["text"]`)
	f.queue("p-main")
	f.addPost("post-1", "", "明日 21時から歌枠配信します", "2026-04-28T03:00:00.000Z", "")
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
	}
	return f
}

func (f *fixture) requestReprocess(postID string) {
	f.t.Helper()
	f.exec(`INSERT INTO post_llm_states (post_id, status, reprocess_requested, updated_at) VALUES (?, 'queued', 1, ?)
		ON CONFLICT(post_id) DO UPDATE SET status='queued', reprocess_requested=1`, postID, testTimestamp)
}

func (f *fixture) assertNoSecretStored() {
	f.t.Helper()
	ciphertext := f.text(`SELECT api_key_ciphertext FROM llm_providers LIMIT 1`)
	for _, query := range []string{
		`SELECT COALESCE(group_concat(COALESCE(error,'') || COALESCE(result_json,''), ' '), '') FROM schedule_extractions`,
		`SELECT COALESCE(group_concat(COALESCE(last_error,''), ' '), '') FROM post_llm_states`,
		`SELECT COALESCE(group_concat(COALESCE(last_error,''), ' '), '') FROM llm_providers`,
		`SELECT COALESCE(group_concat(COALESCE(llm_reason,'') || COALESCE(llm_evidence,''), ' '), '') FROM schedule_asset_reviews`,
		`SELECT COALESCE(group_concat(COALESCE(raw_json,''), ' '), '') FROM events`,
	} {
		stored := f.text(query)
		if strings.Contains(stored, testProviderKey) || (ciphertext != "" && strings.Contains(stored, ciphertext)) || strings.Contains(stored, f.store.DataRoot()) {
			f.t.Errorf("protected value leaked through %q: %s", query, stored)
		}
	}
}

func TestTextPostProducesEvents(t *testing.T) {
	f := messageFixture(t)
	result := f.scan()
	if result.Status != "success" {
		t.Fatalf("status = %q, counts = %v", result.Status, result.Counts)
	}
	for key, want := range map[string]int{"attempted": 1, "success": 1, "cached": 0, "skipped": 0, "failed": 0, "uncertain": 0, "boardAttempted": 0, "messageAttempted": 1} {
		if got := countOf(result.Counts, key); got != want {
			t.Errorf("counts[%s] = %d, want %d", key, got, want)
		}
	}
	// The board stage is enabled by default, so its (empty) image summary is
	// always part of the counts, as in the original result.
	if assets, _ := result.Counts["assetVerification"].(map[string]any); assets == nil || countOf(assets, "attempted") != 0 {
		t.Errorf("asset summary = %v", result.Counts["assetVerification"])
	}
	if f.provider.count() != 1 {
		t.Fatalf("provider requests = %d", f.provider.count())
	}
	request := f.provider.requests[0]
	if request.url != "https://provider.example.test/v1/chat/completions" || request.headers.Get("Authorization") != "Bearer "+testProviderKey {
		t.Errorf("unexpected request %s", request.url)
	}
	if !strings.Contains(request.body, "Source post published at 2026-04-28T03:00:00.000Z (2026-04-28 Tue 12:00 Asia/Tokyo). Source URL: https://x.com/synthetic_handle/status/post-1") {
		t.Errorf("request body lacks the source text: %s", request.body)
	}
	if got := f.number(`SELECT COUNT(*) FROM events`); got != 1 {
		t.Fatalf("events = %d", got)
	}
	var title, startsOn, startsAt, source, sourceItem, provenance, status, url string
	var locked, upcoming int
	if err := f.db.QueryRow(`SELECT title, starts_on, starts_at, source, source_item_id, provenance, status, url, manual_locked, is_upcoming FROM events`).
		Scan(&title, &startsOn, &startsAt, &source, &sourceItem, &provenance, &status, &url, &locked, &upcoming); err != nil {
		t.Fatal(err)
	}
	if title != songTitle || startsOn != songDate || startsAt != "2099-05-03T12:00:00.000Z" || source != "x" || sourceItem != "post-1" ||
		provenance != "automatic" || status != "scheduled" || url != "https://x.com/synthetic_handle/status/post-1" || locked != 0 || upcoming != 1 {
		t.Errorf("unexpected event: %q %s %s %s %s %s %s %s %d %d", title, startsOn, startsAt, source, sourceItem, provenance, status, url, locked, upcoming)
	}
	if got := f.number(`SELECT COUNT(*) FROM event_sources WHERE source='x' AND source_item_id='post-1' AND deleted_at IS NULL`); got != 1 {
		t.Errorf("event sources = %d", got)
	}
	if got := f.text(`SELECT status || '|' || route || '|' || COALESCE(last_error,'') || '|' || reprocess_requested FROM post_llm_states WHERE post_id='post-1'`); got != "success|schedule_message||0" {
		t.Errorf("post state = %q", got)
	}
	if got := f.text(`SELECT status || '|' || model FROM schedule_extractions`); got != "success|model-a" {
		t.Errorf("extraction = %q", got)
	}
	if got := f.text(`SELECT last_status FROM llm_providers WHERE id='p-main'`); got != "success" {
		t.Errorf("provider status = %q", got)
	}
	for _, column := range []string{"last_attempt_at", "last_processed_at", "updated_at"} {
		value := f.text(`SELECT ` + column + ` FROM post_llm_states`)
		if len(value) != 24 || !strings.HasSuffix(value, "Z") {
			t.Errorf("%s is not an ISO timestamp: %q", column, value)
		}
	}
	f.assertNoSecretStored()
}

func TestRerunWithUnchangedFingerprintCallsNoProvider(t *testing.T) {
	f := messageFixture(t)
	f.scan()
	before := f.text(`SELECT updated_at || raw_json FROM events`)
	second := f.scan()
	if f.provider.count() != 1 {
		t.Fatalf("provider requests after rerun = %d, want 1", f.provider.count())
	}
	if second.Status != "success" || countOf(second.Counts, "cached") != 1 || countOf(second.Counts, "attempted") != 0 || countOf(second.Counts, "success") != 0 {
		t.Errorf("second scan = %q %v", second.Status, second.Counts)
	}
	if got := f.text(`SELECT updated_at || raw_json FROM events`); got != before {
		t.Error("cached rerun rewrote the event")
	}
	if got := f.number(`SELECT COUNT(*) FROM schedule_extractions`); got != 1 {
		t.Errorf("extractions = %d", got)
	}

	// A changed post has a new fingerprint and is sent again.
	f.exec(`UPDATE posts SET text = text || ' 追記' WHERE id='post-1'`)
	f.scan()
	if f.provider.count() != 2 || f.number(`SELECT COUNT(*) FROM schedule_extractions`) != 2 {
		t.Errorf("edited post was not re-extracted: %d requests", f.provider.count())
	}
}

func TestManualLockAndTombstoneSurviveExtraction(t *testing.T) {
	f := messageFixture(t)
	f.scan()
	eventID := f.text(`SELECT id FROM events`)

	// The operator edits the event; the admin command locks it this way.
	f.exec(`UPDATE events SET title='Operator title', status='operator', detail='operator detail',
		provenance='manual', manual_locked=1 WHERE id=?`, eventID)
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "changed by model"))
	}
	f.requestReprocess("post-1")
	f.scan()
	if f.provider.count() != 2 {
		t.Fatalf("reprocess did not call the provider: %d", f.provider.count())
	}
	locked := `SELECT title || '|' || status || '|' || detail || '|' || provenance || '|' || manual_locked || '|' || (deleted_at IS NULL) FROM events WHERE id=?`
	if got := f.text(locked, eventID); got != "Operator title|operator|operator detail|manual|1|1" {
		t.Errorf("locked event was rewritten: %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM events`); got != 1 {
		t.Errorf("extraction duplicated a locked identity: %d events", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM event_sources WHERE event_id=? AND deleted_at IS NULL`, eventID); got != 1 {
		t.Errorf("source evidence left the locked event: %d", got)
	}

	// The model stops listing the locked event: it is not retired.
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, chatReply(scheduleReply("別枠", "2099-05-06", "20:00", "scheduled"))
	}
	f.requestReprocess("post-1")
	f.scan()
	if got := f.text(locked, eventID); got != "Operator title|operator|operator detail|manual|1|1" {
		t.Errorf("locked event was retired: %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM events WHERE title='別枠' AND deleted_at IS NULL`); got != 1 {
		t.Errorf("replacement event missing: %d", got)
	}

	// The operator deletes the event; a tombstone is a locked, deleted row.
	f.exec(`UPDATE events SET provenance='manual', manual_locked=1, deleted_at=?, updated_at=? WHERE id=?`, testTimestamp, testTimestamp, eventID)
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
	}
	f.requestReprocess("post-1")
	f.scan()
	if got := f.text(`SELECT title || '|' || COALESCE(deleted_at,'') FROM events WHERE id=?`, eventID); got != "Operator title|"+testTimestamp {
		t.Errorf("tombstone was resurrected: %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM events WHERE starts_on=? AND deleted_at IS NULL`, songDate); got != 0 {
		t.Errorf("a second row resurrected the deleted schedule: %d", got)
	}
	// The unlisted automatic replacement is retired now.
	if got := f.number(`SELECT COUNT(*) FROM events WHERE title='別枠' AND deleted_at IS NOT NULL`); got != 1 {
		t.Errorf("unlisted automatic event was not retired: %d", got)
	}
}

func TestProviderFailureKeepsEventsAndRecordsFailure(t *testing.T) {
	cases := []struct {
		name    string
		respond func(recordedRequest) (int, string)
		message string
	}{
		{"http error", func(recordedRequest) (int, string) { return http.StatusBadGateway, `{"secret":"provider detail"}` }, "LLM request failed (HTTP 502)"},
		{"transport error", func(recordedRequest) (int, string) { return 0, "" }, "LLM request failed"},
		{"redirect", func(recordedRequest) (int, string) { return http.StatusFound, "" }, "LLM request failed"},
		{"invalid payload", func(recordedRequest) (int, string) { return http.StatusOK, `not json` }, "LLM request failed"},
		{"oversized payload", func(recordedRequest) (int, string) {
			return http.StatusOK, `{"pad":"` + strings.Repeat("a", maxResponseBytes) + `"}`
		}, "LLM request failed"},
		{"no structured output", func(recordedRequest) (int, string) { return http.StatusOK, `{"choices":[]}` }, "LLM response did not contain structured output"},
		{"invalid model json", func(recordedRequest) (int, string) { return http.StatusOK, chatReply(`{"events":`) }, "LLM response was not valid JSON"},
		{"invalid event", func(recordedRequest) (int, string) {
			return http.StatusOK, chatReply(scheduleReply(songTitle, "2099-02-30", songClock, "scheduled"))
		}, "schedule extraction contained an invalid event"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f := messageFixture(t)
			f.scan()
			before := f.text(`SELECT id || title || starts_at || updated_at || raw_json FROM events WHERE deleted_at IS NULL`)
			f.provider.respond = test.respond
			f.requestReprocess("post-1")
			result := f.scan()
			if got := f.provider.count(); got != 1+llmMaxRetries+1 {
				t.Errorf("provider requests = %d, want one initial request and two retries", got-1)
			}
			for _, host := range f.provider.hosts() {
				if host != "provider.example.test" {
					t.Errorf("request left the configured provider: %s", host)
				}
			}
			if result.Status != "failed" || countOf(result.Counts, "failed") != 1 || countOf(result.Counts, "attempted") != 1 {
				t.Errorf("result = %q %v", result.Status, result.Counts)
			}
			if got := f.text(`SELECT id || title || starts_at || updated_at || raw_json FROM events WHERE deleted_at IS NULL`); got != before {
				t.Error("failed provider changed the known event")
			}
			if got := f.text(`SELECT status || '|' || last_error || '|' || reprocess_requested FROM post_llm_states`); got != "failed|"+test.message+"|0" {
				t.Errorf("post state = %q", got)
			}
			if got := f.text(`SELECT status || '|' || error || '|' || COALESCE(result_json,'null') FROM schedule_extractions`); got != "failed|"+test.message+"|null" {
				t.Errorf("extraction = %q", got)
			}
			if got := f.text(`SELECT last_status || '|' || last_error FROM llm_providers`); got != "failed|"+test.message {
				t.Errorf("provider status = %q", got)
			}
			f.assertNoSecretStored()

			// The failed extraction is not treated as processed: once its
			// first backoff has passed, the next scan tries it again.
			f.provider.respond = func(recordedRequest) (int, string) {
				return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
			}
			f.at(time.Now().Add(extractionBackoff[0]))
			if recovered := f.scan(); recovered.Status != "success" || countOf(recovered.Counts, "success") != 1 {
				t.Errorf("recovery = %q %v", recovered.Status, recovered.Counts)
			}
		})
	}
}

func TestProvidersAreTriedInQueueOrder(t *testing.T) {
	f := newFixture(t)
	f.addProvider("p-third", protocolResponses, "https://third.example.test/v1", "model-3", `["text"]`)
	f.addProvider("p-first", protocolChat, "https://first.example.test/v1", "model-1", `["text"]`)
	f.addProvider("p-second", protocolAnthropic, "https://second.example.test/v1", "model-2", `["text"]`)
	f.addProvider("p-image", protocolChat, "https://image.example.test/v1", "model-i", `["image"]`)
	f.addProvider("p-off", protocolChat, "https://off.example.test/v1", "model-o", `["text"]`)
	f.exec(`UPDATE llm_providers SET enabled=0 WHERE id='p-off'`)
	f.queue("p-off", "p-image", "p-first", "p-second", "p-third")
	f.addPost("post-1", "", "明日 21時から歌枠配信します", "2026-04-28T03:00:00.000Z", "")
	f.provider.respond = func(request recordedRequest) (int, string) {
		if strings.Contains(request.url, "second.example.test") {
			return http.StatusOK, `{"content":[{"type":"tool_use","name":"` + extractionToolName + `","input":` + scheduleReply(songTitle, songDate, songClock, "scheduled") + `}]}`
		}
		return http.StatusServiceUnavailable, `{}`
	}
	result := f.scan()
	want := []string{"first.example.test", "first.example.test", "first.example.test", "second.example.test"}
	if got := f.provider.hosts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("request order = %v, want %v", got, want)
	}
	if result.Status != "success" || countOf(result.Counts, "success") != 1 || countOf(result.Counts, "failed") != 0 {
		t.Errorf("result = %q %v", result.Status, result.Counts)
	}
	if got := f.text(`SELECT group_concat(model || ':' || status, ',') FROM (SELECT model, status FROM schedule_extractions ORDER BY id)`); got != "model-1:failed,model-2:success" {
		t.Errorf("extractions = %q", got)
	}
	if got := f.text(`SELECT group_concat(id || ':' || COALESCE(last_status,'-'), ',') FROM (SELECT id, last_status FROM llm_providers ORDER BY id)`); got != "p-first:failed,p-image:-,p-off:-,p-second:success,p-third:-" {
		t.Errorf("provider statuses = %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`); got != 1 {
		t.Errorf("events = %d", got)
	}

	// A target calls the catalog model it names.
	f.exec(`DELETE FROM llm_route_targets`)
	f.exec(`INSERT INTO llm_models (provider_id, model_id, tags_json, enabled, created_at, updated_at) VALUES ('p-second', 'model-catalog', '["text","reasoning"]', 1, ?, ?)`, testTimestamp, testTimestamp)
	f.exec(`INSERT INTO llm_route_targets (route, provider_id, model_id, priority, created_at, updated_at) VALUES (?, 'p-second', 'model-catalog', 0, ?, ?)`, routeQueue, testTimestamp, testTimestamp)
	f.scan()
	last := f.provider.requests[len(f.provider.requests)-1]
	if !strings.Contains(last.body, `"model":"model-catalog"`) {
		t.Errorf("catalog model was not used: %s", last.body[:80])
	}
}

func TestCancellationOverlayAnnotatesLockedEvent(t *testing.T) {
	f := messageFixture(t)
	f.scan()
	eventID := f.text(`SELECT id FROM events`)
	f.exec(`UPDATE events SET title='歌枠 特別版', provenance='manual', manual_locked=1 WHERE id=?`, eventID)
	schedule := f.text(`SELECT title || starts_on || starts_at || status || provenance || manual_locked FROM events WHERE id=?`, eventID)

	f.addPost("post-2", "", "5/3の配信は中止です", "2026-04-29T03:00:00.000Z", "")
	cancel := stringify(obj(
		"classification", "schedule",
		"events", []any{obj("title", "別名", "detail", nil, "date", songDate, "time", songClock, "endTime", nil,
			"timePrecision", "exact", "status", "cancelled", "eventType", "stream", "url", nil, "confidence", 0.7, "evidence", "中止")},
		"confidence", 0.7, "evidence", "中止です", "reason", "配信中止", "action", "cancel"))
	f.provider.respond = func(request recordedRequest) (int, string) {
		if strings.Contains(request.body, "中止") {
			return http.StatusOK, chatReply(cancel)
		}
		return http.StatusOK, chatReply(scheduleReply(songTitle, songDate, songClock, "scheduled"))
	}
	if result := f.scan(); result.Status != "success" {
		t.Fatalf("status = %q %v", result.Status, result.Counts)
	}
	overlay := `SELECT cancellation_status || '|' || COALESCE(cancellation_source,'') || '|' || COALESCE(cancellation_reason,'') || '|' ||
		COALESCE(cancellation_evidence,'') || '|' || COALESCE(cancellation_source_item_id,'') || '|' || COALESCE(cancellation_confidence,'') FROM events WHERE id=?`
	if got := f.text(overlay, eventID); got != "llm_suspected|llm|配信中止|中止です|post-2|0.7" {
		t.Errorf("overlay = %q", got)
	}
	if got := f.text(`SELECT title || starts_on || starts_at || status || provenance || manual_locked FROM events WHERE id=?`, eventID); got != schedule {
		t.Errorf("overlay changed schedule fields: %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM events`); got != 1 {
		t.Errorf("cancellation evidence created or retired events: %d", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM event_sources WHERE source_item_id='post-2'`); got != 0 {
		t.Errorf("cancellation post was attached as a schedule source: %d", got)
	}

	// Only an operator's confirmation is definitive, and the model cannot replace it.
	f.exec(`UPDATE events SET cancellation_status='manual_confirmed', cancellation_source='manual', cancellation_reason='operator',
		cancellation_evidence=NULL, cancellation_source_item_id=NULL, cancellation_confidence=NULL WHERE id=?`, eventID)
	f.requestReprocess("post-2")
	f.scan()
	if got := f.text(overlay, eventID); got != "manual_confirmed|manual|operator|||" {
		t.Errorf("manual confirmation was replaced: %q", got)
	}
}

func boardFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	f.addProvider("p-vision", protocolResponses, "https://provider.example.test/v1", "model-v", `["text","image"]`)
	f.queue("p-vision")
	// Only schedule images are under test: no post matches a keyword.
	f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_keywords', '["unmatched-keyword"]', ?)`, testTimestamp)
	f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_message_enabled', '0', ?)`, testTimestamp)
	sourceURL := f.addPost("board-post", "", "今週のスケジュールです", "2026-04-28T03:00:00.000Z", "")
	const imageURL = "https://pbs.twimg.com/media/synthetic-weekly.png"
	f.addCachedImage(imageURL, []byte(pngHeader))
	f.exec(`INSERT INTO assets (id, kind, url, source_url, week_start) VALUES ('schedule-2026-04-27', 'schedule', ?, ?, '2026-04-27')`, imageURL, sourceURL)
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, stringify(obj("output_text", `{"classification":"schedule","confidence":0.91,"evidence":"weekly grid","reason":"table of days"}`))
	}
	return f, imageURL
}

func TestScheduleImageVerificationApprovesAsset(t *testing.T) {
	f, _ := boardFixture(t)
	// A stream thumbnail whose post does not read as a board notice, and an
	// image an operator already labelled, never reach a model.
	thumbnailSource := f.addPost("stream-post", "", "今日は22時から配信します", "2026-04-27T03:00:00.000Z", "")
	f.addCachedImage("https://pbs.twimg.com/media/synthetic-thumbnail.png", []byte(pngHeader+"t"))
	f.exec(`INSERT INTO assets (id, kind, url, source_url, week_start) VALUES ('schedule-2026-04-20', 'schedule', 'https://pbs.twimg.com/media/synthetic-thumbnail.png', ?, '2026-04-20')`, thumbnailSource)
	f.addCachedImage("https://pbs.twimg.com/media/synthetic-labelled.png", []byte(pngHeader+"l"))
	f.exec(`INSERT INTO assets (id, kind, url, source_url, week_start) VALUES ('schedule-2026-04-13', 'schedule', 'https://pbs.twimg.com/media/synthetic-labelled.png', NULL, '2026-04-13')`)
	f.exec(`INSERT INTO schedule_asset_reviews (asset_id, llm_status, manual_status, manual_reason, updated_at) VALUES ('schedule-2026-04-13', 'pending', 'not_schedule', 'operator', ?)`, testTimestamp)

	result := f.scan()
	if f.provider.count() != 1 {
		t.Fatalf("provider requests = %d, want 1", f.provider.count())
	}
	request := f.provider.requests[0]
	if !strings.Contains(request.body, verificationToolName) || !strings.Contains(request.body, `"image_url":"data:image/png;base64,iVBORw0KGgoC"`) ||
		!strings.Contains(request.body, "Candidate schedule image. Source URL: https://x.com/synthetic_handle/status/board-post") {
		t.Errorf("unexpected verification request: %s", request.body)
	}
	assets, _ := result.Counts["assetVerification"].(map[string]any)
	if result.Status != "success" || countOf(assets, "success") != 1 || countOf(assets, "schedule") != 1 || countOf(assets, "skipped") != 1 || countOf(assets, "attempted") != 2 {
		t.Errorf("result = %q %v", result.Status, result.Counts)
	}
	review := `SELECT llm_status || '|' || COALESCE(llm_confidence,'') || '|' || COALESCE(llm_reason,'') || '|' || COALESCE(llm_evidence,'') || '|' ||
		COALESCE(llm_model,'') || '|' || manual_status || '|' || (llm_input_fingerprint IS NOT NULL) || '|' || (llm_checked_at IS NOT NULL) FROM schedule_asset_reviews WHERE asset_id=?`
	if got := f.text(review, "schedule-2026-04-27"); got != "schedule|0.91|table of days|weekly grid|model-v|unreviewed|1|1" {
		t.Errorf("approved review = %q", got)
	}
	if got := f.text(review, "schedule-2026-04-20"); got != "skipped||source_not_board|||unreviewed|0|0" {
		t.Errorf("gated review = %q", got)
	}
	if got := f.text(`SELECT llm_status || '|' || manual_status || '|' || manual_reason FROM schedule_asset_reviews WHERE asset_id='schedule-2026-04-13'`); got != "pending|not_schedule|operator" {
		t.Errorf("manual review was touched: %q", got)
	}

	// An unchanged image with an unchanged provider list is not sent again.
	second := f.scan()
	assets, _ = second.Counts["assetVerification"].(map[string]any)
	if f.provider.count() != 1 || countOf(assets, "cached") != 1 {
		t.Errorf("second verification = %d requests, %v", f.provider.count(), assets)
	}
	f.assertNoSecretStored()
}

func TestScheduleAssetWithoutImageIsSettledOnce(t *testing.T) {
	f, _ := boardFixture(t)
	// The legacy compatibility alias names no image and has no media row; a
	// URL-less row an operator already labelled keeps its label.
	f.exec(`INSERT INTO assets (id, kind, url, week_start, period_basis) VALUES ('weekly-schedule', 'schedule', '', '2026-04-20', 'legacy')`)
	f.exec(`INSERT INTO assets (id, kind, url, week_start, period_basis) VALUES ('schedule-2026-04-13', 'schedule', '', '2026-04-13', 'legacy')`)
	f.exec(`INSERT INTO schedule_asset_reviews (asset_id, llm_status, manual_status, manual_reason, updated_at) VALUES ('schedule-2026-04-13', 'pending', 'not_schedule', 'no image', ?)`, testTimestamp)

	first := f.scan()
	assets, _ := first.Counts["assetVerification"].(map[string]any)
	if f.provider.count() != 1 || countOf(assets, "success") != 1 || countOf(assets, "skipped") != 1 || countOf(assets, "attempted") != 2 {
		t.Fatalf("first scan = %v after %d requests", assets, f.provider.count())
	}
	review := `SELECT llm_status || '|' || COALESCE(llm_reason,'') || '|' || manual_status || '|' || COALESCE(manual_reason,'') || '|' || (llm_checked_at IS NOT NULL) FROM schedule_asset_reviews WHERE asset_id=?`
	if got := f.text(review, "weekly-schedule"); got != "skipped|no_image|unreviewed||0" {
		t.Errorf("alias review = %q", got)
	}
	if got := f.text(review, "schedule-2026-04-13"); got != "pending||not_schedule|no image|0" {
		t.Errorf("labelled review was touched: %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM assets WHERE id='weekly-schedule' AND url='' AND period_basis='legacy'`); got != 1 {
		t.Error("the legacy row was rewritten")
	}

	// A settled URL-less asset is not retried, even by a forced scan.
	second := f.scan()
	assets, _ = second.Counts["assetVerification"].(map[string]any)
	if countOf(assets, "skipped") != 0 || countOf(assets, "attempted") != 0 || countOf(assets, "cached") != 1 {
		t.Errorf("second scan = %v", assets)
	}
	forced, err := f.run.verifyPendingAssets(context.Background(), 200, true)
	if err != nil || countOf(forced, "skipped") != 0 {
		t.Errorf("forced scan = %v, %v", forced, err)
	}

	// A newer synced image replaces the alias and is verified as usual.
	const nextURL = "https://pbs.twimg.com/media/synthetic-next-week.png"
	f.addCachedImage(nextURL, []byte(pngHeader+"n"))
	sourceURL := f.text(`SELECT source_url FROM assets WHERE id='schedule-2026-04-27'`)
	f.exec(`UPDATE assets SET url=?, source_url=?, asset_version=asset_version+1 WHERE id='weekly-schedule'`, nextURL, sourceURL)
	f.scan()
	if got := f.text(review, "weekly-schedule"); got != "schedule|table of days|unreviewed||1" {
		t.Errorf("replaced alias review = %q", got)
	}
}

func TestScheduleImageVerdictIsDroppedWhenAssetChanges(t *testing.T) {
	f, _ := boardFixture(t)
	f.provider.respond = func(recordedRequest) (int, string) {
		// The synchronizer replaces the image while the model is deciding. The
		// write succeeds only because no transaction is held across the request.
		f.exec(`UPDATE assets SET url='https://pbs.twimg.com/media/synthetic-next.png', asset_version=asset_version+1 WHERE id='schedule-2026-04-27'`)
		return http.StatusOK, stringify(obj("output_text", `{"classification":"schedule","confidence":0.9,"evidence":"grid","reason":"table"}`))
	}
	result := f.scan()
	assets, _ := result.Counts["assetVerification"].(map[string]any)
	if countOf(assets, "stale") != 1 || countOf(assets, "success") != 0 {
		t.Errorf("asset summary = %v", assets)
	}
	if got := f.text(`SELECT llm_status FROM schedule_asset_reviews WHERE asset_id='schedule-2026-04-27'`); got == "schedule" {
		t.Error("a verdict for the previous image approved the replaced asset")
	}
}

func TestScheduleImageVerificationFailureIsRecorded(t *testing.T) {
	f, _ := boardFixture(t)
	f.provider.respond = func(recordedRequest) (int, string) { return http.StatusTooManyRequests, `{}` }
	result := f.scan()
	assets, _ := result.Counts["assetVerification"].(map[string]any)
	if result.Status != "failed" || countOf(assets, "failed") != 1 || f.provider.count() != llmMaxRetries+1 {
		t.Errorf("result = %q %v after %d requests", result.Status, result.Counts, f.provider.count())
	}
	if got := f.text(`SELECT llm_status || '|' || llm_reason || '|' || (llm_checked_at IS NOT NULL) FROM schedule_asset_reviews`); got != "failed|LLM request failed (HTTP 429)|1" {
		t.Errorf("review = %q", got)
	}
}

func TestDisabledExtractionShortCircuits(t *testing.T) {
	disabled := map[string]any{"attempted": 0, "success": 0, "cached": 0, "skipped": 0, "failed": 0, "disabled": true}
	t.Run("setting", func(t *testing.T) {
		f := messageFixture(t)
		f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_extraction_enabled', '0', ?)`, testTimestamp)
		result := f.scan()
		if result.Status != "success" || !reflect.DeepEqual(result.Counts, disabled) || f.provider.count() != 0 {
			t.Errorf("result = %q %v after %d requests", result.Status, result.Counts, f.provider.count())
		}
		if got := f.number(`SELECT COUNT(*) FROM post_llm_states`); got != 0 {
			t.Errorf("disabled scan wrote %d post states", got)
		}
	})
	t.Run("environment default", func(t *testing.T) {
		f := messageFixture(t)
		f.env["SCHEDULE_EXTRACTION_ENABLED"] = "false"
		if result := f.scan(); !reflect.DeepEqual(result.Counts, disabled) || f.provider.count() != 0 {
			t.Errorf("result = %v after %d requests", result.Counts, f.provider.count())
		}
		// A stored stage setting wins over the environment default.
		f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_message_enabled', '1', ?)`, testTimestamp)
		if result := f.scan(); countOf(result.Counts, "success") != 1 || f.provider.count() != 1 {
			t.Errorf("stage setting did not enable the message detector: %v", result.Counts)
		}
	})
	t.Run("board needs both stages", func(t *testing.T) {
		f, _ := boardFixture(t)
		f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_vision_enabled', '0', ?)`, testTimestamp)
		if result := f.scan(); !reflect.DeepEqual(result.Counts, disabled) || f.provider.count() != 0 {
			t.Errorf("result = %v after %d requests", result.Counts, f.provider.count())
		}
	})
}

func TestMessageAllPostsSelectsEveryPost(t *testing.T) {
	f := messageFixture(t)
	// Neither the keywords nor the local heuristic select this post.
	f.addPost("post-plain", "", "おはよう", "2026-04-27T03:00:00.000Z", "")
	f.scan()
	if got := f.number(`SELECT COUNT(*) FROM post_llm_states WHERE post_id = 'post-plain'`); got != 0 {
		t.Fatalf("heuristic scan selected the plain post")
	}
	f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_message_all_posts', '1', ?)`, testTimestamp)
	f.scan()
	if got := f.text(`SELECT status || '|' || route FROM post_llm_states WHERE post_id = 'post-plain'`); got != "success|schedule_message" {
		t.Errorf("plain post state = %q", got)
	}
	if f.provider.count() != 2 {
		t.Errorf("provider requests = %d, want 2", f.provider.count())
	}
}

func TestMessageAllPostsFollowsEnvironmentDefault(t *testing.T) {
	f := messageFixture(t)
	f.addPost("post-plain", "", "おはよう", "2026-04-27T03:00:00.000Z", "")
	f.env["SCHEDULE_MESSAGE_ALL_POSTS"] = "1"
	f.scan()
	if got := f.number(`SELECT COUNT(*) FROM post_llm_states WHERE post_id = 'post-plain'`); got != 1 {
		t.Errorf("environment default did not select the plain post")
	}
}

func TestReprocessRequestedPostIsRerun(t *testing.T) {
	f := messageFixture(t)
	// The post matches no detector and extraction is disabled: only the
	// operator's request selects it.
	f.exec(`UPDATE posts SET text='おはよう' WHERE id='post-1'`)
	f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_extraction_enabled', '0', ?)`, testTimestamp)
	if result := f.scan(); result.Counts["disabled"] != true {
		t.Fatalf("unrequested post was scanned: %v", result.Counts)
	}
	f.requestReprocess("post-1")
	result := f.scan()
	if f.provider.count() != 1 || countOf(result.Counts, "success") != 1 || countOf(result.Counts, "messageAttempted") != 1 {
		t.Fatalf("requested post was not processed: %v after %d requests", result.Counts, f.provider.count())
	}
	if got := f.text(`SELECT status || '|' || route || '|' || reprocess_requested FROM post_llm_states`); got != "success|schedule_message|0" {
		t.Errorf("post state = %q", got)
	}

	// The operator's route choice is honoured; the board path draws from the
	// same queue, so the first capable target takes it.
	f.addProvider("p-board", protocolChat, "https://board.example.test/v1", "model-b", `["text"]`)
	f.queue("p-board")
	f.exec(`UPDATE post_llm_states SET reprocess_requested=1, route=? WHERE post_id='post-1'`, routeBoard)
	f.scan()
	if got := f.provider.hosts(); len(got) != 2 || got[1] != got[0] {
		t.Errorf("forced rerun requests = %v", got)
	}
	if got := f.text(`SELECT status || '|' || route || '|' || reprocess_requested FROM post_llm_states`); got != "success|schedule_board|0" {
		t.Errorf("post state = %q", got)
	}
}

func TestPostsWithoutUsableInputAreSkipped(t *testing.T) {
	const imageURL = "https://pbs.twimg.com/media/synthetic-post.png"
	post := func(f *fixture) {
		f.addPost("post-img", "", "今週の配信予定です", "2026-04-28T03:00:00.000Z", stringify(obj("media_urls", []any{imageURL})))
		f.exec(`INSERT INTO media_assets (id, source, source_url, status, created_at, updated_at) VALUES (?, 'x', ?, 'pending', ?, ?)`,
			mediaIDForSourceURL(imageURL), imageURL, testTimestamp, testTimestamp)
		f.exec(`INSERT INTO media_links (media_id, owner_type, owner_id, role, position, created_at, updated_at) VALUES (?, 'post', 'post-img', 'post-image', 0, ?, ?)`,
			mediaIDForSourceURL(imageURL), testTimestamp, testTimestamp)
	}
	state := `SELECT status || '|' || last_error FROM post_llm_states WHERE post_id='post-img'`
	t.Run("pending image is not downgraded to text", func(t *testing.T) {
		f := newFixture(t)
		f.addProvider("p-vision", protocolChat, "https://provider.example.test/v1", "model-v", `["text","image"]`)
		f.queue("p-vision")
		post(f)
		result := f.scan()
		if f.provider.count() != 0 || countOf(result.Counts, "skipped") != 1 || f.text(state) != "skipped|media_pending" {
			t.Errorf("result = %v, state = %q, requests = %d", result.Counts, f.text(state), f.provider.count())
		}
	})
	t.Run("text-only provider cannot take an image post", func(t *testing.T) {
		f := newFixture(t)
		f.addProvider("p-text", protocolChat, "https://provider.example.test/v1", "model-t", `["text"]`)
		f.queue("p-text")
		post(f)
		f.exec(`DELETE FROM media_links`)
		f.exec(`DELETE FROM media_assets`)
		f.addCachedImage(imageURL, []byte(pngHeader))
		f.exec(`INSERT INTO media_links (media_id, owner_type, owner_id, role, position, created_at, updated_at) VALUES (?, 'post', 'post-img', 'post-image', 0, ?, ?)`,
			mediaIDForSourceURL(imageURL), testTimestamp, testTimestamp)
		f.scan()
		if f.provider.count() != 0 || f.text(state) != "skipped|no_compatible_provider" {
			t.Errorf("state = %q, requests = %d", f.text(state), f.provider.count())
		}
	})
	t.Run("undecryptable credential", func(t *testing.T) {
		f := messageFixture(t)
		f.run.secretsKey = "synthetic-other-key"
		f.scan()
		if got := f.text(`SELECT status || '|' || last_error FROM post_llm_states`); f.provider.count() != 0 || got != "skipped|missing_api_key" {
			t.Errorf("state = %q, requests = %d", got, f.provider.count())
		}
	})
	t.Run("no route", func(t *testing.T) {
		f := messageFixture(t)
		f.exec(`DELETE FROM llm_route_targets`)
		result := f.scan()
		if got := f.text(`SELECT status || '|' || last_error FROM post_llm_states`); result.Status != "success" || got != "skipped|no_route_target" {
			t.Errorf("state = %q, status = %q", got, result.Status)
		}
	})
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	f := messageFixture(t)
	f.addPost("post-2", "", "明後日 20時から雑談配信", "2026-04-27T03:00:00.000Z", "")
	f.requestReprocess("post-1")
	ctx, cancel := context.WithCancel(context.Background())
	f.provider.respond = func(recordedRequest) (int, string) {
		cancel()
		return http.StatusInternalServerError, `{}`
	}
	result, err := f.run.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if f.provider.count() != 1 {
		t.Errorf("provider requests after cancellation = %d, want 1", f.provider.count())
	}
	if result.Status != "failed" {
		t.Errorf("status = %q", result.Status)
	}
	// Nothing is left in "running", and the consumed request is restored.
	if got := f.text(`SELECT status || '|' || last_error || '|' || reprocess_requested FROM post_llm_states WHERE post_id='post-1'`); got != "failed|LLM request cancelled|1" {
		t.Errorf("post state = %q", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM schedule_extractions WHERE status='running'`); got != 0 {
		t.Errorf("running extractions = %d", got)
	}
	if got := f.number(`SELECT COUNT(*) FROM post_llm_states WHERE post_id='post-2'`); got != 0 {
		t.Errorf("the scan continued to the next post")
	}

	if _, err = f.run.Run(ctx); !errors.Is(err, context.Canceled) || f.provider.count() != 1 {
		t.Errorf("a cancelled context still ran: %v, %d requests", err, f.provider.count())
	}
}
