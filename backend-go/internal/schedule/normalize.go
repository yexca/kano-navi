package schedule

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// messageError carries one of the fixed, sanitized messages stored in
// schedule_extractions.error,
// post_llm_states.last_error and schedule_asset_reviews.llm_reason.
type messageError struct{ message string }

func (e *messageError) Error() string { return e.message }

func fail(message string) error { return &messageError{message} }

// Japan has no daylight saving time, so a fixed zone avoids needing tzdata.
var jst = time.FixedZone("JST", 9*3600)

var (
	datePattern = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	timePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

// parseDate mirrors validDate: Date.UTC maps years 0-99 to 1900-1999, so
// those never round-trip and are rejected.
func parseDate(value string) (year, month, day int, ok bool) {
	match := datePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, 0, 0, false
	}
	year, _ = strconv.Atoi(match[1])
	month, _ = strconv.Atoi(match[2])
	day, _ = strconv.Atoi(match[3])
	if year < 100 || month < 1 || month > 12 || day < 1 {
		return 0, 0, 0, false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || int(date.Month()) != month || date.Day() != day {
		return 0, 0, 0, false
	}
	return year, month, day, true
}

func japanInstant(date, clock string) time.Time {
	year, month, day, _ := parseDate(date)
	hour, _ := strconv.Atoi(clock[:2])
	minute, _ := strconv.Atoi(clock[3:5])
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, jst)
}

var cancelledEventStatuses = map[string]bool{
	"cancelled": true,
	"canceled":  true,
	"cancel":    true,
	"取消":        true,
	"已取消":       true,
	"中止":        true,
	"キャンセル":     true,
}

func isCancelledEventStatus(status any) bool {
	value := jsToLower(jsTrim(stringOr(status, "")))
	return cancelledEventStatuses[value] || strings.HasPrefix(value, "cancel")
}

// normalizedEvent is one model entry after validation, in the shape the event
// writer takes.
type normalizedEvent struct {
	id                    string
	canonicalSource       string
	canonicalSourceItemID string
	canonicalSourceKey    string
	sourceKey             string
	title                 string
	detail                *string
	startsOn              string
	startsAt              *string
	endsAt                *string
	timePrecision         string
	cancellationSuspected bool
	status                string
	eventType             string
	url                   string
	isUpcoming            bool
	confidence            float64
	evidence              string
}

// rawObject reproduces the property order of the JavaScript event object so
// raw_json keeps one stable form across stored rows.
func (e normalizedEvent) rawObject() *object {
	return obj(
		"id", e.id,
		"canonicalSource", e.canonicalSource,
		"canonicalSourceItemId", e.canonicalSourceItemID,
		"canonicalSourceKey", e.canonicalSourceKey,
		"source_key", e.sourceKey,
		"title", e.title,
		"detail", e.detail,
		"starts_on", e.startsOn,
		"starts_at", e.startsAt,
		"ends_at", e.endsAt,
		"timezone", "Asia/Tokyo",
		"time_precision", e.timePrecision,
		"cancellationSuspected", e.cancellationSuspected,
		"status", e.status,
		"event_type", e.eventType,
		"url", e.url,
		"is_upcoming", e.isUpcoming,
		"confidence", e.confidence,
		"raw", obj("evidence", e.evidence),
	)
}

var (
	timePrecisions  = map[string]bool{"exact": true, "approximate": true, "unknown": true}
	eventTypes      = map[string]bool{"event": true, "stream": true, "member": true, "release": true, "appearance": true}
	classifications = map[string]bool{"schedule": true, "not_schedule": true, "uncertain": true}
	actions         = map[string]bool{"add": true, "update": true, "cancel": true, "none": true}
)

func validClock(value any, defined bool) bool {
	return !defined || value == nil || timePattern.MatchString(jsString(value))
}

