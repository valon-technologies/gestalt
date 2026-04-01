package server

import (
	"encoding/json"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", core.ContentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeOperationResult(w http.ResponseWriter, result *core.OperationResult) {
	if result == nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	contentType := core.ContentTypeJSON
	if result.Headers != nil {
		if ct := result.Headers.Get("Content-Type"); ct != "" {
			contentType = ct
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(result.Status)
	_, _ = w.Write([]byte(result.Body))
}
