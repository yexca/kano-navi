package schedule

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// messageError carries one of the fixed, sanitized messages stored in
// schedule_extractions.error,
// post_llm_states.last_error and schedule_asset_reviews.llm_reason.
type messageError struct{ message string }

func (e *messageError) Error() string { return e.message }

func fail(message string) error { return &messageError{message} }

// Japan has no daylight saving time, so a fixed zone avoids needing tzdata.
var jst = time.FixedZone("JST", 9*3600)

// China has no daylight saving time either; Beijing time is UTC+8.
var cst = time.FixedZone("CST", 8*3600)

// entryZones are the time zones an entry may name; stored times are always
// converted to Asia/Tokyo.
var entryZones = map[string]*time.Location{"Asia/Tokyo": jst, "Asia/Shanghai": cst}

var (
	datePattern = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	// Japanese schedules count late-night hours on from the evening's date
	// (25:00 is 01:00 the next day), up to 29:59.
	timePattern = regexp.MustCompile(`^([01]\d|2\d):[0-5]\d$`)
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

// zonedInstant reads a date and an HH:MM time (up to 29:59, counted on from
// the date) in zone; time.Date carries the overflow into the next day.
func zonedInstant(date, clock string, zone *time.Location) time.Time {
	year, month, day, _ := parseDate(date)
	hour, _ := strconv.Atoi(clock[:2])
	minute, _ := strconv.Atoi(clock[3:5])
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, zone)
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
	// endsOn is the Japan close date of a sale period, nil for every other
	// entry.
	endsOn *string
	// evidenceURL is the event_sources link: the event's own URL for an X
	// post, the dynamic's opus page for a Bilibili one.
	evidenceURL           string
	timePrecision         string
	cancellationSuspected bool
	status                string
	eventType             string
	// platform is youtube, bilibili, other or unknown: the first of
	// platforms, the announced platforms in stored order. platformsInferred
	// marks platforms that came from the source's default rather than from
	// the wording.
	platform          string
	platforms         []string
	platformsInferred bool
	tag               *string
	url               string
	isUpcoming        bool
	confidence        float64
	evidence          string
}

// rawObject reproduces the property order of the JavaScript event object so
// raw_json keeps one stable form across stored rows.
func (e normalizedEvent) rawObject() *object {
	pairs := []any{
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
	}
	// Only a sale period carries a close date; other entries keep the
	// recorded shape of stored rows.
	if e.endsOn != nil {
		pairs = append(pairs, "ends_on", *e.endsOn)
	}
	pairs = append(pairs,
		"timezone", "Asia/Tokyo",
		"time_precision", e.timePrecision,
		"cancellationSuspected", e.cancellationSuspected,
		"status", e.status,
		"event_type", e.eventType,
	)
	// An unlabelled entry keeps the recorded shape of stored rows.
	if e.tag != nil {
		pairs = append(pairs, "tag", *e.tag)
	}
	pairs = append(pairs,
		"url", e.url,
		"is_upcoming", e.isUpcoming,
		"confidence", e.confidence,
		"raw", obj("evidence", e.evidence),
	)
	return obj(pairs...)
}

var (
	timePrecisions  = map[string]bool{"exact": true, "approximate": true, "unknown": true}
	eventTypes      = map[string]bool{"event": true, "stream": true, "member": true, "release": true, "appearance": true, "sale": true}
	classifications = map[string]bool{"schedule": true, "not_schedule": true, "uncertain": true}
	actions         = map[string]bool{"add": true, "update": true, "cancel": true, "none": true}
)

// validClock accepts a missing, null or empty time, which all mean the time
// is not known, or HH:MM up to 29:59.
func validClock(value any, defined bool) bool {
	return !defined || value == nil || value == "" || timePattern.MatchString(jsString(value))
}

// extractedTag keeps a model label only when it is one of the known codes.
// The model is asked for null when the entry does not say, so a missing or
// null field means "no label" rather than an error. An unknown value, which a
// provider without strict schema output may return, is dropped the same way
// and leaves the rest of the entry valid.
func extractedTag(value any) *string {
	text, ok := value.(string)
	if !ok || !sqlite.ValidEventTag(text) {
		return nil
	}
	return &text
}

// rolledYear moves a date the model placed about a year before the post to
// the following year, when that lands between a week before and three months
// after the post: a December notice that says 1/5 means the coming January,
// not the past one. A date that is old either way, such as an anniversary or
// a look back at the summer, stays. A zero published time leaves every date
// alone.
func rolledYear(date string, published time.Time) string { return rolledYearIn(date, published, jst) }

