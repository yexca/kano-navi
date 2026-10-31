package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

func newTranslatorFixture(t *testing.T) (*fixture, *Translator) {
	t.Helper()
	f := newFixture(t)
	translator := NewTranslator(f.store, Options{
		SecretsKey: testKey,
		HTTPClient: &http.Client{Transport: f.provider},
		Getenv:     func(key string) string { return f.env[key] },
	})
	translator.e.sleep = func(context.Context, time.Duration) error { return nil }
	return f, translator
}

// queueTranslation appends each provider's first catalog model to the
// translation queue.
func (f *fixture) queueTranslation(providerIDs ...string) {
	f.t.Helper()
	for _, id := range providerIDs {
		f.exec(`INSERT INTO llm_route_targets (route, provider_id, model_id, priority, created_at, updated_at)
			VALUES (?, ?, (SELECT model_id FROM llm_models WHERE provider_id = ? ORDER BY rowid LIMIT 1),
			(SELECT COUNT(*) FROM llm_route_targets WHERE route = ?), ?, ?)`, routeTranslate, id, id, routeTranslate, testTimestamp, testTimestamp)
	}
}

func translationReply(language, english, chinese string) string {
	return chatReply(stringify(obj("sourceLanguage", language, "english", english, "simplifiedChinese", chinese)))
}

func recent(hours int) string {
	return time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format("2006-01-02T15:04:05.000Z")
}

func runTranslator(t *testing.T, translator *Translator) Result {
	t.Helper()
	result, err := translator.Run(context.Background())
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	return result
}

func TestTranslatorReadsOnlyTheTranslationQueue(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("chat", protocolChat, "https://chat.example.test/v1", "synthetic-chat", `["text"]`)
	f.queue("chat")
	f.addPost("1", "", "今日は歌枠です！", recent(1), "")
	result := runTranslator(t, translator)
	if f.provider.count() != 0 || result.Counts["disabled"] != true || result.Counts["reason"] != "no_route_target" {
		t.Fatalf("a schedule-only queue must not translate: %d %v", f.provider.count(), result.Counts)
	}

	f.queueTranslation("chat")
	f.provider.respond = func(request recordedRequest) (int, string) {
		if !strings.Contains(request.body, translationToolName) || !strings.Contains(request.body, "Kind: X post") {
			t.Errorf("unexpected request body: %s", request.body)
		}
		return http.StatusOK, translationReply("ja", "Singing stream today!", "今天是歌回！")
	}
	result = runTranslator(t, translator)
	if result.Status != "success" || result.Counts["success"] != 1 || f.provider.count() != 1 {
		t.Fatalf("result=%v requests=%d", result.Counts, f.provider.count())
	}
	if got := f.text(`SELECT text FROM translations WHERE owner_id = '1' AND locale = 'en' AND origin = 'machine'`); got != "Singing stream today!" {
		t.Fatalf("en=%q", got)
	}
	if got := f.text(`SELECT model_id FROM translations WHERE owner_id = '1' AND locale = 'zh-CN'`); got != "synthetic-chat" {
		t.Fatalf("model=%q", got)
	}
	if got := f.text(`SELECT source_language FROM translation_states WHERE owner_id = '1'`); got != "ja" {
		t.Fatalf("language=%q", got)
	}
	// A complete post is not sent again.
	runTranslator(t, translator)
	if f.provider.count() != 1 {
		t.Fatalf("complete post was translated again: %d", f.provider.count())
	}
}

