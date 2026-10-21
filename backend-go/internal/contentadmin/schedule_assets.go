package contentadmin

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// scheduleAsset is one schedule image with its review state and the browser
// safe media reference. The source post text never leaves the server.
type scheduleAsset struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	URL                 string   `json:"url"`
	SourceURL           *string  `json:"sourceUrl"`
	Alt                 *string  `json:"alt"`
	WeekStart           *string  `json:"weekStart"`
	PeriodStart         *string  `json:"periodStart"`
	PeriodEnd           *string  `json:"periodEnd"`
	PeriodBasis         *string  `json:"periodBasis"`
	InferredPeriodStart *string  `json:"inferredPeriodStart"`
	InferredPeriodEnd   *string  `json:"inferredPeriodEnd"`
	PeriodReason        *string  `json:"periodReason"`
	SourceAccount       *string  `json:"sourceAccount"`
	AssetUpdatedAt      *string  `json:"assetUpdatedAt"`
	AssetVersion        int64    `json:"assetVersion"`
	LLMStatus           string   `json:"llmStatus"`
	LLMConfidence       *float64 `json:"llmConfidence"`
	LLMReason           *string  `json:"llmReason"`
	LLMEvidence         *string  `json:"llmEvidence"`
	LLMModel            *string  `json:"llmModel"`
	LLMCheckedAt        *string  `json:"llmCheckedAt"`
	LLMInputFingerprint *string  `json:"llmInputFingerprint"`
	ManualStatus        string   `json:"manualStatus"`
	ManualReason        *string  `json:"manualReason"`
	ManualCheckedAt     *string  `json:"manualCheckedAt"`
	ReviewUpdatedAt     *string  `json:"reviewUpdatedAt"`
	EffectiveStatus     string   `json:"effectiveStatus"`
	SourceMatchesBoard  bool     `json:"sourceMatchesBoard"`
	Approved            bool     `json:"approved"`
	PreviewURL          *string  `json:"previewUrl"`
	MediaID             *string  `json:"mediaId"`
	MediaStatus         string   `json:"mediaStatus"`
	MediaSourceURL      *string  `json:"mediaSourceUrl"`
}

// The source post text is read from raw_json so a quoted post counts the same
// way it did when synchronization promoted the image.
const scheduleAssetColumns = `
  a.id, a.kind, a.url, a.source_url,
  a.alt, COALESCE(date(a.manual_period_start, 'weekday 0', '-6 days'), a.week_start),
  COALESCE(a.manual_period_start, a.period_start),
  COALESCE(a.manual_period_end, a.period_end),
  CASE WHEN a.manual_period_start IS NOT NULL THEN 'manual' ELSE a.period_basis END,
  a.period_start, a.period_end,
  a.manual_period_reason, a.source_account,
  a.updated_at, a.asset_version,
  (SELECT COALESCE(json_extract(p.raw_json, '$.search_text'), p.text)
     FROM posts p WHERE p.url = a.source_url
     ORDER BY p.published_at DESC LIMIT 1),
  r.llm_status, r.llm_confidence,
  r.llm_reason, r.llm_evidence,
  r.llm_model, r.llm_checked_at,
  r.llm_input_fingerprint,
  COALESCE(r.manual_status, 'unreviewed'),
  r.manual_reason, r.manual_checked_at,
  r.updated_at`

const scheduleAssetFrom = ` FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id = a.id `

// Schedule images must be weekly-board notices, not any post that merely
// mentions a future stream.
var scheduleBoardText = regexp.MustCompile(`(?i)(?:\b(?:weekly[\s_-]+)?schedule(?:[\s_-]+(?:board|table))?\b|\bthis\s+week(?:'s)?\s+schedule\b|スケジュール|予定表|(?:今週|来週|今月|週間).{0,4}(?:予定|スケジュール))`)

func oneOf(value, fallback string, allowed ...string) string {
	normalized := strings.ToLower(jsTrim(value))
	for _, candidate := range allowed {
		if normalized == candidate {
			return normalized
		}
	}
	return fallback
}

