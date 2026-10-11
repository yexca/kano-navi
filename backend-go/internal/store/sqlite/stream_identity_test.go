package sqlite

import "testing"

func TestStreamIdentityMatchesSupportedYouTubeLinks(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "watch", url: "https://www.youtube.com/watch?v=abc_123-xy", want: "youtube:abc_123-xy"},
		{name: "short", url: "https://youtu.be/abc_123-xy", want: "youtube:abc_123-xy"},
		{name: "live", url: "https://youtube.com/live/abc_123-xy", want: "youtube:abc_123-xy"},
		{name: "shorts", url: "https://m.youtube.com/shorts/abc_123-xy/", want: "youtube:abc_123-xy"},
		{name: "query and share", url: "https://youtu.be/abc_123-xy?si=share-token&feature=share", want: "youtube:abc_123-xy"},
		{name: "different video", url: "https://www.youtube.com/watch?v=different", want: "youtube:different"},
		{name: "lookalike host", url: "https://youtube.invalid/watch?v=abc_123-xy", want: "url:https://youtube.invalid/watch?v=abc_123-xy"},
		{name: "ordinary exact URL", url: "https://example.invalid/watch?v=abc_123-xy", want: "url:https://example.invalid/watch?v=abc_123-xy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamIdentity(tt.url); got != tt.want {
				t.Fatalf("streamIdentity(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestStreamIdentityRejectsInvalidVideoIDs(t *testing.T) {
	for _, url := range []string{
		"https://www.youtube.com/watch?v=bad.id",
		"https://youtu.be/one/two",
		"not a URL",
	} {
		if got := streamIdentity(url); got != "url:"+url {
			t.Fatalf("streamIdentity(%q) = %q, want exact URL identity", url, got)
		}
	}
}
