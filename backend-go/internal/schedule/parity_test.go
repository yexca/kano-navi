package schedule

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testdata/parity.json was recorded from the original TypeScript extractor
// against the same synthetic posts, providers and canned model replies used
// here, before that implementation was removed. It is a frozen reference: the
// Go step must keep sending byte-identical requests and leaving byte-identical
// identities, fingerprints and stored JSON behind, so rows written by earlier
// releases stay compatible.
type parityData struct {
	First    map[string]any `json:"first"`
	Requests []struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"requests"`
	AfterFirst     map[string][]map[string]any `json:"afterFirst"`
	Second         map[string]any              `json:"second"`
	SecondRequests int                         `json:"secondRequests"`
	AfterSecond    map[string][]map[string]any `json:"afterSecond"`
}

var parityQueries = map[string]string{
	"events":       `SELECT id, source, source_item_id, source_key, title, detail, starts_on, starts_at, ends_at, timezone, time_precision, status, event_type, url, is_upcoming, provenance, manual_locked, confidence, extraction_id, cancellation_status, cancellation_source, cancellation_reason, cancellation_evidence, cancellation_source_item_id, cancellation_confidence, deleted_at IS NOT NULL AS deleted, raw_json FROM events ORDER BY id`,
	"eventSources": `SELECT event_id, source, source_item_id, source_key, url, raw_json, deleted_at IS NOT NULL AS deleted FROM event_sources ORDER BY id`,
	"extractions":  `SELECT id, source, source_item_id, content_fingerprint, extractor_version, model, status, result_json, error FROM schedule_extractions ORDER BY id`,
	"postStates":   `SELECT post_id, status, route, last_attempt_at IS NOT NULL AS attempted, last_processed_at IS NOT NULL AS processed, last_error, last_extraction_id, reprocess_requested FROM post_llm_states ORDER BY post_id`,
	"providers":    `SELECT id, last_status, last_error FROM llm_providers ORDER BY id`,
}

func (f *fixture) table(query string) []map[string]any {
	f.t.Helper()
	rows, err := f.db.Query(query)
	if err != nil {
		f.t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	columns, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err = rows.Scan(targets...); err != nil {
			f.t.Fatalf("scan: %v", err)
		}
		row := map[string]any{}
		for i, column := range columns {
			switch value := values[i].(type) {
			case int64:
				row[column] = float64(value)
			case []byte:
				row[column] = string(value)
			default:
				row[column] = value
			}
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		f.t.Fatalf("rows: %v", err)
	}
	return out
}

func jsonShape(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err = json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func (f *fixture) compareTables(label string, queries map[string]string, expected map[string][]map[string]any) {
	f.t.Helper()
	for name, query := range queries {
		want := expected[name]
		for _, row := range want {
			// JSON.stringify escapes the lone surrogate left by a title cut
			// inside a surrogate pair; SQLite and Go both hold U+FFFD instead.
			if raw, ok := row["raw_json"].(string); ok {
				row["raw_json"] = strings.ReplaceAll(raw, `\ud83d"`, "\ufffd\"")
			}
		}
		got := f.table(query)
		if len(got) != len(want) {
			f.t.Fatalf("%s %s: %d rows, want %d", label, name, len(got), len(want))
		}
		for i := range want {
			for column, value := range want[i] {
				if !reflect.DeepEqual(got[i][column], value) {
					f.t.Errorf("%s %s[%d].%s:\n got  %v\n want %v", label, name, i, column, got[i][column], value)
				}
			}
		}
	}
}

