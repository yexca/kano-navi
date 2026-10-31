// Package schedule is the LLM schedule extraction step. It reads stored X
// posts and schedule
// board images, asks the configured model providers for structured output and
// writes events, extraction records, post LLM states, image reviews and
// cancellation overlays back to the shared SQLite snapshot.
//
// The step never fetches a source and never replaces operator decisions:
// locked or tombstoned events keep their schedule fields, a pending image is
// never downgraded to text-only input, and a failed provider leaves every
// known row in place.
package schedule

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// ErrStore reports a snapshot read or write failure without driver detail.
var ErrStore = errors.New("schedule extraction store failure")

type Options struct {
	// SecretsKey is LLM_SECRETS_KEY, used to decrypt provider credentials.
	SecretsKey string
	// HTTPClient carries provider requests; redirects are always rejected.
	HTTPClient *http.Client
	// Getenv supplies the SCHEDULE_* defaults; nil reads the process environment.
	Getenv func(string) string
	Now    func() time.Time
}

type Result struct {
	// Status is success, partial or failed (server/scan-outcome.ts).
	Status string
	// Counts is the scan summary stored in sync_runs.counts_json.
	Counts map[string]any
}

type Extractor struct {
	store      *sqlite.Store
	db         *sql.DB
	secretsKey string
	client     *http.Client
	lookupEnv  func(string) (string, bool)
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

func New(store *sqlite.Store, opts Options) *Extractor {
	e := &Extractor{
		store:      store,
		db:         store.DB(),
		secretsKey: opts.SecretsKey,
		client:     newHTTPClient(opts.HTTPClient),
		lookupEnv:  os.LookupEnv,
		now:        opts.Now,
		sleep:      sleepContext,
	}
	if opts.Getenv != nil {
		e.lookupEnv = func(key string) (string, bool) {
			value := opts.Getenv(key)
			return value, value != ""
		}
	}
	if e.now == nil {
		e.now = time.Now
	}
	return e
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run executes the schedule step: operator reprocess requests first, then
// keyword candidates, then pending schedule image reviews. A cancelled
// context stops the scan before the next provider call and is returned.
func (e *Extractor) Run(ctx context.Context) (Result, error) {
	if e == nil || e.db == nil {
		return Result{Status: "failed", Counts: map[string]any{}}, ErrStore
	}
	counts, err := e.extractPending(ctx)
	if counts == nil {
		counts = map[string]any{}
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{Status: "failed", Counts: counts}, ctxErr
		}
		return Result{Status: "failed", Counts: counts}, ErrStore
	}
	return Result{Status: scanStatus(counts), Counts: counts}, nil
}

func countOf(counts map[string]any, key string) int {
	value, _ := counts[key].(int)
	return value
}

// scanStatus mirrors scheduleScanStatus: posts and image reviews are
// independent work within one step.
func scanStatus(counts map[string]any) string {
	parts := []map[string]any{counts}
	if assets, ok := counts["assetVerification"].(map[string]any); ok {
		parts = append(parts, assets)
	}
	failed, succeeded := 0, 0
	for _, part := range parts {
		failed += countOf(part, "failed")
		succeeded += countOf(part, "success") + countOf(part, "cached") + countOf(part, "uncertain")
	}
	switch {
	case failed == 0:
		return "success"
	case succeeded > 0:
		return "partial"
	}
	return "failed"
}

type candidate struct {
	post          post
	detectionType string
	force         bool
	attemptedAt   int64
}

const scanLimit = 20

// extractionBackoff is how long the automatic scan leaves a post alone after
// the first, second and third consecutive failed extraction of the same
// input. The fourth failure stops automatic retries until the input or the
// model queue changes or an operator requests a reprocess.
var extractionBackoff = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}

