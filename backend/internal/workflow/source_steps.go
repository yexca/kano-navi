package workflow

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/yexca/kano-navi/backend/internal/mediadownload"
	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	"github.com/yexca/kano-navi/backend/internal/source/niconico"
	shopsource "github.com/yexca/kano-navi/backend/internal/source/shop"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/variants"
	"github.com/yexca/kano-navi/backend/internal/works"
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
	X        *XRecentExecutor
	Youtube  *ytsource.Adapter
	Bilibili *bilisource.Adapter
	// Shop reads the official shop's sale periods.
	Shop  *shopsource.Adapter
	Media *mediadownload.Client
	// MediaBounds overrides mediadownload.StepBounds when set.
	MediaBounds *mediadownload.Bounds
	// Works is the Works page's upload list whose covers the media step
	// fetches; nil fetches none. Niconico looks up its niconico thumbnails
	// (nil leaves them uncovered), WorkCoverBounds overrides the lookup
	// bounds and Now is the clock of the lookups.
	Niconico        *niconico.Client
	Works           *works.Index
	WorkCoverBounds *WorkCoverBounds
	Now             func() time.Time
	// Variants locates the originals of the variants step; nil leaves the
	// step unavailable. VariantBudget overrides VariantsBudget when set.
	Variants      *variants.Roots
	VariantBudget time.Duration
	Schedule      ScheduleRunner
	Translate     TranslateRunner
	// History serves the operator's manual fetches through their methods.
	History *HistorySource
	Store   *sqlite.Store
	// Skip holds steps disabled for the whole process through SKIP_* flags.
	// They are recorded as skipped instead of being silently dropped.
	Skip map[string]bool
	// BilibiliPageGap spaces the pages of an operator's Bilibili fetch; zero
	// uses DefaultBilibiliPageGap and a negative gap waits not at all.
	BilibiliPageGap time.Duration
}

// applySettings gives the source readers the operator's current bounds (see
// sqlite.SyncSettings) before a source step. Steps run one at a time on the
// queue, so a step reads with one set of values from start to end.
func (s *SourceSteps) applySettings(ctx context.Context) error {
	if s.Store == nil {
		return nil
	}
	settings, err := s.Store.SyncSettings(ctx)
	if err != nil {
		return err
	}
	if s.X != nil {
		s.X.Options.BootstrapDays = settings.XBootstrapDays
		s.X.Options.DiscoveryLimit = settings.XDiscoveryLimit
		s.X.Options.MaxDetailRequests = settings.XMaxStatusRequests
		s.X.Options.RefreshKnown = settings.XRefreshKnown
	}
	if s.Youtube != nil {
		s.Youtube.Options.MaxDetailRequests = settings.YoutubeMaxDetailRequests
	}
	if s.History != nil {
		s.History.RequestLimit = max(1, settings.XMaxStatusRequests)
		if s.History.Fetcher != nil {
			s.History.Fetcher.SetLimits(settings.FetchMaxPages,
				time.Duration(settings.FetchTimeoutMS)*time.Millisecond,
				time.Duration(settings.FetchRequestDelayMS)*time.Millisecond)
		}
	}
	return nil
}

