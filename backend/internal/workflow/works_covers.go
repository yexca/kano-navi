package workflow

import (
	"context"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/niconico"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/works"
)

// WorkCoverBounds limit the lookups one media step makes for the Works
// page's covers. niconico's end at its first 429 answer. Bilibili's few are
// spent only while its backoff allows and end at the first captcha or
// rate-limit answer.
type WorkCoverBounds struct {
	Niconico, Bilibili int
	Budget             time.Duration
}

// WorkCoverStepBounds fill a fresh deployment's covers over a few runs.
var WorkCoverStepBounds = WorkCoverBounds{Niconico: 30, Bilibili: 5, Budget: 2 * time.Minute}

// workCoverCounts is the works part of the media step's result.
type workCoverCounts struct {
	Queued, Lookups, Resolved, Missing, Failed int
	Covered, Waiting, Uncovered                int
	// Bilibili is "blocked" while its backoff lasts or after an answer that
	// started one, and "skipped" when the source is off or not configured.
	Bilibili string
}

func (c workCoverCounts) result() map[string]any {
	out := map[string]any{"queued": c.Queued, "lookups": c.Lookups, "resolved": c.Resolved, "missing": c.Missing, "failed": c.Failed, "covered": c.Covered, "waiting": c.Waiting, "uncovered": c.Uncovered}
	if c.Bilibili != "" {
		out["bilibili"] = c.Bilibili
	}
	return out
}

func (s *SourceSteps) workCoverBounds() WorkCoverBounds {
	if s.WorkCoverBounds != nil {
		return *s.WorkCoverBounds
	}
	return WorkCoverStepBounds
}

// workCovers queues the YouTube thumbnails the Works list still needs and
// makes the due niconico and Bilibili lookups, so the download that follows
// in the same step fetches their images. Only a store failure stops it.
func (s *SourceSteps) workCovers(ctx context.Context) (workCoverCounts, error) {
	out := workCoverCounts{}
	if s.Works == nil {
		return out, nil
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	plan, err := s.Store.PlanWorkCovers(ctx, *s.Works, now())
	if err != nil {
		return out, err
	}
	out.Queued, out.Covered, out.Waiting, out.Uncovered = plan.Queued, plan.Covered, plan.Waiting, plan.Uncovered
	bounds := s.workCoverBounds()
	deadline := now().Add(bounds.Budget)
	nico, bili := 0, 0
	var biliState *bilisource.State
	biliAccount := ""
	nicoOpen := s.Niconico != nil
	biliOpen := s.Bilibili != nil && !s.Skip["bilibili"]
	for _, ref := range plan.Lookups {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !now().Before(deadline) {
			break
		}
		var image, code string
		var lookupErr error
		switch ref.Platform {
		case works.Niconico:
			if !nicoOpen || nico >= bounds.Niconico {
				continue
			}
			nico++
			image, lookupErr = s.Niconico.Thumbnail(ctx, ref.ID)
			if lookupErr != nil {
				code = niconico.ErrorCode(lookupErr)
			}
			// A 429 is recorded like any failure and ends this run's niconico
			// lookups.
			nicoOpen = code != niconico.CodeRateLimited
		case works.Bilibili:
			if !biliOpen {
				if out.Bilibili == "" {
					out.Bilibili = "skipped"
				}
				continue
			}
			if bili >= bounds.Bilibili {
				continue
			}
			if biliState == nil {
				biliAccount = s.Bilibili.Options.Account
				state, err := s.Store.BilibiliState(ctx, biliAccount)
				if err != nil {
					return out, err
				}
				biliState = &state
			}
			if now().Before(biliState.BlockedUntil) {
				out.Bilibili, biliOpen = "blocked", false
				continue
			}
			bili++
			var state bilisource.State
			image, state, lookupErr = s.Bilibili.FetchCover(ctx, *biliState, ref.ID)
			*biliState = state
			if lookupErr != nil {
				code = bilisource.ErrorCode(lookupErr)
			}
			if err := s.Store.SaveBilibiliState(ctx, biliAccount, state); err != nil {
				return out, err
			}
			if bilisource.IsRiskControl(code) {
				// The backoff is shared with the list read; spend nothing more.
				out.Bilibili, biliOpen = "blocked", false
				continue
			}
		default:
			continue
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.Lookups++
		status := sqlite.WorkLookupResolved
		switch {
		case lookupErr == nil:
			out.Resolved++
		case code == niconico.CodeNotFound || code == "bilibili_not_found" || code == "bilibili_invalid_video" || code == niconico.CodeInvalidVideo:
			status = sqlite.WorkLookupMissing
			out.Missing++
		default:
			status = sqlite.WorkLookupFailed
			out.Failed++
		}
		if err := s.Store.SaveWorkCoverLookup(ctx, ref, status, image, code, now()); err != nil {
			return out, err
		}
	}
	return out, nil
}
