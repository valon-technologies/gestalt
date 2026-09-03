package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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

	for name, values := range result.Headers {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

func writeStreamingOperationResult(w http.ResponseWriter, r *http.Request, reader core.StreamReader) {
	if reader == nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		finalizeStreamReader(reader, errors.New("streaming is not supported"))
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	headersWritten := false
	terminalMetadataSeen := false
	writeHeaders := func(meta *core.InvokeMetadata) {
		mediaType := strings.TrimSpace(meta.MediaType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("Cache-Control", "no-cache")
		for name, values := range meta.Headers {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		status := meta.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		headersWritten = true
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			finalizeStreamReader(reader, ctx.Err())
			return
		default:
		}
		frame, err := reader.Recv()
		if err != nil {
			finalizeStreamReader(reader, err)
			if errors.Is(err, io.EOF) {
				if !headersWritten {
					writeError(w, http.StatusInternalServerError, "internal error")
				}
				return
			}
			if !headersWritten {
				writeInvocationStreamError(w, err)
			}
			return
		}
		if frame == nil {
			finalizeStreamReader(reader, nil)
			if !headersWritten {
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
		if frame.Metadata != nil {
			if !headersWritten {
				writeHeaders(frame.Metadata)
				if len(frame.Data) > 0 {
					_, _ = w.Write(frame.Data)
					flusher.Flush()
				}
			} else {
				if len(frame.Data) > 0 {
					_, _ = w.Write(frame.Data)
					flusher.Flush()
				}
				terminalMetadataSeen = true
				continue
			}
			continue
		}
		if !headersWritten {
			writeHeaders(&core.InvokeMetadata{Status: http.StatusOK})
		}
		if len(frame.Data) > 0 {
			_, _ = w.Write(frame.Data)
			flusher.Flush()
		}
		if terminalMetadataSeen {
			finalizeStreamReader(reader, nil)
			return
		}
	}
}

func finalizeStreamReader(reader core.StreamReader, err error) {
	if finalizer, ok := reader.(invocation.StreamFinalizer); ok {
		finalizer.FinalizeStream(err)
	}
}

func writeInvocationStreamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, invocation.ErrNotAuthenticated):
		writeError(w, http.StatusUnauthorized, "not authenticated")
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrReconnectRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, invocation.ErrInvalidInvocation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, invocation.ErrStreamingUnsupported):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, invocation.ErrAmbiguousInstance):
		writeError(w, http.StatusConflict, err.Error())
	default:
		var maxDepthErr *invocation.MaxDepthError
		if errors.As(err, &maxDepthErr) {
			writeError(w, http.StatusTooManyRequests, maxDepthErr.Error())
			return
		}
		var rateLimitErr *invocation.RateLimitError
		if errors.As(err, &rateLimitErr) {
			writeError(w, http.StatusTooManyRequests, rateLimitErr.Error())
			return
		}
		var recursionErr *invocation.RecursionError
		if errors.As(err, &recursionErr) {
			writeError(w, http.StatusBadRequest, recursionErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
