package schedule

import (
	"context"
	"sort"
	"strings"
	"time"
)

type assetResult struct {
	status         string
	classification string
	confidence     *float64
}

func (e *Extractor) skipAsset(ctx context.Context, update reviewUpdate, reason string) (assetResult, error) {
	update.llmStatus = "skipped"
	update.llmReason = reason
	_, err := e.upsertAssetReview(ctx, update)
	return assetResult{status: "skipped"}, err
}

// hasImage reports whether a schedule asset names an image at all. A row
// without a URL has nothing to download, so it is recorded once as
// `skipped` / `no_image` instead of waiting for a cache that never arrives.
func hasImage(review *assetReview) bool {
	return jsTrim(review.url) != ""
}

// settledWithoutImage is a URL-less asset already recorded as `no_image`. It
// stays out of later scans until a writer gives the asset an image URL.
func settledWithoutImage(review *assetReview) bool {
	return !hasImage(review) && review.llmStatus == "skipped" && review.llmReason == "no_image"
}

// assetImage resolves the cached file behind a schedule asset. A missing or
// unfinished download keeps the review pending.
func (e *Extractor) assetImage(ctx context.Context, review *assetReview) (*cachedImage, error) {
	media, err := getMediaAsset(ctx, e.db, mediaIDForSourceURL(review.url))
	if err != nil || media == nil {
		return nil, err
	}
	if media.status != "ready" || media.sha256 == "" || !strings.HasPrefix(media.mimeType, "image/") {
		return nil, nil
	}
	path, ok := e.cachedFile(media.cachePath)
	if !ok {
		return nil, nil
	}
	return &cachedImage{path: path, mimeType: media.mimeType, sha256: media.sha256}, nil
}

// verifyAsset asks an image-capable provider whether one schedule asset
// really is a schedule board. The source-text gate runs first, so an image
// whose post does not read as a board notice never reaches a model and can
// only be published by a manual label.
func (e *Extractor) verifyAsset(ctx context.Context, assetID string, force bool) (assetResult, error) {
	review, err := getAssetReview(ctx, e.db, assetID)
	if err != nil {
		return assetResult{}, err
	}
	if review == nil {
		return assetResult{status: "missing"}, nil
	}
	update := reviewUpdate{assetID: review.id, expectURL: review.url, expectVersion: review.assetVersion}
	if !hasImage(review) {
		return e.skipAsset(ctx, update, "no_image")
	}
	if !review.sourceMatchesBoard {
		return e.skipAsset(ctx, update, "source_not_board")
	}
	image, err := e.assetImage(ctx, review)
	if err != nil {
		return assetResult{}, err
	}
	if image == nil {
		return e.skipAsset(ctx, update, "media_pending")
	}
	update.expectSHA = image.sha256
	providers, reason, err := e.configuredProviders(ctx, "image")
	if err != nil {
		return assetResult{}, err
	}
	if len(providers) == 0 {
		return e.skipAsset(ctx, update, reason)
	}
	identities := make([]any, len(providers))
	for i, target := range providers {
		identities[i] = []any{target.id, target.model, target.protocol, target.baseURL}
	}
	var sourceURL any
	if review.sourceURL.Valid {
		sourceURL = review.sourceURL.String
	}
	// The source text is not part of this identity: the original review row
	// never exposed it to the hash.
	fingerprint := sha256Hex(stringify(obj(
		"version", extractorVersion,
		"image", image.sha256,
		"sourceUrl", sourceURL,
		"providers", identities,
	)))
	if !force && review.llmInputFingerprint == fingerprint && classifications[review.llmStatus] {
		result := assetResult{status: "cached", classification: review.llmStatus}
		if review.llmConfidence.Valid {
			result.confidence = &review.llmConfidence.Float64
		}
		return result, nil
	}
	running := update
	running.llmStatus = "running"
	if _, err = e.upsertAssetReview(ctx, running); err != nil {
		return assetResult{}, err
	}
	abandon := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		failed := update
		failed.llmStatus = "failed"
		failed.llmReason = cancelledMessage
		failed.llmCheckedAt = e.nowISO()
		_, _ = e.upsertAssetReview(cleanup, failed)
	}
	sourceLink := ""
	if review.sourceURL.Valid {
		sourceLink = review.sourceURL.String
	}
	call := modelCall{
		instructions: scheduleAssetVerificationInstructions,
		name:         verificationToolName,
		schema:       scheduleAssetVerificationSchema,
		maxTokens:    800,
		text:         "Candidate schedule image. Source URL: " + sourceLink,
		includeText:  true,
		images:       []cachedImage{*image},
	}
	message := "all configured LLM providers failed"
	for _, target := range providers {
		for attempt := 0; attempt <= llmMaxRetries; attempt++ {
			result, applied, err := e.attemptVerification(ctx, target, call, update, fingerprint)
			if err == nil {
				if !applied {
					return assetResult{status: "stale"}, nil
				}
				return assetResult{status: "success", classification: result.classification, confidence: &result.confidence}, nil
			}
			if ctx.Err() != nil {
				abandon()
				return assetResult{}, ctx.Err()
			}
			message = storedMessage(err)
			if attempt < llmMaxRetries {
				if err = e.sleep(ctx, retryDelay(attempt)); err != nil {
					abandon()
					return assetResult{}, err
				}
			}
		}
	}
	failed := update
	failed.llmStatus = "failed"
	failed.llmReason = message
	failed.llmCheckedAt = e.nowISO()
	_, err = e.upsertAssetReview(ctx, failed)
	return assetResult{status: "failed"}, err
}