func normalizeExtractedEvents(items []any, postID, postURL string, now time.Time) ([]normalizedEvent, error) {
	for _, item := range items {
		event, _ := item.(*object)
		if event == nil {
			return nil, fail("schedule extraction contained an invalid event")
		}
		title, _ := event.get("title")
		date, _ := event.get("date")
		clock, clockDefined := event.get("time")
		endClock, endDefined := event.get("endTime")
		precision, _ := event.get("timePrecision")
		eventType, _ := event.get("eventType")
		confidenceValue, confidenceDefined := event.get("confidence")
		confidence := jsNumber(confidenceValue, confidenceDefined)
		precisionText, precisionIsString := precision.(string)
		typeText, typeIsString := eventType.(string)
		_, _, _, dateOK := parseDate(stringOr(date, ""))
		valid := jsTrim(stringOr(title, "")) != "" &&
			dateOK &&
			validClock(clock, clockDefined) &&
			validClock(endClock, endDefined) &&
			!(truthy(endClock) && !truthy(clock)) &&
			precisionIsString && timePrecisions[precisionText] &&
			typeIsString && eventTypes[typeText] &&
			finite(confidence) && confidence >= 0 && confidence <= 1
		if !valid {
			return nil, fail("schedule extraction contained an invalid event")
		}
	}
	out := make([]normalizedEvent, 0, len(items))
	for _, item := range items {
		event := item.(*object)
		titleValue, _ := event.get("title")
		dateValue, _ := event.get("date")
		clockValue, _ := event.get("time")
		endValue, _ := event.get("endTime")
		precision, _ := event.get("timePrecision")
		eventType, _ := event.get("eventType")
		statusValue, _ := event.get("status")
		detailValue, _ := event.get("detail")
		urlValue, _ := event.get("url")
		evidenceValue, _ := event.get("evidence")
		confidenceValue, confidenceDefined := event.get("confidence")

		title := sliceUTF16(jsTrim(jsString(titleValue)), 240)
		date := jsString(dateValue)
		clock := ""
		if truthy(clockValue) {
			clock = jsString(clockValue)
		}
		clockLabel := clock
		if clockLabel == "" {
			clockLabel = "unknown"
		}
		sourceKey := date + "T" + clockLabel + ":" + sha256Hex(jsToLower(title))[:10]
		eventURL := ""
		if truthy(urlValue) {
			eventURL = jsString(urlValue)
		}
		youtubeID := youtubeIDFromURL(eventURL)
		normalized := normalizedEvent{
			id:                    "x-schedule-" + strings.ReplaceAll(date, "-", "") + "-" + sha256Hex(sourceKey)[:10],
			canonicalSource:       "x",
			canonicalSourceItemID: postID,
			canonicalSourceKey:    sourceKey,
			sourceKey:             sourceKey,
			title:                 title,
			startsOn:              date,
			timePrecision:         "unknown",
			eventType:             eventType.(string),
			url:                   normalizePublicURL(eventURL, postURL),
			isUpcoming:            true,
			evidence:              sliceUTF16(stringOr(evidenceValue, ""), 1000),
		}
		if youtubeID != "" {
			normalized.id = "youtube-" + youtubeID
			normalized.canonicalSource = "youtube"
			normalized.canonicalSourceItemID = youtubeID
			normalized.canonicalSourceKey = "reservation"
		}
		if truthy(detailValue) {
			detail := sliceUTF16(jsTrim(jsString(detailValue)), 500)
			normalized.detail = &detail
		}
		if clock != "" {
			start := japanInstant(date, clock)
			startsAt := sqlite.ISOTime(start)
			normalized.startsAt = &startsAt
			normalized.timePrecision = precision.(string)
			normalized.isUpcoming = !start.Before(now.Truncate(time.Millisecond))
		}
		if truthy(endValue) {
			end := japanInstant(date, jsString(endValue))
			if clock != "" && end.Before(japanInstant(date, clock)) {
				end = end.AddDate(0, 0, 1)
			}
			endsAt := sqlite.ISOTime(end)
			normalized.endsAt = &endsAt
		}
		normalized.cancellationSuspected = isCancelledEventStatus(statusValue)
		status := "pending"
		if !normalized.cancellationSuspected {
			status = stringOr(statusValue, "pending")
		}
		normalized.status = sliceUTF16(jsTrim(status), 80)
		confidence := jsNumber(confidenceValue, confidenceDefined)
		if math.IsNaN(confidence) {
			confidence = 0
		}
		normalized.confidence = math.Min(1, math.Max(0, confidence))
		out = append(out, normalized)
	}
	return out, nil
}

