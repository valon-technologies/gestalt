package appregistryremote

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// PublishInput configures a remote app registry publish.
type PublishInput struct {
	Version        string
	DistDirs       []string
	GestaltURL     string
	GestaltToken   string
	BuilderVersion string
	CommandRunner  commandRunner
	Client         *Client
	Uploader       *Uploader
	Logf           func(string, ...any)
}

// Publish validates local artifacts, creates or resumes a publish session, uploads
// missing archives, finalizes the version, and returns the publish result.
func Publish(ctx context.Context, input PublishInput) (PublishResult, error) {
	var zero PublishResult
	runner := input.CommandRunner
	if runner == nil {
		runner = shellCommandRunner{}
	}
	version := strings.TrimSpace(input.Version)
	if version == "" {
		return zero, fmt.Errorf("--version is required")
	}
	if len(input.DistDirs) == 0 {
		return zero, fmt.Errorf("--dist-dir is required")
	}

	if publishHelpers.CollectReleaseArchivesFromDirs == nil {
		return zero, fmt.Errorf("remote publish helpers are not registered")
	}
	releaseManifest, releaseVersion, daemonArchives, err := publishHelpers.CollectReleaseArchivesFromDirs(input.DistDirs, version)
	if err != nil {
		return zero, err
	}
	archives := releaseArchivesFromDaemon(daemonArchives)
	if err := validateRequiredPlatforms(platformSet(archives), appregistry.DefaultPublishSessionLimits().RequiredPlatforms); err != nil {
		return zero, err
	}

	appName, err := appregistry.AppNameFromManifestSource(releaseManifest.Source)
	if err != nil {
		return zero, fmt.Errorf("manifest source: %w", err)
	}
	manifestPath, relManifestPath, err := resolvePublishManifest(appName, runner)
	if err != nil {
		return zero, err
	}
	sourceManifest, err := readSourceManifest(manifestPath)
	if err != nil {
		return zero, err
	}
	if publishHelpers.ValidateProviderPublishManifest == nil || publishHelpers.BuildProviderReleaseMetadata == nil {
		return zero, fmt.Errorf("remote publish helpers are not registered")
	}
	if err := publishHelpers.ValidateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, version); err != nil {
		return zero, err
	}

	releaseMetadata, err := publishHelpers.BuildProviderReleaseMetadata(sourceManifest, version, daemonArchives, nil)
	if err != nil {
		return zero, fmt.Errorf("build release metadata: %w", err)
	}
	declaration, err := buildPublishDeclaration(buildDeclarationInput{
		AppName:         appName,
		Version:         version,
		ManifestPath:    relManifestPath,
		SourceManifest:  sourceManifest,
		ReleaseMetadata: releaseMetadata,
		Archives:        archives,
		LocalSource:     collectLocalSourceState(manifestPath, runner),
		BuilderVersion:  input.BuilderVersion,
	})
	if err != nil {
		return zero, err
	}

	baseURL := strings.TrimSpace(input.GestaltURL)
	if baseURL == "" {
		baseURL, err = config.ResolveGestaltCLIURL()
		if err != nil {
			return zero, err
		}
	}
	token := strings.TrimSpace(input.GestaltToken)
	if token == "" {
		token, err = config.ResolveGestaltCLIToken()
		if err != nil {
			return zero, err
		}
	}
	if strings.TrimSpace(baseURL) == "" {
		return zero, fmt.Errorf("Gestalt URL is required; set GESTALT_URL or run `gestalt init`")
	}
	if strings.TrimSpace(token) == "" {
		return zero, fmt.Errorf("Gestalt credentials are required; set GESTALT_API_KEY or run `gestalt auth login`")
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

	created, err := client.CreateSession(ctx, appName, &CreateSessionRequest{Declaration: declaration})
	if err != nil {
		return zero, err
	}
	if created.Renewed {
		logf("Renewed expired upload leases for publish session %s", created.PublishID)
	}
	if created.State == sessionStateFailed {
		return zero, sessionFailureError(created)
	}
	if created.State == sessionStatePublished {
		result := publishResultFromSession(baseURL, created)
		printPublishResult(os.Stdout, result)
		return result, nil
	}

	status, err := client.GetSession(ctx, appName, created.PublishID)
	if err != nil {
		return zero, err
	}
	if err := uploadMissingArtifacts(ctx, uploader, archives, status, logf); err != nil {
		return zero, err
	}

	finalized, err := client.FinalizeSession(ctx, appName, created.PublishID)
	if err != nil {
		return zero, err
	}
	if finalized.State == sessionStateFailed {
		return zero, sessionFailureError(finalized)
	}
	result := publishResultFromSession(baseURL, finalized)
	printPublishResult(os.Stdout, result)
	return result, nil
}

