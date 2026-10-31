package adminapi

import "testing"

func TestSyncSettings(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/sync-settings", "")
	settings, _ := payload["settings"].(map[string]any)
	fields, _ := payload["fields"].([]any)
	if code != 200 || settings["xMaxStatusRequests"] != float64(12) || settings["fetchTimeoutMs"] != float64(7000) || settings["llmModelAttempts"] != float64(3) || len(fields) != 11 {
		t.Fatalf("status=%d %v", code, payload)
	}
	if first := fields[0].(map[string]any); first["field"] != "xBootstrapDays" || first["default"] != float64(7) || first["min"] != float64(1) || first["max"] != float64(30) {
		t.Fatalf("field=%v", first)
	}
	for body, message := range map[string]string{
		`{"fetchTimeoutMs":999}`:       "fetchTimeoutMs must be an integer from 1000 to 60000",
		`{"xRefreshKnown":2.5}`:        "xRefreshKnown must be an integer from 0 to 5",
		`{"youtubeBootstrapVideos":6}`: "youtubeBootstrapVideos is not a sync setting",
	} {
		if code, payload := c.call(t, "PUT", "/api/admin/sync-settings", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	code, payload = c.call(t, "PUT", "/api/admin/sync-settings", `{"xMaxStatusRequests":0,"translationScanLimit":40}`)
	settings, _ = payload["settings"].(map[string]any)
	if code != 200 || settings["xMaxStatusRequests"] != float64(0) || settings["translationScanLimit"] != float64(40) || settings["fetchMaxPages"] != float64(4) {
		t.Fatalf("save=%d %v", code, payload)
	}
	if got := c.text(t, `SELECT value FROM app_settings WHERE key='sync_translation_scan_limit'`); got != "40" {
		t.Fatalf("stored scan limit=%s", got)
	}
}
