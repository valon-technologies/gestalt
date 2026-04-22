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

type parsedHTTPBindingRequest struct {
	ContentType string
	Params      map[string]any
	RawBody     []byte
}

func parseHTTPBindingRequest(r *http.Request, binding MountedHTTPBinding, rawBody []byte) (*parsedHTTPBindingRequest, error) {
	params := httpBindingQueryParams(r, binding)
	contentType, err := requestMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	selectedContentType := contentType
	if len(rawBody) == 0 {
		if binding.RequestBody != nil && binding.RequestBody.Required {
			return nil, fmt.Errorf("request body is required")
		}
		return &parsedHTTPBindingRequest{
			ContentType: selectedContentType,
			Params:      params,
			RawBody:     rawBody,
		}, nil
	}

	_, resolvedContentType, err := resolveHTTPBindingRequestContentType(binding.RequestBody, contentType)
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
		formParams, err := decodedHTTPBindingFormParams(binding, values)
		if err != nil {
			return nil, err
		}
		for key, value := range formParams {
			params[key] = value
		}
	default:
		params["rawBody"] = string(rawBody)
	}

	return &parsedHTTPBindingRequest{
		ContentType: selectedContentType,
		Params:      params,
		RawBody:     rawBody,
	}, nil
}

func httpBindingQueryParams(r *http.Request, binding MountedHTTPBinding) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	var excludeKey string
	if binding.Security != nil &&
		binding.Security.Type == providermanifestv1.HTTPSecuritySchemeTypeAPIKey &&
		binding.Security.In == providermanifestv1.HTTPInQuery {
		excludeKey = strings.TrimSpace(binding.Security.Name)
	}
	return firstValueMapExcept(r.URL.Query(), excludeKey)
}

func decodedHTTPBindingFormParams(binding MountedHTTPBinding, values url.Values) (map[string]any, error) {
	params := firstValueMap(values)
	if binding.Security == nil || binding.Security.Type != providermanifestv1.HTTPSecuritySchemeTypeSlackSignature {
		return params, nil
	}
	payloadValue, found := params["payload"]
	if !found {
		return params, nil
	}
	payload, ok := payloadValue.(string)
	if !ok || strings.TrimSpace(payload) == "" {
		return nil, fmt.Errorf("invalid Slack payload form field")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil, fmt.Errorf("invalid Slack payload form field")
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("invalid Slack payload form field")
	}
	params["payload"] = decoded
	return params, nil
}

func resolveHTTPBindingRequestContentType(requestBody *providermanifestv1.HTTPRequestBody, contentType string) (*providermanifestv1.HTTPMediaType, string, error) {
	if requestBody == nil || len(requestBody.Content) == 0 {
		return nil, contentType, nil
	}
	if contentType != "" {
		if mediaType, ok := requestBody.Content[contentType]; ok {
			return mediaType, contentType, nil
		}
		if wildcard, ok := requestBody.Content["*/*"]; ok {
			return wildcard, contentType, nil
		}
		return nil, "", fmt.Errorf("unsupported content type %q", contentType)
	}
	if len(requestBody.Content) == 1 {
		for resolved, mediaType := range requestBody.Content {
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
	return firstValueMapExcept(values, "")
}

func firstValueMapExcept(values url.Values, excludeKey string) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	params := make(map[string]any, len(values))
	for key, items := range values {
		if excludeKey != "" && key == excludeKey {
			continue
		}
		if len(items) == 0 {
			continue
		}
		params[key] = items[0]
	}
	return params
}
