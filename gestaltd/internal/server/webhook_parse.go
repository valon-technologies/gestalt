package server

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type parsedWebhookRequest struct {
	ContentType string
	Params      map[string]any
	RawBody     []byte
}

func parseWebhookRequest(r *http.Request, op *providermanifestv1.WebhookOperation, rawBody []byte) (*parsedWebhookRequest, error) {
	params := firstValueMap(r.URL.Query())
	contentType, err := requestMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	selectedContentType := contentType
	if len(rawBody) == 0 {
		if op != nil && op.RequestBody != nil && op.RequestBody.Required {
			return nil, fmt.Errorf("request body is required")
		}
		return &parsedWebhookRequest{
			ContentType: selectedContentType,
			Params:      params,
			RawBody:     rawBody,
		}, nil
	}

	_, resolvedContentType, err := resolveWebhookRequestContentType(op, contentType)
	if err != nil {
		return nil, err
	}
	if resolvedContentType != "" {
		selectedContentType = resolvedContentType
	}
	switch strings.ToLower(selectedContentType) {
	case "application/json":
		var decoded any
		if err := json.Unmarshal(rawBody, &decoded); err != nil {
			return nil, fmt.Errorf("invalid JSON body")
		}
		switch value := decoded.(type) {
		case map[string]any:
			for key, item := range value {
				params[key] = item
			}
		default:
			params["body"] = value
		}
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(rawBody))
		if err != nil {
			return nil, fmt.Errorf("invalid form body")
		}
		for key, value := range firstValueMap(values) {
			params[key] = value
		}
	default:
		params["rawBody"] = string(rawBody)
	}

	return &parsedWebhookRequest{
		ContentType: selectedContentType,
		Params:      params,
		RawBody:     rawBody,
	}, nil
}

func resolveWebhookRequestContentType(op *providermanifestv1.WebhookOperation, contentType string) (*providermanifestv1.WebhookMediaType, string, error) {
	if op == nil || op.RequestBody == nil || len(op.RequestBody.Content) == 0 {
		return nil, contentType, nil
	}
	if contentType != "" {
		if mediaType, ok := op.RequestBody.Content[contentType]; ok {
			return mediaType, contentType, nil
		}
		if wildcard, ok := op.RequestBody.Content["*/*"]; ok {
			return wildcard, contentType, nil
		}
		return nil, "", fmt.Errorf("unsupported content type %q", contentType)
	}
	if len(op.RequestBody.Content) == 1 {
		for resolved, mediaType := range op.RequestBody.Content {
			return mediaType, resolved, nil
		}
	}
	return nil, "", fmt.Errorf("content type is required")
}

func requestMediaType(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "", fmt.Errorf("invalid Content-Type header")
	}
	return strings.ToLower(mediaType), nil
}

func firstValueMap(values url.Values) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	params := make(map[string]any, len(values))
	for key, items := range values {
		if len(items) == 0 {
			continue
		}
		params[key] = items[0]
	}
	return params
}
