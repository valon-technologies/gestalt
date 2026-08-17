package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type remoteGitRunner interface {
	Run(name string, args ...string) (string, error)
}

type remoteRegistryPublisher struct {
	Version, GestaltURL, GestaltToken, BuilderVersion string
	DistDirs                                          []string
	Client                                            *remoteRegistryClient
	Uploader                                          *remoteRegistryUploader
	GitRunner                                         remoteGitRunner
	Output                                            io.Writer
	Logf                                              func(string, ...any)
	collectArchives                                   func([]string, string) (*providermanifestv1.Manifest, string, []releaseArchive, error)
	resolveManifest                                   func(string) (absPath, relPath string, err error)
	buildReleaseMetadata                              func(*providermanifestv1.Manifest, string, []releaseArchive, []byte) (*providerrelease.Metadata, error)
}

func (p *remoteRegistryPublisher) publish(ctx context.Context) (remoteRegistryPublishResult, error) {
	var zero remoteRegistryPublishResult
	version := strings.TrimSpace(p.Version)
	if version == "" {
		return zero, fmt.Errorf("--version is required")
	}
	if len(p.DistDirs) == 0 {
		return zero, fmt.Errorf("--dist-dir is required")
	}
	collect := p.collectArchives
	if collect == nil {
		collect = collectReleaseArchivesFromDirsWithProgress
	}
	releaseManifest, releaseVersion, archives, err := collect(p.DistDirs, version)
	if err != nil {
		return zero, err
	}
	if err := validateRemoteRequiredPlatforms(archives); err != nil {
		return zero, err
	}
	appName, err := appregistry.AppNameFromManifestSource(releaseManifest.Source)
	if err != nil {
		return zero, fmt.Errorf("manifest source: %w", err)
	}
	resolve := p.resolveManifest
	if resolve == nil {
		resolve = resolveRemotePublishManifest
	}
	manifestPath, relManifestPath, err := resolve(appName)
	if err != nil {
		return zero, err
	}
	_, sourceManifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return zero, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	if err := validateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, version); err != nil {
		return zero, err
	}
	buildMeta := p.buildReleaseMetadata
	if buildMeta == nil {
		buildMeta = buildProviderReleaseMetadata
	}
	releaseMetadata, err := buildMeta(releaseManifest, releaseVersion, archives, nil)
	if err != nil {
		return zero, fmt.Errorf("build release metadata: %w", err)
	}
	runner := p.GitRunner
	if runner == nil {
		runner = appRegistryCommandRunner{}
	}
	localSource, err := collectRemoteLocalSourceState(manifestPath, runner)
	if err != nil {
		return zero, err
	}
	declaration, err := buildRemotePublishDeclaration(appName, version, relManifestPath, sourceManifest, releaseMetadata, archives, localSource, p.BuilderVersion)
	if err != nil {
		return zero, err
	}
	baseURL := strings.TrimSpace(p.GestaltURL)
	token := strings.TrimSpace(p.GestaltToken)
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
	client := p.Client
	if client == nil {
		client = &remoteRegistryClient{BaseURL: baseURL, Token: token}
	}
	uploader := p.Uploader
	if uploader == nil {
		uploader = &remoteRegistryUploader{}
	}
	logf := p.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	created, err := client.begin(ctx, appName, declaration)
	if err != nil {
		return zero, err
	}
	if created.State == appregistry.PublishStatePublished {
		result := remoteRegistryPublishResult{
			PublishID: created.PublishID, App: created.App, Version: created.Version, State: created.State,
			AdminURL: remoteRegistryAdminURL(baseURL, created.App), PublishedAt: created.PublishedAt,
		}
		printRemoteRegistryPublishResult(p.Output, result)
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
		logf("Uploading %s (%s)", filepath.Base(archive.Path), platform)
		if err := uploader.upload(ctx, remoteRegistryUploadInput{
			Platform: platform, LocalPath: archive.Path, SHA256: archive.SHA256, UploadURL: upload.UploadURL, Headers: upload.Headers,
		}); err != nil {
			return zero, err
		}
	}
	finalized, err := client.finalize(ctx, appName, created.PublishID, declaration)
	if err != nil {
		return zero, err
	}
	if finalized.State != appregistry.PublishStatePublished {
		return zero, fmt.Errorf("publish finalize returned state %q", finalized.State)
	}
	result := remoteRegistryPublishResult{
		PublishID: finalized.PublishID, App: finalized.App, Version: finalized.Version, State: finalized.State,
		AdminURL: remoteRegistryAdminURL(baseURL, finalized.App), PublishedAt: finalized.PublishedAt,
	}
	printRemoteRegistryPublishResult(p.Output, result)
	return result, nil
}