func (e *Extractor) extractPending(ctx context.Context) (map[string]any, error) {
	config, err := e.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	boardEnabled := config.keywordEnabled && config.visionEnabled
	requestedPosts, err := e.listRequestedPosts(ctx, scanLimit)
	if err != nil {
		return nil, err
	}
	if !boardEnabled && !config.messageEnabled && len(requestedPosts) == 0 {
		return map[string]any{
			"attempted": 0, "success": 0, "cached": 0, "skipped": 0, "failed": 0,
			"disabled": true,
		}, nil
	}
	// A post selected by the board path is not sent again through the message
	// path in the same scan.
	keyword := []candidate{}
	keywordType := map[string]string{}
	for _, stage := range []struct {
		enabled       bool
		detectionType string
	}{{boardEnabled, "board"}, {config.messageEnabled, "message"}} {
		if !stage.enabled {
			continue
		}
		posts, err := e.listCandidatePosts(ctx, config.keywords, stage.detectionType, config.messageAllPosts)
		if err != nil {
			return nil, err
		}
		for _, item := range posts {
			if _, known := keywordType[item.id]; !known {
				keywordType[item.id] = stage.detectionType
				keyword = append(keyword, candidate{post: item, detectionType: stage.detectionType})
			}
		}
	}
	// Operator reprocess requests run before keyword candidates so a full scan
	// limit cannot starve them, and they bypass the keyword heuristic.
	queue := []candidate{}
	requested := map[string]bool{}
	for _, item := range requestedPosts {
		queue = append(queue, candidate{post: item, detectionType: requestedDetectionType(item, keywordType[item.id]), force: true})
		requested[item.id] = true
	}
	rest := []candidate{}
	for _, item := range keyword {
		state, err := getPostState(ctx, e.db, item.post.id)
		if err != nil {
			return nil, err
		}
		if state != nil {
			item.attemptedAt = instantMillis(state.lastAttemptAt)
		}
		if !requested[item.post.id] {
			rest = append(rest, item)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].attemptedAt != rest[j].attemptedAt {
			return rest[i].attemptedAt < rest[j].attemptedAt
		}
		return instantMillis(rest[i].post.publishedAt) > instantMillis(rest[j].post.publishedAt)
	})
	queue = append(queue, rest...)

	counts := map[string]any{
		"attempted": 0, "success": 0, "cached": 0, "skipped": 0, "failed": 0,
		"uncertain": 0, "boardAttempted": 0, "messageAttempted": 0,
		"keywords":  config.keywords,
		"providers": config.providerOrder,
	}
	for _, item := range queue {
		if countOf(counts, "attempted") >= scanLimit {
			break
		}
		if err = ctx.Err(); err != nil {
			return counts, err
		}
		result, err := e.extractPost(ctx, item.post, item.detectionType, item.force)
		if err != nil {
			return counts, err
		}
		// Cache hits and posts held back by the failure backoff make no
		// provider call and leave the limit to other work. The held posts are
		// counted as backoff or retryStopped, keys present only when used.
		if result.budgetUsed || (result.status != "cached" && !result.heldBack()) {
			counts["attempted"] = countOf(counts, "attempted") + 1
			counts[item.detectionType+"Attempted"] = countOf(counts, item.detectionType+"Attempted") + 1
		}
		counts[result.status] = countOf(counts, result.status) + 1
	}
	if boardEnabled {
		assets, err := e.verifyPendingAssets(ctx, scanLimit, false)
		if assets != nil {
			counts["assetVerification"] = assets
		}
		if err != nil {
			return counts, err
		}
	}
	return counts, nil
}

// requestedDetectionType: the route an operator asked for wins. Without one,
// a post that the keyword paths also selected keeps that route, and any other
// post follows its own modality.
func requestedDetectionType(item post, keywordDetectionType string) string {
	switch {
	case item.llmRoute == routeMessage:
		return "message"
	case item.llmRoute == routeBoard:
		return "board"
	case keywordDetectionType != "":
		return keywordDetectionType
	case len(item.declaredMediaURLs()) > 0:
		return "board"
	}
	return "message"
}

type postImages struct {
	hasImages bool
	complete  bool
	images    []cachedImage
}

var supportedImageTypes = map[string]bool{
	"image/avif": true, "image/gif": true, "image/jpeg": true, "image/png": true, "image/webp": true,
}

// postImageState resolves every declared image of a post to a ready cache
// file. complete=false keeps the post pending instead of sending it without
// the images it was published with.
func (e *Extractor) postImageState(ctx context.Context, item post) (postImages, error) {
	declared := item.declaredMediaURLs()
	links, err := postImageLinks(ctx, e.db, item.id)
	if err != nil {
		return postImages{}, err
	}
	linked := map[string]bool{}
	for _, id := range links {
		linked[id] = true
	}
	expected := links
	if len(declared) > 0 {
		expected = make([]string, len(declared))
		for i, mediaURL := range declared {
			expected[i] = mediaIDForSourceURL(mediaURL)
		}
	}
	state := postImages{hasImages: len(expected) > 0, images: []cachedImage{}}
	for _, id := range expected {
		if !linked[id] {
			continue
		}
		asset, err := getMediaAsset(ctx, e.db, id)
		if err != nil {
			return postImages{}, err
		}
		if asset == nil || asset.status != "ready" || asset.sha256 == "" || !supportedImageTypes[asset.mimeType] {
			continue
		}
		path, ok := e.cachedFile(asset.cachePath)
		if !ok {
			continue
		}
		state.images = append(state.images, cachedImage{path: path, mimeType: asset.mimeType, sha256: asset.sha256})
	}
	state.complete = len(state.images) == len(expected)
	return state, nil
}

