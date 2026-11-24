package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// syntheticBotToken has the Bot API's <bot id>:<secret> shape.
const syntheticBotToken = "synthetic-bot:synthetic-secret"

func call(t *testing.T, admin *Admin, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "/api/admin"+path, reader)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	httpx.NewAdminMux(admin.AdminRoutes()).ServeHTTP(w, r)
	payload := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	return w, payload
}

func storedSetting(t *testing.T, store *sqlite.Store) string {
	t.Helper()
	var value string
	if err := store.DB().QueryRow(`SELECT value FROM app_settings WHERE key = ?`, settingKey).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFreshSettingsHaveEveryRuleAndNoChannels(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true, Production: true}))
	w, payload := call(t, admin, http.MethodGet, "/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if payload["state"] != StateOn || len(payload["channels"].([]any)) != 0 || len(payload["rules"].([]any)) != len(Catalog) {
		t.Fatalf("payload=%v", payload)
	}
	first := payload["rules"].([]any)[0].(map[string]any)
	setting := first["setting"].(map[string]any)
	if first["code"] != RuleRunFailedStreak || setting["enabled"] != true || setting["threshold"] != float64(3) {
		t.Fatalf("rule=%v", first)
	}
}

func TestChannelSecretsAreWriteOnlyAndSealed(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true}))
	body := `{"channels":[{"name":"Phone","type":"telegram","enabled":true,"chatId":"-100123","secret":"` + syntheticBotToken + `"}]}`
	w, payload := call(t, admin, http.MethodPut, "/alerts", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), syntheticBotToken) || strings.Contains(storedSetting(t, store), syntheticBotToken) {
		t.Fatal("the bot token left the process")
	}
	channel := payload["channels"].([]any)[0].(map[string]any)
	if channel["hasSecret"] != true || channel["secretUnreadable"] != false {
		t.Fatalf("channel=%v", channel)
	}
	// A save without the secret keeps it.
	id := channel["id"].(string)
	w, payload = call(t, admin, http.MethodPut, "/alerts", `{"channels":[{"id":"`+id+`","name":"Phone 2","type":"telegram","enabled":true,"chatId":"-100123"}]}`)
	if w.Code != http.StatusOK || payload["channels"].([]any)[0].(map[string]any)["hasSecret"] != true {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	settings, err := admin.service.Settings(t.Context())
	if err != nil || settings.Channels[0].Secret != syntheticBotToken {
		t.Fatalf("kept secret lost: %v", err)
	}
}

func TestASecretIsNotKeptForANewAddress(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true}))
	w, payload := call(t, admin, http.MethodPut, "/alerts", `{"channels":[{"name":"Hook","type":"webhook","enabled":true,"url":"https://example.com/a","secret":"synthetic-signing-key"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	id := payload["channels"].([]any)[0].(map[string]any)["id"].(string)
	w, payload = call(t, admin, http.MethodPut, "/alerts", `{"channels":[{"id":"`+id+`","name":"Hook","type":"webhook","enabled":true,"url":"https://example.org/a"}]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(payload["message"].(string), "entered again") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestPrivateAndPlainAddressesAreRefused(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true}))
	private := (&url.URL{Scheme: "https", Host: "[fd00::10]", Path: "/hook"}).String()
	tailnet := (&url.URL{Scheme: "https", Host: "[fd7a:115c:a1e0::1]"}).String()
	for _, body := range []string{
		fmt.Sprintf(`{"channels":[{"type":"webhook","enabled":true,"url":%q}]}`, private),
		`{"channels":[{"type":"webhook","enabled":true,"url":"http://example.com/hook"}]}`,
		`{"channels":[{"type":"ntfy","enabled":true,"server":"https://ntfy.localhost","topic":"alerts"}]}`,
		`{"channels":[{"type":"ntfy","enabled":true,"server":"https://example.com#x","topic":"alerts"}]}`,
		fmt.Sprintf(`{"channels":[{"type":"ntfy","enabled":true,"server":%q,"topic":"alerts"}]}`, tailnet),
		`{"heartbeat":{"enabled":true,"url":"https://127.0.0.1/push"}}`,
		`{"rules":{"run_failed_streak":{"enabled":true,"threshold":99}}}`,
		`{"rules":{"unknown_rule":{"enabled":true}}}`,
	} {
		if w, _ := call(t, admin, http.MethodPut, "/alerts", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", body, w.Code, w.Body)
		}
	}
}

