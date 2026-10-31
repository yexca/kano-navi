package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultYTDLPBinary = "yt-dlp"
	ytdlpTimeout       = 45 * time.Second
	ytdlpUpdateTimeout = 2 * time.Minute
	ytdlpMaxOutput     = 8 * 1024 * 1024
)

// YTDLP runs the optional yt-dlp executable. The binary is deliberately
// configured by the process environment and every invocation receives a
// fixed argument list; the admin API never accepts a command or URL.
type YTDLP struct {
	Binary                 string
	Timeout, UpdateTimeout time.Duration
	MaxOutputBytes         int

	mu      sync.Mutex
	runFunc func(context.Context, ...string) ([]byte, []byte, error)
}

// YTDLPStatus is the sanitized operator projection of the local executable.
type YTDLPStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
}

// NewYTDLP creates a runner for binary. An empty path uses the executable
// named yt-dlp on PATH.
func NewYTDLP(binary string) *YTDLP {
	if strings.TrimSpace(binary) == "" {
		binary = defaultYTDLPBinary
	}
	return &YTDLP{Binary: binary, Timeout: ytdlpTimeout, UpdateTimeout: ytdlpUpdateTimeout, MaxOutputBytes: ytdlpMaxOutput}
}

// YTDLPBinaryFromEnv returns the configured executable path.
func YTDLPBinaryFromEnv() string { return strings.TrimSpace(os.Getenv("YTDLP_BINARY")) }

// Available reports whether the configured executable can be resolved.
func (y *YTDLP) Available() bool {
	if y == nil {
		return false
	}
	if y.runFunc != nil {
		return true
	}
	_, err := exec.LookPath(y.Binary)
	return err == nil
}

// Status reports availability and the executable's version without revealing
// the configured filesystem path or process output.
func (y *YTDLP) Status(ctx context.Context) YTDLPStatus {
	status := YTDLPStatus{Available: y != nil && y.Available()}
	if !status.Available {
		return status
	}
	if version, err := y.Version(ctx); err == nil {
		status.Version = version
	}
	return status
}

// Version returns yt-dlp's version string without exposing command output.
func (y *YTDLP) Version(ctx context.Context) (string, error) {
	if y == nil || !y.Available() {
		return "", &Error{"youtube_ytdlp_unavailable"}
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	out, _, err := y.run(ctx, y.timeout(), "--version")
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(out))
	if version == "" || len(version) > 64 {
		return "", &Error{"youtube_ytdlp_invalid_version"}
	}
	return version, nil
}

