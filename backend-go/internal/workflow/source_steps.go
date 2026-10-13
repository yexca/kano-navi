package workflow

import (
	"context"
	"errors"

	"github.com/yexca/kano-navi/backend-go/internal/mediadownload"
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
	Store    *sqlite.Store
	// Skip holds steps disabled for the whole process through SKIP_* flags.
	// They are recorded as skipped instead of being silently dropped.
	Skip map[string]bool
}

func (s *SourceSteps) Execute(ctx context.Context, in StepInput) (AttemptResult, error) {
	if s.Skip[in.StepID] {
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": "environment"}}, nil
	}
	switch in.StepID {
	case "x":
		if s.X != nil {
			return s.X.Execute(ctx, in)
		}
	case "youtube":
		if s.Youtube == nil {
			break
		}
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
				case "youtube_unavailable", "youtube_http_error":
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
		return AttemptResult{Status: status, Result: map[string]any{"videos": len(result.Videos), "inspected": result.Inspected, "requested": result.Requested}}, nil
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