func TestTranslatorSkipsLocalesInTheSourceLanguageAndEmptyText(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("chat", protocolChat, "https://chat.example.test/v1", "synthetic-chat", `["text"]`)
	f.queueTranslation("chat")
	f.addPost("english", "", "Thank you for watching!", recent(1), "")
	f.addPost("links", "", "https://example.test/a #鹿乃まほろ @synthetic_handle ✨", recent(2), "")
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, translationReply("en", "", "感谢观看！")
	}
	result := runTranslator(t, translator)
	if f.provider.count() != 1 || result.Counts["success"] != 1 || result.Counts["skipped"] != 1 {
		t.Fatalf("result=%v requests=%d", result.Counts, f.provider.count())
	}
	if n := f.number(`SELECT COUNT(*) FROM translations WHERE owner_id = 'english' AND locale = 'en'`); n != 0 {
		t.Fatalf("an English post needs no English translation")
	}
	if got := f.text(`SELECT source_language FROM translation_states WHERE owner_id = 'links'`); got != "none" {
		t.Fatalf("links language=%q", got)
	}
	summary, err := f.store.TranslationSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if posts := summary[sqlite.TranslationOwnerPost]; posts.Total != 2 || posts.Incomplete != 0 {
		t.Fatalf("summary=%v", summary)
	}
	runTranslator(t, translator)
	if f.provider.count() != 1 {
		t.Fatalf("finished posts were sent again: %d", f.provider.count())
	}
}

func TestTranslatorFailsOverAndRecordsInvalidResults(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("first", protocolChat, "https://first.example.test/v1", "first-model", `["text"]`)
	f.addProvider("second", protocolChat, "https://second.example.test/v1", "second-model", `["text"]`)
	f.addProvider("vision", protocolChat, "https://vision.example.test/v1", "vision-model", `["image"]`)
	f.queueTranslation("vision", "first", "second")
	f.addPost("1", "", "明日も配信します", recent(1), "")
	f.provider.respond = func(request recordedRequest) (int, string) {
		if strings.Contains(request.url, "first.example.test") {
			// A missing locale is an invalid result.
			return http.StatusOK, translationReply("ja", "I will stream tomorrow too", "")
		}
		return http.StatusOK, translationReply("ja", "Streaming tomorrow too", "明天也会直播")
	}
	result := runTranslator(t, translator)
	if result.Counts["success"] != 1 || f.provider.count() != 3 {
		t.Fatalf("result=%v requests=%d hosts=%v", result.Counts, f.provider.count(), f.provider.hosts())
	}
	if hosts := f.provider.hosts(); hosts[0] != "first.example.test" || hosts[2] != "second.example.test" {
		t.Fatalf("an image-only model must be skipped and the first retried: %v", hosts)
	}
	if got := f.text(`SELECT provider_id FROM translations WHERE owner_id = '1' AND locale = 'zh-CN'`); got != "second" {
		t.Fatalf("provider=%q", got)
	}

	f.addPost("2", "", "おやすみ", recent(2), "")
	f.provider.respond = func(recordedRequest) (int, string) { return http.StatusInternalServerError, `{}` }
	result = runTranslator(t, translator)
	if result.Status != "failed" || result.Counts["failed"] != 1 {
		t.Fatalf("result=%v", result.Counts)
	}
	if got := f.text(`SELECT status FROM translation_states WHERE owner_id = '2'`); got != "failed" {
		t.Fatalf("state=%q", got)
	}
	if n := f.number(`SELECT COUNT(*) FROM translations WHERE owner_id = '2'`); n != 0 {
		t.Fatalf("a failed post must keep no rows")
	}
}

func TestAnthropicTranslationUsesJSONWithoutThinking(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("anth", protocolAnthropic, "https://anth.example.test", "claude-haiku-5-5", `["text"]`)
	f.queueTranslation("anth")
	f.addPost("1", "", "おはよう", recent(1), "")
	f.provider.respond = func(request recordedRequest) (int, string) {
		var body map[string]any
		if err := json.Unmarshal([]byte(request.body), &body); err != nil {
			t.Fatal(err)
		}
		output := body["output_config"].(map[string]any)
		format := output["format"].(map[string]any)
		if body["tools"] != nil || body["tool_choice"] != nil || format["type"] != "json_schema" || output["effort"] != "low" || body["thinking"].(map[string]any)["type"] != "disabled" || body["max_tokens"] != float64(1024) {
			t.Fatalf("translation request=%v", body)
		}
		return http.StatusOK, stringify(obj("stop_reason", "end_turn", "usage", obj("input_tokens", 120, "output_tokens", 40), "content", []any{
			obj("type", "thinking", "thinking", "synthetic private reasoning"),
			obj("type", "text", "text", stringify(obj("sourceLanguage", "ja", "english", "Good morning", "simplifiedChinese", "早上好"))),
		}))
	}
	result := runTranslator(t, translator)
	if result.Status != "success" || f.provider.count() != 1 {
		t.Fatalf("result=%v requests=%d", result, f.provider.count())
	}
	entry := result.Counts["diagnostics"].([]any)[0].(map[string]any)
	if entry["stopReason"] != "end_turn" || entry["inputTokens"] != 120 || entry["outputTokens"] != 40 || entry["model"] != "claude-haiku-5-5" || strings.Contains(stringify(entry), "private reasoning") {
		t.Fatalf("diagnostic=%v", entry)
	}
}

