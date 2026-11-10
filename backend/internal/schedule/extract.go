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
	// ignoreSettled judges settled posts like any other; tests compare the
	// two paths with it.
	ignoreSettled bool
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
	for _, key := range []string{"assetVerification", "eventTags", "bilibiliForms", "videoTags"} {
		if part, ok := counts[key].(map[string]any); ok {
			parts = append(parts, part)
		}
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

const scanLimit = 20

// extractionBackoff is how long the automatic scan leaves a post alone after
// the first, second and third consecutive failed extraction of the same
// input. The fourth failure stops automatic retries until the input or the
// model queue changes or an operator requests a reprocess.
var extractionBackoff = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}

func (e *Extractor) extractPending(ctx context.Context) (map[string]any, error) {
	scan, err := e.buildQueue(ctx)
	if err != nil {
		return nil, err
	}
	if scan.disabled {
		return map[string]any{
			"attempted": 0, "success": 0, "cached": 0, "skipped": 0, "failed": 0,
			"disabled": true,
		}, nil
	}
	counts := map[string]any{
		"attempted": 0, "success": 0, "cached": 0, "skipped": 0, "failed": 0,
		"uncertain": 0, "boardAttempted": 0, "messageAttempted": 0,
		"keywords":  scan.config.keywords,
		"providers": scan.config.providerOrder,
		// The candidate phase: how long selecting and ordering the queue
		// took, the rows the detection stages read, and the posts queued;
		// then how many posts the scan judged and how long loading and
		// judging them took, model calls and writes excluded, and how many
		// settled posts it counted as cache hits without loading them.
		"candidateMs":    scan.elapsed.Milliseconds(),
		"postsRead":      scan.postsRead,
		"candidatePosts": len(scan.queue),
		"judged":         0,
		"judgeMs":        int64(0),
		"settled":        0,
	}
	var judging time.Duration
	_, err = walkQueue(ctx, scan.queue, func(item candidate) (postResult, error) {
		result, err := e.extractPost(ctx, item, &judging)
		if item.post.settled {
			counts["settled"] = countOf(counts, "settled") + 1
		} else {
			counts["judged"] = countOf(counts, "judged") + 1
		}
		counts["judgeMs"] = judging.Milliseconds()
		if err != nil {
			return result, err
		}
		// Cache hits and posts held back by the failure backoff make no
		// provider call and leave the limit to other work. The held posts are
		// counted as backoff or retryStopped, keys present only when used.
		if result.takesSlot() {
			counts["attempted"] = countOf(counts, "attempted") + 1
			counts[item.detectionType+"Attempted"] = countOf(counts, item.detectionType+"Attempted") + 1
		}
		counts[result.status] = countOf(counts, result.status) + 1
		return result, nil
	})
	if err != nil {
		return counts, err
	}
	if scan.boardEnabled {
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
func (e *Extractor) postImageState(ctx context.Context, reads candidateReads, item post) (postImages, error) {
	declared := item.declaredMediaURLs()
	links, err := reads.imageLinks(ctx, item.id)
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
		asset, err := reads.mediaAsset(ctx, id)
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
	status string
	reason string
}

// heldBack is a failed post the scan left alone: still waiting for its
// backoff ("backoff") or out of automatic retries ("retryStopped").
func (r postResult) heldBack() bool {
	return r.status == "backoff" || r.status == "retryStopped"
}

// takesSlot reports whether the outcome uses one of the scan's scanLimit
// places: every outcome but a cache hit and a held-back post.
func (r postResult) takesSlot() bool {
	return r.status != "cached" && !r.heldBack()
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

// useCachedExtraction records a stored result as the post's state without a
// provider call, with the post's input stamp as its settled stamp. A post
// whose state already records exactly this is left unwritten, its
// updated_at included. Only this write records a stamp: the state it leaves
// is the one every later cache hit of the input would write again, so a
// later scan may count the post as a cache hit without judging it.
func (e *Extractor) useCachedExtraction(ctx context.Context, item post, route string, previous *postState, cached *extractionRow) (postResult, error) {
	processedAt := cached.updatedAt
	stamp := item.stamp
	if processedAt == "" {
		// Every later cache hit would write a new time.
		processedAt, stamp = e.nowISO(), ""
	}
	cleared := false
	update := stateUpdate{
		status:           cached.status,
		route:            route,
		lastProcessedAt:  processedAt,
		lastExtractionID: cached.id,
		reprocess:        &cleared,
		stamp:            stamp,
	}
	if previous.holds(update) {
		return postResult{status: "cached"}, nil
	}
	return postResult{status: "cached"}, e.markPost(ctx, item.id, update)
}

// extractPost judges one candidate and carries the decision out, adding the
// time loading and judging took to judging.
func (e *Extractor) extractPost(ctx context.Context, item candidate, judging *time.Duration) (postResult, error) {
	started := time.Now()
	input, decided, err := e.judgeCandidate(ctx, liveReads{e.db}, item, e.liveTargets(ctx), e.now())
	*judging += time.Since(started)
	if err != nil {
		return postResult{}, err
	}
	return e.execute(ctx, input, decided, item.detectionType)
}

// execute carries out a decision: it records a skip or a cache hit, leaves a
// held-back post alone, or calls the targets in order and records the
// failure backoff when none succeeds.
func (e *Extractor) execute(ctx context.Context, input candidateInput, decided decision, detectionType string) (postResult, error) {
	item := input.post
	route := routeForDetectionType(detectionType)
	switch decided.disposition {
	case dispositionSkip:
		return e.skipPost(ctx, item.id, route, decided.reason, decided.force)
	case dispositionCached:
		if decided.settled {
			return postResult{status: "cached"}, nil
		}
		return e.useCachedExtraction(ctx, item, route, input.state, decided.cached)
	case dispositionRetryStopped:
		return postResult{status: "retryStopped"}, nil
	case dispositionBackoff:
		return postResult{status: "backoff"}, nil
	}
	for _, target := range decided.providers {
		status, err := e.extractWithProvider(ctx, item, input.media.images, decided.fingerprint, target, detectionType, decided.inputMode, route, decided.force)
		if err != nil {
			return postResult{}, err
		}
		if status == "success" || status == "uncertain" {
			return postResult{status: status}, nil
		}
	}
	failures := 1
	if decided.repeated {
		failures = input.state.failureCount + 1
	}
	retryAt := ""
	if failures <= len(extractionBackoff) {
		retryAt = sqlite.ISOTime(e.now().Add(extractionBackoff[failures-1]))
	}
	return postResult{status: "failed"}, e.recordFailure(ctx, item.id, decided.failureKey, failures, retryAt)
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
	cleared := false
	if err := e.markPost(ctx, item.id, stateUpdate{
		status:        "running",
		route:         route,
		lastAttemptAt: e.nowISO(),
		reprocess:     &cleared,
	}); err != nil {
		return "", err
	}
	running, err := e.upsertExtraction(ctx, item.extractionSource(), item.id, fingerprint, version, target.model, "running", nil, "")
	if err != nil {
		return "", err
	}
	// abandon records a cancelled run instead of leaving rows in "running",
	// and restores a consumed reprocess request so the next scan retries it.
	abandon := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = e.upsertExtraction(cleanup, item.extractionSource(), item.id, fingerprint, version, target.model, "failed", nil, cancelledMessage)
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
	if _, err = e.upsertExtraction(ctx, item.extractionSource(), item.id, fingerprint, version, target.model, "failed", nil, message); err != nil {
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
func publishedAtText(value string) string { return xProfile.publishedAtText(value) }

// publishedAtText gives the publication time in the source's zone: Beijing
// time for a Bilibili dynamic, so 今天 and 明天 resolve there.
func (p sourceProfile) publishedAtText(value string) string {
	if value == "" {
		return "undefined"
	}
	instant, ok := parseInstant(value)
	if !ok {
		return value
	}
	return value + " (" + instant.In(p.zone).Format("2006-01-02 Mon 15:04") + " " + p.zoneName + ")"
}

// quotedInput marks the wording of the status a post quotes as context after
// the post's own text: the post points at it, often with a word or two, and
// the schedule is in the quoted wording. A Bilibili forward names the
// forwarded dynamic's author by display name, and times without a zone in
// its wording are Beijing time, as in the forward's own. Quoted images never
// join the input.
func quotedInput(quote quotedContext) string {
	if quote.text == "" {
		return ""
	}
	author := "another account"
	if quote.forward {
		if quote.author != "" {
			author = quote.author
		}
		return "\n\n[Forwarded dynamic by " + author + ". The source post forwards this wording; it is context, not the source post's own text. Times written in it without a zone are Beijing time, as in the source post.]\n" + quote.text
	}
	if quote.author != "" {
		author = "@" + quote.author
	}
	return "\n\n[Quoted post by " + author + ". The source post quotes this wording; it is context, not the source post's own text.]\n" + quote.text
}

// extractionSource is the source recorded with the post's extractions and
// events.
func (p post) extractionSource() string { return profileFor(p.source).source }

func (e *Extractor) attemptExtraction(ctx context.Context, item post, images []cachedImage, fingerprint, version string, target provider, inputMode, route string, extractionID int64) (string, error) {
	profile := profileFor(item.source)
	instructions, name, schema := profile.instructions, extractionToolName, profile.schema
	if inputMode != "text" {
		instructions = imageScheduleInstructions + " " + profile.instructions
		name = imageExtractionToolName
		schema = imageScheduleSchema(profile.schema)
	}
	call := modelCall{
		instructions: instructions,
		name:         name,
		schema:       schema,
		maxTokens:    4000,
		text:         "Source post published at " + profile.publishedAtText(item.publishedAt) + ". Source URL: " + item.url + "\n\n" + item.text + quotedInput(item.quote),
		includeText:  inputMode != "image",
	}
	if inputMode != "text" {
		call.images = images
	}
	result, err := e.callModel(ctx, target, call)
	if err != nil {
		return "", err
	}
	published, _ := parseInstant(item.publishedAt)
	normalized, err := profile.normalizeResult(result, item.id, item.url, item.matchText(), published, e.now())
	if err != nil {
		return "", err
	}
	status := "success"
	if normalized.classification == "uncertain" {
		status = "uncertain"
	} else if err = e.applyExtraction(ctx, profile.source, item.id, extractionID, normalized); err != nil {
		return "", err
	}
	if _, err = e.upsertExtraction(ctx, profile.source, item.id, fingerprint, version, target.model, status, result, ""); err != nil {
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