func uploadMissingArtifacts(ctx context.Context, uploader *Uploader, archives []releaseArchive, status SessionResponse, logf func(string, ...any)) error {
	if status.State == sessionStatePublished {
		return nil
	}
	if len(status.MismatchedUploads) > 0 {
		return fmt.Errorf("%w: mismatched uploads for %s", appregistry.ErrPublishUploadMismatch, strings.Join(status.MismatchedUploads, ", "))
	}
	if len(status.MissingUploads) == 0 {
		return nil
	}
	missing := make(map[string]struct{}, len(status.MissingUploads))
	for _, platform := range status.MissingUploads {
		missing[strings.TrimSpace(platform)] = struct{}{}
	}
	uploads := status.uploadByPlatform()
	archiveByPlatform := make(map[string]releaseArchive, len(archives))
	for _, archive := range archives {
		archiveByPlatform[strings.TrimSpace(archive.Target)] = archive
	}
	for platform := range missing {
		archive, ok := archiveByPlatform[platform]
		if !ok {
			return fmt.Errorf("local archive for platform %q is missing from --dist-dir", platform)
		}
		upload, ok := uploads[platform]
		if !ok {
			return fmt.Errorf("publish session did not return an upload lease for platform %q", platform)
		}
		logf("Uploading %s (%s)", archive.Filename, platform)
		if err := uploader.Upload(ctx, ArtifactUploadInput{
			Platform:  platform,
			LocalPath: archive.Path,
			SHA256:    archive.SHA256,
			UploadURL: upload.UploadURL,
		}); err != nil {
			return err
		}
	}
	return nil
}

func publishResultFromSession(baseURL string, session SessionResponse) PublishResult {
	return PublishResult{
		PublishID:   session.PublishID,
		App:         session.App,
		Version:     session.Version,
		State:       session.State,
		AdminURL:    adminRegistryURL(baseURL, session.App),
		Renewed:     session.Renewed,
		PublishedAt: session.PublishedAt,
	}
}

func sessionFailureError(session SessionResponse) error {
	reason := strings.TrimSpace(session.FailureReason)
	if reason == "" {
		reason = "publish session failed"
	}
	return fmt.Errorf("%w: %s", appregistry.ErrPublishSessionFailed, reason)
}

func printPublishResult(w interface{ Write([]byte) (int, error) }, result PublishResult) {
	_, _ = fmt.Fprintf(w, "publishId: %s\n", result.PublishID)
	_, _ = fmt.Fprintf(w, "app: %s\n", result.App)
	_, _ = fmt.Fprintf(w, "version: %s\n", result.Version)
	_, _ = fmt.Fprintf(w, "state: %s\n", result.State)
	if result.PublishedAt != "" {
		_, _ = fmt.Fprintf(w, "publishedAt: %s\n", result.PublishedAt)
	}
	_, _ = fmt.Fprintf(w, "adminUrl: %s\n", result.AdminURL)
}

func readSourceManifest(path string) (*providermanifestv1.Manifest, error) {
	_, manifest, err := providerpkg.ReadSourceManifestFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return manifest, nil
}

// PublishHelpers supplies daemon-local archive and manifest helpers without an import cycle.
type PublishHelpers struct {
	CollectReleaseArchivesFromDirs  func([]string, string) (*providermanifestv1.Manifest, string, []DaemonReleaseArchive, error)
	BuildProviderReleaseMetadata    func(*providermanifestv1.Manifest, string, []DaemonReleaseArchive, []byte) (*providerrelease.Metadata, error)
	ValidateProviderPublishManifest func(*providermanifestv1.Manifest, *providermanifestv1.Manifest, string, string) error
}

var publishHelpers PublishHelpers

func RegisterPublishHelpers(helpers PublishHelpers) {
	publishHelpers = helpers
}
