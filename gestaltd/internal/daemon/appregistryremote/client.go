package appregistryremote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultHTTPTimeout = 2 * time.Minute

// Client calls the authenticated app-admin registry publish API.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func (c *Client) CreateSession(ctx context.Context, app string, declaration *CreateSessionRequest) (SessionResponse, error) {
	var zero SessionResponse
	if c == nil {
		return zero, fmt.Errorf("publish client is not configured")
	}
	body, err := json.Marshal(declaration)
	if err != nil {
		return zero, err
	}
	path := fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes", url.PathEscape(strings.TrimSpace(app)))
	return c.doJSON(ctx, http.MethodPost, path, body)
}

func (c *Client) GetSession(ctx context.Context, app, publishID string) (SessionResponse, error) {
	path := fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes/%s", url.PathEscape(strings.TrimSpace(app)), url.PathEscape(strings.TrimSpace(publishID)))
	return c.doJSON(ctx, http.MethodGet, path, nil)
}

func (c *Client) FinalizeSession(ctx context.Context, app, publishID string) (SessionResponse, error) {
	path := fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes/%s/finalize", url.PathEscape(strings.TrimSpace(app)), url.PathEscape(strings.TrimSpace(publishID)))
	return c.doJSON(ctx, http.MethodPost, path, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body []byte) (SessionResponse, error) {
	var zero SessionResponse
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return zero, fmt.Errorf("gestalt URL is required; set GESTALT_URL or run `gestalt auth login`")
	}
	token := strings.TrimSpace(c.Token)
	if token == "" {
		return zero, fmt.Errorf("gestalt credentials are required; set GESTALT_API_KEY or run `gestalt auth login`")
	}
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bodyReader)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return zero, fmt.Errorf("%s %s: %w", method, redactSecrets(path), err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("read %s response: %w", redactSecrets(path), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, parseAPIError(resp.StatusCode, respBody)
	}
	var parsed SessionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return zero, fmt.Errorf("decode %s response: %w", redactSecrets(path), err)
	}
	return parsed, nil
}

func parseAPIError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if trimmed := strings.TrimSpace(payload.Error); trimmed != "" {
			message = trimmed
		}
	}
	message = redactSecrets(message)
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("publish API returned %d: %s", status, message)
}

var bearerTokenRedactor = regexp.MustCompile(`(?i)Bearer\s+\S+`)

func redactSecrets(value string) string {
	value = bearerTokenRedactor.ReplaceAllString(value, "Bearer [REDACTED]")
	for _, prefix := range []string{"api_token", "GESTALT_API_KEY", "token="} {
		if idx := strings.Index(strings.ToLower(value), strings.ToLower(prefix)); idx >= 0 {
			value = value[:idx] + prefix + "[REDACTED]"
		}
	}
	return value
}

func adminRegistryURL(baseURL, app string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	app = strings.Trim(strings.TrimSpace(app), "/")
	return base + "/apps/" + url.PathEscape(app) + "/admin/registry"
}