func (s *SourceSteps) Execute(ctx context.Context, in StepInput) (AttemptResult, error) {
	if s.Skip[in.StepID] {
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": "environment"}}, nil
	}
	if in.StepID == "x" || in.StepID == sqlite.XRelatedStep || in.StepID == "youtube" || in.StepID == "live" {
		if err := s.applySettings(ctx); err != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
	}
	// Source chains, step runs, full syncs and scans carry no window and read each
	// source by its sync setting, whatever credentials are set; a
	// manual fetch reads through the one method its window names.
	if in.Window != nil && (in.StepID == "x" || in.StepID == "youtube") && s.History == nil {
		return AttemptResult{}, NewStepError("history_unavailable", ErrDependency)
	}
	switch in.StepID {
	case "x":
		if in.Window != nil {
			return s.History.FetchX(ctx, *in.Window)
		}
		// A count setting reads the newest posts from the timeline every run;
		// the incremental cursor and pending queue stay as they are. Operator
		// refreshes take their detail requests after that read.
		if in.Window == nil && in.Sync.Mode == sqlite.SyncCount {
			if s.History == nil {
				return AttemptResult{}, NewStepError("history_unavailable", ErrDependency)
			}
			out, err := s.History.FetchX(ctx, history.Window{Method: history.MethodTimeline, Mode: history.ModeCount, Count: in.Sync.Count})
			if err != nil {
				return out, err
			}
			refreshed, refreshErr := s.X.Refresh(ctx)
			if ctx.Err() != nil {
				return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
			}
			if refreshErr != nil {
				return AttemptResult{}, NewStepError("x_source_write_failed", ErrSchema)
			}
			if refreshed > 0 {
				if out.Result == nil {
					out.Result = map[string]any{}
				}
				out.Result["refreshed"] = refreshed
			}
			return out, nil
		}
		if s.X != nil {
			return s.X.Execute(ctx, in)
		}
	case sqlite.XRelatedStep:
		if s.X != nil {
			return s.X.ExecuteRelated(ctx, in)
		}
	case "youtube":
		if in.Window != nil {
			return s.History.FetchYoutube(ctx, *in.Window)
		}
		if s.Youtube == nil {
			break
		}
		return s.youtubeRecent(ctx, in)
	case "live":
		if s.Youtube == nil && s.Bilibili == nil {
			break
		}
		return s.live(ctx, in)
	case "bilibili":
		if s.Bilibili == nil {
			break
		}
		if in.Window != nil {
			return s.bilibiliFetch(ctx, history.SourceBilibili, *in.Window)
		}
		return s.bilibiliRecent(ctx)
	case sqlite.BilibiliDynamicsSource:
		if s.Bilibili == nil {
			break
		}
		if in.Window != nil {
			return s.bilibiliFetch(ctx, history.SourceBilibiliDynamics, *in.Window)
		}
		return s.bilibiliDynamics(ctx)
	case sqlite.ShopSource:
		if s.Shop == nil {
			break
		}
		return s.shopSales(ctx, in)
	case "media":
		if s.Media == nil {
			break
		}
		// The Works list's covers join the queue first, so this download
		// fetches them.
		covers, err := s.workCovers(ctx)
		if err == nil {
			var result mediadownload.Summary
			result, err = s.Media.DownloadPending(ctx, s.Store, s.mediaBounds())
			if err == nil {
				return mediaResult(result, covers), nil
			}
		}
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	case "variants":
		if s.Variants == nil {
			break
		}
		return s.makeVariants(ctx)
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
			"eventsTimed": counts.EventsTimed, "eventsCreated": counts.EventsCreated,
			"videosConsidered": videos.Considered, "videosMerged": videos.Merged, "videosReleased": videos.Released,
			"videosKept": videos.Kept, "videosAmbiguous": videos.Ambiguous,
		}}, nil
	}
	return AttemptResult{}, NewStepError("capability_not_migrated", ErrBlocked)
}

// youtubeList selects the uploads a recent run considers by the sync setting,
// incremental by default.
func youtubeList(in StepInput, listings []string) ytsource.List {
	switch {
	case in.Sync.Mode == sqlite.SyncCount:
		return ytsource.List{Limit: in.Sync.Count, Count: true, Listings: listings}
	case in.Sync.Mode == sqlite.SyncIncremental && in.Sync.Max > 0:
		return ytsource.List{Limit: in.Sync.Max, Listings: listings}
	}
	return ytsource.List{Limit: sqlite.DefaultSyncMax, Listings: listings}
}

// VariantsBudget bounds one variants step, so the copies of a large archive
// hold the single-flight queue for minutes at most; the operator's next run
// continues with the rest.
const VariantsBudget = 5 * time.Minute

// makeVariants makes the missing copies of every group within one budget,
// in variants.Groups order. A group reached after the budget lists its
// originals as remaining without loading any.
func (s *SourceSteps) makeVariants(ctx context.Context) (AttemptResult, error) {
	budget := VariantsBudget
	if s.VariantBudget > 0 {
		budget = s.VariantBudget
	}
	deadline := time.Now().Add(budget)
	total := variants.Result{}
	for _, group := range variants.Groups {
		result, err := variants.Make(ctx, s.Store, *s.Variants, group, variants.Bounds{Budget: max(time.Nanosecond, time.Until(deadline))})
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		if err != nil {
			return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
		}
		total.Attempted += result.Attempted
		total.Made += result.Made
		total.Kept += result.Kept
		total.Skipped += result.Skipped
		total.Failed += result.Failed
		total.Remaining += result.Remaining
		if total.Stopped == "" {
			total.Stopped = result.Stopped
		}
	}
	status := "succeeded"
	if total.Failed > 0 || total.Remaining > 0 {
		status = "partial"
	}
	counts := map[string]any{"attempted": total.Attempted, "made": total.Made, "kept": total.Kept, "skipped": total.Skipped, "failed": total.Failed, "remaining": total.Remaining}
	if total.Stopped != "" {
		counts["stopped"] = total.Stopped
	}
	return AttemptResult{Status: status, Result: counts}, nil
}