// inferredClassification accepts results stored before the classification
// field existed: those are schedules exactly when they carry events.
func inferredClassification(result any) (string, error) {
	root, _ := result.(*object)
	if value, ok := root.get("classification"); ok {
		if text, isString := value.(string); isString {
			normalized := jsToLower(jsTrim(text))
			if classifications[normalized] {
				return normalized, nil
			}
			return "", fail("schedule extraction returned an invalid classification")
		}
	}
	if events, _ := root.get("events"); events != nil {
		if list, isArray := events.([]any); isArray && len(list) > 0 {
			return "schedule", nil
		}
	}
	return "not_schedule", nil
}

type normalizedExtraction struct {
	classification string
	events         []normalizedEvent
	confidence     float64
	evidence       string
	reason         string
	action         string
}

func normalizeExtractionResult(result any, postID, postURL string, now time.Time) (normalizedExtraction, error) {
	classification, err := inferredClassification(result)
	if err != nil {
		return normalizedExtraction{}, err
	}
	root, _ := result.(*object)
	var items []any
	if value, _ := root.get("events"); value != nil {
		items, _ = value.([]any)
	}
	events, err := normalizeExtractedEvents(items, postID, postURL, now)
	if err != nil {
		return normalizedExtraction{}, err
	}
	if classification == "not_schedule" && len(events) > 0 {
		return normalizedExtraction{}, fail("not_schedule extraction contained events")
	}
	if classification == "uncertain" && len(events) > 0 {
		return normalizedExtraction{}, fail("uncertain extraction contained events")
	}
	confidence := 0.0
	if value, _ := root.get("confidence"); value == nil {
		for _, event := range events {
			confidence = math.Max(confidence, event.confidence)
		}
	} else {
		confidence = jsNumber(value, true)
	}
	if !finite(confidence) || confidence < 0 || confidence > 1 {
		return normalizedExtraction{}, fail("schedule extraction returned an invalid confidence")
	}
	fallbackAction := "none"
	if classification == "schedule" {
		fallbackAction = "add"
	}
	actionValue, _ := root.get("action")
	action := jsToLower(jsTrim(stringOr(actionValue, fallbackAction)))
	if !actions[action] {
		return normalizedExtraction{}, fail("schedule extraction returned an invalid action")
	}
	evidence, _ := root.get("evidence")
	reason, _ := root.get("reason")
	return normalizedExtraction{
		classification: classification,
		events:         events,
		confidence:     confidence,
		evidence:       sliceUTF16(jsTrim(stringOr(evidence, "")), 1000),
		reason:         sliceUTF16(jsTrim(stringOr(reason, "")), 500),
		action:         action,
	}, nil
}

type assetVerification struct {
	classification string
	confidence     float64
	evidence       string
	reason         string
}

func normalizeAssetVerification(result any) (assetVerification, error) {
	root, _ := result.(*object)
	value, _ := root.get("classification")
	classification := jsToLower(jsTrim(stringOr(value, "")))
	if !classifications[classification] {
		return assetVerification{}, fail("schedule image verification returned an invalid classification")
	}
	confidenceValue, defined := root.get("confidence")
	confidence := jsNumber(confidenceValue, defined)
	if !finite(confidence) || confidence < 0 || confidence > 1 {
		return assetVerification{}, fail("schedule image verification returned an invalid confidence")
	}
	evidence, _ := root.get("evidence")
	reason, _ := root.get("reason")
	return assetVerification{
		classification: classification,
		confidence:     confidence,
		evidence:       sliceUTF16(jsTrim(stringOr(evidence, "")), 1000),
		reason:         sliceUTF16(jsTrim(stringOr(reason, "")), 500),
	}, nil
}

// parseInstant mirrors Date.parse for the timestamp shapes stored in the
// snapshot. ok=false stands for NaN.
func parseInstant(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// instantMillis returns epoch milliseconds, or 0 when the value is not a date.
func instantMillis(value string) int64 {
	parsed, ok := parseInstant(value)
	if !ok {
		return 0
	}
	return parsed.UnixMilli()
}