func resolveRemotePublishManifest(appName string) (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve working directory: %w", err)
	}
	rootOut, err := runProviderPublishCommand("git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		if isGitNotRepository(err) {
			rootOut = cwd
		} else {
			return "", "", fmt.Errorf("resolve git root from %s: %w", cwd, err)
		}
	} else {
		rootOut = normalizeGitRoot(strings.TrimSpace(rootOut))
	}
	path, err := resolveAppPublishManifestFromGitRoot(rootOut, appName)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(rootOut, path)
	if err != nil {
		return "", "", err
	}
	return path, filepath.ToSlash(rel), nil
}

func normalizeGitRoot(gitRoot string) string {
	if absGitRoot, err := filepath.Abs(gitRoot); err == nil {
		gitRoot = absGitRoot
	}
	if evaluatedGitRoot, err := filepath.EvalSymlinks(gitRoot); err == nil {
		gitRoot = evaluatedGitRoot
	}
	return gitRoot
}

func buildRemotePublishDeclaration(appName, version, manifestPath string, source *providermanifestv1.Manifest, release *providerrelease.Metadata, archives []releaseArchive, localSource *appregistry.LocalSourceState, builderVersion string) (*appregistry.PublishDeclaration, error) {
	artifacts := make([]appregistry.PublishDeclarationArtifact, 0, len(archives))
	for _, archive := range archives {
		info, err := os.Stat(archive.Path)
		if err != nil {
			return nil, fmt.Errorf("stat archive %s: %w", archive.Path, err)
		}
		artifacts = append(artifacts, appregistry.PublishDeclarationArtifact{
			Platform: strings.TrimSpace(archive.Target), Filename: filepath.Base(archive.Path),
			SHA256: strings.ToLower(strings.TrimSpace(archive.SHA256)), Size: info.Size(),
		})
	}
	manifest := *source
	manifest.Version = strings.TrimSpace(version)
	declaration := &appregistry.PublishDeclaration{
		Schema: appregistry.PublishDeclarationSchemaVersion, Manifest: &manifest, ManifestPath: strings.TrimSpace(manifestPath),
		ReleaseMetadata: release, Artifacts: artifacts, PublicationKind: appregistry.PublicationKindLocal,
		LocalSource: localSource, BuilderVersion: strings.TrimSpace(builderVersion),
	}
	if err := appregistry.ValidatePublishDeclaration(appName, declaration, remoteRegistryPublishLimits); err != nil {
		return nil, err
	}
	return declaration, nil
}

func collectRemoteLocalSourceState(manifestPath string, runner remoteGitRunner) (*appregistry.LocalSourceState, error) {
	if runner == nil {
		runner = appRegistryCommandRunner{}
	}
	manifestDir := filepath.Dir(manifestPath)
	if _, err := runner.Run("git", "-C", manifestDir, "rev-parse", "--show-toplevel"); err != nil {
		if isGitNotRepository(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	commitOut, err := runner.Run("git", "-C", manifestDir, "rev-parse", "HEAD")
	if err != nil {
		if isGitNotRepository(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	commitSHA := strings.ToLower(strings.TrimSpace(commitOut))
	if !fullGitSHARe.MatchString(commitSHA) {
		return nil, fmt.Errorf("git rev-parse HEAD returned invalid commit SHA %q", commitSHA)
	}
	state := &appregistry.LocalSourceState{CommitSHA: commitSHA}
	statusOut, err := runner.Run("git", "-C", manifestDir, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status --porcelain: %w", err)
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
	return state, nil
}

func validateRemoteRequiredPlatforms(archives []releaseArchive) error {
	platforms := make(map[string]struct{}, len(archives))
	for _, archive := range archives {
		platforms[strings.TrimSpace(archive.Target)] = struct{}{}
	}
	var missing []string
	for _, platform := range remoteRegistryPublishLimits.RequiredPlatforms {
		if _, ok := platforms[strings.TrimSpace(platform)]; !ok {
			missing = append(missing, platform)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: missing %s", appregistry.ErrPublishRequiredPlatform, strings.Join(missing, ", "))
}

var remoteRegistryPublishLimits = appregistry.PublishLimits{MaxArtifacts: 16, MaxArtifactBytes: 512 << 20, RequiredPlatforms: []string{"linux/amd64", "darwin/arm64"}}
