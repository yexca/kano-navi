package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestMediaFreshETagValidators(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		matched bool
	}{
		{"exact", `"abc"`, true},
		{"weak", `W/"abc"`, true},
		{"other", `"other"`, false},
		{"multiple", `"other", "abc"`, true},
		{"wildcard", `*`, true},
		{"prefix must not match", `"prefix-abc"`, false},
		{"suffix must not match", `"abc-suffix"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("If-None-Match", tt.value)
			if got := mediaFresh(r, "abc", time.Time{}); got != tt.matched {
				t.Fatalf("mediaFresh(%q)=%v, want %v", tt.value, got, tt.matched)
			}
		})
	}
}