func (e *Extractor) attemptVerification(ctx context.Context, target provider, call modelCall, update reviewUpdate, fingerprint string) (assetVerification, bool, error) {
	raw, err := e.callModel(ctx, target, call)
	if err != nil {
		return assetVerification{}, false, err
	}
	result, err := normalizeAssetVerification(raw)
	if err != nil {
		return assetVerification{}, false, err
	}
	update.llmStatus = result.classification
	update.llmConfidence = &result.confidence
	update.llmReason = result.reason
	update.llmEvidence = result.evidence
	update.llmModel = target.model
	update.llmCheckedAt = e.nowISO()
	update.llmInputFingerprint = fingerprint
	applied, err := e.upsertAssetReview(ctx, update)
	if err != nil || !applied {
		return result, false, err
	}
	return result, true, e.updateProviderStatus(ctx, target.id, "success", "")
}

// verifyPendingAssets reviews unlabelled schedule images, least recently
// checked first. Assets that share one image URL are verified once and the
// verdict is copied to the others.
func (e *Extractor) verifyPendingAssets(ctx context.Context, limit int, force bool) (map[string]any, error) {
	budget := min(200, max(1, limit))
	all, err := listAssetReviews(ctx, e.db)
	if err != nil {
		return nil, err
	}
	// Date.parse(0) is the year 2000 in V8, which sorts never-checked assets
	// ahead of every real timestamp.
	never := time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	checkedAt := func(review *assetReview) int64 {
		value := review.llmCheckedAt
		if value == "" {
			value = review.reviewUpdatedAt
		}
		if parsed, ok := parseInstant(value); ok {
			return parsed.UnixMilli()
		}
		return never
	}
	candidates := []*assetReview{}
	for _, review := range all {
		if review.manualStatus == "unreviewed" && !settledWithoutImage(review) {
			candidates = append(candidates, review)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return checkedAt(candidates[i]) < checkedAt(candidates[j]) })
	// A Map keeps the first position of a key but its last value.
	unique := []*assetReview{}
	position := map[string]int{}
	for _, review := range candidates {
		key := review.url
		if key == "" {
			key = review.id
		}
		if index, known := position[key]; known {
			unique[index] = review
			continue
		}
		position[key] = len(unique)
		unique = append(unique, review)
	}
	summary := map[string]any{
		"attempted": 0, "success": 0, "cached": 0, "skipped": 0, "failed": 0,
		"uncertain": 0, "schedule": 0, "notSchedule": 0,
	}
	for _, review := range unique {
		if countOf(summary, "attempted") >= budget {
			break
		}
		if err = ctx.Err(); err != nil {
			return summary, err
		}
		result, err := e.verifyAsset(ctx, review.id, force)
		if err != nil {
			return summary, err
		}
		if result.status != "cached" {
			summary["attempted"] = countOf(summary, "attempted") + 1
		}
		summary[result.status] = countOf(summary, result.status) + 1
		switch result.classification {
		case "uncertain":
			summary["uncertain"] = countOf(summary, "uncertain") + 1
		case "schedule":
			summary["schedule"] = countOf(summary, "schedule") + 1
		case "not_schedule":
			summary["notSchedule"] = countOf(summary, "notSchedule") + 1
		}
		if result.status != "success" && result.status != "cached" {
			continue
		}
		verified, err := getAssetReview(ctx, e.db, review.id)
		if err != nil {
			return summary, err
		}
		for _, duplicate := range candidates {
			if duplicate.id == review.id || duplicate.url == "" || duplicate.url != review.url {
				continue
			}
			copied := reviewUpdate{
				assetID:       duplicate.id,
				expectURL:     duplicate.url,
				expectVersion: duplicate.assetVersion,
				llmStatus:     result.classification,
				llmConfidence: result.confidence,
			}
			if verified != nil {
				copied.llmReason = verified.llmReason
				copied.llmEvidence = verified.llmEvidence
				copied.llmModel = verified.llmModel
				copied.llmInputFingerprint = verified.llmInputFingerprint
				copied.llmCheckedAt = verified.llmCheckedAt
			}
			if copied.llmCheckedAt == "" {
				copied.llmCheckedAt = e.nowISO()
			}
			if _, err = e.upsertAssetReview(ctx, copied); err != nil {
				return summary, err
			}
		}
	}
	return summary, nil
}
