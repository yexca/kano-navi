package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	bilisource "github.com/yexca/kano-navi/backend-go/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Video corrections are operator decisions layered over what the platform
// reports. Synchronization keeps writing the platform fields; the history
// keeps every title and thumbnail a creator replaced.

func (a *api) videoHistory(w http.ResponseWriter, r *http.Request) {
	history, err := a.Store.VideoHistory(r.Context(), r.PathValue("id"))
	if err != nil {
		writeVideoError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, history)
}

// updateVideo accepts { title?, kind?, thumbnail? }. A string or a
// { mediaId, sha256 } file from the history sets a correction, null clears
// it, and an omitted field keeps it.
func (a *api) updateVideo(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var in sqlite.VideoOverrides
	for key, raw := range body {
		switch key {
		case "title", "kind":
			var value *string
			if !jsonNull(raw) {
				var text string
				if json.Unmarshal(raw, &text) != nil {
					httpx.WriteError(w, httpx.Invalid("%s must be a string or null", key))
					return
				}
				value = &text
			}
			if key == "title" {
				in.SetTitle, in.Title = true, value
			} else {
				in.SetKind, in.Kind = true, value
			}
		case "thumbnail":
			in.SetThumbnail = true
			if jsonNull(raw) {
				continue
			}
			var file struct {
				MediaID string `json:"mediaId"`
				SHA256  string `json:"sha256"`
			}
			if json.Unmarshal(raw, &file) != nil || file.MediaID == "" || file.SHA256 == "" {
				httpx.WriteError(w, httpx.Invalid("thumbnail must name a mediaId and sha256 or be null"))
				return
			}
			in.ThumbnailMediaID, in.ThumbnailSHA256 = &file.MediaID, &file.SHA256
		default:
			httpx.WriteError(w, httpx.Invalid("%s is not supported", key))
			return
		}
	}
	video, err := a.Store.SetVideoOverrides(r.Context(), r.PathValue("id"), in, a.Now())
	if err != nil {
		writeVideoError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"video": video})
}

// refreshVideo records a refresh request and starts the YouTube and media
// steps. A busy queue keeps the request for the next run that reads YouTube.
// A Bilibili upload is read again at once instead.
func (a *api) refreshVideo(w http.ResponseWriter, r *http.Request) {
	current, err := a.Store.AdminVideoByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeVideoError(w, err)
		return
	}
	if current.Source == "bilibili" {
		a.readBilibili(w, r, current.ID)
		return
	}
	if a.skipped("youtube") {
		httpx.WriteError(w, httpx.Invalid("the YouTube source is disabled for this process"))
		return
	}
	video, err := a.Store.RequestVideoRefresh(r.Context(), r.PathValue("id"), a.Now())
	if err != nil {
		writeVideoError(w, err)
		return
	}
	response := map[string]any{"video": video, "started": false, "run": nil}
	if a.Runs != nil {
		result, startErr := a.Runs.Start(r.Context(), jobs.KindWorkflow, "admin", jobs.Options{Steps: []string{"youtube", "media"}})
		if startErr != nil {
			workflowError(w, startErr, "workflow_not_found")
			return
		}
		response["started"], response["run"] = result.Accepted, result.Job
	}
	httpx.WriteJSON(w, http.StatusAccepted, response)
}

// videoMerges lists the Bilibili uploads the merge rule would attach to a
// YouTube video, or leaves to the operator, without writing.
func (a *api) videoMerges(w http.ResponseWriter, r *http.Request) {
	a.writeMergeSuggestions(w, r, "")
}

// uploadMerges describes one upload's merge state and candidates, whatever
// its state; another ID lists nothing.
func (a *api) uploadMerges(w http.ResponseWriter, r *http.Request) {
	a.writeMergeSuggestions(w, r, r.PathValue("id"))
}

func (a *api) writeMergeSuggestions(w http.ResponseWriter, r *http.Request, only string) {
	items, err := a.Store.VideoMergeSuggestions(r.Context(), only)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// mergeVideo accepts { into } with a YouTube video ID or watch link and
// attaches the Bilibili upload to that video as an operator decision.
func (a *api) mergeVideo(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var into string
	if json.Unmarshal(body["into"], &into) != nil || into == "" {
		httpx.WriteError(w, httpx.Invalid("into must name a YouTube video"))
		return
	}
	video, err := a.Store.MergeVideo(r.Context(), r.PathValue("id"), into, a.Now())
	if err != nil {
		writeVideoError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"video": video})
}

