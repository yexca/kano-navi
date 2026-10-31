package adminapi

import (
	"errors"
	"net/http"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

func (a *api) ytdlpStatus(w http.ResponseWriter, r *http.Request) {
	if a.YoutubeYTDLP == nil {
		httpx.WriteJSON(w, http.StatusOK, ytsource.YTDLPStatus{})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.YoutubeYTDLP.Status(r.Context()))
}

func (a *api) updateYTDLP(w http.ResponseWriter, r *http.Request) {
	if a.YoutubeYTDLP == nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "youtube_ytdlp_unavailable")
		return
	}
	if err := a.YoutubeYTDLP.Update(r.Context()); err != nil {
		var sourceErr *ytsource.Error
		if errors.As(err, &sourceErr) {
			httpx.WriteCode(w, http.StatusServiceUnavailable, sourceErr.Code)
			return
		}
		httpx.WriteCode(w, http.StatusServiceUnavailable, "youtube_ytdlp_update_failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.YoutubeYTDLP.Status(r.Context()))
}
