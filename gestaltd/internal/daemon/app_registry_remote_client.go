package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

const (
	remotePublishHTTPTimeout   = 2 * time.Minute
	remotePublishUploadTimeout = 30 * time.Minute
)

var (
	remotePublishBearerRedactor     = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	remotePublishURLRedactor        = regexp.MustCompile(`https?://\S+`)
	remotePublishGoogSignatureValue = regexp.MustCompile(`(?i)(X-Goog-Signature=)[^&\s"]+`)
	remoteSignedUploadHeaders       = []string{
		appregistry.UploadHeaderContentLength,
		appregistry.UploadHeaderXGoogIfGenerationMatch,
		appregistry.UploadHeaderXGoogMetaSHA256,
		appregistry.UploadHeaderXGoogContentSHA256,
	}
)

type remoteRegistryPublishResult struct {
	PublishID, App, Version, State, AdminURL, PublishedAt string
}

type remoteRegistryClient struct {
	BaseURL, Token string
	HTTPClient     *http.Client
}

type remoteRegistryUploadInput struct {
	Platform, LocalPath, SHA256, UploadURL string
	Headers                                map[string]string
}

type remoteRegistryUploader struct{ HTTPClient *http.Client }

func (c *remoteRegistryClient) begin(ctx context.Context, app string, declaration *appregistry.PublishDeclaration) (appregistry.AdminPublishResponse, error) {
	path := fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes", url.PathEscape(strings.TrimSpace(app)))
	return c.postDeclaration(ctx, path, declaration)
}

func (c *remoteRegistryClient) finalize(ctx context.Context, app, publishID string, declaration *appregistry.PublishDeclaration) (appregistry.AdminPublishResponse, error) {
	path := fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes/%s/finalize", url.PathEscape(strings.TrimSpace(app)), url.PathEscape(strings.TrimSpace(publishID)))
	return c.postDeclaration(ctx, path, declaration)
}

func (c *remoteRegistryClient) postDeclaration(ctx context.Context, path string, declaration *appregistry.PublishDeclaration) (appregistry.AdminPublishResponse, error) {
	body, err := json.Marshal(struct {
		Declaration *appregistry.PublishDeclaration `json:"declaration"`
	}{declaration})
	if err != nil {
		return appregistry.AdminPublishResponse{}, err
	}
	return c.doJSON(ctx, http.MethodPost, path, body)
}

func (c *remoteRegistryClient) doJSON(ctx context.Context, method, path string, body []byte) (appregistry.AdminPublishResponse, error) {
	var zero appregistry.AdminPublishResponse
	if c == nil {
		return zero, fmt.Errorf("publish client is not configured")
	}
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return zero, fmt.Errorf("gestalt URL is required; set GESTALT_URL or run `gestalt init`")
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
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: remotePublishHTTPTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("%s %s: %w", method, redactRemotePublishSecrets(path), err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("read %s response: %w", redactRemotePublishSecrets(path), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, parseRemoteRegistryAPIError(resp.StatusCode, respBody)
	}
	var parsed appregistry.AdminPublishResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return zero, fmt.Errorf("decode %s response: %w", redactRemotePublishSecrets(path), err)
	}
	return parsed, nil
}

func (u *remoteRegistryUploader) upload(ctx context.Context, input remoteRegistryUploadInput) error {
	if u == nil {
		return fmt.Errorf("upload client is not configured")
	}
	platform := strings.TrimSpace(input.Platform)
	localPath := strings.TrimSpace(input.LocalPath)
	if localPath == "" {
		return fmt.Errorf("upload local path is required")
	}
	if strings.TrimSpace(input.UploadURL) == "" {
		return fmt.Errorf("upload URL is required for platform %q", platform)
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", localPath, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", localPath, err)
	}
	if err := validateRemoteSignedUploadHeaders(platform, input.Headers, info.Size(), input.SHA256); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, input.UploadURL, file)
	if err != nil {
		return remoteUploadError(platform, err)
	}
	req.ContentLength = info.Size()
	for key, value := range input.Headers {
		req.Header.Set(key, value)
	}
	httpClient := u.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: remotePublishUploadTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return remoteUploadError(platform, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return remoteUploadHTTPError(platform, resp.StatusCode, body)
	}
	return nil
}

func validateRemoteSignedUploadHeaders(platform string, headers map[string]string, fileSize int64, sha256Hex string) error {
	if len(headers) == 0 {
		return fmt.Errorf("upload platform %q: signed upload headers are required", platform)
	}
	expected, err := appregistry.BuildSignedUploadHeaders(fileSize, sha256Hex)
	if err != nil {
		return fmt.Errorf("upload platform %q: %w", platform, err)
	}
	for _, name := range remoteSignedUploadHeaders {
		got, ok := headers[name]
		if !ok || got != expected[name] {
			return fmt.Errorf("upload platform %q: signed upload header %q mismatch", platform, name)
		}
	}
	if len(headers) != len(remoteSignedUploadHeaders) {
		return fmt.Errorf("upload platform %q: unexpected signed upload headers", platform)
	}
	return nil
}

func parseRemoteRegistryAPIError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		message = strings.TrimSpace(payload.Error)
	}
	message = redactRemotePublishSecrets(message)
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("publish API returned %d: %s", status, message)
}

func redactRemotePublishSecrets(value string) string {
	value = remotePublishBearerRedactor.ReplaceAllString(value, "Bearer [REDACTED]")
	value = remotePublishURLRedactor.ReplaceAllString(value, "[REDACTED-URL]")
	value = remotePublishGoogSignatureValue.ReplaceAllString(value, `${1}[REDACTED]`)
	for _, header := range remoteSignedUploadHeaders {
		pattern := regexp.MustCompile(`(?i)(` + regexp.QuoteMeta(header) + `=)[^&\s"]+`)
		value = pattern.ReplaceAllString(value, `${1}[REDACTED]`)
	}
	for _, prefix := range []string{"api_token", "GESTALT_API_KEY", "token=", "uploadUrl"} {
		if idx := strings.Index(strings.ToLower(value), strings.ToLower(prefix)); idx >= 0 {
			value = value[:idx] + prefix + "[REDACTED]"
		}
	}
	return value
}

func remoteUploadError(platform string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("upload platform %q: %s", platform, redactRemotePublishSecrets(err.Error()))
}

func remoteUploadHTTPError(platform string, status int, body []byte) error {
	message := redactRemotePublishSecrets(strings.TrimSpace(string(body)))
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("upload platform %q returned %d: %s", platform, status, message)
}

func remoteRegistryAdminURL(baseURL, app string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/apps/" + url.PathEscape(strings.Trim(strings.TrimSpace(app), "/")) + "/admin/registry"
}

func printRemoteRegistryPublishResult(w io.Writer, result remoteRegistryPublishResult) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "publishId: %s\napp: %s\nversion: %s\nstate: %s\n", result.PublishID, result.App, result.Version, result.State)
	if result.PublishedAt != "" {
		_, _ = fmt.Fprintf(w, "publishedAt: %s\n", result.PublishedAt)
	}
	_, _ = fmt.Fprintf(w, "adminUrl: %s\n", result.AdminURL)
}
