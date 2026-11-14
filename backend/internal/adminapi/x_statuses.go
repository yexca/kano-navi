package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// Related X accounts and the X status pool. The Sources page
// configures the related accounts; the External posts view lists the pool
// and records the operator's promote and demote decisions. None of these
// routes contacts X: the next X run reads the related timelines.

type relatedAccountsBody struct {
	Accounts        []sqlite.XRelatedAccount `json:"accounts"`
	DefaultKeywords []string                 `json:"defaultKeywords"`
	Status          []sqlite.XRelatedStatus  `json:"status"`
}

func (a *api) writeRelatedAccounts(w http.ResponseWriter, r *http.Request, accounts []sqlite.XRelatedAccount) {
	status, err := a.Store.XRelatedStatuses(r.Context(), accounts)
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, relatedAccountsBody{Accounts: accounts, DefaultKeywords: sqlite.DefaultXRelatedKeywords, Status: status})
}

func (a *api) relatedAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := a.Store.XRelatedAccounts(r.Context())
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	a.writeRelatedAccounts(w, r, accounts)
}

// updateRelatedAccounts replaces the related accounts. Every pooled status's
// reasons are evaluated again against the new keyword rules.
func (a *api) updateRelatedAccounts(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	for key := range body {
		if key != "accounts" {
			httpx.WriteError(w, httpx.Invalid("unknown field %s", key))
			return
		}
	}
	var accounts []sqlite.XRelatedAccount
	if raw, ok := body["accounts"]; !ok || json.Unmarshal(raw, &accounts) != nil || accounts == nil {
		httpx.WriteError(w, httpx.Invalid("accounts must be a list of {handle, keywords}"))
		return
	}
	saved, err := a.Store.SetXRelatedAccounts(r.Context(), accounts, a.XHandles)
	if err != nil {
		writeXStatusError(w, err)
		return
	}
	a.writeRelatedAccounts(w, r, saved)
}

func (a *api) listXStatuses(w http.ResponseWriter, r *http.Request) {
	page, err := a.Store.ListAdminXStatuses(r.Context(), sqlite.AdminXStatusFilter{
		Page:     queryInt(r, "page", 1),
		PageSize: queryInt(r, "pageSize", 25),
		Author:   query(r, "author"),
		Reason:   query(r, "reason"),
		State:    query(r, "state"),
		Search:   query(r, "search"),
		// The own accounts' statuses and posts belong to Posts.
		ExcludeOwn: true,
		Own:        a.XHandles,
	})
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// listBilibiliStatuses lists the Bilibili status pool: the dynamics own
// forwards forward. They are never promoted, as the forward's quote card
// shows them; their translations use the translation routes with owner
// bilibili_status.
func (a *api) listBilibiliStatuses(w http.ResponseWriter, r *http.Request) {
	kind := query(r, "kind")
	if kind != "" && kind != sqlite.BilibiliStatusKindDynamic && kind != sqlite.BilibiliStatusKindVideo {
		httpx.WriteError(w, httpx.Invalid("kind must be dynamic or video"))
		return
	}
	page, err := a.Store.ListBilibiliStatuses(r.Context(), sqlite.AdminBilibiliStatusFilter{
		Page:     queryInt(r, "page", 1),
		PageSize: queryInt(r, "pageSize", 25),
		Author:   query(r, "author"),
		Search:   query(r, "search"),
		Kind:     kind,
	})
	if err != nil {
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// decideXStatus records a manual promote or demote with its reason, or
// returns the status to the rules with "automatic".
func (a *api) decideXStatus(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var decision, reason string
	if raw, ok := body["decision"]; !ok || json.Unmarshal(raw, &decision) != nil {
		httpx.WriteError(w, httpx.Invalid("decision must be promote, demote or automatic"))
		return
	}
	if raw, ok := body["reason"]; ok && json.Unmarshal(raw, &reason) != nil {
		httpx.WriteError(w, httpx.Invalid("reason must be a string"))
		return
	}
	status, err := a.Store.SetXStatusDecision(r.Context(), r.PathValue("id"), decision, reason, a.XHandles...)
	if err != nil {
		writeXStatusError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": status})
}

func writeXStatusError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sqlite.ErrXRelatedInvalid):
		httpx.WriteError(w, httpx.Invalid("%s", strings.TrimPrefix(err.Error(), sqlite.ErrXRelatedInvalid.Error()+": ")))
	case errors.Is(err, sqlite.ErrXStatusNotFound):
		httpx.WriteCode(w, http.StatusNotFound, "not_found")
	case errors.Is(err, sqlite.ErrReadOnly):
		httpx.ReadOnly(w)
	default:
		httpx.WriteCode(w, http.StatusServiceUnavailable, "database_unavailable")
	}
}