func TestTranslationTruncationRaisesBudgetWithoutSavingPartialText(t *testing.T) {
	for _, protocol := range []string{protocolAnthropic, protocolChat, protocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			f, translator := newTranslatorFixture(t)
			f.addProvider("main", protocol, "https://translate.example.test", "synthetic-model", `["text"]`)
			f.queueTranslation("main")
			f.addPost("1", "", "今日は配信します", recent(1), "")
			var budgets []int
			f.provider.respond = func(request recordedRequest) (int, string) {
				body, err := parseJSON([]byte(request.body))
				if err != nil {
					t.Fatal(err)
				}
				key := map[string]string{protocolAnthropic: "max_tokens", protocolChat: "max_completion_tokens", protocolResponses: "max_output_tokens"}[protocol]
				budgets = append(budgets, int(property(body, key).(float64)))
				if f.number(`SELECT COUNT(*) FROM translations`) != 0 {
					t.Fatal("a truncated translation was saved")
				}
				value := stringify(obj("sourceLanguage", "ja", "english", "Streaming today", "simplifiedChinese", "今天直播"))
				// Even valid JSON must not be saved with a truncated stop reason.
				truncated := len(budgets) < 3
				switch protocol {
				case protocolAnthropic:
					reason := "end_turn"
					if truncated {
						reason = "max_tokens"
					}
					return 200, stringify(obj("stop_reason", reason, "content", []any{obj("type", "text", "text", value)}))
				case protocolChat:
					reason := "stop"
					if truncated {
						reason = "length"
					}
					return 200, stringify(obj("choices", []any{obj("finish_reason", reason, "message", obj("content", value))}))
				default:
					status := "completed"
					if truncated {
						status = "incomplete"
					}
					return 200, stringify(obj("status", status, "incomplete_details", obj("reason", "max_output_tokens"), "output_text", value))
				}
			}
			result := runTranslator(t, translator)
			if result.Status != "success" || !reflect.DeepEqual(budgets, []int{1024, 2048, 4000}) || f.number(`SELECT COUNT(*) FROM translations`) != 2 {
				t.Fatalf("result=%v budgets=%v", result, budgets)
			}
		})
	}
}

func TestTranslationRepairAndFallbackResetTheRequest(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	for _, id := range []string{"first", "second"} {
		f.addProvider(id, protocolChat, "https://"+id+".example.test", "synthetic-"+id, `["text"]`)
	}
	f.queueTranslation("first", "second")
	f.addPost("1", "", "明日の配信", recent(1), "")
	f.provider.respond = func(request recordedRequest) (int, string) {
		repair := strings.Contains(request.body, "previous response failed validation")
		if strings.Contains(request.url, "first.example.test") {
			if repair != (f.provider.count() == 2) {
				t.Fatal("missing repair instruction")
			}
			return 200, translationReply("ja", "Tomorrow's stream", "")
		}
		if repair {
			t.Fatal("fallback inherited a repair for another model")
		}
		return 200, translationReply("ja", "Tomorrow's stream", "明天的直播")
	}
	result := runTranslator(t, translator)
	if result.Status != "success" || f.provider.count() != 3 {
		t.Fatalf("result=%v", result)
	}
}

