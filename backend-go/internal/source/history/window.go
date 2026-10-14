// Package history fetches bounded past windows of source records through the
// official X and YouTube APIs. Windows are inclusive Japan dates, pages are
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
	ModeRecent = "recent"
	ModeBefore = "before"
	ModeRange  = "range"

	day = 24 * time.Hour
	// isoLayout matches the timestamps earlier releases stored in checkpoints.
	isoLayout = "2006-01-02T15:04:05.000Z"
)

// ValidationError carries a message that is safe to show to the operator.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

// ErrNoRecords means a "before" backfill has nothing stored to continue from.
var ErrNoRecords = invalid("no stored records to backfill; select a date range first")

// Window is an operator-selected fetch window. StartTime and EndTime are the
// resolved half-open bounds and stay empty until Resolve.
type Window struct {
	Mode      string `json:"mode"`
	Days      int    `json:"days,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
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

// Parse validates {mode, days} or {mode, startDate, endDate}.
func Parse(raw json.RawMessage) (Window, error) {
	var input map[string]json.RawMessage
	if jsonNull(raw) || json.Unmarshal(raw, &input) != nil || input == nil {
		return Window{}, invalid("fetch window is required")
	}
	var mode string
	_ = json.Unmarshal(input["mode"], &mode)
	switch mode {
	case ModeRecent, ModeBefore:
		var days float64
		if jsonNull(input["days"]) || json.Unmarshal(input["days"], &days) != nil || math.Trunc(days) != days || days < 1 || days > 365 {
			return Window{}, invalid("days must be an integer from 1 to 365")
		}
		return Window{Mode: mode, Days: int(days)}, nil
	case ModeRange:
		var startDate, endDate string
		if json.Unmarshal(input["startDate"], &startDate) != nil || json.Unmarshal(input["endDate"], &endDate) != nil {
			return Window{}, invalid("dates must use YYYY-MM-DD")
		}
		return (Window{Mode: mode, StartDate: startDate, EndDate: endDate}).Normalize()
	}
	return Window{}, invalid("unsupported fetch mode")
}

// Normalize drops resolved bounds and validates the selection.
func (w Window) Normalize() (Window, error) {
	switch w.Mode {
	case ModeRecent, ModeBefore:
		if w.Days < 1 || w.Days > 365 {
			return Window{}, invalid("days must be an integer from 1 to 365")
		}
		return Window{Mode: w.Mode, Days: w.Days}, nil
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
		return Window{Mode: w.Mode, StartDate: w.StartDate, EndDate: w.EndDate}, nil
	}
	return Window{}, invalid("unsupported fetch mode")
}

// Resolve computes the half-open bounds. Range dates include both selected
// Japan days; a backfill ends strictly before the oldest stored record.
func (w Window) Resolve(oldest string, now time.Time) (Window, error) {
	window, err := w.Normalize()
	if err != nil {
		return Window{}, err
	}
	if window.Mode == ModeRange {
		start, _ := japanMidnight(window.StartDate)
		end, _ := japanMidnight(window.EndDate)
		window.StartTime, window.EndTime = formatTime(start), formatTime(end.Add(day))
		return window, nil
	}
	end := now
	if window.Mode == ModeBefore {
		parsed, ok := parseTime(oldest)
		if !ok {
			return Window{}, ErrNoRecords
		}
		end = parsed
	}
	window.StartTime = formatTime(end.Add(-time.Duration(window.Days) * day))
	window.EndTime = formatTime(end)
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
// the one earlier releases hashed, so their checkpoints keep resuming.
func (w Window) checkpointKey(account string) string {
	selection := "before"
	switch w.Mode {
	case ModeRecent:
		selection = `{"mode":"recent","days":` + strconv.Itoa(w.Days) + `}`
	case ModeRange:
		start, _ := json.Marshal(w.StartDate)
		end, _ := json.Marshal(w.EndDate)
		selection = `{"mode":"range","startDate":` + string(start) + `,"endDate":` + string(end) + `}`
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
