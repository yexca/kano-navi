package config

import "testing"

func TestPublicOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                "",
		"  ":                              "",
		"https://kano.example.invalid":    "https://kano.example.invalid",
		"https://kano.example.invalid/":   "https://kano.example.invalid",
		"https://Kano.Example.INVALID":    "https://kano.example.invalid",
		"http://127.0.0.1:7657":           "http://127.0.0.1:7657",
		"http://[::1]:7657":               "http://[::1]:7657",
		"https://xn--n8j.example.invalid": "https://xn--n8j.example.invalid",
	} {
		if got, err := PublicOrigin(raw); err != nil || got != want {
			t.Fatalf("%q => %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{
		"kano.example.invalid", "ftp://kano.example.invalid", "https://kano.example.invalid/path", "https://kano.example.invalid?x=1",
		"https://kano.example.invalid?", "javascript:alert(1)",
		// The host is printed into HTML attributes and feeds. url.Parse keeps
		// quotes and markup in a host; only a DNS name or IP address passes.
		`https://kano.example.invalid">`, "https://kano.example.invalid<b>", "https://kano.example.invalid'x",
		"https://a_b.example.invalid", "https://-kano.example.invalid", "https://kano-.example.invalid", "https://kano..example.invalid",
		"https://kano.example.invalid.", "https://kano.example.invalid:", "https://kano.example.invalid:0", "https://kano.example.invalid:080",
		"https://ｋａｎｏ.example.invalid",
	} {
		if got, err := PublicOrigin(raw); err == nil {
			t.Fatalf("%q accepted as %q", raw, got)
		}
	}
}