func TestTranslationRepairKeepsUnchangedSongTitlesNonEmpty(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("main", protocolChat, "https://translate.example.test", "synthetic-model", `["text"]`)
	f.queueTranslation("main")
	const title = "星のうた - 架空アーティスト cover."
	f.exec(`INSERT INTO videos(id,source,title,published_at,url,kind) VALUES ('song','youtube',?,?,'https://video.example.test/song','VIDEO')`, title, recent(1))
	f.provider.respond = func(request recordedRequest) (int, string) {
		if !strings.Contains(request.body, "even when the whole title stays unchanged") {
			t.Fatal("song-title instruction missing")
		}
		if f.provider.count() == 1 {
			return 200, translationReply("ja", "", title)
		}
		if !strings.Contains(request.body, "missing_translation_en") || !strings.Contains(request.body, "copy it into the complete displayed text") {
			t.Fatal("missing-locale repair is not explicit")
		}
		return 200, translationReply("ja", title, title)
	}
	result := runTranslator(t, translator)
	if result.Status != "success" || f.provider.count() != 2 || f.text(`SELECT text FROM translations WHERE owner_id='song' AND locale='en'`) != title {
		t.Fatalf("result=%v requests=%d", result, f.provider.count())
	}
}

func TestTranslationRetriesOnlyTransientRequestsAndKeepsKnownRows(t *testing.T) {
	for _, check := range []struct{ status, calls, waits int }{
		{401, 1, 0}, {400, 1, 0}, {429, 3, 2}, {503, 3, 2}, {0, 3, 2},
	} {
		t.Run(strconv.Itoa(check.status), func(t *testing.T) {
			f, translator := newTranslatorFixture(t)
			f.addProvider("main", protocolChat, "https://translate.example.test", "synthetic-model", `["text"]`)
			f.queueTranslation("main")
			f.addPost("1", "", "おはよう", recent(1), "")
			f.provider.respond = func(recordedRequest) (int, string) { return 200, translationReply("ja", "Good morning", "早上好") }
			runTranslator(t, translator)
			before := f.text(`SELECT group_concat(text) FROM translations`)
			if _, err := f.store.RequestTranslations(context.Background(), "post", []string{"1"}); err != nil {
				t.Fatal(err)
			}
			initial, waits := f.provider.count(), 0
			translator.e.sleep = func(context.Context, time.Duration) error { waits++; return nil }
			f.provider.respond = func(recordedRequest) (int, string) {
				return check.status, `{"message":"synthetic private provider message"}`
			}
			result := runTranslator(t, translator)
			if result.Status != "failed" || f.provider.count()-initial != check.calls || waits != check.waits || f.text(`SELECT group_concat(text) FROM translations`) != before {
				t.Fatalf("result=%v calls=%d waits=%d", result, f.provider.count()-initial, waits)
			}
			if strings.Contains(f.text(`SELECT last_error FROM translation_states`), "private") {
				t.Fatal("provider payload was stored")
			}
		})
	}
}