func TestParityWithTypeScriptExtractor(t *testing.T) {
	raw, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var data parityData
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Requests) != 6 || len(data.AfterFirst["events"]) != 3 || len(data.AfterFirst["extractions"]) != 4 {
		t.Fatalf("unexpected fixture shape")
	}
	replies := map[string]string{}
	for _, row := range data.AfterFirst["extractions"] {
		if result, ok := row["result_json"].(string); ok {
			replies[row["source_item_id"].(string)] = result
		}
	}

	f := newFixture(t)
	f.addProvider("p-resp", protocolResponses, "https://provider.example.test/v1", "model-a", `["text","image"]`)
	f.addProvider("p-chat", protocolChat, "https://chat.example.test", "model-b", `["text","image"]`)
	f.addProvider("p-anth", protocolAnthropic, "https://anthropic.example.test/v1/", "model-c", `["text","image"]`)
	f.route(routeMessage, "p-resp")
	f.route(routeBoard, "p-chat", "p-anth")
	for _, item := range []struct{ id, label, text, publishedAt string }{
		{"post-a", "SCHEDULE / 日程", "今週のスケジュール 😀 です\n5/3 21:00 歌枠 <b>&\u2028", "2026-04-28T03:00:00.000Z"},
		{"post-c", "", "予定していた5/3の配信は中止です", "2026-04-27T03:00:00.000Z"},
		{"post-b", "", "明日 20時から歌います", "2026-04-26T03:00:00.000Z"},
		{"post-d", "", "おはよう", "2026-04-25T03:00:00.000Z"},
	} {
		f.addPost(item.id, item.label, item.text, item.publishedAt, stringify(obj("id", item.id, "text", item.text)))
	}
	f.provider.respond = func(request recordedRequest) (int, string) {
		switch {
		case strings.Contains(request.url, "chat.example.test"):
			if strings.Contains(request.body, "中止") {
				return http.StatusInternalServerError, `{"error":"synthetic"}`
			}
			return http.StatusOK, chatReply(replies["post-a"])
		case strings.Contains(request.url, "anthropic.example.test"):
			return http.StatusOK, `{"content":[{"type":"text","text":"x"},{"type":"tool_use","name":"kano_schedule_extraction","input":` + replies["post-c"] + `}]}`
		}
		return http.StatusOK, stringify(obj("output", []any{
			obj("type", "reasoning"),
			obj("type", "message", "content", []any{obj("type", "output_text", "text", replies["post-b"])}),
		}))
	}

	first := f.scan()
	if first.Status != "success" {
		t.Errorf("first status = %q", first.Status)
	}
	if got := jsonShape(t, first.Counts); !reflect.DeepEqual(got, jsonShape(t, data.First)) {
		t.Errorf("first counts:\n got  %v\n want %v", got, data.First)
	}
	if len(f.provider.requests) != len(data.Requests) {
		t.Fatalf("%d provider requests, want %d", len(f.provider.requests), len(data.Requests))
	}
	for i, want := range data.Requests {
		got := f.provider.requests[i]
		if got.url != want.URL {
			t.Errorf("request %d url = %q, want %q", i, got.url, want.URL)
		}
		if got.body != want.Body {
			t.Errorf("request %d body differs:\n got  %s\n want %s", i, got.body, want.Body)
		}
		if got.headers.Get("Content-Type") != want.Headers["content-type"] {
			t.Errorf("request %d content type = %q", i, got.headers.Get("Content-Type"))
		}
		if _, anthropic := want.Headers["x-api-key"]; anthropic {
			if got.headers.Get("X-Api-Key") != testProviderKey || got.headers.Get("Anthropic-Version") != want.Headers["anthropic-version"] || got.headers.Get("Authorization") != "" {
				t.Errorf("request %d anthropic headers are wrong", i)
			}
		} else if got.headers.Get("Authorization") != "Bearer "+testProviderKey || got.headers.Get("X-Api-Key") != "" {
			t.Errorf("request %d bearer header is wrong", i)
		}
	}
	f.compareTables("first", parityQueries, data.AfterFirst)

	second := f.scan()
	if got := jsonShape(t, second.Counts); !reflect.DeepEqual(got, jsonShape(t, data.Second)) {
		t.Errorf("second counts:\n got  %v\n want %v", got, data.Second)
	}
	if got := len(f.provider.requests) - len(data.Requests); got != data.SecondRequests {
		t.Errorf("second scan made %d requests, want %d", got, data.SecondRequests)
	}
	f.compareTables("second", parityQueries, data.AfterSecond)
}

type mediaParityData struct {
	Files map[string]struct {
		RelativePath string `json:"relativePath"`
		SHA256       string `json:"sha256"`
		Base64       string `json:"base64"`
		MediaID      string `json:"mediaId"`
	} `json:"files"`
	MediaLinks []struct {
		MediaID   string `json:"media_id"`
		OwnerType string `json:"owner_type"`
		OwnerID   string `json:"owner_id"`
		Role      string `json:"role"`
		Position  int    `json:"position"`
	} `json:"mediaLinks"`
	MediaAssets []struct {
		ID        string `json:"id"`
		SourceURL string `json:"source_url"`
	} `json:"mediaAssets"`
	First    map[string]any `json:"first"`
	Requests []struct {
		URL  string `json:"url"`
		Body string `json:"body"`
	} `json:"requests"`
	AfterFirst     map[string][]map[string]any `json:"afterFirst"`
	Second         map[string]any              `json:"second"`
	SecondRequests []any                       `json:"secondRequests"`
	AfterSecond    map[string][]map[string]any `json:"afterSecond"`
}

