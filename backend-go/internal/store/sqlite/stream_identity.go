package sqlite

import (
	"database/sql/driver"
	"net/url"
	"regexp"
	"strings"

	moderncsqlite "modernc.org/sqlite"
)

var youtubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var youtuBePath = regexp.MustCompile(`^/([^/]+)/?$`)
var youtubePath = regexp.MustCompile(`^/(?:live|shorts)/([^/]+)/?$`)

// streamIdentity mirrors src/lib/stream-identity.ts. YouTube links are
// compared by their public video ID; every other value keeps exact URL
// identity so unrelated hosts and malformed links cannot collapse together.
func streamIdentity(value string) string {
	if parsed, err := url.Parse(value); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		host := strings.ToLower(parsed.Hostname())
		var id string
		switch {
		case host == "youtu.be":
			path := parsed.EscapedPath()
			if matches := youtuBePath.FindStringSubmatch(path); len(matches) == 2 {
				id = matches[1]
			}
		case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
			id = parsed.Query().Get("v")
			if id == "" {
				path := parsed.EscapedPath()
				if matches := youtubePath.FindStringSubmatch(path); len(matches) == 2 {
					id = matches[1]
				}
			}
		}
		if id != "" && youtubeVideoID.MatchString(id) {
			return "youtube:" + id
		}
	}
	return "url:" + value
}

// Register the same deterministic SQLite function that the TypeScript
// service installs during database initialization. This lets Go reuse the
// indexed stream_identity(url) expressions without writing to the snapshot.
func init() {
	if err := moderncsqlite.RegisterDeterministicScalarFunction("stream_identity", 1, func(_ *moderncsqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if len(args) == 0 || args[0] == nil {
			return streamIdentity(""), nil
		}
		switch value := args[0].(type) {
		case string:
			return streamIdentity(value), nil
		case []byte:
			return streamIdentity(string(value)), nil
		default:
			return streamIdentity(""), nil
		}
	}); err != nil {
		panic(err)
	}
}
