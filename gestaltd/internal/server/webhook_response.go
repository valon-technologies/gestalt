package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func writeWebhookResponse(w http.ResponseWriter, code string, response *providermanifestv1.WebhookResponse) error {
	if response == nil {
		return fmt.Errorf("webhook response %q is not configured", code)
	}
	status, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return fmt.Errorf("parse webhook response status %q: %w", code, err)
	}
	for key, value := range response.Headers {
		w.Header().Set(key, value)
	}
	contentType := w.Header().Get("Content-Type")
	if contentType == "" && len(response.Content) == 1 {
		for mediaType := range response.Content {
			contentType = mediaType
		}
	}
	if contentType == "" && response.Body != nil {
		contentType = "application/json"
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	if response.Body == nil {
		return nil
	}
	switch {
	case strings.HasPrefix(contentType, "text/plain"):
		_, err = fmt.Fprint(w, response.Body)
	default:
		data, marshalErr := json.Marshal(response.Body)
		if marshalErr != nil {
			return marshalErr
		}
		_, err = w.Write(data)
	}
	return err
}

func defaultWebhookAcceptedResponseCode(op *providermanifestv1.WebhookOperation) string {
	if op == nil || len(op.Responses) == 0 {
		return ""
	}
	codes := make([]int, 0, len(op.Responses))
	codeByInt := make(map[int]string, len(op.Responses))
	for code := range op.Responses {
		parsed, err := strconv.Atoi(code)
		if err != nil {
			continue
		}
		codes = append(codes, parsed)
		codeByInt[parsed] = code
	}
	sort.Ints(codes)
	for _, code := range codes {
		if code >= 200 && code < 300 {
			return codeByInt[code]
		}
	}
	return ""
}
