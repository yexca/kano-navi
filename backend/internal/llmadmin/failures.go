package llmadmin

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/httpx"
)

// recentFailureLimit bounds the recent failure list.
const recentFailureLimit = 30

// cancelledMessage marks a run that was stopped, which is not a model failure.
const cancelledMessage = "LLM request cancelled"

// Failure is one stored failed model result. Message is one of the fixed,
// sanitized messages the schedule and translate steps store; it never holds
// upstream response wording.
type Failure struct {
	// Route is the queue the call came from: schedule, verify or translate.
	Route string `json:"route"`
	// Subject is what the call was about: post, image or video.
	Subject    string  `json:"subject"`
	SubjectID  string  `json:"subjectId"`
	URL        *string `json:"url"`
	ProviderID *string `json:"providerId"`
	Model      *string `json:"model"`
	Message    string  `json:"message"`
	At         string  `json:"at"`
}

// RecentFailures lists the newest failed schedule extractions, schedule-image
// checks and machine translations, newest first. A translation counts only
// while its last attempt failed; extraction and image rows keep the outcome of
// their own input and target until that pair runs again.
func RecentFailures(ctx context.Context, db Querier) ([]Failure, error) {
	return collect(ctx, db, scanFailure, `SELECT route, subject, subject_id, url, version, model, message, at FROM (
		SELECT 'schedule' AS route, 'post' AS subject, e.source_item_id AS subject_id,
			(SELECT p.url FROM posts p WHERE p.id = e.source_item_id) AS url,
			e.extractor_version AS version, e.model AS model, e.error AS message, e.updated_at AS at
		FROM schedule_extractions e
		WHERE e.status = 'failed' AND COALESCE(e.error, '') NOT IN ('', ?)
			AND e.source_item_id NOT IN (SELECT id FROM posts WHERE deleted_at IS NOT NULL)
		UNION ALL
		SELECT 'verify', 'image', r.asset_id, a.source_url, NULL, r.llm_model, r.llm_reason,
			COALESCE(r.llm_checked_at, r.updated_at)
		FROM schedule_asset_reviews r LEFT JOIN assets a ON a.id = r.asset_id
		WHERE r.llm_status = 'failed' AND COALESCE(r.llm_reason, '') NOT IN ('', ?)
			AND r.asset_id NOT IN (SELECT 'schedule-x-'||id FROM posts WHERE deleted_at IS NOT NULL)
		UNION ALL
		SELECT 'translate', ts.owner_type, ts.owner_id,
			CASE ts.owner_type
				WHEN 'post' THEN (SELECT p.url FROM posts p WHERE p.id = ts.owner_id)
				WHEN 'video' THEN (SELECT v.url FROM videos v WHERE v.id = ts.owner_id)
			END,
			NULL, NULL, ts.last_error, COALESCE(ts.last_attempt_at, ts.updated_at)
		FROM translation_states ts
		WHERE ts.status = 'failed' AND COALESCE(ts.last_error, '') NOT IN ('', ?)
			AND NOT (ts.owner_type = 'post' AND ts.owner_id IN (SELECT id FROM posts WHERE deleted_at IS NOT NULL))
	) ORDER BY julianday(at) DESC, route ASC, subject_id ASC LIMIT ?`,
		cancelledMessage, cancelledMessage, cancelledMessage, recentFailureLimit)
}

func scanFailure(scan scanner) (Failure, error) {
	var f Failure
	var url, version, model sql.NullString
	if err := scan(&f.Route, &f.Subject, &f.SubjectID, &url, &version, &model, &f.Message, &f.At); err != nil {
		return f, err
	}
	f.URL, f.Model = nullableString(url.String), nullableString(model.String)
	f.ProviderID = extractionProvider(version.String)
	return f, nil
}

// extractionProvider reads the provider ID from an extractor version
// ("<version>:<detection>:<input>:<provider>:<protocol>:<hash>"). Provider IDs
// never contain a colon.
func extractionProvider(version string) *string {
	parts := strings.Split(version, ":")
	if len(parts) != 6 {
		return nil
	}
	return nullableString(parts[3])
}

func (m *Module) getFailures(w http.ResponseWriter, r *http.Request) {
	items, err := RecentFailures(r.Context(), m.db)
	if err != nil {
		m.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