// unmergeVideo shows a merged upload on its own again. The decision is
// durable: the rules never merge that upload automatically afterwards.
func (a *api) unmergeVideo(w http.ResponseWriter, r *http.Request) {
	video, err := a.Store.UnmergeVideo(r.Context(), r.PathValue("id"), a.Now())
	if err != nil {
		writeVideoError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"video": video})
}

// addBilibiliVideo accepts { input } with a BV ID or a bilibili.com video
// link and stores that upload of the configured account.
func (a *api) addBilibiliVideo(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var input string
	_ = json.Unmarshal(body["input"], &input)
	bvid, ok := bilisource.ParseBVID(input)
	if !ok {
		httpx.WriteError(w, httpx.Invalid("enter a BV ID or a bilibili.com video link"))
		return
	}
	a.readBilibili(w, r, bvid)
}

// readBilibili reads one upload now, which costs one or two Bilibili
// requests, stores it and starts the media step for its cover. A captcha
// block is respected rather than retried.
func (a *api) readBilibili(w http.ResponseWriter, r *http.Request, bvid string) {
	if a.Bilibili == nil || a.skipped("bilibili") {
		httpx.WriteError(w, httpx.Invalid("the Bilibili source is disabled for this process"))
		return
	}
	ctx := r.Context()
	account := a.Bilibili.Options.Account
	state, err := a.Store.BilibiliState(ctx, account)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	upload, next, err := a.Bilibili.FetchVideo(ctx, state, bvid)
	if err != nil {
		_ = a.Store.SaveBilibiliState(ctx, account, next)
		writeBilibiliError(w, err, next)
		return
	}
	if _, err = a.Store.WriteBilibiliVideo(ctx, account, upload, next); err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	video, err := a.Store.AdminVideoByID(ctx, upload.BVID)
	if err != nil {
		writeVideoError(w, err)
		return
	}
	response := map[string]any{"video": video, "started": false, "run": nil}
	if a.Runs != nil && !a.skipped("media") {
		if result, startErr := a.Runs.Start(ctx, jobs.KindWorkflow, "admin", jobs.Options{Steps: []string{"media"}}); startErr == nil {
			response["started"], response["run"] = result.Accepted, result.Job
		}
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func writeBilibiliError(w http.ResponseWriter, err error, state bilisource.State) {
	switch code := bilisource.ErrorCode(err); code {
	case "bilibili_risk_control", "bilibili_rate_limited":
		until := state.BlockedUntil.UTC().Format(time.RFC3339)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "bilibili_blocked", "blockedUntil": until,
			"message": "Bilibili is asking this server for a captcha; requests resume after " + until,
		})
	case "bilibili_not_found":
		httpx.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "video_not_found", "message": "Bilibili does not list this video"})
	case "bilibili_other_account":
		httpx.WriteError(w, httpx.Invalid("this video was not uploaded by the configured Bilibili account"))
	default:
		httpx.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": code, "message": "Bilibili did not answer (" + code + ")"})
	}
}

// bilibiliStatus is the Bilibili source as the console sees it.
func (a *api) bilibiliStatus(ctx context.Context) (map[string]any, error) {
	if a.Bilibili == nil {
		return map[string]any{"available": false, "skipped": a.skipped("bilibili")}, nil
	}
	status, err := a.Store.BilibiliStatus(ctx, a.Bilibili.Options.Account)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"available": true, "skipped": a.skipped("bilibili"), "account": status.Account, "videos": status.Videos,
		"signedIn":           a.Bilibili.Options.SignedIn(),
		"minIntervalMinutes": int(a.Bilibili.Options.MinInterval / time.Minute),
		"lastAttemptAt":      status.LastAttemptAt, "lastSuccessAt": status.LastSuccessAt,
		"blockedUntil": status.BlockedUntil, "lastError": status.LastError,
	}, nil
}

func writeVideoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrVideoNotFound):
		httpx.WriteCode(w, http.StatusNotFound, "video_not_found")
	case errors.Is(err, sqlite.ErrVideoMergeInvalid):
		httpx.WriteError(w, httpx.Invalid("only a Bilibili upload can be merged, into a stored YouTube video"))
	case errors.Is(err, sqlite.ErrVideoMergeConflict):
		httpx.WriteCode(w, http.StatusConflict, "merge_conflict")
	case errors.Is(err, sqlite.ErrVideoNotMerged):
		httpx.WriteError(w, httpx.Invalid("video is not merged"))
	case errors.Is(err, sqlite.ErrVideoInvalid):
		httpx.WriteError(w, httpx.Invalid("video correction is invalid"))
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.ReadOnly(w)
	default:
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
	}
}
