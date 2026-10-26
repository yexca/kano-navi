package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/mediadownload"
	bilisource "github.com/yexca/kano-navi/backend-go/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// ScheduleRunner is the schedule extraction step. Status is success, partial
// or failed; Counts become the step's recorded result.
type ScheduleRunner interface {
	RunSchedule(context.Context) (status string, counts map[string]any, err error)
}

// TranslateRunner is the machine translation step, with the same result
// contract as ScheduleRunner.
type TranslateRunner interface {
	RunTranslate(context.Context) (status string, counts map[string]any, err error)
}

type SourceSteps struct {
	X         *XRecentExecutor
	Youtube   *ytsource.Adapter
	Bilibili  *bilisource.Adapter
	Media     *mediadownload.Client
	Schedule  ScheduleRunner
	Translate TranslateRunner
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
		// A selected window uses the API whenever its credential is present.
		// Without it, recent and range windows page the public timeline back to
		// their start, as count windows always do; a backfill then fails with
		// the credential message. Only a recent window without either source
		// stays incremental.
		if in.Window != nil && (in.Window.Mode != history.ModeRecent || s.History.Available(history.SourceX) || s.History.timeline(history.ModeRecent)) {
			return s.History.FetchX(ctx, *in.Window)
		}
		if s.X != nil {
			return s.X.Execute(ctx, in)
		}
	case "youtube":
		if in.Window != nil && in.Window.Mode == history.ModeCount {
			return failedResult("count windows are available for X only"), nil
		}
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
	case "live":
		if s.Youtube == nil {
			break
		}
		return s.youtubeLive(ctx)
	case "bilibili":
		if s.Bilibili == nil {
			break
		}
		return s.bilibiliRecent(ctx)
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
		return AttemptResult{Status: status, Result: map[string]any{"attempted": result.Attempted, "ready": result.Ready, "unchanged": result.Unchanged, "failed": result.Failed}}, nil
	case "schedule", "translate":
		var status string
		var counts map[string]any
		var err error
		switch {
		case in.StepID == "schedule" && s.Schedule != nil:
			status, counts, err = s.Schedule.RunSchedule(ctx)
		case in.StepID == "translate" && s.Translate != nil:
			status, counts, err = s.Translate.RunTranslate(ctx)
		default:
			return AttemptResult{}, NewStepError("capability_not_migrated", ErrBlocked)
		}
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
	case "reconcile":
		if s.Store == nil {
			break
		}
		counts, err := s.Store.ReconcileEventMerges(ctx, time.Now())
		var videos sqlite.VideoMergeCounts
		if err == nil {
			videos, err = s.Store.ReconcileVideoMerges(ctx, time.Now())
		}
		if err != nil {
			if ctx.Err() != nil {
				return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
			}
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		// Video merges have their own flat counts, which the console lists.
		return AttemptResult{Status: "succeeded", Result: map[string]any{
			"from": counts.From, "to": counts.To, "considered": counts.Considered, "merged": counts.Merged,
			"retargeted": counts.Retargeted, "released": counts.Released, "kept": counts.Kept, "ambiguous": counts.Ambiguous,
			"videosConsidered": videos.Considered, "videosMerged": videos.Merged, "videosReleased": videos.Released,
			"videosKept": videos.Kept, "videosAmbiguous": videos.Ambiguous,
		}}, nil
	}
	return AttemptResult{}, NewStepError("capability_not_migrated", ErrBlocked)
}

// youtubeRecent lists the newest uploads (Data API, RSS feed or uploads tab)
// and refreshes active reservations. It fails only when no listing answered
// and no detail check succeeded.
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
	// Feed entries YouTube links as Shorts; zero across runs may mean the
	// feed stopped naming the format.
	shorts := 0
	for _, short := range result.Shorts {
		if short {
			shorts++
		}
	}
	return AttemptResult{Status: status, Result: map[string]any{"count": len(result.Videos), "videos": len(result.Videos), "inspected": result.Inspected, "requested": result.Requested, "shorts": shorts, "listing": result.Listing, "apiRequests": result.APIRequests}}, nil
}

// bilibiliRecent reads the account's newest uploads at most once per
// interval and never during a backoff, so a workflow timer cannot drive
// Bilibili's captcha. A failed read is recorded, not retried: the next run
// after the interval or backoff asks again.
func (s *SourceSteps) bilibiliRecent(ctx context.Context) (AttemptResult, error) {
	account := s.Bilibili.Options.Account
	state, err := s.Store.BilibiliState(ctx, account)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	result, fetchErr := s.Bilibili.FetchRecent(ctx, state)
	if ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	if result.Skipped != "" {
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": result.Skipped, "requested": 0}}, nil
	}
	changed, err := s.Store.WriteBilibiliRecent(ctx, account, result)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	counts := map[string]any{"count": len(result.Videos), "videos": len(result.Videos), "requested": result.Requested, "changed": changed}
	if fetchErr != nil {
		counts["errors"] = []string{bilisource.ErrorCode(fetchErr)}
		return AttemptResult{Status: "failed", Result: counts}, nil
	}
	return AttemptResult{Status: "succeeded", Result: counts}, nil
}

// youtubeLive requests only the watch pages of the few active videos that are
// due by their phase and stores the broadcast state they report. Nothing due
// means no request.
func (s *SourceSteps) youtubeLive(ctx context.Context) (AttemptResult, error) {
	now := time.Now()
	if s.Youtube.Now != nil {
		now = s.Youtube.Now()
	}
	settings, err := s.Store.LiveProbeSettings(ctx)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	targets, err := s.Store.LiveProbeTargets(ctx, now, settings)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	candidates := []ytsource.Video{}
	for _, target := range targets {
		if !now.Before(target.DueAt) && len(candidates) < ytsource.MaxLiveProbes {
			candidates = append(candidates, target.Video)
		}
	}
	counts := map[string]any{"active": len(targets), "candidates": len(candidates), "requested": 0, "inspected": 0, "live": 0, "changed": false}
	if len(candidates) == 0 {
		return AttemptResult{Status: "succeeded", Result: counts}, nil
	}
	result, err := s.Youtube.ProbeLive(ctx, candidates)
	if err != nil || ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	changed, err := s.Store.WriteYoutubeLive(ctx, result)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	live := 0
	for _, v := range result.Videos {
		if v.LiveNow {
			live++
		}
	}
	counts["requested"], counts["inspected"], counts["live"], counts["changed"] = result.Requested, result.Inspected, live, changed
	if len(result.Errors) == 0 {
		return AttemptResult{Status: "succeeded", Result: counts}, nil
	}
	counts["errors"] = result.Errors
	// Failed pages keep their snapshots; the next probe asks again.
	if result.Inspected == 0 {
		return AttemptResult{Status: "failed", Result: counts}, nil
	}
	return AttemptResult{Status: "partial", Result: counts}, nil
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
