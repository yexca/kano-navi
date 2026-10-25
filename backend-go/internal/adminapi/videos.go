package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
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
func (a *api) refreshVideo(w http.ResponseWriter, r *http.Request) {
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

func writeVideoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrVideoNotFound):
		httpx.WriteCode(w, http.StatusNotFound, "video_not_found")
	case errors.Is(err, sqlite.ErrVideoInvalid):
		httpx.WriteError(w, httpx.Invalid("video correction is invalid"))
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.ReadOnly(w)
	default:
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
	}
}