func TestTranslationCancellationStopsBackoff(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("main", protocolChat, "https://translate.example.test", "synthetic-model", `["text"]`)
	f.queueTranslation("main")
	f.addPost("1", "", "おはよう", recent(1), "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	translator.e.sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	_, err := translator.Run(ctx)
	if !errors.Is(err, context.Canceled) || f.provider.count() != 1 {
		t.Fatalf("err=%v requests=%d", err, f.provider.count())
	}
}

func TestTranslatorKeepsOperatorAndPlatformTranslations(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("chat", protocolChat, "https://chat.example.test/v1", "synthetic-chat", `["text"]`)
	f.queueTranslation("chat")
	ctx := context.Background()
	for _, video := range []struct{ id, source, title string }{
		{"localized", "youtube", "【歌枠】秋の夜"},
		{"plain", "youtube", "【雑談】おはよう"},
		{"bilibili", "bilibili", "鹿乃 翻唱"},
	} {
		f.exec(`INSERT INTO videos (id, source, title, published_at, url, kind) VALUES (?, ?, ?, ?, ?, 'VIDEO')`,
			video.id, video.source, video.title, recent(3), "https://video.example.test/"+video.id)
	}
	for locale, text := range map[string]string{"en": "[Singing] Autumn night", "zh-CN": "【歌回】秋夜"} {
		if err := f.store.UpsertPlatformTranslation(ctx, sqlite.TranslationWrite{
			OwnerType: sqlite.TranslationOwnerVideo, OwnerID: "localized", Field: "title", Locale: locale,
			Platform: "youtube", Text: text, SourceText: "【歌枠】秋の夜",
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.provider.respond = func(request recordedRequest) (int, string) {
		if !strings.Contains(request.body, "【雑談】おはよう") {
			t.Errorf("only the untranslated YouTube title may be sent: %s", request.body)
		}
		return http.StatusOK, translationReply("ja", "[Chat] Good morning", "【杂谈】早上好")
	}
	runTranslator(t, translator)
	if f.provider.count() != 1 {
		t.Fatalf("requests=%d", f.provider.count())
	}

	// A changed title retires the machine rows but keeps the manual one.
	f.addPost("1", "", "おはよう", recent(1), "")
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, translationReply("ja", "Good morning", "早上好")
	}
	runTranslator(t, translator)
	if err := f.store.SetManualTranslation(ctx, sqlite.TranslationOwnerPost, "1", "en", "Morning!"); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE posts SET text = 'おはようございます' WHERE id = '1'`)
	items, err := f.store.AdminTranslations(ctx, sqlite.TranslationOwnerPost, []string{"1"})
	if err != nil {
		t.Fatal(err)
	}
	item := items["1"]
	if item.Status != "incomplete" || item.MachineStatus != "outdated" || item.Locales[0].Text == nil || *item.Locales[0].Text != "Morning!" || !item.Locales[0].ManualStale || item.Locales[1].Status != "stale" {
		t.Fatalf("admin=%+v en=%+v zh=%+v", item, item.Locales[0], item.Locales[1])
	}
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, translationReply("ja", "Good morning to you", "早上好呀")
	}
	runTranslator(t, translator)
	dashboard, err := f.store.GetDashboard(ctx, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var post sqlite.Post
	for _, candidate := range dashboard.Posts {
		if candidate.ID == "1" {
			post = candidate
		}
	}
	en, zh := post.Translations.Locales["en"], post.Translations.Locales["zh-CN"]
	if en.Origin != "manual" || en.Text != "Morning!" || zh.Origin != "machine" || zh.Text != "早上好呀" {
		t.Fatalf("translations=%+v", post.Translations)
	}
	for _, video := range dashboard.Videos {
		if video.ID == "localized" {
			if title := video.Translations.Locales["en"]; title.Origin != "platform" || title.Platform == nil || *title.Platform != "youtube" {
				t.Fatalf("video=%+v", video.Translations)
			}
		}
		if video.ID == "bilibili" && (len(video.Translations.Locales) != 0 || video.Translations.SourceLanguage == nil || *video.Translations.SourceLanguage != "zh") {
			t.Fatalf("a Bilibili title is Chinese and not translated: %+v", video.Translations)
		}
	}
}

func TestTranslationRequestsBypassTheLookback(t *testing.T) {
	f, translator := newTranslatorFixture(t)
	f.addProvider("chat", protocolChat, "https://chat.example.test/v1", "synthetic-chat", `["text"]`)
	f.queueTranslation("chat")
	f.env["TRANSLATION_LOOKBACK_DAYS"] = "7"
	f.addPost("old", "", "昔のお知らせ", recent(24*30), "")
	f.provider.respond = func(recordedRequest) (int, string) {
		return http.StatusOK, translationReply("ja", "An old notice", "以前的通知")
	}
	runTranslator(t, translator)
	if f.provider.count() != 0 {
		t.Fatalf("a post outside the lookback was translated")
	}
	queued, err := f.store.RequestMissingTranslations(context.Background(), "")
	if err != nil || queued != 1 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	runTranslator(t, translator)
	if f.provider.count() != 1 || f.number(`SELECT requested FROM translation_states WHERE owner_id = 'old'`) != 0 {
		t.Fatalf("the request was not answered: %d", f.provider.count())
	}
	// A forced request retranslates a complete post.
	if n, err := f.store.RequestTranslations(context.Background(), sqlite.TranslationOwnerPost, []string{"old"}); err != nil || n != 1 {
		t.Fatalf("request=%d err=%v", n, err)
	}
	runTranslator(t, translator)
	if f.provider.count() != 2 {
		t.Fatalf("forced request was not translated: %d", f.provider.count())
	}
}
