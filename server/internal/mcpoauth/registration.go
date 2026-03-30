package mcpoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

type Registration struct {
	AuthServerURL         string
	RedirectURI           string
	ClientID              string
	ClientSecret          string // empty for public clients
	AuthorizationEndpoint string
	TokenEndpoint         string
	ScopesSupported       string // JSON array
	DiscoveredAt          time.Time
}

func RegisterClient(ctx context.Context, endpoint, redirectURI, clientName, tokenAuthMethod string) (*Registration, error) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating DCR request: %w", err)
	}
	req.Header.Set("Content-Type", core.ContentTypeJSON)

	client := &http.Client{Timeout: discoveryTimeout}
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

	return &Registration{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		DiscoveredAt: time.Now(),
	}, nil
}
