package server

import (
	"encoding/json"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
)

type apiErrorResponse struct {
	Error       string `json:"error"`
	Code        string `json:"code,omitempty"`
	Integration string `json:"integration,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiErrorResponse{Error: message})
}

func writeTypedError(w http.ResponseWriter, status int, code, integration, message string) {
	writeJSON(w, status, apiErrorResponse{
		Error:       message,
		Code:        code,
		Integration: integration,
	})
}

func writeOperationResult(w http.ResponseWriter, result *core.OperationResult) {
	if result == nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	contentType := "application/json"
	if result.Headers != nil {
		if ct := result.Headers.Get("Content-Type"); ct != "" {
			contentType = ct
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(result.Status)
	_, _ = w.Write([]byte(result.Body))
}
