package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func (s *Server) handleWebhook(mounted MountedWebhook, w http.ResponseWriter, r *http.Request) {
	rawBody, err := readWebhookBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	verified, err := s.verifyWebhookRequest(r, mounted, rawBody)
	if err != nil {
		var requestErr *webhookRequestError
		if errors.As(err, &requestErr) {
			if requestErr.status >= 500 {
				slog.ErrorContext(r.Context(), "webhook verification failed", "plugin", mounted.PluginName, "webhook", mounted.Name, "error", err)
			} else {
				slog.WarnContext(r.Context(), "webhook verification rejected request", "plugin", mounted.PluginName, "webhook", mounted.Name, "error", err)
			}
			writeError(w, requestErr.status, requestErr.message)
			return
		}
		slog.ErrorContext(r.Context(), "webhook verification failed", "plugin", mounted.PluginName, "webhook", mounted.Name, "error", err)
		writeError(w, http.StatusUnauthorized, "webhook verification failed")
		return
	}

	parsed, err := parseWebhookRequest(r, mounted.Operation, rawBody)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	executionMode := providermanifestv1.WebhookExecutionModeSync
	if mounted.Execution != nil && mounted.Execution.Mode != "" {
		executionMode = mounted.Execution.Mode
	}

	switch executionMode {
	case providermanifestv1.WebhookExecutionModeAsyncAck:
		s.dispatchWebhookAsync(mounted, verified, parsed.Params)
		responseCode := strings.TrimSpace(mounted.Execution.AcceptedResponse)
		response := mounted.Operation.Responses[responseCode]
		if err := writeWebhookResponse(w, responseCode, response); err != nil {
			slog.ErrorContext(r.Context(), "write webhook accepted response", "plugin", mounted.PluginName, "webhook", mounted.Name, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to write webhook response")
		}
		return
	default:
		switch {
		case mounted.Target != nil && strings.TrimSpace(mounted.Target.Operation) != "":
			result, err := s.webhookOperationInvocation(r.Context(), mounted, verified, parsed.Params)
			if err != nil {
				s.writeInvocationError(w, r, mounted.PluginName, mounted.Target.Operation, err)
				return
			}
			writeOperationResult(w, result)
			return
		case mounted.Target != nil && mounted.Target.Workflow != nil:
			run, err := s.startWebhookWorkflowRun(r.Context(), mounted, verified, parsed.Params)
			if err != nil {
				slog.ErrorContext(r.Context(), "webhook workflow start failed", "plugin", mounted.PluginName, "webhook", mounted.Name, "error", err)
				writeError(w, http.StatusBadGateway, "workflow start failed")
				return
			}
			if responseCode := defaultWebhookAcceptedResponseCode(mounted.Operation); responseCode != "" {
				response := mounted.Operation.Responses[responseCode]
				if response != nil && (response.Body != nil || len(response.Headers) > 0) {
					if err := writeWebhookResponse(w, responseCode, response); err != nil {
						slog.ErrorContext(r.Context(), "write webhook workflow response", "plugin", mounted.PluginName, "webhook", mounted.Name, "error", err)
						writeError(w, http.StatusInternalServerError, "failed to write webhook response")
					}
					return
				}
			}
			writeJSON(w, http.StatusAccepted, run)
			return
		default:
			writeError(w, http.StatusInternalServerError, "webhook target is not configured")
			return
		}
	}
}

func readWebhookBody(r *http.Request) ([]byte, error) {
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
