package mcpoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Registration struct {
	AuthServerURL         string
	RedirectURI           string
	ClientID              string
	ClientSecret          string // empty for public clients
	ExpiresAt             *time.Time
	AuthorizationEndpoint string
	TokenEndpoint         string
	ScopesSupported       string // JSON array
	DiscoveredAt          time.Time
}

func (r *Registration) Expired() bool {
	if r.ExpiresAt != nil {
		return time.Now().After(*r.ExpiresAt)
	}
	return false
}

func RegisterClient(ctx context.Context, endpoint, redirectURI, clientName, tokenAuthMethod string) (*Registration, error) {
	return registerClient(ctx, endpoint, redirectURI, clientName, tokenAuthMethod, discoveryPolicy{})
}

func registerClient(ctx context.Context, endpoint, redirectURI, clientName, tokenAuthMethod string, policy discoveryPolicy) (*Registration, error) {
	if tokenAuthMethod == "" {
		tokenAuthMethod = "none"
	}
	reqBody := map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": tokenAuthMethod,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encoding DCR request: %w", err)
	}

	if err := validateDiscoveryURL(ctx, endpoint, policy); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating DCR request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client, closeIdleConnections := newHTTPClient(discoveryTimeout, policy)
	defer closeIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending DCR request to %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading DCR response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("DCR endpoint %s returned %d: %s", endpoint, resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding DCR response: %w", err)
	}

	clientID, _ := result["client_id"].(string)
	if clientID == "" {
		return nil, fmt.Errorf("DCR response missing client_id")
	}
	clientSecret, _ := result["client_secret"].(string)

	reg := &Registration{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		DiscoveredAt: time.Now(),
	}

	if expiresAt, ok := result["client_secret_expires_at"].(float64); ok && expiresAt > 0 {
		t := time.Unix(int64(expiresAt), 0)
		reg.ExpiresAt = &t
	}

	return reg, nil
}
