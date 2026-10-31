package adminapi

import (
	"net/http"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// syncSettings is the operator's read bounds with each field's default and
// range, which the console renders as its inputs.
func (a *api) syncSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.Store.SyncSettings(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"settings": settings, "fields": sqlite.SyncSettingSpecs})
}

// updateSyncSettings accepts any subset of the fields as integers within
// their ranges; omitted fields keep their value. The next run reads them.
func (a *api) updateSyncSettings(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	settings, err := a.Store.SyncSettings(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	values := map[string]int{}
	for field, raw := range body {
		spec, ok := syncSettingSpec(field)
		if !ok {
			httpx.WriteError(w, httpx.Invalid("%s is not a sync setting", field))
			return
		}
		n, rangeErr := integer(raw, field, spec.Min, spec.Max)
		if rangeErr != nil {
			httpx.WriteError(w, rangeErr)
			return
		}
		values[field] = n
	}
	if err = settings.Set(values); err != nil {
		workflowError(w, err, "not_found")
		return
	}
	if err = a.Store.SaveSyncSettings(r.Context(), settings); err != nil {
		workflowError(w, err, "not_found")
		return
	}
	a.syncSettings(w, r)
}

func syncSettingSpec(field string) (sqlite.SyncSettingSpec, bool) {
	for _, spec := range sqlite.SyncSettingSpecs {
		if spec.Field == field {
			return spec, true
		}
	}
	return sqlite.SyncSettingSpec{}, false
}
