package workflow

import (
	"context"
	"errors"

	"github.com/yexca/kano-navi/backend-go/internal/mediadownload"
	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// ScheduleRunner is the schedule extraction step. Status is success, partial
// or failed; Counts become the step's recorded result.
type ScheduleRunner interface {
	RunSchedule(context.Context) (status string, counts map[string]any, err error)
}

type SourceSteps struct {
	X        *XRecentExecutor
	Youtube  *ytsource.Adapter
	Media    *mediadownload.Client
	Schedule ScheduleRunner
	// History serves operator-selected windows through the source APIs.
	History *HistorySource
	Store   *sqlite.Store
	// Skip holds steps disabled for the whole process through SKIP_* flags.
	// They are recorded as skipped instead of being silently dropped.
	Skip map[string]bool
}

func (s *SourceSteps) Execute(ctx context.Context, in StepInput) (AttemptResult, error) {
	if s.Skip[in.StepID] {
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": "environment"}}, nil
	}
	// A saved workflow runs its rolling recent window, which the source API
	// serves when its credential is configured. A full sync or scan without a
	// window stays incremental.
	if in.Window == nil && in.LookbackDays > 0 && !sqlite.IsAdhocWorkflow(in.WorkflowID) && s.History.Available(in.StepID) {
		in.Window = &history.Window{Mode: history.ModeRecent, Days: in.LookbackDays}
	}
	switch in.StepID {
	case "x":
		// A selected window uses the API whenever its credential is present;
		// past windows have no public-page fallback.
		if in.Window != nil && (in.Window.Mode != history.ModeRecent || s.History.Available(history.SourceX)) {
			return s.History.FetchX(ctx, *in.Window)
		}
		if s.X != nil {
			return s.X.Execute(ctx, in)
		}
	case "youtube":
		if in.Window != nil && in.Window.Mode != history.ModeRecent {
			return s.History.FetchYoutube(ctx, *in.Window)
		}
		if s.Youtube == nil {
			break
		}
		if in.Window != nil && s.History.Available(history.SourceYoutube) {
			return s.youtubeWindowAndFeed(ctx, in)
		}
		return s.youtubeRecent(ctx, in)
	case "media":
		if s.Media == nil {
			break
		}
		result, err := s.Media.DownloadPending(ctx, s.Store, 20)
		if err != nil {
			if ctx.Err() != nil {
				return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
			}
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		status := "succeeded"
		if result.Failed > 0 || result.Missing > 0 {
			status = "partial"
		}
		return AttemptResult{Status: status, Result: map[string]any{"attempted": result.Attempted, "ready": result.Ready, "failed": result.Failed}}, nil
	case "schedule":
		if s.Schedule == nil {
			break
		}
		status, counts, err := s.Schedule.RunSchedule(ctx)
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		if err != nil {
			return AttemptResult{}, NewStepError("permanent", ErrPermanent)
		}
		if counts == nil {
			counts = map[string]any{}
		}
		switch status {
		case "failed":
			return AttemptResult{Status: "failed", Result: counts}, nil
		case "partial":
			return AttemptResult{Status: "partial", Result: counts}, nil
		}
		return AttemptResult{Status: "succeeded", Result: counts}, nil
	}
	return AttemptResult{}, NewStepError("capability_not_migrated", ErrBlocked)
}

// youtubeRecent reads the public feed and refreshes active reservations.
func (s *SourceSteps) youtubeRecent(ctx context.Context, in StepInput) (AttemptResult, error) {
	state, err := s.Store.YoutubeRecentState(ctx, s.Youtube.Options.ChannelID)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	days := in.LookbackDays
	if days == 0 {
		days = 14
	}
	result, err := s.Youtube.FetchRecent(ctx, state, days)
	if err != nil {
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		category := ErrPermanent
		var e *ytsource.Error
		if errors.As(err, &e) {
			switch e.Code {
			case "youtube_timeout":
				category = ErrTimeout
			case "youtube_rate_limited":
				category = ErrRateLimited
			case "youtube_unavailable", "youtube_upstream_5xx", "youtube_http_error":
				category = ErrDependency
			}
		}
		return AttemptResult{}, NewStepError(string(category), category)
	}
	if s.Store.WriteYoutubeRecent(ctx, s.Youtube.Options.ChannelID, result) != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	status := "succeeded"
	if len(result.Errors) > 0 {
		status = "partial"
	}
	return AttemptResult{Status: status, Result: map[string]any{"count": len(result.Videos), "videos": len(result.Videos), "inspected": result.Inspected, "requested": result.Requested}}, nil
}

// youtubeWindowAndFeed serves a recent window from the API and still refreshes
// reservations from the public feed, which the search API does not describe.
func (s *SourceSteps) youtubeWindowAndFeed(ctx context.Context, in StepInput) (AttemptResult, error) {
	searched, err := s.History.FetchYoutube(ctx, *in.Window)
	if err != nil || searched.Status == "failed" {
		return searched, err
	}
	feed, feedErr := s.youtubeRecent(ctx, in)
	if ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	messages, _ := searched.Result["errors"].([]string)
	if feedErr != nil {
		searched.Result["errors"] = append(messages, "youtube_feed_unavailable; reservation refresh was skipped")
		searched.Status = "partial"
		return searched, nil
	}
	count, _ := searched.Result["count"].(int)
	added, _ := feed.Result["count"].(int)
	searched.Result["count"], searched.Result["videos"] = count+added, count+added
	searched.Result["inspected"] = feed.Result["inspected"]
	if feed.Status == "partial" {
		searched.Status = "partial"
	}
	return searched, nil
}
