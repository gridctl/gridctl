package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/reload"
)

func writePreflightFailure(w http.ResponseWriter, result *reload.ReloadResult) bool {
	if result.Code == "" {
		return false
	}
	status := http.StatusBadRequest
	if result.Code == "restart_required" {
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{
		"success":        false,
		"code":           result.Code,
		"message":        result.Message,
		"changed_fields": result.ChangedFields,
		"error": map[string]any{
			"code":           result.Code,
			"message":        result.Message,
			"changed_fields": result.ChangedFields,
		},
	}
	if len(result.Errors) > 0 {
		body["errors"] = result.Errors
		body["error"].(map[string]any)["errors"] = result.Errors
	}
	writeJSON(w, body)
	return true
}