func TestHeartbeatAddressIsSealed(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true}))
	const address = "https://example.com/api/push/synthetic-push-token?status=up"
	w, payload := call(t, admin, http.MethodPut, "/alerts", `{"heartbeat":{"enabled":true,"intervalMinutes":5,"url":"`+address+`"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "synthetic-push-token") || strings.Contains(storedSetting(t, store), "synthetic-push-token") {
		t.Fatal("the heartbeat address left the process")
	}
	if heartbeat := payload["heartbeat"].(map[string]any); heartbeat["hasUrl"] != true || heartbeat["enabled"] != true {
		t.Fatalf("heartbeat=%v", heartbeat)
	}
}

func TestAChangedSecretsKeyMarksTheChannelUnreadable(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true}))
	body := `{"channels":[{"name":"Phone","type":"telegram","enabled":true,"chatId":"-100123","secret":"` + syntheticBotToken + `"}]}`
	if w, _ := call(t, admin, http.MethodPut, "/alerts", body); w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	service := New(Options{Store: store, Key: "another-synthetic-key", Enabled: true})
	settings, err := service.Settings(t.Context())
	if err != nil || !settings.Channels[0].Unreadable || settings.Channels[0].usable() {
		t.Fatalf("channel=%+v err=%v", settings.Channels, err)
	}
	findings, err := service.unreadableSecrets(t.Context(), settings)
	if err != nil || len(findings) != 1 || findings[0].Subject != "alerts" {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	other := NewAdmin(service)
	id := settings.Channels[0].ID
	if w, _ := call(t, other, http.MethodPost, "/alerts/channels/"+id+"/test", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("test of an unreadable channel: status=%d", w.Code)
	}
}

func TestWebhookAddressIsSealedAndKept(t *testing.T) {
	store := openStore(t)
	admin := NewAdmin(New(Options{Store: store, Key: testKey, Enabled: true}))
	const address = "https://example.com/hooks/synthetic-hook-token"
	w, payload := call(t, admin, http.MethodPut, "/alerts", `{"channels":[{"name":"Hook","type":"webhook","enabled":true,"url":"`+address+`"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "synthetic-hook-token") || strings.Contains(storedSetting(t, store), "synthetic-hook-token") {
		t.Fatal("the webhook address left the process")
	}
	channel := payload["channels"].([]any)[0].(map[string]any)
	if channel["urlHost"] != "example.com" {
		t.Fatalf("channel=%v", channel)
	}
	// A save without the address keeps it.
	id := channel["id"].(string)
	if w, _ = call(t, admin, http.MethodPut, "/alerts", `{"channels":[{"id":"`+id+`","name":"Hook 2","type":"webhook","enabled":true}]}`); w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	reloaded := New(Options{Store: store, Key: testKey, Enabled: true})
	settings, err := reloaded.Settings(t.Context())
	if err != nil || settings.Channels[0].URL != address || settings.Channels[0].Name != "Hook 2" {
		t.Fatalf("channels=%+v err=%v", settings.Channels, err)
	}
}

func TestAcknowledgingAnIncidentTakesItOffTheBadgeUntilItReturns(t *testing.T) {
	store := openStore(t)
	service, now := newTestService(t, store, false, Signals{})
	configure(t, service)
	admin := NewAdmin(service)
	ctx := context.Background()
	workflow := sqlite.SourceScheduleWorkflow("x")
	enableSchedule(t, store, "x", now.at.Add(-time.Hour))
	for index := range 3 {
		addRun(t, store, fmt.Sprintf("run-%d", index), workflow, "failed", "", now.at.Add(-time.Duration(30*(3-index))*time.Minute))
	}
	service.Tick(ctx)
	badge := func() int {
		t.Helper()
		count, err := store.OpenAlertIncidents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	first := incidents(t, store)[0]
	if badge() != 1 || first.AcknowledgedAt != nil {
		t.Fatalf("badge=%d incident=%+v", badge(), first)
	}

	path := fmt.Sprintf("/alerts/incidents/%d/acknowledge", first.ID)
	w, payload := call(t, admin, http.MethodPost, path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	listed := payload["incidents"].([]any)[0].(map[string]any)
	if listed["acknowledgedAt"] != formatTime(now.at) || badge() != 0 {
		t.Fatalf("incident=%v badge=%d", listed, badge())
	}

	// A later pass while the problem holds keeps the acknowledgement, and
	// acknowledging again keeps its first time.
	now.at = now.at.Add(time.Minute)
	service.Tick(ctx)
	if w, _ = call(t, admin, http.MethodPost, path, ""); w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if got := incidents(t, store)[0].AcknowledgedAt; got == nil || *got != formatTime(now.at.Add(-time.Minute)) || badge() != 0 {
		t.Fatalf("acknowledgedAt=%v badge=%d", got, badge())
	}

	if w, _ = call(t, admin, http.MethodDelete, path, ""); w.Code != http.StatusOK || badge() != 1 {
		t.Fatalf("status=%d badge=%d", w.Code, badge())
	}
	if w, _ = call(t, admin, http.MethodPost, path, ""); w.Code != http.StatusOK || badge() != 0 {
		t.Fatalf("status=%d badge=%d", w.Code, badge())
	}

	// Once resolved it cannot be acknowledged; the same problem returning
	// opens a new incident that counts again.
	addRun(t, store, "run-ok", workflow, "succeeded", "", now.at)
	now.at = now.at.Add(time.Minute)
	service.Tick(ctx)
	if w, _ = call(t, admin, http.MethodDelete, path, ""); w.Code != http.StatusNotFound {
		t.Fatalf("resolved incident status=%d", w.Code)
	}
	for index := range 3 {
		addRun(t, store, fmt.Sprintf("run-again-%d", index), workflow, "failed", "", now.at.Add(time.Duration(index+1)*time.Second))
	}
	now.at = now.at.Add(time.Minute)
	service.Tick(ctx)
	found := incidents(t, store)
	if len(found) != 2 || found[1].Status != "open" || found[1].AcknowledgedAt != nil || badge() != 1 {
		t.Fatalf("incidents=%+v badge=%d", found, badge())
	}
	if w, _ = call(t, admin, http.MethodPost, "/alerts/incidents/not-a-number/acknowledge", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
}