// scanScheduleAsset applies the approval rule: an operator label wins;
// otherwise the image check must say `schedule` and the source post, when it
// is still known, must read as a board notice.
func scanScheduleAsset(row rowScanner) (*scheduleAsset, error) {
	var asset scheduleAsset
	var sourceURL, alt, weekStart, periodStart, periodEnd, periodBasis, inferredStart, inferredEnd, periodReason sql.NullString
	var sourceAccount, assetUpdatedAt, sourceText, llmStatus, llmReason, llmEvidence, llmModel, llmCheckedAt, fingerprint sql.NullString
	var manualStatus, manualReason, manualCheckedAt, reviewUpdatedAt sql.NullString
	var confidence sql.NullFloat64
	var version sql.NullInt64
	err := row.Scan(&asset.ID, &asset.Kind, &asset.URL, &sourceURL, &alt, &weekStart, &periodStart, &periodEnd, &periodBasis,
		&inferredStart, &inferredEnd, &periodReason, &sourceAccount, &assetUpdatedAt, &version, &sourceText,
		&llmStatus, &confidence, &llmReason, &llmEvidence, &llmModel, &llmCheckedAt, &fingerprint,
		&manualStatus, &manualReason, &manualCheckedAt, &reviewUpdatedAt)
	if err != nil {
		return nil, err
	}
	asset.SourceURL, asset.Alt, asset.WeekStart = textPointer(sourceURL), textPointer(alt), textPointer(weekStart)
	asset.PeriodStart, asset.PeriodEnd, asset.PeriodBasis = textPointer(periodStart), textPointer(periodEnd), textPointer(periodBasis)
	asset.InferredPeriodStart, asset.InferredPeriodEnd, asset.PeriodReason = textPointer(inferredStart), textPointer(inferredEnd), textPointer(periodReason)
	asset.SourceAccount, asset.AssetUpdatedAt = textPointer(sourceAccount), textPointer(assetUpdatedAt)
	asset.AssetVersion = version.Int64
	asset.LLMReason, asset.LLMEvidence, asset.LLMModel = textPointer(llmReason), textPointer(llmEvidence), textPointer(llmModel)
	asset.LLMCheckedAt, asset.LLMInputFingerprint = textPointer(llmCheckedAt), textPointer(fingerprint)
	asset.ManualReason, asset.ManualCheckedAt, asset.ReviewUpdatedAt = textPointer(manualReason), textPointer(manualCheckedAt), textPointer(reviewUpdatedAt)
	if confidence.Valid && !math.IsNaN(confidence.Float64) && !math.IsInf(confidence.Float64, 0) {
		asset.LLMConfidence = &confidence.Float64
	}
	asset.LLMStatus = oneOf(llmStatus.String, "pending", "pending", "running", "schedule", "not_schedule", "uncertain", "failed", "skipped")
	asset.ManualStatus = oneOf(manualStatus.String, "unreviewed", "unreviewed", "schedule", "not_schedule")
	asset.EffectiveStatus = asset.ManualStatus
	if asset.ManualStatus == "unreviewed" {
		asset.EffectiveStatus = asset.LLMStatus
	}
	asset.SourceMatchesBoard = !sourceText.Valid || scheduleBoardText.MatchString(sourceText.String)
	asset.Approved = asset.ManualStatus == "schedule" ||
		(asset.ManualStatus == "unreviewed" && asset.LLMStatus == "schedule" && asset.SourceMatchesBoard)
	return &asset, nil
}