// Update asks a release binary to update itself. The binary path and update
// channel are fixed by deployment configuration; no URL is accepted here.
func (y *YTDLP) Update(ctx context.Context) error {
	if y == nil || !y.Available() {
		return &Error{"youtube_ytdlp_unavailable"}
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	_, _, err := y.run(ctx, y.updateTimeout(), "-U")
	return err
}

// ListUploads lists the newest channel entries without downloading media.
// yt-dlp returns a single playlist JSON document; flat extraction keeps this
// discovery step bounded and leaves detailed extraction to Inspect.
func (y *YTDLP) ListUploads(ctx context.Context, channel string, limit int) ([]Video, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	if limit < 1 || limit > MaxListLimit {
		return nil, &Error{"youtube_invalid_options"}
	}
	if y == nil || !y.Available() {
		return nil, &Error{"youtube_ytdlp_unavailable"}
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	out, _, err := y.run(ctx, y.timeout(), "--flat-playlist", "--dump-single-json", "--skip-download", "--quiet", "--no-warnings", "--playlist-end", strconv.Itoa(limit), "https://www.youtube.com/channel/"+channel+"/videos")
	if err != nil {
		return nil, err
	}
	return parseYTDLPPlaylist(out, limit)
}

// Inspect extracts the full JSON information for one public YouTube URL and
// maps the fields used by the board. The raw object stays inside the backend.
func (y *YTDLP) Inspect(ctx context.Context, target string) (Video, map[string]any, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Video{}, nil, &Error{"youtube_ytdlp_invalid_target"}
	}
	if y == nil || !y.Available() {
		return Video{}, nil, &Error{"youtube_ytdlp_unavailable"}
	}
	y.mu.Lock()
	defer y.mu.Unlock()
	out, _, err := y.run(ctx, y.timeout(), "--dump-single-json", "--skip-download", "--no-playlist", "--quiet", "--no-warnings", target)
	if err != nil {
		return Video{}, nil, err
	}
	var raw map[string]any
	if json.Unmarshal(out, &raw) != nil || raw == nil {
		return Video{}, nil, &Error{"youtube_ytdlp_invalid_json"}
	}
	video, ok := videoFromYTDLP(raw)
	if !ok {
		return Video{}, nil, &Error{"youtube_ytdlp_invalid_video"}
	}
	return video, raw, nil
}

func (y *YTDLP) timeout() time.Duration {
	if y.Timeout > 0 {
		return y.Timeout
	}
	return ytdlpTimeout
}

func (y *YTDLP) updateTimeout() time.Duration {
	if y.UpdateTimeout > 0 {
		return y.UpdateTimeout
	}
	return ytdlpUpdateTimeout
}

func (y *YTDLP) run(parent context.Context, timeout time.Duration, args ...string) ([]byte, []byte, error) {
	if y.runFunc != nil {
		return y.runFunc(parent, args...)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, y.Binary, args...)
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = y.outputLimit(), y.outputLimit()
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, nil, &Error{"youtube_ytdlp_timeout"}
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, nil, &Error{"youtube_ytdlp_output_oversized"}
	}
	if err != nil {
		return nil, stderr.Bytes(), &Error{"youtube_ytdlp_failed"}
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func (y *YTDLP) outputLimit() int {
	if y.MaxOutputBytes > 0 {
		return y.MaxOutputBytes
	}
	return ytdlpMaxOutput
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && b.Len()+len(p) > b.limit {
		b.exceeded = true
		return len(p), errors.New("output limit")
	}
	return b.Buffer.Write(p)
}

func parseYTDLPPlaylist(body []byte, limit int) ([]Video, error) {
	var document map[string]any
	if json.Unmarshal(body, &document) != nil || document == nil {
		return nil, &Error{"youtube_ytdlp_invalid_json"}
	}
	entries, _ := document["entries"].([]any)
	out := make([]Video, 0, min(len(entries), limit))
	seen := map[string]bool{}
	for _, item := range entries {
		object, _ := item.(map[string]any)
		video, ok := videoFromYTDLP(object)
		if !ok || seen[video.ID] {
			continue
		}
		seen[video.ID] = true
		out = append(out, video)
		if len(out) == limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, &Error{"youtube_ytdlp_empty"}
	}
	return out, nil
}

func videoFromYTDLP(raw map[string]any) (Video, bool) {
	id, _ := raw["id"].(string)
	if !videoID.MatchString(id) {
		return Video{}, false
	}
	title, _ := raw["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "YouTube 更新"
	}
	published := ytdlpTime(raw["timestamp"])
	if published == "" {
		published = ytdlpDate(raw["upload_date"])
	}
	scheduled := ytdlpTime(raw["release_timestamp"])
	if scheduled == "" {
		scheduled = ytdlpTime(raw["release_date"])
	}
	thumbnail, _ := raw["thumbnail"].(string)
	if thumbnail == "" {
		if thumbs, ok := raw["thumbnails"].([]any); ok && len(thumbs) > 0 {
			if last, ok := thumbs[len(thumbs)-1].(map[string]any); ok {
				thumbnail, _ = last["url"].(string)
			}
		}
	}
	if thumbnail == "" {
		thumbnail = "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"
	}
	url, _ := raw["webpage_url"].(string)
	if url == "" {
		url = "https://www.youtube.com/watch?v=" + id
	}
	liveStatus, _ := raw["live_status"].(string)
	video := Video{ID: id, Title: title, PublishedAt: published, ScheduledAt: scheduled, URL: url, ThumbnailURL: thumbnail, Kind: Classify(title), DurationSeconds: intValue(raw["duration"])}
	switch liveStatus {
	case "is_upcoming", "upcoming":
		video.IsUpcoming, video.LiveStatus = true, LiveStatusUpcoming
	case "is_live", "live":
		video.LiveNow, video.LiveStatus = true, LiveStatusLive
	case "post_live", "was_live", "ended":
		video.LiveStatus = LiveStatusEnded
	}
	if video.IsUpcoming && video.ScheduledAt == "" {
		video.ScheduledAt = video.PublishedAt
	}
	if short, ok := raw["categories"].([]any); ok {
		_ = short
	}
	return video, true
}

func ytdlpTime(value any) string {
	var seconds int64
	switch value := value.(type) {
	case float64:
		seconds = int64(value)
	case int64:
		seconds = value
	case json.Number:
		seconds, _ = value.Int64()
	}
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano)
}

func ytdlpDate(value any) string {
	date, _ := value.(string)
	if len(date) != 8 {
		return ""
	}
	parsed, err := time.ParseInLocation("20060102", date, time.UTC)
	if err != nil {
		return ""
	}
	return parsed.Format(time.RFC3339Nano)
}

func intValue(value any) int {
	switch value := value.(type) {
	case float64:
		return max(0, int(value))
	case int:
		return max(0, value)
	case json.Number:
		n, _ := strconv.Atoi(string(value))
		return max(0, n)
	default:
		return 0
	}
}
