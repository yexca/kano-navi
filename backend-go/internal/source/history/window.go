// Package history serves the operator's manual X and YouTube fetches. Each
// fetch names one listing method (see Methods) and a selection the method can
// serve: the newest records by count, the whole listing, or for the X API a
// Japan date range or a backfill before the oldest stored post. Pages are
// bounded, and every page commits together with its continuation checkpoint.
package history

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"time"
)

const (
	// ModeCount reads the newest Count records per X account or for the
	// YouTube channel; it has no time bounds.
	ModeCount = "count"
	// ModeAll pages the method's listing to its end.
	ModeAll    = "all"
	ModeRange  = "range"
	ModeBefore = "before"
	// MaxCount bounds a count selection per account.
	MaxCount = 200

	day = 24 * time.Hour
	// isoLayout is the timestamp form stored in checkpoints.
	isoLayout = "2006-01-02T15:04:05.000Z"
)

// ValidationError carries a message that is safe to show to the operator.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

// ErrNoRecords means a "before" backfill has nothing stored to continue from.
var ErrNoRecords = invalid("no stored records to backfill; select a date range first")

// ErrRetired answers a stored selection without a method or with a mode no
// method serves, such as a run frozen before fetches named their method.
var ErrRetired = invalid("this fetch uses a retired selection; start a new fetch")

// Window is an operator-selected fetch: the method, its mode and the mode's
// count or dates. StartTime and EndTime are the resolved half-open bounds of
// a date selection and stay empty until Resolve.
type Window struct {
	Method    string `json:"method,omitempty"`
	Mode      string `json:"mode"`
	Days      int    `json:"days,omitempty"`
	Count     int    `json:"count,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
	// Refresh rewrites stored records the method lists again; only methods
	// whose description allows it accept it.
	Refresh   bool   `json:"refresh,omitempty"`
	StartTime string `json:"startTime,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
}

var (
	datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	japan       = time.FixedZone("JST", 9*3600)
)

func japanMidnight(value string) (time.Time, error) {
	if !datePattern.MatchString(value) {
		return time.Time{}, invalid("dates must use YYYY-MM-DD")
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, japan)
	if err != nil {
		return time.Time{}, invalid("date is invalid")
	}
	return parsed, nil
}

func jsonNull(raw json.RawMessage) bool { return len(raw) == 0 || string(raw) == "null" }

// Select validates a fetch request: the source's method, then {mode},
// {mode, count}, {mode, days} or {mode, startDate, endDate} as that method
// serves it, and the refresh option.
func Select(source, method string, raw json.RawMessage, refresh bool) (Window, error) {
	described, ok := Lookup(source, method)
	if !ok {
		return Window{}, invalid("method must be one of " + methodNames(source))
	}
	var input map[string]json.RawMessage
	if jsonNull(raw) || json.Unmarshal(raw, &input) != nil || input == nil {
		return Window{}, invalid("fetch window is required")
	}
	var mode string
	_ = json.Unmarshal(input["mode"], &mode)
	window := Window{Method: method, Mode: mode, Refresh: refresh}
	switch mode {
	case ModeCount:
		var count float64
		if jsonNull(input["count"]) || json.Unmarshal(input["count"], &count) != nil || math.Trunc(count) != count {
			return Window{}, countError(described)
		}
		window.Count = int(count)
	case ModeBefore:
		var days float64
		if jsonNull(input["days"]) || json.Unmarshal(input["days"], &days) != nil || math.Trunc(days) != days || days < 1 || days > 365 {
			return Window{}, invalid("days must be an integer from 1 to 365")
		}
		window.Days = int(days)
	case ModeRange:
		if json.Unmarshal(input["startDate"], &window.StartDate) != nil || json.Unmarshal(input["endDate"], &window.EndDate) != nil {
			return Window{}, invalid("dates must use YYYY-MM-DD")
		}
	}
	return window.Normalize(source)
}

func countError(method Method) error {
	return invalid("count must be an integer from 1 to " + strconv.Itoa(method.MaxCount))
}

