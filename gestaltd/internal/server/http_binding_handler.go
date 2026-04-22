package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/invocation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func (s *Server) handleHTTPBinding(binding MountedHTTPBinding, w http.ResponseWriter, r *http.Request) {
	rawBody, err := readHTTPBindingBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	verified, err := s.verifyHTTPBindingRequest(r, binding, rawBody)
	if err != nil {
		var requestErr *httpBindingRequestError
		if errors.As(err, &requestErr) {
			if requestErr.status > 0 && requestErr.status < 400 {
				if binding.Ack != nil {
					if err := writeHTTPBindingAck(w, binding.Ack); err != nil {
						slog.ErrorContext(r.Context(), "write http binding ack", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
						writeError(w, http.StatusInternalServerError, "failed to write http binding response")
					}
				} else {
					w.WriteHeader(requestErr.status)
				}
				return
			}
			if requestErr.status >= 500 {
				slog.ErrorContext(r.Context(), "http binding verification failed", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
			} else {
				slog.WarnContext(r.Context(), "http binding verification rejected request", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
			}
			writeError(w, requestErr.status, requestErr.message)
			return
		}
		slog.ErrorContext(r.Context(), "http binding verification failed", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
		writeError(w, http.StatusUnauthorized, "http binding verification failed")
		return
	}

	parsed, err := parseHTTPBindingRequest(r, binding, rawBody)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resolvedPrincipal, err := s.resolveHTTPBindingPrincipal(r.Context(), binding, r, verified, parsed)
	if err != nil {
		var requestErr *httpBindingRequestError
		if errors.As(err, &requestErr) {
			if requestErr.status >= 500 {
				slog.ErrorContext(r.Context(), "http binding subject resolution failed", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
			} else {
				slog.WarnContext(r.Context(), "http binding subject resolution rejected request", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
			}
			writeError(w, requestErr.status, requestErr.message)
			return
		}
		slog.ErrorContext(r.Context(), "http binding subject resolution failed", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve http binding subject")
		return
	}

	if binding.Ack != nil {
		if err := writeHTTPBindingAck(w, binding.Ack); err != nil {
			slog.ErrorContext(r.Context(), "write http binding ack", "plugin", binding.PluginName, "binding", binding.Name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to write http binding response")
			return
		}
		s.dispatchHTTPBindingAsync(binding, resolvedPrincipal, verified, parsed, invocation.RequestMetaFromContext(r.Context()))
		return
	}

	result, err := s.httpBindingOperationInvocation(r.Context(), binding, resolvedPrincipal, verified, parsed)
	if err != nil {
		s.writeInvocationError(w, r, binding.PluginName, binding.Target, err)
		return
	}
	writeOperationResult(w, result)
}

func readHTTPBindingBody(r *http.Request) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, nil
	}
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errors.New("failed to read request body")
	}
	return body, nil
}

func writeHTTPBindingAck(w http.ResponseWriter, ack *providermanifestv1.HTTPAck) error {
	if ack == nil {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	status := ack.Status
	if status == 0 {
		status = http.StatusOK
	}
	for key, value := range ack.Headers {
		w.Header().Set(key, value)
	}
	if ack.Body == nil {
		w.WriteHeader(status)
		return nil
	}
	if contentType := strings.TrimSpace(w.Header().Get("Content-Type")); contentType != "" && !strings.Contains(strings.ToLower(contentType), "json") {
		switch body := ack.Body.(type) {
		case string:
			w.WriteHeader(status)
			_, err := w.Write([]byte(body))
			return err
		case []byte:
			w.WriteHeader(status)
			_, err := w.Write(body)
			return err
		default:
			return errors.New("non-JSON ack bodies must be string or bytes")
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(ack.Body)
}