// rolledYearIn is rolledYear counted from the post's date in zone.
func rolledYearIn(date string, published time.Time, zone *time.Location) string {
	if published.IsZero() {
		return date
	}
	year, month, day, ok := parseDate(date)
	if !ok || month == 2 && day == 29 {
		return date
	}
	posted := published.In(zone)
	postedDay := time.Date(posted.Year(), posted.Month(), posted.Day(), 0, 0, 0, 0, time.UTC)
	value := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if !value.Before(postedDay.AddDate(0, 0, -180)) {
		return date
	}
	next := value.AddDate(1, 0, 0)
	if next.Before(postedDay.AddDate(0, 0, -7)) || next.After(postedDay.AddDate(0, 0, 92)) {
		return date
	}
	return next.Format("2006-01-02")
}

// saleEndDate returns the rolled close date of a sale entry, "" when the
// entry is not a sale or names no close, and ok=false when the close is not a
// calendar date or falls before the opening date. Every other type ignores
// endDate: only a sale stores a period.
func saleEndDate(event *object, eventType string, startDate string, published time.Time, zone *time.Location) (string, bool) {
	value, defined := event.get("endDate")
	if eventType != "sale" || !defined || value == nil || value == "" {
		return "", true
	}
	text, isString := value.(string)
	if !isString {
		return "", false
	}
	if _, _, _, ok := parseDate(text); !ok {
		return "", false
	}
	end := rolledYearIn(text, published, zone)
	if end < rolledYearIn(startDate, published, zone) {
		return "", false
	}
	return end, true
}

// sourceProfile is what extraction varies by the source of a post: the
// prompt and schema, the time zone of times written without one, the ID
// prefix of its events and the link a stream gets.
type sourceProfile struct {
	source, idPrefix string
	zone             *time.Location
	zoneName         string
	instructions     string
	schema           rawJSON
	// streamPlatform is the platform of a stream whose entry names none:
	// a Bilibili dynamic's streams are on Bilibili.
	streamPlatform string
}

var (
	xProfile = sourceProfile{source: "x", idPrefix: "x-schedule-", zone: jst, zoneName: "Asia/Tokyo",
		instructions: scheduleInstructions, schema: scheduleSchema}
	// A Bilibili dynamic is written for Chinese viewers in Beijing time, and
	// its streams are on the account's Bilibili live room.
	bilibiliProfile = sourceProfile{source: "bilibili", idPrefix: "bilibili-schedule-", zone: cst, zoneName: "Asia/Shanghai",
		instructions: bilibiliScheduleInstructions, schema: bilibiliScheduleSchema, streamPlatform: "bilibili"}
)

// profileFor is the extraction profile of a post source; any other source
// is read as X.
func profileFor(source string) sourceProfile {
	if source == "bilibili" {
		return bilibiliProfile
	}
	return xProfile
}

// zoneAliases are the other spellings of the two zones a model may give.
var zoneAliases = map[string]*time.Location{
	"cst": cst, "utc+8": cst, "utc+08:00": cst, "gmt+8": cst, "asia/beijing": cst, "北京时间": cst, "beijing": cst,
	"jst": jst, "utc+9": jst, "utc+09:00": jst, "gmt+9": jst, "日本时间": jst, "日本時間": jst,
}

// entryZone is the zone an entry's date and times are written in: its
// timeZone or a known alias of it, or else the source's. X posts are always
// Japan time: their schema names no zone, so a stray value is ignored.
func (p sourceProfile) entryZone(event *object) *time.Location {
	if p.source == "x" {
		return p.zone
	}
	value, _ := event.get("timeZone")
	text, _ := value.(string)
	if zone, known := entryZones[jsTrim(text)]; known {
		return zone
	}
	if zone, known := zoneAliases[jsToLower(strings.ReplaceAll(jsTrim(text), " ", ""))]; known {
		return zone
	}
	return p.zone
}

func normalizeExtractedEvents(items []any, postID, postURL string, published, now time.Time) ([]normalizedEvent, error) {
	return xProfile.normalizeEvents(items, postID, postURL, "", published, now)
}

// Event platforms: where an entry happens. An unknown or invalid model
// value is unknown rather than an invalid entry.
var eventPlatforms = map[string]bool{"youtube": true, "bilibili": true, "other": true, "unknown": true}

