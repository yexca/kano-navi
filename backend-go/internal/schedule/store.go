package schedule

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// This file ports the server/database.ts reads and writes the extractor
// needs. The store has a single connection: every *sql.Rows is drained and
// closed before the next statement, and no transaction spans a provider call.

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (e *Extractor) nowISO() string { return sqlite.ISOTime(e.now()) }

func appSetting(ctx context.Context, q queryer, key string) (string, bool, error) {
	var value sql.NullString
	err := q.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value.String, value.Valid, nil
}

// ---- schedule configuration ----

type scheduleConfig struct {
	keywordEnabled bool
	visionEnabled  bool
	messageEnabled bool
	// messageAllPosts sends every X post with text or images through the
	// message route instead of only the posts the local heuristic selects.
	messageAllPosts bool
	keywords        []string
	providerOrder   []string
}

func settingBoolean(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	switch jsToLower(jsTrim(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func isListSeparator(r rune) bool { return r == '\n' || r == ',' || r == '，' }

func uniqueTrimmed(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = jsTrim(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// parseListText mirrors parseListSetting for a stored string: a JSON array
// wins, anything else is split on newlines and (full-width) commas.
func parseListText(value string) []string {
	if value == "" {
		return []string{}
	}
	if parsed, err := parseJSON([]byte(value)); err == nil {
		if list, ok := parsed.([]any); ok {
			values := make([]string, len(list))
			for i, item := range list {
				values[i] = jsString(item)
			}
			return uniqueTrimmed(values)
		}
	}
	return uniqueTrimmed(strings.FieldsFunc(value, isListSeparator))
}

// parseListValues mirrors parseListSetting for an array argument, which the
// original code round-trips through String(array) before splitting.
func parseListValues(values []string) []string {
	joined := strings.Join(values, ",")
	if parsed, err := parseJSON([]byte(joined)); err == nil {
		if list, ok := parsed.([]any); ok {
			out := make([]string, len(list))
			for i, item := range list {
				out[i] = jsString(item)
			}
			return uniqueTrimmed(out)
		}
		return uniqueTrimmed(values)
	}
	return uniqueTrimmed(strings.FieldsFunc(joined, isListSeparator))
}

func boundedKeywords(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = jsTrim(value)
		if value == "" || utf16Length(value) > 80 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (e *Extractor) loadConfig(ctx context.Context) (scheduleConfig, error) {
	var config scheduleConfig
	// setting mirrors getAppSetting(key, process.env.NAME ?? fallback).
	setting := func(key, env, fallback string) (string, error) {
		value, ok, err := appSetting(ctx, e.db, key)
		if err != nil || ok {
			return value, err
		}
		if value, ok = e.lookupEnv(env); ok {
			return value, nil
		}
		return fallback, nil
	}
	enabledSetting, err := setting("schedule_extraction_enabled", "SCHEDULE_EXTRACTION_ENABLED", "1")
	if err != nil {
		return config, err
	}
	legacyEnabled := settingBoolean(enabledSetting, true)
	stages := []struct {
		key, env string
		target   *bool
	}{
		{"schedule_keyword_enabled", "SCHEDULE_KEYWORD_ENABLED", &config.keywordEnabled},
		{"schedule_vision_enabled", "SCHEDULE_VISION_ENABLED", &config.visionEnabled},
		{"schedule_message_enabled", "SCHEDULE_MESSAGE_ENABLED", &config.messageEnabled},
	}
	for _, stage := range stages {
		value, err := setting(stage.key, stage.env, enabledSetting)
		if err != nil {
			return config, err
		}
		*stage.target = settingBoolean(value, legacyEnabled)
	}
	// Unlike the stages, the all-posts switch does not follow the legacy
	// switch: it is off unless explicitly enabled.
	allPosts, err := setting("schedule_message_all_posts", "SCHEDULE_MESSAGE_ALL_POSTS", "0")
	if err != nil {
		return config, err
	}
	config.messageAllPosts = settingBoolean(allPosts, false)
	stored, ok, err := appSetting(ctx, e.db, "schedule_keywords")
	if err != nil {
		return config, err
	}
	if !ok {
		stored, ok = e.lookupEnv("SCHEDULE_KEYWORDS")
	}
	if ok {
		config.keywords = boundedKeywords(parseListText(stored))
	} else {
		config.keywords = boundedKeywords(parseListValues(defaultScheduleKeywords))
	}
	config.providerOrder, err = queueProviders(ctx, e.db)
	return config, err
}

// ---- providers, models and the queue (read-only apart from the status) ----

type routeTarget struct {
	providerID string
	modelID    string
}

func queueTargets(ctx context.Context, q queryer, route string) ([]routeTarget, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.provider_id, r.model_id
		FROM llm_route_targets r
		JOIN llm_providers p ON p.id = r.provider_id
		WHERE r.route = ? ORDER BY r.priority ASC, r.provider_id ASC, r.model_id ASC`, route)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []routeTarget{}
	for rows.Next() {
		var providerID string
		var modelID sql.NullString
		if err = rows.Scan(&providerID, &modelID); err != nil {
			return nil, err
		}
		out = append(out, routeTarget{providerID, modelID.String})
	}
	return out, rows.Err()
}

func queueProviders(ctx context.Context, q queryer) ([]string, error) {
	targets, err := queueTargets(ctx, q, routeQueue)
	if err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for _, target := range targets {
		if !seen[target.providerID] {
			seen[target.providerID] = true
			out = append(out, target.providerID)
		}
	}
	return out, nil
}

type providerRow struct {
	id         string
	protocol   string
	baseURL    string
	enabled    bool
	timeoutMs  float64
	ciphertext string
}

func columnNumber(value any) float64 {
	switch v := value.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	case string:
		return jsNumber(v, true)
	case []byte:
		return jsNumber(string(v), true)
	}
	return 0
}

func llmProvider(ctx context.Context, q queryer, id string) (*providerRow, error) {
	var row providerRow
	var protocol, baseURL, ciphertext sql.NullString
	var enabled sql.NullInt64
	var timeout any
	err := q.QueryRowContext(ctx, `SELECT id, protocol, base_url, enabled, timeout_ms, api_key_ciphertext
		FROM llm_providers WHERE id = ?`, id).
		Scan(&row.id, &protocol, &baseURL, &enabled, &timeout, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.protocol, row.baseURL = protocol.String, baseURL.String
	row.enabled = enabled.Int64 != 0
	row.timeoutMs = columnNumber(timeout)
	row.ciphertext = ciphertext.String
	return &row, nil
}

type modelRow struct {
	id           string
	enabled      bool
	capabilities []string
}

func llmModel(ctx context.Context, q queryer, providerID, modelID string) (*modelRow, error) {
	var row modelRow
	var tags sql.NullString
	var enabled sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT model_id, tags_json, enabled FROM llm_models
		WHERE provider_id = ? AND model_id = ?`, providerID, modelID).Scan(&row.id, &tags, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.enabled = enabled.Int64 != 0
	row.capabilities = modelCapabilities(tags.String)
	return &row, nil
}

func (e *Extractor) updateProviderStatus(ctx context.Context, id, status, message string) error {
	timestamp := e.nowISO()
	_, err := e.db.ExecContext(ctx, `UPDATE llm_providers SET last_status=?, last_error=?,
		last_checked_at=?, updated_at=? WHERE id=?`,
		nullable(status), nullable(sliceUTF16(message, 500)), timestamp, timestamp, id)
	return err
}

// ---- posts ----

type post struct {
	id          string
	postType    string
	label       string
	text        string
	publishedAt string
	url         string
	mediaURL    string
	mediaURLs   []any
	raw         *object
	llmRoute    string
}

// rawObjectOf returns the stored source record when it is a JSON object.
func rawObjectOf(rawJSON sql.NullString) *object {
	if !rawJSON.Valid || rawJSON.String == "" {
		return nil
	}
	parsed, err := parseJSON([]byte(rawJSON.String))
	if err != nil {
		return nil
	}
	root, _ := parsed.(*object)
	return root
}

// declaredMediaURLs mirrors declaredPostMediaUrls: every spelling the two
// writers used for a post's media list, de-duplicated in declaration order.
func (p post) declaredMediaURLs() []string {
	values := append([]any{}, p.mediaURLs...)
	if p.mediaURL != "" {
		values = append(values, p.mediaURL)
	}
	for _, key := range []string{"mediaUrls", "media_urls", "mediaURLs"} {
		value, ok := p.raw.get(key)
		if !ok {
			continue
		}
		if list, isArray := value.([]any); isArray {
			values = append(values, list...)
		} else {
			values = append(values, value)
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		text, isString := value.(string)
		if !isString {
			continue
		}
		text = jsTrim(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}

const jsSpaceClass = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	// JavaScript's \s covers Unicode spaces (notably U+3000) and its dot
	// excludes every line terminator, so both are spelled out for RE2.
	noticeLabelPattern     = regexp.MustCompile(`(?i)(?:notice|schedule|日程|告知|お知らせ|公告)`)
	scheduleMessagePattern = regexp.MustCompile(`(?i)(?:今日|明日|明後日|今週|来週|(?:\d{1,2})` + jsSpaceClass + `*[/:月日]` + jsSpaceClass + `*(?:\d{1,2})?|(?:\d{1,2})` + jsSpaceClass + `*時)|(?:配信|放送|出演|イベント|ライブ|開始|開催|予定|告知)`)
	scheduleBoardPattern   = regexp.MustCompile(`(?i)(?:\b(?:weekly(?:` + jsSpaceClass + `|[_-])+)?schedule(?:(?:` + jsSpaceClass + `|[_-])+(?:board|table))?\b|\bthis` + jsSpaceClass + `+week(?:'s)?` + jsSpaceClass + `+schedule\b|スケジュール|予定表|(?:今週|来週|今月|週間)[^\n\r\x{2028}\x{2029}]{0,4}(?:予定|スケジュール))`)
)

// isLikelyScheduleBoardText mirrors server/schedule-asset.ts.
func isLikelyScheduleBoardText(value string) bool {
	return scheduleBoardPattern.MatchString(value)
}

// listCandidatePosts selects the posts a detector stage scans. With allPosts,
// the message stage takes every post that has text or images.
func (e *Extractor) listCandidatePosts(ctx context.Context, keywords []string, detectionType string, allPosts bool) ([]post, error) {
	lowered := []string{}
	for _, keyword := range boundedKeywords(parseListValues(keywords)) {
		lowered = append(lowered, jsToLower(keyword))
	}
	rows, err := e.db.QueryContext(ctx, `SELECT id, type, label, text, published_at, url, media_url, raw_json
		FROM posts
		WHERE source='x'
		ORDER BY published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []post{}
	for rows.Next() {
		var item post
		var postType, label, text, publishedAt, postURL, mediaURL, rawJSON sql.NullString
		if err = rows.Scan(&item.id, &postType, &label, &text, &publishedAt, &postURL, &mediaURL, &rawJSON); err != nil {
			return nil, err
		}
		item.postType, item.label, item.text = postType.String, label.String, text.String
		item.publishedAt, item.url, item.mediaURL = publishedAt.String, postURL.String, mediaURL.String
		item.raw = rawObjectOf(rawJSON)
		item.mediaURLs = []any{}
		if list, ok := arrayProperty(item.raw, "media_urls"); ok {
			item.mediaURLs = list
		} else if list, ok = arrayProperty(item.raw, "mediaUrls"); ok {
			item.mediaURLs = list
		} else if item.mediaURL != "" {
			item.mediaURLs = []any{item.mediaURL}
		}
		matchesKeyword := func() bool {
			haystack := jsToLower(item.label + " " + item.text)
			for _, keyword := range lowered {
				if strings.Contains(haystack, keyword) {
					return true
				}
			}
			return false
		}
		selected := false
		switch {
		case detectionType != "message":
			selected = matchesKeyword()
		case allPosts:
			selected = jsTrim(item.text) != "" || len(item.mediaURLs) > 0
		case jsTrim(item.text) == "":
			selected = len(item.mediaURLs) > 0 && noticeLabelPattern.MatchString(item.postType+" "+item.label)
		default:
			selected = matchesKeyword() || scheduleMessagePattern.MatchString(item.text)
		}
		if selected {
			out = append(out, item)
		}
	}
	return out, rows.Err()
}

func arrayProperty(root *object, key string) ([]any, bool) {
	value, ok := root.get(key)
	if !ok {
		return nil, false
	}
	list, isArray := value.([]any)
	return list, isArray
}

func (e *Extractor) listRequestedPosts(ctx context.Context, limit int) ([]post, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT p.id, p.text, p.published_at, p.url, p.media_url,
		p.raw_json, s.route
		FROM posts p LEFT JOIN post_llm_states s ON s.post_id = p.id
		WHERE s.reprocess_requested = 1
		ORDER BY p.published_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []post{}
	for rows.Next() {
		var item post
		var text, publishedAt, postURL, mediaURL, rawJSON, route sql.NullString
		if err = rows.Scan(&item.id, &text, &publishedAt, &postURL, &mediaURL, &rawJSON, &route); err != nil {
			return nil, err
		}
		item.text, item.publishedAt, item.url = text.String, publishedAt.String, postURL.String
		item.mediaURL, item.llmRoute = mediaURL.String, route.String
		item.raw = rawObjectOf(rawJSON)
		out = append(out, item)
	}
	return out, rows.Err()
}

// ---- post LLM state ----

type postState struct {
	lastAttemptAt      string
	reprocessRequested bool
	// failureCount counts consecutive failed extractions of the input named by
	// failureFingerprint. nextAttemptAt is empty once automatic retries stop.
	failureCount       int
	failureFingerprint string
	nextAttemptAt      string
}

func getPostState(ctx context.Context, q queryer, postID string) (*postState, error) {
	var lastAttemptAt, fingerprint, nextAttemptAt sql.NullString
	var reprocess, failures sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT last_attempt_at, reprocess_requested,
		failure_count, failure_fingerprint, next_attempt_at
		FROM post_llm_states WHERE post_id = ?`, postID).
		Scan(&lastAttemptAt, &reprocess, &failures, &fingerprint, &nextAttemptAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &postState{
		lastAttemptAt:      lastAttemptAt.String,
		reprocessRequested: reprocess.Int64 != 0,
		failureCount:       int(failures.Int64),
		failureFingerprint: fingerprint.String,
		nextAttemptAt:      nextAttemptAt.String,
	}, nil
}

// recordFailure stores the consecutive failure count of one input and when
// the automatic scan may try it again; an empty retryAt stops retries.
func (e *Extractor) recordFailure(ctx context.Context, postID, fingerprint string, count int, retryAt string) error {
	_, err := e.db.ExecContext(ctx, `UPDATE post_llm_states SET failure_count=?,
		failure_fingerprint=?, next_attempt_at=? WHERE post_id=?`,
		count, fingerprint, nullable(retryAt), postID)
	return err
}

var postStatuses = map[string]bool{
	"never": true, "queued": true, "running": true, "success": true,
	"uncertain": true, "failed": true, "skipped": true,
}

type stateUpdate struct {
	status           string
	route            string
	lastAttemptAt    string
	lastProcessedAt  string
	lastError        string
	lastExtractionID int64
	// reprocess nil keeps the stored request flag.
	reprocess *bool
}

// markPost mirrors markPostLlm + upsertPostLlmState. Empty strings are stored
// as NULL, and NULL keeps the previous route, timestamps and extraction ID.
// A success or uncertain outcome, cached or not, clears the failure backoff;
// every other status keeps it.
func (e *Extractor) markPost(ctx context.Context, postID string, update stateUpdate) error {
	if postID == "" {
		return nil
	}
	reprocess := false
	if update.reprocess != nil {
		reprocess = *update.reprocess
	} else {
		previous, err := getPostState(ctx, e.db, postID)
		if err != nil {
			return err
		}
		reprocess = previous != nil && previous.reprocessRequested
	}
	status := jsToLower(jsTrim(update.status))
	if !postStatuses[status] {
		status = "never"
	}
	var extractionID any
	if update.lastExtractionID > 0 {
		extractionID = update.lastExtractionID
	}
	flag := 0
	if reprocess {
		flag = 1
	}
	_, err := e.db.ExecContext(ctx, `INSERT INTO post_llm_states (
			post_id, status, route, last_attempt_at, last_processed_at,
			last_error, last_extraction_id, reprocess_requested, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(post_id) DO UPDATE SET
			status=excluded.status, route=COALESCE(excluded.route, post_llm_states.route),
			last_attempt_at=COALESCE(excluded.last_attempt_at, post_llm_states.last_attempt_at),
			last_processed_at=COALESCE(excluded.last_processed_at, post_llm_states.last_processed_at),
			last_error=excluded.last_error,
			last_extraction_id=COALESCE(excluded.last_extraction_id, post_llm_states.last_extraction_id),
			reprocess_requested=excluded.reprocess_requested,
			updated_at=excluded.updated_at,
			failure_count=CASE WHEN excluded.status IN ('success', 'uncertain') THEN 0 ELSE post_llm_states.failure_count END,
			failure_fingerprint=CASE WHEN excluded.status IN ('success', 'uncertain') THEN NULL ELSE post_llm_states.failure_fingerprint END,
			next_attempt_at=CASE WHEN excluded.status IN ('success', 'uncertain') THEN NULL ELSE post_llm_states.next_attempt_at END`,
		postID, status, nullable(update.route), nullable(update.lastAttemptAt),
		nullable(update.lastProcessedAt), nullable(update.lastError), extractionID, flag, e.nowISO())
	return err
}

// ---- schedule extractions ----

type extractionRow struct {
	id         int64
	model      string
	status     string
	resultJSON sql.NullString
	updatedAt  string
}

func getExtraction(ctx context.Context, q queryer, sourceItemID, fingerprint, version string) (*extractionRow, error) {
	var row extractionRow
	var model, status, updatedAt sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, model, status, result_json, updated_at
		FROM schedule_extractions
		WHERE source=? AND source_item_id=? AND content_fingerprint=?
			AND extractor_version=?`, "x", sourceItemID, fingerprint, version).
		Scan(&row.id, &model, &status, &row.resultJSON, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.model, row.status, row.updatedAt = model.String, status.String, updatedAt.String
	return &row, nil
}

// upsertExtraction stores result (nil -> NULL) the way JSON.stringify did.
func (e *Extractor) upsertExtraction(ctx context.Context, sourceItemID, fingerprint, version, model, status string, result any, message string) (*extractionRow, error) {
	timestamp := e.nowISO()
	var resultJSON any
	if result != nil {
		resultJSON = stringify(result)
	}
	_, err := e.db.ExecContext(ctx, `INSERT INTO schedule_extractions (
			source, source_item_id, content_fingerprint, extractor_version,
			model, status, result_json, error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source, source_item_id, content_fingerprint, extractor_version)
		DO UPDATE SET model=excluded.model, status=excluded.status,
			result_json=excluded.result_json, error=excluded.error,
			updated_at=excluded.updated_at`,
		"x", sourceItemID, fingerprint, version, model, status, resultJSON, nullable(message), timestamp, timestamp)
	if err != nil {
		return nil, err
	}
	row, err := getExtraction(ctx, e.db, sourceItemID, fingerprint, version)
	if err == nil && row == nil {
		err = sql.ErrNoRows
	}
	return row, err
}

// ---- cached media ----

type mediaAsset struct {
	id        string
	cachePath string
	mimeType  string
	sha256    string
	status    string
}

func getMediaAsset(ctx context.Context, q queryer, id string) (*mediaAsset, error) {
	if !mediaIDPattern.MatchString(id) {
		return nil, nil
	}
	var cachePath, mimeType, hash, status sql.NullString
	err := q.QueryRowContext(ctx, `SELECT cache_path, mime_type, sha256, status FROM media_assets WHERE id = ?`, id).
		Scan(&cachePath, &mimeType, &hash, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &mediaAsset{id, cachePath.String, mimeType.String, hash.String, status.String}, nil
}

func postImageLinks(ctx context.Context, q queryer, postID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT media_id FROM media_links
		WHERE owner_type = 'post' AND owner_id = ? AND role = 'post-image'
		ORDER BY position, id`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func insideDirectory(candidate, root string) bool {
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// cachedFile resolves a database cache path to a regular file inside the
// runtime data directory, following the namespace rules of
// resolveMediaCachePath and rejecting symlinks that leave the data root.
func (e *Extractor) cachedFile(relative string) (string, bool) {
	root := e.store.DataRoot()
	if root == "" || strings.TrimSpace(relative) == "" {
		return "", false
	}
	normalized := strings.ReplaceAll(relative, `\`, "/")
	if strings.ContainsRune(normalized, 0) || strings.HasPrefix(normalized, "/") || filepath.IsAbs(normalized) || filepath.VolumeName(normalized) != "" {
		return "", false
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == ".." {
			return "", false
		}
	}
	root = filepath.Clean(root)
	base := root
	if strings.HasPrefix(normalized, "sha256/") {
		base = filepath.Join(root, "cache", "media")
	} else {
		switch strings.ToLower(jsTrim(parts[0])) {
		case "x", "youtube", "avatar":
		default:
			return "", false
		}
	}
	candidate := filepath.Join(base, filepath.FromSlash(normalized))
	if !insideDirectory(candidate, base) {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || !insideDirectory(resolved, resolvedRoot) {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return resolved, true
}

// ---- schedule image reviews ----

type assetReview struct {
	id                  string
	url                 string
	sourceURL           sql.NullString
	assetVersion        int64
	llmStatus           string
	llmConfidence       sql.NullFloat64
	llmReason           string
	llmEvidence         string
	llmModel            string
	llmCheckedAt        string
	llmInputFingerprint string
	manualStatus        string
	reviewUpdatedAt     string
	sourceMatchesBoard  bool
}

// The source post text is read from raw_json so a quoted post counts the same
// way it did when sync promoted the image. json_valid keeps one malformed row
// from failing the whole step; such a row falls back to its text column.
const assetReviewColumns = `a.id, a.url, a.source_url, a.asset_version,
	(SELECT COALESCE(CASE WHEN json_valid(p.raw_json) THEN json_extract(p.raw_json, '$.search_text') END, p.text)
		FROM posts p WHERE p.url = a.source_url
		ORDER BY p.published_at DESC LIMIT 1),
	r.llm_status, r.llm_confidence, r.llm_reason, r.llm_evidence, r.llm_model,
	r.llm_checked_at, r.llm_input_fingerprint,
	COALESCE(r.manual_status, 'unreviewed'), r.updated_at`

var (
	assetLLMStatuses = map[string]bool{
		"pending": true, "running": true, "schedule": true, "not_schedule": true,
		"uncertain": true, "failed": true, "skipped": true,
	}
	assetManualStatuses = map[string]bool{"unreviewed": true, "schedule": true, "not_schedule": true}
)

func normalizedAssetLLMStatus(value string) string {
	if value == "" {
		return "pending"
	}
	value = jsToLower(jsTrim(value))
	if assetLLMStatuses[value] {
		return value
	}
	return "pending"
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAssetReview(row rowScanner) (*assetReview, error) {
	var review assetReview
	var sourceText, llmStatus, reason, evidence, model, checkedAt, fingerprint, manual, updatedAt sql.NullString
	err := row.Scan(&review.id, &review.url, &review.sourceURL, &review.assetVersion, &sourceText,
		&llmStatus, &review.llmConfidence, &reason, &evidence, &model, &checkedAt, &fingerprint,
		&manual, &updatedAt)
	if err != nil {
		return nil, err
	}
	review.llmStatus = normalizedAssetLLMStatus(llmStatus.String)
	review.manualStatus = jsToLower(jsTrim(manual.String))
	if !assetManualStatuses[review.manualStatus] {
		review.manualStatus = "unreviewed"
	}
	review.llmReason, review.llmEvidence, review.llmModel = reason.String, evidence.String, model.String
	review.llmCheckedAt, review.llmInputFingerprint = checkedAt.String, fingerprint.String
	review.reviewUpdatedAt = updatedAt.String
	review.sourceMatchesBoard = !sourceText.Valid || isLikelyScheduleBoardText(sourceText.String)
	return &review, nil
}

func getAssetReview(ctx context.Context, q queryer, assetID string) (*assetReview, error) {
	review, err := scanAssetReview(q.QueryRowContext(ctx, `SELECT `+assetReviewColumns+`
		FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id = a.id
		WHERE a.id = ? AND a.kind = 'schedule'`, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return review, err
}

func listAssetReviews(ctx context.Context, q queryer) ([]*assetReview, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+assetReviewColumns+`
		FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id = a.id
		WHERE a.kind = 'schedule'
		ORDER BY COALESCE(a.week_start, a.updated_at) DESC, a.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*assetReview{}
	for rows.Next() {
		review, err := scanAssetReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, review)
	}
	return out, rows.Err()
}

type reviewUpdate struct {
	assetID       string
	expectURL     string
	expectVersion int64
	// expectSHA is checked only when the caller already resolved the image.
	expectSHA           string
	llmStatus           string
	llmConfidence       *float64
	llmReason           string
	llmEvidence         string
	llmModel            string
	llmCheckedAt        string
	llmInputFingerprint string
}

// upsertAssetReview writes a model verdict only while the asset still shows
// the image that was sent. It never touches the manual review columns of an
// existing row, and reports applied=false for a replaced or removed asset.
func (e *Extractor) upsertAssetReview(ctx context.Context, update reviewUpdate) (bool, error) {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	assetID := jsTrim(update.assetID)
	if assetID == "" {
		return false, nil
	}
	current, err := getAssetReview(ctx, tx, assetID)
	if err != nil || current == nil {
		return false, err
	}
	if current.url != update.expectURL || current.assetVersion != update.expectVersion {
		return false, nil
	}
	if update.expectSHA != "" {
		media, err := getMediaAsset(ctx, tx, mediaIDForSourceURL(current.url))
		if err != nil {
			return false, err
		}
		if media == nil || media.sha256 != update.expectSHA {
			return false, nil
		}
	}
	var confidence any
	if update.llmConfidence != nil && finite(*update.llmConfidence) {
		confidence = min(1, max(0, *update.llmConfidence))
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_asset_reviews (
			asset_id, llm_status, llm_confidence, llm_reason, llm_evidence,
			llm_model, llm_checked_at, llm_input_fingerprint, manual_status, manual_reason,
			manual_checked_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'unreviewed', NULL, NULL, ?)
		ON CONFLICT(asset_id) DO UPDATE SET
			llm_status=excluded.llm_status,
			llm_confidence=excluded.llm_confidence,
			llm_reason=excluded.llm_reason,
			llm_evidence=excluded.llm_evidence,
			llm_model=excluded.llm_model,
			llm_checked_at=excluded.llm_checked_at,
			llm_input_fingerprint=excluded.llm_input_fingerprint,
			updated_at=excluded.updated_at`,
		assetID, normalizedAssetLLMStatus(update.llmStatus), confidence, nullable(update.llmReason),
		nullable(update.llmEvidence), nullable(update.llmModel), nullable(update.llmCheckedAt),
		nullable(update.llmInputFingerprint), e.nowISO())
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}
