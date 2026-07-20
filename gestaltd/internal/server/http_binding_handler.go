package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
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
				w.WriteHeader(requestErr.status)
				return
			}
			if requestErr.status >= 500 {
				slog.ErrorContext(r.Context(), "http binding verification failed", "app", binding.AppName, "binding", binding.Name, "error", err)
			} else {
				slog.WarnContext(r.Context(), "http binding verification rejected request", "app", binding.AppName, "binding", binding.Name, "error", err)
			}
			writeError(w, requestErr.status, requestErr.message)
			return
		}
		slog.ErrorContext(r.Context(), "http binding verification failed", "app", binding.AppName, "binding", binding.Name, "error", err)
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
				slog.ErrorContext(r.Context(), "http binding subject resolution failed", "app", binding.AppName, "binding", binding.Name, "error", err)
			} else {
				slog.WarnContext(r.Context(), "http binding subject resolution rejected request", "app", binding.AppName, "binding", binding.Name, "error", err)
			}
			writeError(w, requestErr.status, requestErr.message)
			return
		}
		slog.ErrorContext(r.Context(), "http binding subject resolution failed", "app", binding.AppName, "binding", binding.Name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve http binding subject")
		return
	}

	outcome, err := s.httpBindingInvocation(r.Context(), binding, r, resolvedPrincipal, verified, parsed)
	if err != nil {
		s.writeInvocationError(w, r, binding.AppName, binding.Target, err)
		return
	}
	if outcome.IsStream() {
		writeStreamingOperationResult(w, r, outcome.Stream)
		return
	}
	writeOperationResult(w, outcome.Unary)
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