var mediaParityQueries = map[string]string{
	"extractions": `SELECT id, source_item_id, content_fingerprint, extractor_version, model, status, result_json, error FROM schedule_extractions ORDER BY id`,
	"postStates":  `SELECT post_id, status, route, last_error, last_extraction_id, reprocess_requested FROM post_llm_states ORDER BY post_id`,
	"reviews":     `SELECT asset_id, llm_status, llm_confidence, llm_reason, llm_evidence, llm_model, llm_checked_at IS NOT NULL AS checked, llm_input_fingerprint, manual_status FROM schedule_asset_reviews ORDER BY asset_id`,
	"providers":   `SELECT id, last_status, last_error FROM llm_providers ORDER BY id`,
	"events":      `SELECT id, source_item_id, title, raw_json FROM events ORDER BY id`,
}

// The image fixture covers what the text one cannot: media identities, the
// legacy cache namespace, capability filtering, image request bodies, pending
// media, and schedule image verification with its fingerprint, provider
// failover and duplicate propagation.
func TestParityWithTypeScriptImagePaths(t *testing.T) {
	raw, err := os.ReadFile("testdata/parity-media.json")
	if err != nil {
		t.Fatal(err)
	}
	var data mediaParityData
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Requests) != 8 || len(data.AfterFirst["reviews"]) != 5 {
		t.Fatalf("unexpected fixture shape")
	}
	const (
		boardURL   = "https://pbs.twimg.com/media/synthetic-board.png?name=orig#frag"
		otherURL   = "https://pbs.twimg.com/media/synthetic-other.png"
		pendingURL = "https://pbs.twimg.com/media/synthetic-pending.png"
	)
	f := newFixture(t)
	f.addProvider("p-text", protocolChat, "https://chat.example.test/v1", "model-t", `["text"]`)
	f.addProvider("p-vision", protocolResponses, "https://provider.example.test/v1", "model-v", `["text","image"]`)
	f.addProvider("p-anth", protocolAnthropic, "https://anthropic.example.test", "model-c", `["text","image"]`)
	f.addProvider("p-chat", protocolChat, "https://vision-chat.example.test/v1", "model-w", `["image"]`)
	f.route(routeVision, "p-text", "p-vision", "p-anth", "p-chat")
	f.exec(`INSERT INTO app_settings (key, value, updated_at) VALUES ('schedule_message_enabled', '0', ?)`, testTimestamp)
	for _, item := range []struct {
		id, postType, label, text, publishedAt string
		media                                  []any
	}{
		{"img-a", "notice", "SCHEDULE / 日程", "今週のスケジュールです", "2026-04-28T03:00:00.000Z", []any{boardURL, otherURL}},
		{"img-b", "notice", "SCHEDULE / 日程", "", "2026-04-27T03:00:00.000Z", []any{otherURL}},
		{"img-c", "daily", "", "来週の予定表", "2026-04-26T03:00:00.000Z", []any{pendingURL}},
		{"img-d", "daily", "", "今日は22時から配信予定です", "2026-04-25T03:00:00.000Z", []any{otherURL}},
	} {
		f.addPost(item.id, item.label, item.text, item.publishedAt, stringify(obj("id", item.id, "media_urls", item.media)))
		f.exec(`UPDATE posts SET type = ?, media_url = ? WHERE id = ?`, item.postType, item.media[0], item.id)
	}
	for _, asset := range data.MediaAssets {
		f.exec(`INSERT INTO media_assets (id, source, source_url, status, created_at, updated_at)
			VALUES (?, 'x', ?, 'pending', ?, ?)`, asset.ID, asset.SourceURL, testTimestamp, testTimestamp)
	}
	for sourceURL, file := range data.Files {
		if got := mediaIDForSourceURL(sourceURL); got != file.MediaID {
			t.Fatalf("media id for %s = %s, want %s", sourceURL, got, file.MediaID)
		}
		content, err := base64.StdEncoding.DecodeString(file.Base64)
		if err != nil {
			t.Fatal(err)
		}
		// The original writer used the legacy `sha256/` namespace, which lives
		// below data/cache/media.
		target := filepath.Join(f.store.DataRoot(), "cache", "media", filepath.FromSlash(file.RelativePath))
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, content, 0o644); err != nil {
			t.Fatal(err)
		}
		f.exec(`UPDATE media_assets SET status='ready', cache_path=?, mime_type='image/png', sha256=?, byte_size=? WHERE id=?`,
			file.RelativePath, file.SHA256, len(content), file.MediaID)
	}
	for _, link := range data.MediaLinks {
		f.exec(`INSERT INTO media_links (media_id, owner_type, owner_id, role, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, link.MediaID, link.OwnerType, link.OwnerID, link.Role, link.Position, testTimestamp, testTimestamp)
	}
	status := func(id string) any { return "https://x.com/synthetic_handle/status/" + id }
	for index, asset := range []struct {
		id, url, week string
		source        any
	}{
		{"schedule-2026-04-27", boardURL, "2026-04-27", status("img-a")},
		{"weekly-schedule", boardURL, "2026-04-27", status("img-a")},
		{"schedule-2026-04-20", otherURL, "2026-04-20", status("img-d")},
		{"schedule-2026-04-13", pendingURL, "2026-04-13", status("img-c")},
		{"schedule-2026-04-06", otherURL, "2026-04-06", nil},
	} {
		f.exec(`INSERT INTO assets (id, kind, url, source_url, week_start) VALUES (?, 'schedule', ?, ?, ?)`,
			asset.id, asset.url, asset.source, asset.week)
		f.exec(`INSERT INTO schedule_asset_reviews (asset_id, llm_status, manual_status, updated_at)
			VALUES (?, 'pending', 'unreviewed', ?)`, asset.id, fmt.Sprintf("2026-04-01T00:00:0%d.000Z", index+1))
	}

	replies := map[string]string{}
	for _, row := range data.AfterFirst["extractions"] {
		replies[row["source_item_id"].(string)] = row["result_json"].(string)
	}
	verdicts := []string{
		`{"classification":"Schedule ","confidence":0.93,"evidence":" weekly table ","reason":"grid of days"}`,
		`{"classification":"not_schedule","confidence":0.2,"evidence":"thumbnail","reason":"single card"}`,
	}
	verified := 0
	f.provider.respond = func(request recordedRequest) (int, string) {
		if strings.Contains(request.body, verificationToolName) {
			if strings.Contains(request.url, "provider.example.test") && verified > 0 {
				return http.StatusServiceUnavailable, `{"error":"synthetic"}`
			}
			verdict := verdicts[verified]
			verified++
			if strings.Contains(request.url, "anthropic.example.test") {
				return http.StatusOK, `{"content":[{"type":"tool_use","name":"` + verificationToolName + `","input":` + verdict + `}]}`
			}
			return http.StatusOK, stringify(obj("output_text", verdict))
		}
		if strings.Contains(request.body, `"type":"input_text"`) {
			return http.StatusOK, stringify(obj("output_text", replies["img-a"]))
		}
		return http.StatusOK, stringify(obj("output_text", replies["img-b"]))
	}

	first := f.scan()
	if first.Status != "success" {
		t.Errorf("first status = %q", first.Status)
	}
	if got := jsonShape(t, first.Counts); !reflect.DeepEqual(got, jsonShape(t, data.First)) {
		t.Errorf("first counts:\n got  %v\n want %v", got, data.First)
	}
	if len(f.provider.requests) != len(data.Requests) {
		t.Fatalf("%d provider requests, want %d", len(f.provider.requests), len(data.Requests))
	}
	for i, want := range data.Requests {
		if got := f.provider.requests[i]; got.url != want.URL || got.body != want.Body {
			t.Errorf("request %d differs:\n got  %s %s\n want %s %s", i, got.url, got.body, want.URL, want.Body)
		}
	}
	f.compareTables("first", mediaParityQueries, data.AfterFirst)

	second := f.scan()
	if got := jsonShape(t, second.Counts); !reflect.DeepEqual(got, jsonShape(t, data.Second)) {
		t.Errorf("second counts:\n got  %v\n want %v", got, data.Second)
	}
	if got := len(f.provider.requests) - len(data.Requests); got != len(data.SecondRequests) {
		t.Errorf("second scan made %d requests, want %d", got, len(data.SecondRequests))
	}
	f.compareTables("second", mediaParityQueries, data.AfterSecond)
}
