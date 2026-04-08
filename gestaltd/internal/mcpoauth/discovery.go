package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

const discoveryTimeout = 10 * time.Second

type DiscoveredMetadata struct {
	AuthServerURL                 string
	AuthorizationEndpoint         string
	TokenEndpoint                 string
	RegistrationEndpoint          string
	ScopesSupported               []string
	CodeChallengeMethodsSupported []string
	TokenEndpointAuthMethods      []string
}

// SupportsPKCE returns true if S256 is in the advertised code challenge methods,
// or if no methods are advertised (MCP OAuth 2.1 mandates S256).
func (m *DiscoveredMetadata) SupportsPKCE() bool {
	if len(m.CodeChallengeMethodsSupported) == 0 {
		return true
	}
	for _, method := range m.CodeChallengeMethodsSupported {
		if method == "S256" {
			return true
		}
	}
	return false
}

// PreferredAuthMethod selects the best client auth method from the advertised
// token endpoint auth methods. Prefers "none" (public client), then
// "client_secret_post", then "client_secret_basic".
func (m *DiscoveredMetadata) PreferredAuthMethod() string {
	if len(m.TokenEndpointAuthMethods) == 0 {
		return "none"
	}
	methods := make(map[string]bool, len(m.TokenEndpointAuthMethods))
	for _, method := range m.TokenEndpointAuthMethods {
		methods[method] = true
	}
	if methods["none"] {
		return "none"
	}
	if methods["client_secret_post"] {
		return "client_secret_post"
	}
	if methods["client_secret_basic"] {
		return "client_secret_basic"
	}
	return m.TokenEndpointAuthMethods[0]
}

// Discover probes an MCP server to find its OAuth endpoints. Handles both
// RFC 9728 indirection (authorization_servers) and servers that embed
// endpoints directly in resource metadata (ClickHouse).
func Discover(ctx context.Context, mcpURL string) (*DiscoveredMetadata, error) {
	client := &http.Client{Timeout: discoveryTimeout}

	resourceMetadataURL, err := probeForResourceMetadata(ctx, client, mcpURL)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth discovery: probing %s: %w", mcpURL, err)
	}

	resourceMeta, err := fetchJSON(ctx, client, resourceMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth discovery: fetching resource metadata from %s: %w", resourceMetadataURL, err)
	}

	return resolveMetadata(ctx, client, resourceMeta)
}

// probeForResourceMetadata sends an unauthenticated request to the MCP URL and
// looks for a resource_metadata URL in the WWW-Authenticate header. Falls back
// to constructing the .well-known URL from the MCP URL's origin.
func probeForResourceMetadata(ctx context.Context, client *http.Client, mcpURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		return "", fmt.Errorf("creating probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending probe request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		if metaURL := parseResourceMetadataFromWWWAuth(resp.Header.Get("WWW-Authenticate")); metaURL != "" {
			return metaURL, nil
		}
	}

	return wellKnownResourceMetadataURL(mcpURL)
}

func parseResourceMetadataFromWWWAuth(header string) string {
	if header == "" {
		return ""
	}
	header = strings.TrimPrefix(header, core.BearerScheme)
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "resource_metadata=") {
			val := strings.TrimPrefix(part, "resource_metadata=")
			val = strings.Trim(val, `"`)
			return val
		}
	}
	return ""
}

func wellKnownResourceMetadataURL(mcpURL string) (string, error) {
	u, err := url.Parse(mcpURL)
	if err != nil {
		return "", fmt.Errorf("parsing MCP URL: %w", err)
	}
	// RFC 9728 §3: /.well-known/oauth-protected-resource/<path>
	wellKnown := &url.URL{
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   "/.well-known/oauth-protected-resource" + u.Path,
	}
	return wellKnown.String(), nil
}

func resolveMetadata(ctx context.Context, client *http.Client, resourceMeta map[string]any) (*DiscoveredMetadata, error) {
	md := &DiscoveredMetadata{}

	// Try RFC 9728 indirection: authorization_servers -> AS metadata
	if servers, ok := resourceMeta["authorization_servers"].([]any); ok && len(servers) > 0 {
		serverURL, _ := servers[0].(string)
		if serverURL != "" {
			md.AuthServerURL = serverURL
			asMeta, err := fetchASMetadata(ctx, client, serverURL)
			if err != nil {
				return nil, fmt.Errorf("fetching AS metadata from %s: %w", serverURL, err)
			}
			populateFromASMetadata(md, asMeta)
		}
	}

	// Fallback: resource metadata may embed endpoints directly when
	// authorization_servers is absent.
	if md.AuthorizationEndpoint == "" {
		if v, ok := resourceMeta["authorization_endpoint"].(string); ok {
			md.AuthorizationEndpoint = v
		}
	}
	if md.TokenEndpoint == "" {
		if v, ok := resourceMeta["token_endpoint"].(string); ok {
			md.TokenEndpoint = v
		}
	}
	if md.RegistrationEndpoint == "" {
		if v, ok := resourceMeta["registration_endpoint"].(string); ok {
			md.RegistrationEndpoint = v
		}
	}
	if len(md.ScopesSupported) == 0 {
		md.ScopesSupported = stringSlice(resourceMeta, "scopes_supported")
	}
	if len(md.CodeChallengeMethodsSupported) == 0 {
		md.CodeChallengeMethodsSupported = stringSlice(resourceMeta, "code_challenge_methods_supported")
	}
	if len(md.TokenEndpointAuthMethods) == 0 {
		md.TokenEndpointAuthMethods = stringSlice(resourceMeta, "token_endpoint_auth_methods_supported")
	}

	if md.AuthorizationEndpoint == "" || md.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery did not find authorization_endpoint and token_endpoint")
	}

	// Derive AuthServerURL from the authorization endpoint origin when not
	// set via the authorization_servers field.
	if md.AuthServerURL == "" {
		if u, err := url.Parse(md.AuthorizationEndpoint); err == nil {
			md.AuthServerURL = u.Scheme + "://" + u.Host
		}
	}

	return md, nil
}

func fetchASMetadata(ctx context.Context, client *http.Client, serverURL string) (map[string]any, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parsing AS URL: %w", err)
	}
	// RFC 8414 §3: /.well-known/oauth-authorization-server/<path>
	wellKnown := &url.URL{
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   "/.well-known/oauth-authorization-server" + u.Path,
	}
	return fetchJSON(ctx, client, wellKnown.String())
}

func populateFromASMetadata(md *DiscoveredMetadata, asMeta map[string]any) {
	if v, ok := asMeta["authorization_endpoint"].(string); ok {
		md.AuthorizationEndpoint = v
	}
	if v, ok := asMeta["token_endpoint"].(string); ok {
		md.TokenEndpoint = v
	}
	if v, ok := asMeta["registration_endpoint"].(string); ok {
		md.RegistrationEndpoint = v
	}
	md.ScopesSupported = stringSlice(asMeta, "scopes_supported")
	md.CodeChallengeMethodsSupported = stringSlice(asMeta, "code_challenge_methods_supported")
	md.TokenEndpointAuthMethods = stringSlice(asMeta, "token_endpoint_auth_methods_supported")
}

func fetchJSON(ctx context.Context, client *http.Client, rawURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", rawURL, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d: %s", rawURL, resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding JSON from %s: %w", rawURL, err)
	}
	return result, nil
}

func stringSlice(m map[string]any, key string) []string {
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
