package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/jsonbody"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func (h *adminHandler) saveWorkflow(w http.ResponseWriter, r *http.Request, path string) {
	if h.queue == nil {
		writeJSON(w, 501, map[string]string{"error": "capability_not_migrated"}, "")
		return
	}
	id := strings.TrimPrefix(path, "/workflows/")
	create := r.Method == http.MethodPost && path == "/workflows"
	if r.Method == http.MethodDelete && path != "/workflows" && !strings.Contains(id, "/") {
		err := h.store.DeleteWorkflow(r.Context(), id)
		if err != nil {
			if errors.Is(err, sqlite.ErrWorkflowBusy) {
				writeJSON(w, 409, map[string]string{"error": "workflow_busy"}, "")
			} else {
				workflowUnavailable(w, err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !create && (r.Method != http.MethodPut || path == "/workflows" || strings.Contains(id, "/")) {
		writeMethodNotAllowed(w, "GET")
		return
	}
	input := sqlite.WorkflowDefinition{IntervalMinutes: 60, SourceLookbackDays: map[string]int{"x": 7, "youtube": 14}}
	if !create {
		existing, err := h.store.GetWorkflow(r.Context(), id)
		if err != nil {
			workflowUnavailable(w, err)
			return
		}
		input = *existing
	}
	body, err := jsonbody.Object(w, r)
	if err != nil {
		status := 400
		if errors.Is(err, jsonbody.ErrLarge) {
			status = 413
		}
		writeJSON(w, status, map[string]string{"error": err.Error()}, "")
		return
	}
	for key, raw := range body {
		var target any
		switch key {
		case "name":
			target = &input.Name
		case "steps":
			target = &input.Steps
		case "sourceLookbackDays":
			target = &input.SourceLookbackDays
		case "scheduleEnabled":
			target = &input.ScheduleEnabled
		case "intervalMinutes":
			target = &input.IntervalMinutes
		default:
			writeJSON(w, 400, map[string]string{"error": "unsupported_parameter"}, "")
			return
		}
		if string(raw) == "null" || json.Unmarshal(raw, target) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid_input"}, "")
			return
		}
	}
	item, err := h.store.SaveWorkflow(r.Context(), input, create)
	if err != nil {
		if errors.Is(err, sqlite.ErrWorkflowValidation) {
			writeJSON(w, 400, map[string]string{"error": "invalid_input"}, "")
			return
		}
		workflowUnavailable(w, err)
		return
	}
	status := 200
	if create {
		status = 201
	}
	writeJSON(w, status, map[string]any{"workflow": item}, "")
}