// Normalize drops resolved bounds and validates the selection against its
// source's method. A selection without a known method and mode is ErrRetired.
func (w Window) Normalize(source string) (Window, error) {
	method, ok := Lookup(source, w.Method)
	if !ok || !method.serves(w.Mode) {
		return Window{}, ErrRetired
	}
	if w.Refresh && !method.Refresh {
		return Window{}, invalid("refresh is not supported for this method")
	}
	out := Window{Method: w.Method, Mode: w.Mode, Refresh: w.Refresh}
	switch w.Mode {
	case ModeCount:
		if w.Count < 1 || w.Count > method.MaxCount {
			return Window{}, countError(method)
		}
		out.Count = w.Count
	case ModeBefore:
		if w.Days < 1 || w.Days > 365 {
			return Window{}, invalid("days must be an integer from 1 to 365")
		}
		out.Days = w.Days
	case ModeRange:
		start, err := japanMidnight(w.StartDate)
		if err != nil {
			return Window{}, err
		}
		end, err := japanMidnight(w.EndDate)
		if err != nil {
			return Window{}, err
		}
		end = end.Add(day)
		if !end.After(start) || end.Sub(start) > 365*day {
			return Window{}, invalid("date range must cover 1 to 365 days")
		}
		out.StartDate, out.EndDate = w.StartDate, w.EndDate
	}
	return out, nil
}

// Resolve computes the half-open bounds of a date selection. Range dates
// include both selected Japan days; a backfill ends strictly before the
// oldest stored record. Count and all selections have no bounds to resolve.
func (w Window) Resolve(oldest string) (Window, error) {
	window := w
	switch window.Mode {
	case ModeRange:
		start, err := japanMidnight(window.StartDate)
		if err != nil {
			return Window{}, err
		}
		end, _ := japanMidnight(window.EndDate)
		window.StartTime, window.EndTime = formatTime(start), formatTime(end.Add(day))
	case ModeBefore:
		end, ok := parseTime(oldest)
		if !ok {
			return Window{}, ErrNoRecords
		}
		window.StartTime = formatTime(end.Add(-time.Duration(window.Days) * day))
		window.EndTime = formatTime(end)
	}
	return window, nil
}

// Contains reports whether a timestamp falls inside the resolved bounds.
func (w Window) Contains(value string) bool {
	instant, ok := parseTime(value)
	start, startOK := parseTime(w.StartTime)
	end, endOK := parseTime(w.EndTime)
	return ok && startOK && endOK && !instant.Before(start) && instant.Before(end)
}

// checkpointKey identifies one selection per account. The serialization is
// fixed, so a stored checkpoint keeps resuming; the method is not part of it
// because each method keeps its checkpoints under its own sync_state source.
func (w Window) checkpointKey(account string) string {
	selection := "before"
	switch w.Mode {
	case ModeRange:
		start, _ := json.Marshal(w.StartDate)
		end, _ := json.Marshal(w.EndDate)
		selection = `{"mode":"range","startDate":` + string(start) + `,"endDate":` + string(end) + `}`
	case ModeCount:
		selection = `{"mode":"count","count":` + strconv.Itoa(w.Count) + `}`
	case ModeAll:
		selection = `{"mode":"all"}`
	}
	sum := sha256.Sum256([]byte(selection))
	return account + ":" + hex.EncodeToString(sum[:])[:24]
}

func formatTime(value time.Time) string { return value.UTC().Format(isoLayout) }

func parseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// windowFromMetadata reads a window saved in checkpoint metadata.
func windowFromMetadata(value any) (Window, bool) {
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return Window{}, false
	}
	var window Window
	if json.Unmarshal(raw, &window) != nil {
		return Window{}, false
	}
	if window.Mode == ModeCount {
		return window, window.Count > 0
	}
	if window.Mode == ModeAll {
		return window, true
	}
	if _, ok := parseTime(window.StartTime); !ok {
		return Window{}, false
	}
	if _, ok := parseTime(window.EndTime); !ok {
		return Window{}, false
	}
	return window, true
}

// IsValidation reports whether err is an operator-facing validation message.
func IsValidation(err error) bool {
	var validation *ValidationError
	return errors.As(err, &validation)
}
