package authcallback

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
)

type statePayload struct {
	HostState     string `json:"host_state"`
	ProviderState string `json:"provider_state,omitempty"`
	UpstreamState string `json:"upstream_state,omitempty"`
}

func EncodeState(hostState string, providerState []byte, upstreamState string) (string, error) {
	payload := statePayload{HostState: hostState}
	if len(providerState) > 0 {
		payload.ProviderState = base64.RawURLEncoding.EncodeToString(providerState)
	}
	if upstreamState != "" {
		payload.UpstreamState = upstreamState
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode auth callback state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeState(raw string) (string, []byte, string, error) {
	if raw == "" {
		return "", nil, "", nil
	}
	data, ok := decodeOptionalBase64URL(raw)
	if !ok {
		return raw, nil, raw, nil
	}
	payload, ok := decodeOptionalStatePayload(data)
	if !ok {
		return raw, nil, raw, nil
	}
	if payload.ProviderState == "" {
		return payload.HostState, nil, payload.UpstreamState, nil
	}
	providerState, err := base64.RawURLEncoding.DecodeString(payload.ProviderState)
	if err != nil {
		return "", nil, "", fmt.Errorf("decode auth callback state: %w", err)
	}
	return payload.HostState, providerState, payload.UpstreamState, nil
}

func StateFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse authorization URL: %w", err)
	}
	return parsed.Query().Get("state"), nil
}

func WithWrappedStateParam(rawURL, state string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse authorization URL: %w", err)
	}
	values := parsed.Query()
	originalState := values.Get("state")
	values.Set("state", state)
	parsed.RawQuery = values.Encode()
	return parsed.String(), originalState, nil
}

func FirstQueryValues(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, candidates := range values {
		if len(candidates) > 0 {
			out[key] = candidates[0]
		}
	}
	return out
}

func decodeOptionalBase64URL(raw string) ([]byte, bool) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	return data, err == nil
}

func decodeOptionalStatePayload(data []byte) (statePayload, bool) {
	var payload statePayload
	if err := json.Unmarshal(data, &payload); err != nil || payload.HostState == "" {
		return statePayload{}, false
	}
	return payload, true
}
