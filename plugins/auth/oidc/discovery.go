package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type DiscoveryDocument struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
}

func Discover(ctx context.Context, issuerURL string, client *http.Client) (*DiscoveryDocument, error) {
	wellKnownURL := issuerURL + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create discovery request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery endpoint returned %d: %s", resp.StatusCode, body)
	}

	var doc DiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode discovery document: %w", err)
	}

	if doc.Issuer != issuerURL {
		return nil, fmt.Errorf("issuer mismatch: expected %q, got %q", issuerURL, doc.Issuer)
	}

	if doc.AuthorizationEndpoint == "" {
		return nil, fmt.Errorf("discovery document missing authorization_endpoint")
	}
	if doc.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery document missing token_endpoint")
	}
	if doc.UserinfoEndpoint == "" {
		return nil, fmt.Errorf("discovery document missing userinfo_endpoint")
	}

	return &doc, nil
}