// getScheduleAsset returns nil without an error for an unknown schedule image.
func (m *Module) getScheduleAsset(ctx context.Context, id string) (*scheduleAsset, error) {
	asset, err := scanScheduleAsset(m.db.QueryRowContext(ctx, `SELECT `+scheduleAssetColumns+scheduleAssetFrom+`WHERE a.id = ? AND a.kind = 'schedule'`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return asset, m.attachMedia(ctx, asset)
}

func (m *Module) listScheduleAssets(ctx context.Context, limit int) ([]scheduleAsset, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT `+scheduleAssetColumns+scheduleAssetFrom+`WHERE a.kind = 'schedule'
	   ORDER BY COALESCE(a.week_start, a.updated_at) DESC, a.id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := []scheduleAsset{}
	for rows.Next() {
		asset, scanErr := scanScheduleAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		assets = append(assets, *asset)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// The cursor must be closed before the per-asset media lookups run on the
	// single pooled connection.
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range assets {
		if err = m.attachMedia(ctx, &assets[i]); err != nil {
			return nil, err
		}
	}
	return assets, nil
}

// attachMedia maps the stored image URL to a safe browser URL while keeping
// the source and cache status visible to the operator.
func (m *Module) attachMedia(ctx context.Context, asset *scheduleAsset) error {
	asset.PreviewURL, asset.MediaID, asset.MediaSourceURL, asset.MediaStatus = nil, nil, nil, "missing"
	source := asset.URL
	if source == "" {
		return nil
	}
	if strings.HasPrefix(source, "/assets/") {
		if m.publicAssetExists(source) {
			asset.MediaStatus, asset.PreviewURL = "ready", &source
		}
		return nil
	}
	normalized, _ := normalizeSourceURL(source)
	if normalized == "" {
		return nil
	}
	var id, status string
	var sourceURL, cachePath, contentHash sql.NullString
	err := m.db.QueryRowContext(ctx, `SELECT id, source_url, cache_path, sha256, status FROM media_assets WHERE source_url = ?`, normalized).
		Scan(&id, &sourceURL, &cachePath, &contentHash, &status)
	if errors.Is(err, sql.ErrNoRows) {
		pendingID := sha256Hex([]byte(normalized))
		asset.MediaID, asset.MediaSourceURL, asset.MediaStatus = &pendingID, &normalized, "pending"
		return nil
	}
	if err != nil {
		return err
	}
	asset.MediaID, asset.MediaSourceURL = &id, textPointer(sourceURL)
	ready := status == "ready" && cachePath.Valid && safeHash(contentHash.String)
	if ready {
		_, pathErr := m.containedMediaFile(cachePath.String, m.allMediaRoots()...)
		ready = pathErr == nil
	}
	switch {
	case ready:
		asset.MediaStatus = "ready"
		// Only opaque 64-hex identities are served by the public media route.
		if safeHash(id) {
			publicURL := "/media/" + id + "?v=" + contentHash.String
			asset.PreviewURL = &publicURL
		}
	case status == "ready" || status == "":
		asset.MediaStatus = "missing"
	default:
		asset.MediaStatus = status
	}
	return nil
}

// boundedLimit mirrors `Math.min(200, Math.max(1, Number(limit) || 100))`.
func boundedLimit(values []string) int {
	if len(values) != 1 {
		return 100
	}
	number, err := strconv.ParseFloat(jsTrim(values[0]), 64)
	if err != nil || math.IsNaN(number) || number == 0 {
		return 100
	}
	return int(math.Min(200, math.Max(1, math.Trunc(number))))
}

func (m *Module) listScheduleAssetsRoute(w http.ResponseWriter, r *http.Request) {
	assets, err := m.listScheduleAssets(r.Context(), boundedLimit(r.URL.Query()["limit"]))
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": assets})
}

func (m *Module) writeScheduleAsset(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := m.getScheduleAsset(r.Context(), id)
	if err != nil {
		writeFailure(w, err)
		return
	}
	if asset == nil {
		httpx.WriteCode(w, http.StatusNotFound, "schedule_image_not_found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"image": asset})
}

var errScheduleAssetNotFound = errors.New("schedule asset not found")

func scheduleAssetExists(ctx context.Context, tx *sql.Tx, id string) error {
	var found int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM assets WHERE id = ? AND kind = 'schedule'`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return errScheduleAssetNotFound
	}
	return err
}

func (m *Module) manualReviewRoute(w http.ResponseWriter, r *http.Request, id string) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	statusText, err := textField(body, "status", true, 24)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	status := *statusText
	if status != "unreviewed" && status != "schedule" && status != "not_schedule" {
		httpx.WriteError(w, httpx.Invalid("status is invalid"))
		return
	}
	var reason *string
	if status != "unreviewed" {
		if reason, err = textField(body, "reason", true, 500); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	ctx, now := r.Context(), m.timestamp()
	var checkedAt any
	if status != "unreviewed" {
		checkedAt = now
	}
	// The manual label survives later image checks: the model writer only
	// updates the llm_* columns of an existing review.
	err = m.inTx(ctx, func(tx *sql.Tx) error {
		if existsErr := scheduleAssetExists(ctx, tx, id); existsErr != nil {
			return existsErr
		}
		if _, execErr := tx.ExecContext(ctx, `INSERT INTO schedule_asset_reviews (
		     asset_id, llm_status, llm_confidence, llm_reason, llm_evidence,
		     llm_model, llm_checked_at, manual_status, manual_reason,
		     manual_checked_at, updated_at
		   ) VALUES (?, 'pending', NULL, NULL, NULL, NULL, NULL, ?, ?, ?, ?)
		   ON CONFLICT(asset_id) DO UPDATE SET
		     manual_status=excluded.manual_status,
		     manual_reason=excluded.manual_reason,
		     manual_checked_at=excluded.manual_checked_at,
		     updated_at=excluded.updated_at`, id, status, nullString(reason), checkedAt, now); execErr != nil {
			return execErr
		}
		return sqlite.BumpRevisionTx(ctx, tx, now)
	})
	if errors.Is(err, errScheduleAssetNotFound) {
		httpx.WriteCode(w, http.StatusNotFound, "schedule_image_not_found")
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	m.writeScheduleAsset(w, r, id)
}

func (m *Module) periodRoute(w http.ResponseWriter, r *http.Request, id string) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	reason, err := textField(body, "reason", true, 500)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	start, startIsText := stringField(body, "start")
	end, endIsText := stringField(body, "end")
	startDate, startOK := calendarDate(start)
	endDate, endOK := calendarDate(end)
	if !startIsText || !endIsText || !startOK || !endOK || end < start || endDate.Sub(startDate).Hours() > 366*24 {
		httpx.WriteError(w, httpx.Invalid("period is invalid"))
		return
	}
	ctx, now := r.Context(), m.timestamp()
	err = m.inTx(ctx, func(tx *sql.Tx) error {
		result, execErr := tx.ExecContext(ctx, `UPDATE assets SET manual_period_start=?, manual_period_end=?, manual_period_reason=? WHERE id=? AND kind='schedule'`,
			start, end, jsSlice(*reason, 500), id)
		if execErr != nil {
			return execErr
		}
		if changed, countErr := result.RowsAffected(); countErr != nil {
			return countErr
		} else if changed == 0 {
			return errScheduleAssetNotFound
		}
		return sqlite.BumpRevisionTx(ctx, tx, now)
	})
	if errors.Is(err, errScheduleAssetNotFound) {
		httpx.WriteCode(w, http.StatusNotFound, "schedule_image_not_found")
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	m.writeScheduleAsset(w, r, id)
}
