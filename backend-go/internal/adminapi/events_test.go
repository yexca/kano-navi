package adminapi

import (
	"regexp"
	"testing"
)

func TestManualEventLifecycleLocksAndTombstones(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	for body, message := range map[string]string{
		`{}`:                                    "startsOn is required",
		`{"startsOn":"2026-02-30","title":"x"}`: "startsOn is invalid",
		`{"startsOn":"2026-03-02"}`:             "title is required",
		`{"startsOn":"2026-03-02","title":"x","startsAt":"soon"}`:                                                     "startsAt is invalid",
		`{"startsOn":"2026-03-02","title":"x","startsAt":"2026-03-02T16:00:00.000Z"}`:                                 "startsAt must fall on startsOn in Asia/Tokyo",
		`{"startsOn":"2026-03-02","title":"x","endsAt":"2026-03-02T12:00:00.000Z"}`:                                   "endsAt requires startsAt",
		`{"startsOn":"2026-03-02","title":"x","startsAt":"2026-03-02T11:00:00.000Z","endsAt":"2026-03-02T10:00:00Z"}`: "endsAt must not be earlier than startsAt",
		`{"startsOn":"2026-03-02","title":"x","eventType":"party"}`:                                                   "eventType is invalid",
		`{"startsOn":"2026-03-02","title":"x","cancellationStatus":"maybe"}`:                                          "cancellationStatus is invalid",
		`{"startsOn":"2026-03-02","title":"x","cancellationStatus":"manual_confirmed"}`:                               "cancellationReason is required",
		`{"startsOn":"2026-03-02","title":"x","url":"ftp://files.example.test/a"}`:                                    "url must use HTTP or HTTPS",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/events", body); code != 400 || payload["error"] != "invalid_input" || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	revision := c.text(t, `SELECT value FROM app_settings WHERE key='dashboard_revision'`)
	code, payload := c.call(t, "POST", "/api/admin/events", `{"title":" Synthetic stream ","startsOn":"2026-03-02","startsAt":"2026-03-02T20:00:00+09:00","endsAt":"2026-03-02T13:00:00.000Z","eventType":"stream","url":"https://Stream.Example.test"}`)
	if code != 201 {
		t.Fatalf("create=%d %v", code, payload)
	}
	event := payload["event"].(map[string]any)
	id := event["id"].(string)
	if !regexp.MustCompile(`^manual-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("id=%s", id)
	}
	for field, want := range map[string]any{
		"title": "Synthetic stream", "source": "manual", "sourceItemId": id, "sourceKey": id, "provenance": "manual", "manualLocked": true,
		"startsAt": "2026-03-02T11:00:00.000Z", "endsAt": "2026-03-02T13:00:00.000Z", "timePrecision": "exact", "eventType": "stream",
		"detail": nil, "status": nil, "url": "https://stream.example.test/", "cancellationStatus": "none", "deletedAt": nil, "timezone": "Asia/Tokyo",
	} {
		if event[field] != want {
			t.Fatalf("created %s=%v, want %v", field, event[field], want)
		}
	}
	if c.text(t, `SELECT source_key FROM event_sources WHERE event_id=?`, id) != id || c.text(t, `SELECT value FROM app_settings WHERE key='dashboard_revision'`) == revision {
		t.Fatal("create must record its source identity and bump the revision")
	}
	// Editing an unknown ID never creates a row.
	if code, payload = c.call(t, "PUT", "/api/admin/events/unknown", `{"title":"x","startsOn":"2026-03-02"}`); code != 404 || payload["error"] != "event_not_found" {
		t.Fatalf("unknown update=%d %v", code, payload)
	}
	if c.text(t, `SELECT CAST(COUNT(*) AS TEXT) FROM events`) != "1" {
		t.Fatal("an unknown update created an event")
	}
	code, payload = c.call(t, "PUT", "/api/admin/events/"+id, `{"title":"Edited","startsOn":"2026-03-03","detail":"note","cancellationStatus":"manual_confirmed","cancellationReason":" operator decision "}`)
	edited := payload["event"].(map[string]any)
	if code != 200 || edited["startsAt"] != nil || edited["endsAt"] != nil || edited["timePrecision"] != "unknown" || edited["detail"] != "note" ||
		edited["cancellationStatus"] != "manual_confirmed" || edited["cancellationSource"] != "manual" || edited["cancellationReason"] != "operator decision" || edited["cancellationAt"] == nil {
		t.Fatalf("edit=%d %v", code, edited)
	}
	c.exec(t, `UPDATE events SET cancellation_at='2026-01-01T00:00:00.000Z' WHERE id=?`, id)
	_, payload = c.call(t, "PUT", "/api/admin/events/"+id, `{"title":"Edited","startsOn":"2026-03-03","cancellationStatus":"manual_confirmed","cancellationReason":"still cancelled"}`)
	if again := payload["event"].(map[string]any); again["cancellationAt"] != "2026-01-01T00:00:00.000Z" {
		t.Fatalf("a confirmed cancellation must keep its decision time: %v", again["cancellationAt"])
	}
	if code, _ = c.call(t, "DELETE", "/api/admin/events/"+id, ""); code != 204 {
		t.Fatalf("delete=%d", code)
	}
	if c.text(t, `SELECT manual_locked||'/'||(deleted_at IS NOT NULL) FROM events WHERE id=?`, id) != "1/1" {
		t.Fatal("delete must leave a locked tombstone")
	}
	if _, payload = c.call(t, "GET", "/api/admin/events", ""); len(payload["items"].([]any)) != 0 {
		t.Fatalf("a tombstone is hidden by default: %v", payload)
	}
	if _, payload = c.call(t, "GET", "/api/admin/events?deleted=1", ""); len(payload["items"].([]any)) != 1 {
		t.Fatalf("deleted=1 includes tombstones: %v", payload)
	}
	if code, payload = c.call(t, "POST", "/api/admin/events/"+id+"/confirm", ""); code != 404 || payload["error"] != "event_not_found" {
		t.Fatalf("confirming a tombstone=%d %v", code, payload)
	}
	if code, payload = c.call(t, "DELETE", "/api/admin/events/unknown", ""); code != 404 || payload["error"] != "event_not_found" {
		t.Fatalf("unknown delete=%d %v", code, payload)
	}
	// Saving a tombstoned entry restores it, still locked.
	if code, payload = c.call(t, "PUT", "/api/admin/events/"+id, `{"title":"Restored","startsOn":"2026-03-03"}`); code != 200 || payload["event"].(map[string]any)["deletedAt"] != nil {
		t.Fatalf("restore=%d %v", code, payload)
	}
}

func TestConfirmingAutomaticEventKeepsIdentityAndCancellationOverlay(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	c.exec(t, `INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,timezone,time_precision,status,event_type,url,
 provenance,manual_locked,confidence,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_source_item_id,
 cancellation_confidence,cancellation_at,created_at,updated_at)
 VALUES('automatic-1','x','synthetic-post','slot-1','Automatic stream','detected','2026-03-05','2026-03-05T21:00:00+09:00','Asia/Tokyo','exact',
 'scheduled','stream','https://video.example.test/watch','automatic',0,0.9,'llm_suspected','llm','model suspects cancellation','quoted evidence',
 'synthetic-cancel-post',0.7,'2026-03-04T00:00:00.000Z','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`)
	code, payload := c.call(t, "POST", "/api/admin/events/automatic-1/confirm", "")
	if code != 200 {
		t.Fatalf("confirm=%d %v", code, payload)
	}
	event := payload["event"].(map[string]any)
	for field, want := range map[string]any{
		"source": "x", "sourceItemId": "synthetic-post", "sourceKey": "slot-1", "provenance": "manual", "manualLocked": true,
		"title": "Automatic stream", "detail": "detected", "status": "scheduled", "startsAt": "2026-03-05T12:00:00.000Z",
		"cancellationStatus": "llm_suspected", "cancellationSource": "llm", "cancellationReason": "model suspects cancellation",
		"cancellationEvidence": "quoted evidence", "cancellationSourceItemId": "synthetic-cancel-post", "cancellationConfidence": 0.7,
		"cancellationAt": "2026-03-04T00:00:00.000Z",
	} {
		if event[field] != want {
			t.Fatalf("confirmed %s=%v, want %v", field, event[field], want)
		}
	}
	if c.text(t, `SELECT source||'/'||source_item_id||'/'||source_key FROM event_sources WHERE event_id='automatic-1'`) != "manual/automatic-1/confirmed" {
		t.Fatal("confirmation must add a manual source identity")
	}
	// Dismissing the overlay clears every cancellation field.
	_, payload = c.call(t, "PUT", "/api/admin/events/automatic-1", `{"title":"Automatic stream","startsOn":"2026-03-05","cancellationStatus":"none"}`)
	event = payload["event"].(map[string]any)
	for _, field := range []string{"cancellationSource", "cancellationReason", "cancellationEvidence", "cancellationSourceItemId", "cancellationConfidence", "cancellationAt"} {
		if event[field] != nil {
			t.Fatalf("dismissed %s=%v", field, event[field])
		}
	}
	// llm_suspected cannot be introduced by an operator save.
	_, payload = c.call(t, "PUT", "/api/admin/events/automatic-1", `{"title":"Automatic stream","startsOn":"2026-03-05","cancellationStatus":"llm_suspected"}`)
	if payload["event"].(map[string]any)["cancellationStatus"] != "none" {
		t.Fatalf("operator introduced an LLM overlay: %v", payload)
	}
}
