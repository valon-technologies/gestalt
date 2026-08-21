package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const remoteRegistryUploadParallelism = 4

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
	collect := p.collectArchives
	if collect == nil {
		collect = collectReleaseArchivesFromDirsWithProgress
	}
	resolve := p.resolveManifest
	if resolve == nil {
		resolve = resolveRemotePublishManifest
	}
	prepared, err := prepareAppPublishRelease(prepareAppPublishReleaseInput{
		VersionGuard:         p.Version,
		DistDirs:             p.DistDirs,
		CollectArchives:      collect,
		ResolveManifest:      resolve,
		BuildReleaseMetadata: p.buildReleaseMetadata,
	})
	if err != nil {
		return zero, err
	}
	runner := p.GitRunner
	if runner == nil {
		runner = appRegistryCommandRunner{}
	}
	localSource, err := collectRemoteLocalSourceState(prepared.ManifestPath, runner)
	if err != nil {
		return zero, err
	}
	warnRemotePublishProvenance(os.Stderr, localSource)
	declaration, err := buildRemotePublishDeclaration(
		prepared.AppName, prepared.VersionGuard, prepared.RelativeManifestPath,
		prepared.SourceManifest, prepared.ReleaseMetadata, prepared.Archives, localSource, p.BuilderVersion,
	)
	if err != nil {
		return zero, err
	}
	appName := prepared.AppName
	baseURL := strings.TrimSpace(p.GestaltURL)
	token := strings.TrimSpace(p.GestaltToken)
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
	archiveByPlatform := make(map[string]releaseArchive, len(prepared.Archives))
	for _, archive := range prepared.Archives {
		archiveByPlatform[strings.TrimSpace(archive.Target)] = archive
	}
	uploadInputs := make([]remoteRegistryUploadInput, 0, len(created.Uploads))
	for _, upload := range created.Uploads {
		platform := strings.TrimSpace(upload.Platform)
		archive, ok := archiveByPlatform[platform]
		if !ok {
			return zero, fmt.Errorf("local archive for platform %q is missing from --dist-dir", platform)
		}
		uploadInputs = append(uploadInputs, remoteRegistryUploadInput{
			Platform: platform, LocalPath: archive.Path, SHA256: archive.SHA256, UploadURL: upload.UploadURL, Headers: upload.Headers,
		})
	}
	if err := uploadRemoteRegistryArtifacts(ctx, uploader, uploadInputs, logf); err != nil {
		return zero, err
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

func uploadRemoteRegistryArtifacts(ctx context.Context, uploader *remoteRegistryUploader, inputs []remoteRegistryUploadInput, logf func(string, ...any)) error {
	if len(inputs) == 0 {
		return nil
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	workerCount := min(remoteRegistryUploadParallelism, len(inputs))
	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan remoteRegistryUploadInput, len(inputs))
	for _, input := range inputs {
		jobs <- input
	}
	close(jobs)

	var (
		firstErr     error
		firstErrOnce sync.Once
		logMu        sync.Mutex
		workers      sync.WaitGroup
	)
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-uploadCtx.Done():
					return
				case input, ok := <-jobs:
					if !ok {
						return
					}
					// Both cases can be ready after cancellation, so check again
					// before opening a file or starting another request.
					if uploadCtx.Err() != nil {
						return
					}
					logMu.Lock()
					logf("Uploading %s (%s)", filepath.Base(input.LocalPath), input.Platform)
					logMu.Unlock()
					if err := uploader.upload(uploadCtx, input); err != nil {
						firstErrOnce.Do(func() {
							firstErr = err
							cancel()
						})
						return
					}
				}
			}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return firstErr
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
	if err := appregistry.ValidatePublishDeclaration(appName, declaration, appregistry.DefaultPublishLimits()); err != nil {
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

func warnRemotePublishProvenance(w io.Writer, localSource *appregistry.LocalSourceState) {
	if w == nil {
		return
	}
	switch {
	case localSource == nil:
		_, _ = fmt.Fprintln(w, "warning: remote publish has no git provenance; version metadata will omit commitSha")
	case localSource.Dirty:
		_, _ = fmt.Fprintf(w, "warning: remote publish is from a dirty checkout at %s\n", localSource.CommitSHA)
	case localSource.Untracked:
		_, _ = fmt.Fprintf(w, "warning: remote publish has untracked files at commit %s\n", localSource.CommitSHA)
	}
}