// mediaResult is the media step's outcome. Images a bound left queued and
// failed Works lookups make it partial; the next run continues with them.
func mediaResult(result mediadownload.Summary, covers workCoverCounts) AttemptResult {
	status := "succeeded"
	if result.Failed > 0 || result.Missing > 0 || result.Remaining > 0 || covers.Failed > 0 {
		status = "partial"
	}
	counts := map[string]any{"attempted": result.Attempted, "ready": result.Ready, "unchanged": result.Unchanged, "failed": result.Failed, "measured": result.Measured, "resized": result.Resized, "remaining": result.Remaining, "works": covers.result()}
	if result.Stopped != "" {
		counts["stopped"] = result.Stopped
	}
	return AttemptResult{Status: status, Result: counts}
}

func (s *SourceSteps) mediaBounds() mediadownload.Bounds {
	if s.MediaBounds != nil {
		return *s.MediaBounds
	}
	return mediadownload.StepBounds
}

// youtubeRecent lists the newest uploads through the run's chain (Data API,
// RSS feed or uploads tab) and refreshes active reservations. It fails only when no listing answered
// and no detail check succeeded.
func (s *SourceSteps) youtubeRecent(ctx context.Context, in StepInput) (AttemptResult, error) {
	state, err := s.Store.YoutubeRecentState(ctx, s.Youtube.Options.ChannelID)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	youtube, listings := s.youtubeFor(in.Chain)
	result, err := youtube.FetchUploads(ctx, state, youtubeList(in, listings))
	if err != nil {
		if ctx.Err() != nil {
			return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
		}
		category := ErrPermanent
		var e *ytsource.Error
		if errors.As(err, &e) {
			switch e.Code {
			// The run fails only without a listing, so the code is the feed's.
			case "youtube_feed_timeout":
				category = ErrTimeout
			case "youtube_feed_rate_limited":
				category = ErrRateLimited
			case "youtube_feed_unavailable", "youtube_feed_upstream_5xx", "youtube_feed_http_error":
				category = ErrDependency
			}
		}
		// The failed run still rotated the detail queue past the IDs it tried.
		// Saving only the queue leaves cursors and snapshots alone; a failed
		// save does not change the step's failure.
		if len(result.Queue) > 0 {
			_ = s.Store.SaveYoutubeQueue(ctx, s.Youtube.Options.ChannelID, result.Queue)
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
	counts := map[string]any{"count": len(result.Videos), "videos": len(result.Videos), "inspected": result.Inspected, "requested": result.Requested, "shorts": shorts, "listing": result.Listing, "tried": result.Tried, "apiRequests": result.APIRequests,
		"members": result.Members, "membersFrom": result.MembersFrom}
	if len(result.Errors) > 0 {
		counts["errors"] = tallyCodes(result.Errors)
	}
	return AttemptResult{Status: status, Result: counts}, nil
}

// tallyCodes lists each controlled error code once, in order of first
// appearance, with "×n" when it was recorded n > 1 times.
func tallyCodes(codes []string) []string {
	order, seen := []string{}, map[string]int{}
	for _, code := range codes {
		if seen[code] == 0 {
			order = append(order, code)
		}
		seen[code]++
	}
	out := make([]string, 0, len(order))
	for _, code := range order {
		if n := seen[code]; n > 1 {
			code += " ×" + strconv.Itoa(n)
		}
		out = append(out, code)
	}
	return out
}

// bilibiliRecent reads the account's newest uploads at most once per
// interval and never during a backoff, so no timer or manual run can drive
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
		// A read skipped without a request changes no state.
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": result.Skipped, "requested": 0}}, nil
	}
	changed, err := s.Store.WriteBilibiliRecent(ctx, account, result)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	counts := map[string]any{"count": len(result.Videos), "videos": len(result.Videos), "requested": result.Requested, "changed": changed}
	if result.Fallback != "" {
		// The list failed and a fallback listing gave the uploads.
		counts["fallback"], counts["listError"] = result.Fallback, result.ListError
		if len(result.FallbackErrors) > 0 {
			counts["fallbackErrors"] = result.FallbackErrors
		}
	}
	if fetchErr != nil {
		counts["errors"] = append([]string{bilisource.ErrorCode(fetchErr)}, result.FallbackErrors...)
		return AttemptResult{Status: "failed", Result: counts}, nil
	}
	if result.Fallback != "" {
		return AttemptResult{Status: "partial", Result: counts}, nil
	}
	return AttemptResult{Status: "succeeded", Result: counts}, nil
}

