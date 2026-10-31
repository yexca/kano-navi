package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"testing"
)

func TestRequestLogOnlyRecordsAllowedFields(t *testing.T) {
	store, _ := openFixture(t)
	var output bytes.Buffer
	handler := RequestLog(NewHandler(store), &output)
	for _, tc := range []struct {
		method, path, route string
		status              int
	}{
		{"GET", "/api/health?private=synthetic", "/api/health", 200},
		{"POST", "/api/dashboard", "/api/dashboard", 405},
		{"PRIVATE", "/api/private-value?private=synthetic", "unmatched", 404},
		{"GET", "/api/dashboard/revision", "/api/dashboard/revision", 503},
	} {
		if tc.status == 503 {
			_ = store.Close()
		}
		output.Reset()
		response := request(t, handler, tc.method, tc.path)
		var record map[string]any
		decoder := json.NewDecoder(&output)
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(record))
		for key := range record {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if !reflect.DeepEqual(keys, []string{"duration_ms", "method", "response_bytes", "route", "status"}) {
			t.Fatalf("unexpected log fields: %v", keys)
		}
		wantMethod := tc.method
		if wantMethod == "PRIVATE" {
			wantMethod = "OTHER"
		}
		if record["route"] != tc.route || record["method"] != wantMethod || record["status"] != float64(tc.status) || record["response_bytes"] != float64(response.Body.Len()) || record["duration_ms"].(float64) < 0 {
			t.Fatalf("unexpected request metrics: %v", record)
		}
		if decoder.More() {
			t.Fatal("more than one log record for a request")
		}
		if response.Code != tc.status || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatal("logging changed the HTTP response")
		}
	}
}

var _ http.ResponseWriter = (*responseMetrics)(nil)