type postResult struct {
	status     string
	reason     string
	budgetUsed bool
}

// heldBack is a failed post the scan left alone: still waiting for its
// backoff ("backoff") or out of automatic retries ("retryStopped").
func (r postResult) heldBack() bool {
	return r.status == "backoff" || r.status == "retryStopped"
}

func (e *Extractor) skipPost(ctx context.Context, postID, route, reason string, force bool) (postResult, error) {
	err := e.markPost(ctx, postID, stateUpdate{
		status:        "skipped",
		route:         route,
		lastAttemptAt: e.nowISO(),
		lastError:     reason,
		reprocess:     &force,
	})
	return postResult{status: "skipped", reason: reason}, err
}

func (e *Extractor) extractPost(ctx context.Context, item post, detectionType string, force bool) (postResult, error) {
	route := routeForDetectionType(detectionType)
	previous, err := getPostState(ctx, e.db, item.id)
	if err != nil {
		return postResult{}, err
	}
	force = force || (previous != nil && previous.reprocessRequested)
	media, err := e.postImageState(ctx, item)
	if err != nil {
		return postResult{}, err
	}
	hasText := jsTrim(item.text) != ""
	inputMode := ""
	switch {
	case hasText && media.hasImages:
		inputMode = "text_image"
	case hasText:
		inputMode = "text"
	case media.hasImages:
		inputMode = "image"
	}
	if inputMode == "" {
		return e.skipPost(ctx, item.id, route, "no_content", force)
	}
	if media.hasImages && !media.complete {
		return e.skipPost(ctx, item.id, route, "media_pending", force)
	}
	providers, reason, err := e.configuredProviders(ctx, inputMode)
	if err != nil {
		return postResult{}, err
	}
	if len(providers) == 0 {
		return e.skipPost(ctx, item.id, route, reason, force)
	}
	hashes := make([]any, len(media.images))
	for i, image := range media.images {
		hashes[i] = image.sha256
	}
	identity := obj("detectionType", detectionType, "inputMode", inputMode, "text", item.text)
	if item.publishedAt != "" {
		identity.set("publishedAt", item.publishedAt)
	}
	identity.set("url", item.url)
	identity.set("images", hashes)
	fingerprint := sha256Hex(stringify(identity))

	// The backoff belongs to this input and these targets: new content, a
	// different queue or model, or an operator request starts it over.
	targets := []any{fingerprint}
	for _, target := range providers {
		targets = append(targets, extractorVersionFor(target, detectionType, inputMode))
	}
	failureKey := sha256Hex(stringify(targets))
	repeated := !force && previous != nil && previous.failureCount > 0 && previous.failureFingerprint == failureKey
	if repeated {
		if previous.nextAttemptAt == "" {
			return postResult{status: "retryStopped"}, nil
		}
		if retryAt, ok := parseInstant(previous.nextAttemptAt); ok && e.now().Before(retryAt) {
			return postResult{status: "backoff"}, nil
		}
	}

	budgetUsed := false
	for _, target := range providers {
		status, err := e.extractWithProvider(ctx, item, media.images, fingerprint, target, detectionType, inputMode, route, force)
		if err != nil {
			return postResult{}, err
		}
		if status != "cached" {
			budgetUsed = true
		}
		if status == "success" || status == "cached" || status == "uncertain" {
			return postResult{status: status, budgetUsed: budgetUsed}, nil
		}
	}
	failures := 1
	if repeated {
		failures = previous.failureCount + 1
	}
	retryAt := ""
	if failures <= len(extractionBackoff) {
		retryAt = sqlite.ISOTime(e.now().Add(extractionBackoff[failures-1]))
	}
	return postResult{status: "failed"}, e.recordFailure(ctx, item.id, failureKey, failures, retryAt)
}

const (
	writeFailure     = "schedule result could not be saved"
	cancelledMessage = "LLM request cancelled"
)

func storedMessage(err error) string {
	var message *messageError
	if errors.As(err, &message) {
		return message.message
	}
	return writeFailure
}