// bilibiliDynamics reads the first page of the account's dynamics with the
// login cookie, at most once per interval and never during the backoff it
// shares with the upload read: a captcha either read meets holds both back.
// Without a cookie, or while its last check found it expired, the step is
// skipped without a listing request. A failure is recorded, not retried.
func (s *SourceSteps) bilibiliDynamics(ctx context.Context) (AttemptResult, error) {
	account := s.Bilibili.Options.Account
	state, err := s.Store.BilibiliState(ctx, account)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	dynamics, fetchErr := s.Bilibili.FetchDynamics(ctx, state)
	if ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	if dynamics.Skipped != "" && dynamics.Requested == 0 {
		// A listing skipped without a request changes no state.
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": dynamics.Skipped, "requested": 0}}, nil
	}
	changed, err := s.Store.WriteBilibiliDynamics(ctx, account, dynamics)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	counts := map[string]any{"count": len(dynamics.Dynamics), "posts": len(dynamics.Dynamics), "videos": len(dynamics.Uploads), "requested": dynamics.Requested, "changed": changed}
	if dynamics.Skipped != "" {
		// The login check found the cookie expired.
		counts["skipped"], counts["reason"] = true, dynamics.Skipped
	}
	if fetchErr != nil {
		counts["errors"] = []string{bilisource.ErrorCode(fetchErr)}
		return AttemptResult{Status: "failed", Result: counts}, nil
	}
	return AttemptResult{Status: "succeeded", Result: counts}, nil
}

