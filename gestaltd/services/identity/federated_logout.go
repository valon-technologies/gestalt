package identity

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

// FederatedLogoutProvider is implemented by identity providers that can build
// an upstream SSO logout URL.
type FederatedLogoutProvider interface {
	FederatedLogoutURL(returnTo string) (string, error)
}

// FederatedLogoutURL returns the upstream federated logout URL when supported.
func FederatedLogoutURL(provider core.IdentityProvider, returnTo string) (string, error) {
	if provider == nil {
		return "", errors.New("auth is not configured")
	}
	federated, ok := provider.(FederatedLogoutProvider)
	if !ok {
		return "", errors.New("federated logout is not supported")
	}
	return federated.FederatedLogoutURL(returnTo)
}

// BuildOIDCFederatedLogoutURL builds an Auth0 /v2/logout URL.
func BuildOIDCFederatedLogoutURL(issuerURL, clientID, returnTo string) (string, error) {
	returnTo = strings.TrimSpace(returnTo)
	if returnTo == "" {
		return "", fmt.Errorf("oidc auth: returnTo is required")
	}
	issuer := strings.TrimRight(strings.TrimSpace(issuerURL), "/")
	clientID = strings.TrimSpace(clientID)
	if issuer == "" || clientID == "" {
		return "", fmt.Errorf("oidc auth: federated logout is not configured")
	}
	issuerParsed, err := url.Parse(issuer)
	if err != nil || issuerParsed.Scheme == "" || issuerParsed.Host == "" {
		return "", fmt.Errorf("oidc auth: invalid issuer url")
	}
	if !strings.HasSuffix(strings.ToLower(issuerParsed.Hostname()), ".auth0.com") {
		return "", fmt.Errorf("oidc auth: federated logout is not supported for issuer")
	}
	parsed, err := url.Parse(issuer + "/v2/logout")
	if err != nil {
		return "", fmt.Errorf("oidc auth: build logout url: %w", err)
	}
	query := parsed.Query()
	query.Set("client_id", clientID)
	query.Set("returnTo", returnTo)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func oidcLogoutConfigFromMap(config map[string]any) (issuerURL, clientID string) {
	if len(config) == 0 {
		return "", ""
	}
	return configString(config, "issuerUrl"), configString(config, "clientId")
}

func configString(config map[string]any, key string) string {
	raw, ok := config[key]
	if !ok || raw == nil {
		return ""
	}
	value, _ := raw.(string)
	return strings.TrimSpace(value)
}
