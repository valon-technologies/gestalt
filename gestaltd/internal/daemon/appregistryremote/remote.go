package appregistryremote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const (
	defaultHTTPTimeout   = 2 * time.Minute
	defaultUploadTimeout = 30 * time.Minute
)

var (
	fullGitSHARe            = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	bearerTokenRedactor     = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	signedUploadHeaderOrder = []string{
		appregistry.UploadHeaderContentLength,
		appregistry.UploadHeaderXGoogIfGenerationMatch,
		appregistry.UploadHeaderXGoogMetaSHA256,
		appregistry.UploadHeaderXGoogContentSHA256,
	}
)

type PublishUpload struct {
	Platform  string            `json:"platform"`
	UploadURL string            `json:"uploadUrl"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type PublishResponse struct {
	PublishID   string          `json:"publishId"`
	App         string          `json:"app"`
	Version     string          `json:"version"`
	State       string          `json:"state"`
	Uploads     []PublishUpload `json:"uploads,omitempty"`
	PublishedAt string          `json:"publishedAt,omitempty"`
}

type PublishResult struct {
	PublishID, App, Version, State, AdminURL, PublishedAt string
}

type PublishInput struct {
	Version, GestaltURL, GestaltToken, BuilderVersion string
	DistDirs                                          []string
	CommandRunner                                     commandRunner
	Client                                            *Client
	Uploader                                          *Uploader
	Output                                            io.Writer
	Logf                                              func(string, ...any)
}

type Client struct {
	BaseURL, Token string
	HTTPClient     *http.Client
}

type ArtifactUploadInput struct {
	Platform, LocalPath, SHA256, UploadURL string
	Headers                                map[string]string
}

type Uploader struct{ HTTPClient *http.Client }

type PublishHelpers struct {
	CollectReleaseArchivesFromDirs  func([]string, string) (*providermanifestv1.Manifest, string, []DaemonReleaseArchive, error)
	BuildProviderReleaseMetadata    func(*providermanifestv1.Manifest, string, []DaemonReleaseArchive, []byte) (*providerrelease.Metadata, error)
	ValidateProviderPublishManifest func(*providermanifestv1.Manifest, *providermanifestv1.Manifest, string, string) error
	ResolvePublishManifest          func(appName string) (absPath, relPath string, err error)
}

type DaemonReleaseArchive struct {
	Path, SHA256, Target string
	Size                 int64
}

type commandRunner interface {
	Run(name string, args ...string) (string, error)
}

type shellCommandRunner struct{}

func (shellCommandRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

var publishHelpers PublishHelpers

func RegisterPublishHelpers(helpers PublishHelpers) { publishHelpers = helpers }

func Publish(ctx context.Context, input PublishInput) (PublishResult, error) {
	var zero PublishResult
	version := strings.TrimSpace(input.Version)
	if version == "" {
		return zero, fmt.Errorf("--version is required")
	}
	if len(input.DistDirs) == 0 {
		return zero, fmt.Errorf("--dist-dir is required")
	}
	if publishHelpers.CollectReleaseArchivesFromDirs == nil || publishHelpers.ResolvePublishManifest == nil {
		return zero, fmt.Errorf("remote publish helpers are not registered")
	}
	releaseManifest, releaseVersion, daemonArchives, err := publishHelpers.CollectReleaseArchivesFromDirs(input.DistDirs, version)
	if err != nil {
		return zero, err
	}
	archives := releaseArchivesFromDaemon(daemonArchives)
	platforms := make(map[string]struct{}, len(archives))
	for _, archive := range archives {
		platforms[strings.TrimSpace(archive.Target)] = struct{}{}
	}
	var missing []string
	for _, platform := range defaultPublishLimits().RequiredPlatforms {
		if _, ok := platforms[strings.TrimSpace(platform)]; !ok {
			missing = append(missing, platform)
		}
	}
	if len(missing) > 0 {
		return zero, fmt.Errorf("%w: missing %s", appregistry.ErrPublishRequiredPlatform, strings.Join(missing, ", "))
	}
	appName, err := appregistry.AppNameFromManifestSource(releaseManifest.Source)
	if err != nil {
		return zero, fmt.Errorf("manifest source: %w", err)
	}
	manifestPath, relManifestPath, err := publishHelpers.ResolvePublishManifest(appName)
	if err != nil {
		return zero, err
	}
	_, sourceManifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return zero, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if err := publishHelpers.ValidateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, version); err != nil {
		return zero, err
	}
	releaseMetadata, err := publishHelpers.BuildProviderReleaseMetadata(sourceManifest, version, daemonArchives, nil)
	if err != nil {
		return zero, fmt.Errorf("build release metadata: %w", err)
	}
	runner := input.CommandRunner
	if runner == nil {
		runner = shellCommandRunner{}
	}
	declaration, err := buildPublishDeclaration(appName, version, relManifestPath, sourceManifest, releaseMetadata, archives, collectLocalSourceState(manifestPath, runner), input.BuilderVersion)
	if err != nil {
		return zero, err
	}
	baseURL := strings.TrimSpace(input.GestaltURL)
	token := strings.TrimSpace(input.GestaltToken)
	if baseURL == "" {
		baseURL, err = config.ResolveGestaltCLIURL()
		if err != nil {
			return zero, err
		}
	}
	if token == "" {
		token, err = config.ResolveGestaltCLIToken()
		if err != nil {
			return zero, err
		}
	}
	if baseURL == "" {
		return zero, fmt.Errorf("gestalt URL is required; set GESTALT_URL or run `gestalt init`")
	}
	if token == "" {
		return zero, fmt.Errorf("gestalt credentials are required; set GESTALT_API_KEY or run `gestalt auth login`")
	}
	client := input.Client
	if client == nil {
		client = &Client{BaseURL: baseURL, Token: token}
	}
	uploader := input.Uploader
	if uploader == nil {
		uploader = &Uploader{}
	}
	logf := input.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	created, err := client.Begin(ctx, appName, declaration)
	if err != nil {
		return zero, err
	}
	if created.State == appregistry.PublishStatePublished {
		result := PublishResult{
			PublishID: created.PublishID, App: created.App, Version: created.Version, State: created.State,
			AdminURL: adminRegistryURL(baseURL, created.App), PublishedAt: created.PublishedAt,
		}
		printPublishResult(input.Output, result)
		return result, nil
	}
	archiveByPlatform := make(map[string]releaseArchive, len(archives))
	for _, archive := range archives {
		archiveByPlatform[strings.TrimSpace(archive.Target)] = archive
	}
	for _, upload := range created.Uploads {
		platform := strings.TrimSpace(upload.Platform)
		archive, ok := archiveByPlatform[platform]
		if !ok {
			return zero, fmt.Errorf("local archive for platform %q is missing from --dist-dir", platform)
		}
		logf("Uploading %s (%s)", archive.Filename, platform)
		if err := uploader.Upload(ctx, ArtifactUploadInput{
			Platform: platform, LocalPath: archive.Path, SHA256: archive.SHA256, UploadURL: upload.UploadURL, Headers: upload.Headers,
		}); err != nil {
			return zero, err
		}
	}
	finalized, err := client.Finalize(ctx, appName, created.PublishID, declaration)
	if err != nil {
		return zero, err
	}
	if finalized.State != appregistry.PublishStatePublished {
		return zero, fmt.Errorf("publish finalize returned state %q", finalized.State)
	}
	result := PublishResult{
		PublishID: finalized.PublishID, App: finalized.App, Version: finalized.Version, State: finalized.State,
		AdminURL: adminRegistryURL(baseURL, finalized.App), PublishedAt: finalized.PublishedAt,
	}
	printPublishResult(input.Output, result)
	return result, nil
}

func (c *Client) Begin(ctx context.Context, app string, declaration *appregistry.PublishDeclaration) (PublishResponse, error) {
	return c.postDeclaration(ctx, fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes", url.PathEscape(strings.TrimSpace(app))), declaration)
}

func (c *Client) Finalize(ctx context.Context, app, publishID string, declaration *appregistry.PublishDeclaration) (PublishResponse, error) {
	path := fmt.Sprintf("/api/v1/apps/%s/admin/registry/publishes/%s/finalize", url.PathEscape(strings.TrimSpace(app)), url.PathEscape(strings.TrimSpace(publishID)))
	return c.postDeclaration(ctx, path, declaration)
}

func (c *Client) postDeclaration(ctx context.Context, path string, declaration *appregistry.PublishDeclaration) (PublishResponse, error) {
	body, err := json.Marshal(struct {
		Declaration *appregistry.PublishDeclaration `json:"declaration"`
	}{declaration})
	if err != nil {
		return PublishResponse{}, err
	}
	return c.doJSON(ctx, http.MethodPost, path, body)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body []byte) (PublishResponse, error) {
	var zero PublishResponse
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
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := httpClient.Do(req)
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
	var parsed PublishResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return zero, fmt.Errorf("decode %s response: %w", redactSecrets(path), err)
	}
	return parsed, nil
}

func (u *Uploader) Upload(ctx context.Context, input ArtifactUploadInput) error {
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
	if err := validateSignedUploadLeaseHeaders(platform, input.Headers, info.Size(), input.SHA256); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, input.UploadURL, file)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	for key, value := range input.Headers {
		req.Header.Set(key, value)
	}
	httpClient := u.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultUploadTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload platform %q: %w", platform, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upload platform %q returned %d: %s", platform, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func buildPublishDeclaration(appName, version, manifestPath string, source *providermanifestv1.Manifest, release *providerrelease.Metadata, archives []releaseArchive, localSource *appregistry.LocalSourceState, builderVersion string) (*appregistry.PublishDeclaration, error) {
	artifacts := make([]appregistry.PublishDeclarationArtifact, 0, len(archives))
	for _, archive := range archives {
		artifacts = append(artifacts, appregistry.PublishDeclarationArtifact{
			Platform: strings.TrimSpace(archive.Target), Filename: strings.TrimSpace(archive.Filename),
			SHA256: strings.ToLower(strings.TrimSpace(archive.SHA256)), Size: archive.Size,
		})
	}
	manifest := *source
	manifest.Version = strings.TrimSpace(version)
	declaration := &appregistry.PublishDeclaration{
		Schema: appregistry.PublishDeclarationSchemaVersion, Manifest: &manifest, ManifestPath: strings.TrimSpace(manifestPath),
		ReleaseMetadata: release, Artifacts: artifacts, PublicationKind: appregistry.PublicationKindLocal,
		LocalSource: localSource, BuilderVersion: strings.TrimSpace(builderVersion),
	}
	if err := appregistry.ValidatePublishDeclaration(appName, declaration, defaultPublishLimits()); err != nil {
		return nil, err
	}
	return declaration, nil
}

type releaseArchive struct {
	Path, SHA256, Target, Filename string
	Size                           int64
}

func releaseArchivesFromDaemon(archives []DaemonReleaseArchive) []releaseArchive {
	out := make([]releaseArchive, 0, len(archives))
	for _, archive := range archives {
		out = append(out, releaseArchive{
			Path: archive.Path, SHA256: archive.SHA256, Target: archive.Target,
			Filename: filepath.Base(archive.Path), Size: archive.Size,
		})
	}
	return out
}

func collectLocalSourceState(manifestPath string, runner commandRunner) *appregistry.LocalSourceState {
	if runner == nil {
		runner = shellCommandRunner{}
	}
	manifestDir := filepath.Dir(manifestPath)
	if _, err := runner.Run("git", "-C", manifestDir, "rev-parse", "--show-toplevel"); err != nil {
		return nil
	}
	commitOut, err := runner.Run("git", "-C", manifestDir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	commitSHA := strings.ToLower(strings.TrimSpace(commitOut))
	if !fullGitSHARe.MatchString(commitSHA) {
		return nil
	}
	state := &appregistry.LocalSourceState{CommitSHA: commitSHA}
	statusOut, err := runner.Run("git", "-C", manifestDir, "status", "--porcelain")
	if err != nil {
		return state
	}
	for _, line := range strings.Split(statusOut, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 || strings.TrimSpace(line[:2]) == "" {
			continue
		}
		if line[:2] == "??" {
			state.Untracked = true
		} else {
			state.Dirty = true
		}
	}
	return state
}

func defaultPublishLimits() appregistry.PublishLimits {
	return appregistry.PublishLimits{MaxArtifacts: 16, MaxArtifactBytes: 512 << 20, RequiredPlatforms: []string{"linux/amd64", "darwin/arm64"}}
}

func validateSignedUploadLeaseHeaders(platform string, headers map[string]string, fileSize int64, sha256Hex string) error {
	if len(headers) == 0 {
		return fmt.Errorf("upload platform %q: signed upload headers are required", platform)
	}
	expected, err := appregistry.BuildSignedUploadHeaders(fileSize, sha256Hex)
	if err != nil {
		return fmt.Errorf("upload platform %q: %w", platform, err)
	}
	for _, name := range signedUploadHeaderOrder {
		got, ok := headers[name]
		if !ok || got != expected[name] {
			return fmt.Errorf("upload platform %q: signed upload header %q mismatch", platform, name)
		}
	}
	if len(headers) != len(signedUploadHeaderOrder) {
		return fmt.Errorf("upload platform %q: unexpected signed upload headers", platform)
	}
	return nil
}

func printPublishResult(w io.Writer, result PublishResult) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "publishId: %s\napp: %s\nversion: %s\nstate: %s\n", result.PublishID, result.App, result.Version, result.State)
	if result.PublishedAt != "" {
		_, _ = fmt.Fprintf(w, "publishedAt: %s\n", result.PublishedAt)
	}
	_, _ = fmt.Fprintf(w, "adminUrl: %s\n", result.AdminURL)
}

func parseAPIError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		message = strings.TrimSpace(payload.Error)
	}
	message = redactSecrets(message)
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("publish API returned %d: %s", status, message)
}

func redactSecrets(value string) string {
	value = bearerTokenRedactor.ReplaceAllString(value, "Bearer [REDACTED]")
	for _, prefix := range []string{"api_token", "GESTALT_API_KEY", "token=", "X-Goog-Signature", "uploadUrl"} {
		if idx := strings.Index(strings.ToLower(value), strings.ToLower(prefix)); idx >= 0 {
			value = value[:idx] + prefix + "[REDACTED]"
		}
	}
	return value
}

func adminRegistryURL(baseURL, app string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/apps/" + url.PathEscape(strings.Trim(strings.TrimSpace(app), "/")) + "/admin/registry"
}