// shopSales reads the shop's sale periods: the newest products on a timed
// read, the whole catalog for the operator's read-all fetch. During a 429's
// Retry-After it is skipped without a request. A failure is recorded, not
// retried in the run, and changes no entry.
func (s *SourceSteps) shopSales(ctx context.Context, in StepInput) (AttemptResult, error) {
	state, err := s.Store.ShopState(ctx)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	stored, err := s.Store.ShopSales(ctx)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	// The shop's only window is the whole catalog (history.MethodCatalog).
	all := in.Window != nil
	result, fetchErr := s.Shop.Read(ctx, state, stored, all)
	if ctx.Err() != nil {
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
	if result.Skipped != "" {
		// A read skipped without a request changes no state.
		return AttemptResult{Status: "succeeded", Result: map[string]any{"skipped": true, "reason": result.Skipped, "requested": 0}}, nil
	}
	changed, err := s.Store.WriteShopRead(ctx, result)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	counts := map[string]any{"count": len(result.Sales), "sales": len(result.Sales), "listed": result.Listed, "candidates": result.Candidates,
		"requested": result.Requested, "collections": result.Collections, "changed": changed}
	if len(result.Outcomes) > 0 {
		counts["outcomes"] = result.Outcomes
	}
	if all {
		counts["pages"], counts["reachedEnd"] = result.Pages, result.ReachedEnd
	}
	if fetchErr != nil {
		counts["errors"] = []string{shopsource.ErrorCode(fetchErr)}
		return AttemptResult{Status: "failed", Result: counts}, nil
	}
	return AttemptResult{Status: "succeeded", Result: counts}, nil
}

// live runs the live-status probe: the YouTube videos due by their phase
// and, in the same run, at most one check of the Bilibili room when it is
// due. Each platform's part is left out while its source is disabled.
func (s *SourceSteps) live(ctx context.Context, in StepInput) (AttemptResult, error) {
	out := AttemptResult{Status: "succeeded", Result: map[string]any{}}
	if s.Youtube != nil && !s.Skip["youtube"] {
		var err error
		if out, err = s.youtubeLive(ctx, in); err != nil {
			return out, err
		}
	}
	if s.Bilibili == nil || s.Skip["bilibili"] {
		return out, nil
	}
	counts, status, err := s.bilibiliLive(ctx)
	if err != nil {
		return AttemptResult{}, err
	}
	if counts == nil {
		return out, nil
	}
	out.Result["bilibili"] = counts
	switch {
	case status == "succeeded":
	case out.Status == "succeeded" && (s.Youtube == nil || s.Skip["youtube"] || out.Result["candidates"] == 0):
		out.Status = status
	default:
		out.Status = "partial"
	}
	return out, nil
}

// bilibiliLive checks the configured room once when the plan says it is
// due. It returns nil counts when the check is off or not due.
func (s *SourceSteps) bilibiliLive(ctx context.Context) (map[string]any, string, error) {
	now := s.Bilibili.Now()
	settings, err := s.Store.LiveProbeSettings(ctx)
	if err != nil {
		return nil, "", NewStepError("schema_failure", ErrSchema)
	}
	if !settings.BilibiliEnabled {
		return nil, "", nil
	}
	account := s.Bilibili.Options.Account
	plan, err := s.Store.BilibiliLivePlan(ctx, account, now, settings)
	if err != nil {
		return nil, "", NewStepError("schema_failure", ErrSchema)
	}
	if now.Before(plan.DueAt) {
		return nil, "", nil
	}
	state, requested, checkErr := s.Bilibili.CheckLive(ctx, plan.Live.State)
	if ctx.Err() != nil {
		return nil, "", NewStepError("cancelled", ErrCancelled)
	}
	counts := map[string]any{"targets": len(plan.Targets), "phase": plan.Phase, "requested": 0, "live": false, "changed": false}
	if !requested {
		return counts, "succeeded", nil
	}
	changed, err := s.Store.WriteBilibiliLive(ctx, account, state)
	if err != nil {
		return nil, "", NewStepError("schema_failure", ErrSchema)
	}
	counts["requested"], counts["live"], counts["changed"] = 1, checkErr == nil && state.Room.LiveStatus == bilisource.RoomLive, changed
	if checkErr != nil {
		counts["errors"] = []string{bilisource.ErrorCode(checkErr)}
		return counts, "failed", nil
	}
	return counts, "succeeded", nil
}

// youtubeLive asks about only the few active videos that are due by their
// phase, through one Data API call when a key is set and YouTube's chain
// enables the api method, and otherwise their watch pages, and stores the broadcast state they report. Nothing due means
// no request.
func (s *SourceSteps) youtubeLive(ctx context.Context, in StepInput) (AttemptResult, error) {
	now := time.Now()
	if s.Youtube.Now != nil {
		now = s.Youtube.Now()
	}
	settings, err := s.Store.LiveProbeSettings(ctx)
	if err != nil {
		return AttemptResult{}, NewStepError("schema_failure", ErrSchema)
	}
	if !settings.Enabled {
		// The operator turned the YouTube part off: no YouTube request, even
		// when the run is for the Bilibili room.
		return AttemptResult{Status: "succeeded", Result: map[string]any{"active": 0, "candidates": 0, "requested": 0, "inspected": 0, "live": 0, "changed": false, "apiRequests": 0, "disabled": true}}, nil
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
	counts := map[string]any{"active": len(targets), "candidates": len(candidates), "requested": 0, "inspected": 0, "live": 0, "changed": false, "apiRequests": 0}
	if len(candidates) == 0 {
		return AttemptResult{Status: "succeeded", Result: counts}, nil
	}
	youtube, _ := s.youtubeFor(in.Chain)
	result, err := youtube.ProbeLive(ctx, candidates)
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
	counts["requested"], counts["inspected"], counts["live"], counts["changed"], counts["apiRequests"] = result.Requested, result.Inspected, live, changed, result.APIRequests
	if len(result.Unavailable) > 0 {
		// Private or removed videos leave the active set rather than failing
		// every later run.
		counts["unavailable"] = len(result.Unavailable)
	}
	if len(result.Errors) == 0 {
		return AttemptResult{Status: "succeeded", Result: counts}, nil
	}
	counts["errors"] = result.Errors
	// Unanswered videos keep their snapshots; the next probe asks again.
	if result.Inspected == 0 {
		return AttemptResult{Status: "failed", Result: counts}, nil
	}
	return AttemptResult{Status: "partial", Result: counts}, nil
}
