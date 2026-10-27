package config

import "testing"

func TestPublicOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"":                              "",
		"  ":                            "",
		"https://kano.example.invalid":  "https://kano.example.invalid",
		"https://kano.example.invalid/": "https://kano.example.invalid",
		"http://127.0.0.1:7657":         "http://127.0.0.1:7657",
	} {
		if got, err := PublicOrigin(raw); err != nil || got != want {
			t.Fatalf("%q => %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"kano.example.invalid", "ftp://kano.example.invalid", "https://kano.example.invalid/path", "https://kano.example.invalid?x=1", "javascript:alert(1)"} {
		if got, err := PublicOrigin(raw); err == nil {
			t.Fatalf("%q accepted as %q", raw, got)
		}
	}
}