// bilibiliOnly names a Bilibili-only stream or upload, such as
// bilibili限定配信; youtubeWording names YouTube, which makes a post that also
// says it a simulcast or a mix.
var (
	// B限 counts only as its own word: the b of WEB限定 is not Bilibili.
	bilibiliOnly   = regexp.MustCompile(`(?i)bilibili限定|b站限定|(?:^|[^a-z])b限|ビリビリ限定`)
	youtubeWording = regexp.MustCompile(`(?i)youtube|ユーチューブ|油管`)
)

// entryPlatform is the platform of one entry: the model's value, or the
// source's default for a stream that names none, or bilibili for a stream
// or release of an X post that names Bilibili. A YouTube link makes it
// youtube.
func (p sourceProfile) entryPlatform(value any, eventType, text, youtubeID string) string {
	platform := jsToLower(jsTrim(stringOr(value, "unknown")))
	if !eventPlatforms[platform] {
		platform = "unknown"
	}
	stream := eventType == "stream" || eventType == "member"
	switch {
	case youtubeID != "":
		return "youtube"
	case platform != "unknown":
		return platform
	case stream && p.streamPlatform != "":
		return p.streamPlatform
	case (stream || eventType == "release") && bilibiliOnly.MatchString(text) && !youtubeWording.MatchString(text):
		return "bilibili"
	}
	return platform
}

// simulcastWording names a broadcast on two platforms at once;
// bilibiliWording names Bilibili, and simultaneous a time shared with
// another platform, which with both platforms named is a simulcast too.
var (
	simulcastWording = regexp.MustCompile(`(?i)同時配信|同時生配信|同時放送|同時生放送|同时直播|同时配信|同步直播|simulcast`)
	bilibiliWording  = regexp.MustCompile(`(?i)bilibili|ビリビリ|b站|哔哩哔哩`)
	simultaneous     = regexp.MustCompile(`同時|同时`)
)

