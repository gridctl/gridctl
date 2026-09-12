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
	writeJSON(w, map[string]any{
		"success":        false,
		"code":           result.Code,
		"message":        result.Message,
		"changed_fields": result.ChangedFields,
		"error": map[string]any{
			"code":           result.Code,
			"message":        result.Message,
			"changed_fields": result.ChangedFields,
		},
	})
	return true
}
