package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Translations are edited per owner and locale. A manual translation outranks
// platform and machine ones; a request queues a machine pass that the
// translate step answers, first among its candidates.

func translationOwner(r *http.Request) (string, bool) {
	owner := r.PathValue("owner")
	return owner, sqlite.TranslationField(owner) != ""
}

func writeTranslationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.ReadOnly(w)
	case errors.Is(err, sqlite.ErrTranslationOwner):
		httpx.WriteCode(w, http.StatusNotFound, "not_found")
	case errors.Is(err, sqlite.ErrTranslationInvalid):
		httpx.WriteError(w, httpx.Invalid("translation is invalid"))
	default:
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
	}
}

func (a *api) translationSummary(w http.ResponseWriter, r *http.Request) {
	counts, err := a.Store.TranslationSummary(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"counts": counts, "locales": sqlite.TranslationLocales})
}

// writeOwnerTranslation answers a manual write with the owner's new console
// summary.
func (a *api) writeOwnerTranslation(w http.ResponseWriter, r *http.Request, owner, id string) {
	items, err := a.Store.AdminTranslations(r.Context(), owner, []string{id})
	if err != nil {
		writeTranslationError(w, err)
		return
	}
	item := items[id]
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"translation": item})
}

func (a *api) putManualTranslation(w http.ResponseWriter, r *http.Request) {
	owner, ok := translationOwner(r)
	if !ok {
		httpx.WriteCode(w, http.StatusNotFound, "not_found")
		return
	}
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	for key := range body {
		if key != "text" {
			httpx.WriteError(w, httpx.Invalid("%s is not supported", key))
			return
		}
	}
	var text string
	if json.Unmarshal(body["text"], &text) != nil {
		httpx.WriteError(w, httpx.Invalid("text must be a string"))
		return
	}
	id, locale := r.PathValue("id"), r.PathValue("locale")
	if err = a.Store.SetManualTranslation(r.Context(), owner, id, locale, text); err != nil {
		writeTranslationError(w, err)
		return
	}
	a.writeOwnerTranslation(w, r, owner, id)
}

func (a *api) deleteManualTranslation(w http.ResponseWriter, r *http.Request) {
	owner, ok := translationOwner(r)
	if !ok {
		httpx.WriteCode(w, http.StatusNotFound, "not_found")
		return
	}
	id, locale := r.PathValue("id"), r.PathValue("locale")
	if err := a.Store.DeleteManualTranslation(r.Context(), owner, id, locale); err != nil {
		writeTranslationError(w, err)
		return
	}
	a.writeOwnerTranslation(w, r, owner, id)
}

// requestTranslations queues machine passes, either for the listed owners
// (a forced retranslation) or for every owner lacking a usable translation,
// and starts the translate step when the queue is free. A busy queue keeps
// the requests for the next translate step.
func (a *api) requestTranslations(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	for key := range body {
		if key != "ownerType" && key != "ids" && key != "missing" {
			httpx.WriteError(w, httpx.Invalid("%s is not supported", key))
			return
		}
	}
	var owner string
	if raw, present := body["ownerType"]; present {
		if json.Unmarshal(raw, &owner) != nil || sqlite.TranslationField(owner) == "" {
			httpx.WriteError(w, httpx.Invalid("ownerType must be post or video"))
			return
		}
	}
	var ids []string
	var missing bool
	if raw, present := body["ids"]; present && json.Unmarshal(raw, &ids) != nil {
		httpx.WriteError(w, httpx.Invalid("ids must be an array of strings"))
		return
	}
	if raw, present := body["missing"]; present && json.Unmarshal(raw, &missing) != nil {
		httpx.WriteError(w, httpx.Invalid("missing must be a boolean"))
		return
	}
	var queued int
	switch {
	case missing && len(ids) == 0:
		queued, err = a.Store.RequestMissingTranslations(r.Context(), owner)
	case !missing && len(ids) > 0 && owner != "":
		queued, err = a.Store.RequestTranslations(r.Context(), owner, ids)
	default:
		httpx.WriteError(w, httpx.Invalid("send missing: true, or ownerType with ids"))
		return
	}
	if err != nil {
		writeTranslationError(w, err)
		return
	}
	response := map[string]any{"queued": queued, "run": nil, "busy": false}
	if a.Runs != nil && queued > 0 {
		result, startErr := a.Runs.Start(r.Context(), jobs.KindTranslate, "admin", jobs.Options{})
		if startErr == nil {
			response["run"], response["busy"] = result.Job, !result.Accepted
		}
	}
	httpx.WriteJSON(w, http.StatusAccepted, response)
}