// extractWithProvider runs one provider for one post: the initial request plus
// two retries, as the original did before failing over to the next target.
func (e *Extractor) extractWithProvider(ctx context.Context, item post, images []cachedImage, fingerprint string, target provider, detectionType, inputMode, route string, force bool) (string, error) {
	version := extractorVersionFor(target, detectionType, inputMode)
	existing, err := getExtraction(ctx, e.db, item.id, fingerprint, version)
	if err != nil {
		return "", err
	}
	cleared := false
	if !force && existing != nil && (existing.status == "success" || existing.status == "uncertain") && existing.model == target.model {
		processedAt := existing.updatedAt
		if processedAt == "" {
			processedAt = e.nowISO()
		}
		err = e.markPost(ctx, item.id, stateUpdate{
			status:           existing.status,
			route:            route,
			lastProcessedAt:  processedAt,
			lastExtractionID: existing.id,
			reprocess:        &cleared,
		})
		return "cached", err
	}
	if err = e.markPost(ctx, item.id, stateUpdate{
		status:        "running",
		route:         route,
		lastAttemptAt: e.nowISO(),
		reprocess:     &cleared,
	}); err != nil {
		return "", err
	}
	running, err := e.upsertExtraction(ctx, item.id, fingerprint, version, target.model, "running", nil, "")
	if err != nil {
		return "", err
	}
	// abandon records a cancelled run instead of leaving rows in "running",
	// and restores a consumed reprocess request so the next scan retries it.
	abandon := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = e.upsertExtraction(cleanup, item.id, fingerprint, version, target.model, "failed", nil, cancelledMessage)
		_ = e.markPost(cleanup, item.id, stateUpdate{
			status:           "failed",
			route:            route,
			lastAttemptAt:    e.nowISO(),
			lastExtractionID: running.id,
			lastError:        cancelledMessage,
			reprocess:        &force,
		})
	}
	message := "provider request failed"
	attempts := e.modelAttempts(ctx)
	for attempt := 0; attempt < attempts; attempt++ {
		status, err := e.attemptExtraction(ctx, item, images, fingerprint, version, target, inputMode, route, running.id)
		if err == nil {
			return status, nil
		}
		if ctx.Err() != nil {
			abandon()
			return "", ctx.Err()
		}
		message = storedMessage(err)
		if attempt < attempts-1 {
			if err = e.sleep(ctx, retryDelay(attempt)); err != nil {
				abandon()
				return "", err
			}
		}
	}
	if _, err = e.upsertExtraction(ctx, item.id, fingerprint, version, target.model, "failed", nil, message); err != nil {
		return "", err
	}
	if err = e.updateProviderStatus(ctx, target.id, "failed", message); err != nil {
		return "", err
	}
	err = e.markPost(ctx, item.id, stateUpdate{
		status:           "failed",
		route:            route,
		lastAttemptAt:    e.nowISO(),
		lastExtractionID: running.id,
		lastError:        message,
	})
	return "failed", err
}

// publishedAtText is the publication time given to the model: the stored
// timestamp plus its Asia/Tokyo date, weekday and time, so 今日, 明日 and bare
// weekdays resolve without the model converting from UTC. The extraction
// identity keeps the stored timestamp alone.
func publishedAtText(value string) string {
	if value == "" {
		return "undefined"
	}
	instant, ok := parseInstant(value)
	if !ok {
		return value
	}
	return value + " (" + instant.In(jst).Format("2006-01-02 Mon 15:04") + " Asia/Tokyo)"
}

func (e *Extractor) attemptExtraction(ctx context.Context, item post, images []cachedImage, fingerprint, version string, target provider, inputMode, route string, extractionID int64) (string, error) {
	call := modelCall{
		instructions: scheduleInstructions,
		name:         extractionToolName,
		schema:       scheduleSchema,
		maxTokens:    4000,
		text:         "Source post published at " + publishedAtText(item.publishedAt) + ". Source URL: " + item.url + "\n\n" + item.text,
		includeText:  inputMode != "image",
	}
	if inputMode != "text" {
		call.images = images
	}
	result, err := e.callModel(ctx, target, call)
	if err != nil {
		return "", err
	}
	normalized, err := normalizeExtractionResult(result, item.id, item.url, e.now())
	if err != nil {
		return "", err
	}
	status := "success"
	if normalized.classification == "uncertain" {
		status = "uncertain"
	} else if err = e.applyExtraction(ctx, item.id, extractionID, normalized); err != nil {
		return "", err
	}
	if _, err = e.upsertExtraction(ctx, item.id, fingerprint, version, target.model, status, result, ""); err != nil {
		return "", err
	}
	if err = e.updateProviderStatus(ctx, target.id, "success", ""); err != nil {
		return "", err
	}
	cleared := false
	err = e.markPost(ctx, item.id, stateUpdate{
		status:           status,
		route:            route,
		lastProcessedAt:  e.nowISO(),
		lastExtractionID: extractionID,
		reprocess:        &cleared,
	})
	return status, err
}