// entryWording is what an entry itself says: its title, detail and cited
// evidence, and the post's text when the post has this one entry only. A
// post of several entries may name platforms that only some of them use.
func entryWording(event *object, text string, single bool) string {
	parts := []string{}
	for _, key := range []string{"title", "detail", "evidence"} {
		if value, _ := event.get(key); value != nil {
			parts = append(parts, jsString(value))
		}
	}
	if single {
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

// namesSimulcast reports wording that announces an entry on both YouTube and
// Bilibili at once: a simulcast word, or both platforms named together with
// 同時 (YouTubeとbilibiliで同時に). Naming both platforms alone is not one.
func namesSimulcast(wording string) bool {
	return simulcastWording.MatchString(wording) ||
		bilibiliWording.MatchString(wording) && youtubeWording.MatchString(wording) && simultaneous.MatchString(wording)
}

// modelPlatforms reads a Bilibili entry's platforms array; a value outside
// the enum is dropped.
func modelPlatforms(event *object) []string {
	values := []string{}
	list, _ := event.get("platforms")
	items, _ := list.([]any)
	for _, item := range items {
		if text, isString := item.(string); isString {
			values = append(values, text)
		}
	}
	return sqlite.NormalizePlatforms(values)
}

// entryPlatforms is where one entry happens, in announced order, and
// whether that came from the source's default rather than the wording.
//
// An X entry keeps the single platform of entryPlatform (a live-room link
// makes it bilibili, as before), and a stream or release whose own wording
// announces a simulcast adds the other of YouTube and Bilibili after it; a
// collaboration elsewhere (other) stays other.
//
// A Bilibili entry lists what the model read, a YouTube link adding youtube
// and a live-room link bilibili unless the entry is other. Code decides
// whether bilibili alone is stated: only Bilibili-only wording (bilibili限定,
// B站限定) states it; a dynamic's plain stream, named on B站 or not, is
// bilibili by the source's default (inferred).
func (p sourceProfile) entryPlatforms(event *object, eventType, text, eventURL, youtubeID string, single bool) ([]string, bool) {
	stream := eventType == "stream" || eventType == "member"
	room := isBilibiliRoomLink(eventURL)
	wording := entryWording(event, text, single)
	if p.source == "x" {
		platformValue, _ := event.get("platform")
		platform := p.entryPlatform(platformValue, eventType, text, youtubeID)
		if room && platform != sqlite.PlatformOther {
			platform = sqlite.PlatformBilibili
		}
		if (stream || eventType == "release") && platform != sqlite.PlatformOther && namesSimulcast(wording) {
			return sqlite.NormalizePlatforms([]string{platform, sqlite.PlatformYouTube, sqlite.PlatformBilibili}), false
		}
		return sqlite.NormalizePlatforms([]string{platform}), false
	}
	platforms := modelPlatforms(event)
	if youtubeID != "" {
		platforms = append(platforms, sqlite.PlatformYouTube)
	}
	if room && !slices.Contains(platforms, sqlite.PlatformOther) {
		platforms = append(platforms, sqlite.PlatformBilibili)
	}
	platforms = sqlite.NormalizePlatforms(platforms)
	only := bilibiliOnly.MatchString(wording) && !youtubeWording.MatchString(wording)
	switch {
	case len(platforms) == 0 && (stream || eventType == "release") && only:
		return []string{sqlite.PlatformBilibili}, false
	case len(platforms) == 0 && stream && p.streamPlatform != "":
		return []string{p.streamPlatform}, true
	case len(platforms) == 1 && platforms[0] == sqlite.PlatformBilibili:
		return platforms, !only
	}
	return platforms, false
}

// isBilibiliRoomLink reports a Bilibili live-room link, which is the same
// for every broadcast and so never an entry's own link.
func isBilibiliRoomLink(value string) bool {
	u, ok := parseURL(value)
	return ok && u.web() && (u.host == "live.bilibili.com" || strings.HasSuffix(u.host, ".live.bilibili.com"))
}

func (p sourceProfile) normalizeEvents(items []any, postID, postURL, text string, published, now time.Time) ([]normalizedEvent, error) {
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
		zone := p.entryZone(event)
		endDate, endDateOK := "", false
		if dateOK && typeIsString {
			endDate, endDateOK = saleEndDate(event, typeText, stringOr(date, ""), published, zone)
		}
		// An end time needs a start time, except the deadline of a sale period
		// whose close date is given.
		valid := jsTrim(stringOr(title, "")) != "" &&
			dateOK && endDateOK &&
			validClock(clock, clockDefined) &&
			validClock(endClock, endDefined) &&
			!(truthy(endClock) && !truthy(clock) && endDate == "") &&
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
		tagValue, _ := event.get("tag")
		statusValue, _ := event.get("status")
		detailValue, _ := event.get("detail")
		urlValue, _ := event.get("url")
		evidenceValue, _ := event.get("evidence")
		confidenceValue, confidenceDefined := event.get("confidence")

		title := sliceUTF16(jsTrim(jsString(titleValue)), 240)
		zone := p.entryZone(event)
		// postedDate is the date the post counts the times from, in the
		// entry's zone; a late-night start such as 25:00 belongs to the next
		// calendar date, and a Beijing-time start may fall on the next Japan
		// date. A date-only entry keeps its written date.
		postedDate := rolledYearIn(jsString(dateValue), published, zone)
		date := postedDate
		clock := ""
		var start time.Time
		if truthy(clockValue) {
			start = zonedInstant(postedDate, jsString(clockValue), zone)
			date, clock = start.In(jst).Format("2006-01-02"), start.In(jst).Format("15:04")
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
		eventTypeText := eventType.(string)
		youtubeID := youtubeIDFromURL(eventURL)
		// A live-room link stays only on a collaboration in someone else's
		// room; any other entry with one is a Bilibili stream.
		platforms, inferred := p.entryPlatforms(event, eventTypeText, text, eventURL, youtubeID, len(items) == 1)
		platform := sqlite.PrimaryPlatform(platforms)
		if isBilibiliRoomLink(eventURL) && !slices.Contains(platforms, sqlite.PlatformOther) {
			// The room link is the same for every broadcast; reads add it as
			// liveUrl, so the entry keeps its announcement's link.
			eventURL = ""
		}
		normalized := normalizedEvent{
			id:                    p.idPrefix + strings.ReplaceAll(date, "-", "") + "-" + sha256Hex(sourceKey)[:10],
			canonicalSource:       p.source,
			canonicalSourceItemID: postID,
			canonicalSourceKey:    sourceKey,
			sourceKey:             sourceKey,
			title:                 title,
			startsOn:              date,
			timePrecision:         "unknown",
			eventType:             eventTypeText,
			platform:              platform,
			platforms:             platforms,
			platformsInferred:     inferred,
			tag:                   extractedTag(tagValue),
			url:                   normalizePublicURL(eventURL, postURL),
			isUpcoming:            true,
			evidence:              sliceUTF16(stringOr(evidenceValue, ""), 1000),
		}
		normalized.evidenceURL = normalized.url
		if p.source != "x" {
			normalized.evidenceURL = postURL
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
			startsAt := sqlite.ISOTime(start)
			normalized.startsAt = &startsAt
			normalized.timePrecision = precision.(string)
			normalized.isUpcoming = !start.Before(now.Truncate(time.Millisecond))
		}
		endDate, _ := saleEndDate(event, normalized.eventType, jsString(dateValue), published, zone)
		switch {
		case endDate != "" && truthy(endValue):
			// A sale closes at its stated date and time; 24:00 belongs to the
			// next calendar date, as a start time does.
			end := zonedInstant(endDate, jsString(endValue), zone)
			endsOn := end.In(jst).Format("2006-01-02")
			normalized.endsOn = &endsOn
			if clock == "" || !end.Before(start) {
				endsAt := sqlite.ISOTime(end)
				normalized.endsAt = &endsAt
			}
		case endDate != "":
			normalized.endsOn = &endDate
		case truthy(endValue):
			end := zonedInstant(postedDate, jsString(endValue), zone)
			if clock != "" && end.Before(start) {
				end = end.AddDate(0, 0, 1)
			}
			endsAt := sqlite.ISOTime(end)
			normalized.endsAt = &endsAt
			// A sale's close time always has its close date, the date it
			// falls on in Japan.
			if normalized.eventType == "sale" {
				endsOn := end.In(jst).Format("2006-01-02")
				normalized.endsOn = &endsOn
			}
		}
		if normalized.endsOn != nil && *normalized.endsOn < normalized.startsOn {
			normalized.endsOn, normalized.endsAt = nil, nil
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

// inferredClassification accepts results without the classification field
// when they carry an events list: those are schedules exactly when the list
// has entries. A result that is not an object, or names neither, is invalid.
func inferredClassification(result any) (string, error) {
	root, isObject := result.(*object)
	if !isObject || root == nil {
		return "", fail("schedule extraction did not return an object")
	}
	if value, ok := root.get("classification"); ok {
		if text, isString := value.(string); isString {
			normalized := jsToLower(jsTrim(text))
			if classifications[normalized] {
				return normalized, nil
			}
			return "", fail("schedule extraction returned an invalid classification")
		}
	}
	events, ok := root.get("events")
	if !ok || events == nil {
		return "", fail("schedule extraction returned no classification")
	}
	if list, isArray := events.([]any); isArray && len(list) > 0 {
		return "schedule", nil
	}
	return "not_schedule", nil
}

type normalizedExtraction struct {
	classification string
	events         []normalizedEvent
	extractedText  string
	confidence     float64
	evidence       string
	reason         string
	action         string
}

func normalizeExtractionResult(result any, postID, postURL string, published, now time.Time) (normalizedExtraction, error) {
	return xProfile.normalizeResult(result, postID, postURL, "", published, now)
}

func (p sourceProfile) normalizeResult(result any, postID, postURL, text string, published, now time.Time) (normalizedExtraction, error) {
	classification, err := inferredClassification(result)
	if err != nil {
		return normalizedExtraction{}, err
	}
	root, _ := result.(*object)
	var items []any
	if value, _ := root.get("events"); value != nil {
		list, isArray := value.([]any)
		if !isArray {
			return normalizedExtraction{}, fail("schedule extraction returned non-array events")
		}
		items = list
	}
	events, err := p.normalizeEvents(items, postID, postURL, text, published, now)
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
	// A schedule without entries would retire every automatic event of the
	// post; only cancellation evidence may come without entries.
	if classification == "schedule" && len(events) == 0 && action != "cancel" {
		return normalizedExtraction{}, fail("schedule extraction contained no events")
	}
	evidence, _ := root.get("evidence")
	reason, _ := root.get("reason")
	extractedText, _ := root.get("text")
	return normalizedExtraction{
		classification: classification,
		events:         events,
		extractedText:  sliceUTF16(jsTrim(stringOr(extractedText, "")), 12000),
		confidence:     confidence,
		evidence:       sliceUTF16(jsTrim(stringOr(evidence, "")), 1000),
		reason:         sliceUTF16(jsTrim(stringOr(reason, "")), 500),
		action:         action,
	}, nil
}

type assetVerification struct {
	classification string
	extractedText  string
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
	extractedText, _ := root.get("text")
	return assetVerification{
		classification: classification,
		extractedText:  sliceUTF16(jsTrim(stringOr(extractedText, "")), 12000),
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
