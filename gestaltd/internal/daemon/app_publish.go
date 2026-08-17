package daemon

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type appPublishDistDirs []string

func (d *appPublishDistDirs) String() string {
	return strings.Join(*d, ",")
}

func (d *appPublishDistDirs) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("--dist-dir requires a value")
	}
	*d = append(*d, value)
	return nil
}

func runAppPublish(args []string) error {
	return runAppPublishCommandWithArgs("gestaltd app publish", printAppPublishUsage, args)
}

type appPublishCommandOptions struct {
	CommandName      string
	Usage            func(io.Writer)
	Bucket           string
	AppName          string
	Version          string
	Ref              string
	WorkflowRunURL   string
	TriggerPRNumber  int
	TriggerPRURL     string
	TriggerPRTitle   string
	TriggerCommitSHA string
	TriggerCommitURL string
	DryRun           bool
	DistDirs         appPublishDistDirs
}

func runAppPublishCommandWithArgs(commandName string, usage func(io.Writer), args []string) error {
	fs := flag.NewFlagSet(commandName, flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	bucket := fs.String("bucket", "", "GCS bucket name for registry uploads")
	appName := fs.String("app", "", "app name under apps/{app}/manifest.yaml")
	version := fs.String("version", "", "semantic version guard")
	ref := fs.String("ref", "", "full source commit SHA")
	workflowRunURL := fs.String("workflow-run-url", "", "GitHub Actions workflow run URL")
	triggerPRNumber := fs.Int("trigger-pr-number", 0, "pull request number that triggered publication")
	triggerPRURL := fs.String("trigger-pr-url", "", "pull request URL that triggered publication")
	triggerPRTitle := fs.String("trigger-pr-title", "", "pull request title that triggered publication")
	triggerCommitSHA := fs.String("trigger-commit-sha", "", "commit SHA that triggered publication")
	triggerCommitURL := fs.String("trigger-commit-url", "", "commit URL that triggered publication")
	dryRun := fs.Bool("dry-run", false, "print the publish plan as JSON without uploading")
	var distDirs appPublishDistDirs
	fs.Var(&distDirs, "dist-dir", "directory containing release archives (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return runAppPublishCommand(appPublishCommandOptions{
		CommandName:      commandName,
		Bucket:           *bucket,
		AppName:          *appName,
		Version:          *version,
		Ref:              *ref,
		WorkflowRunURL:   *workflowRunURL,
		TriggerPRNumber:  *triggerPRNumber,
		TriggerPRURL:     *triggerPRURL,
		TriggerPRTitle:   *triggerPRTitle,
		TriggerCommitSHA: *triggerCommitSHA,
		TriggerCommitURL: *triggerCommitURL,
		DryRun:           *dryRun,
		DistDirs:         distDirs,
	})
}

func runAppPublishCommand(opts appPublishCommandOptions) error {
	if strings.TrimSpace(opts.Bucket) == "" {
		return fmt.Errorf("--bucket is required")
	}
	if strings.TrimSpace(opts.AppName) == "" {
		return fmt.Errorf("--app is required")
	}
	if err := providerregistry.ValidateRepositoryName(opts.AppName); err != nil {
		return fmt.Errorf("--app: %w", err)
	}
	if len(opts.DistDirs) == 0 {
		return fmt.Errorf("--dist-dir is required")
	}
	if strings.TrimSpace(opts.Version) == "" {
		return fmt.Errorf("--version is required")
	}
	sourceRef := strings.ToLower(strings.TrimSpace(opts.Ref))
	if !fullGitSHARe.MatchString(sourceRef) {
		return fmt.Errorf("--ref must be a 40-character commit SHA")
	}
	publication, err := appPublishPublication(
		opts.WorkflowRunURL,
		opts.TriggerPRNumber,
		opts.TriggerPRURL,
		opts.TriggerPRTitle,
		opts.TriggerCommitSHA,
		opts.TriggerCommitURL,
	)
	if err != nil {
		return err
	}

	registry, err := config.NewGCSAppRegistry(opts.Bucket)
	if err != nil {
		return fmt.Errorf("--bucket: %w", err)
	}
	manifestPath, err := resolveAppPublishManifest(opts.AppName)
	if err != nil {
		return err
	}
	_, sourceManifest, err := readAppPublishManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	manifestApp, err := appregistry.AppNameFromManifestSource(sourceManifest.Source)
	if err != nil {
		return fmt.Errorf("%s: invalid manifest source: %w", manifestPath, err)
	}
	if manifestApp != strings.TrimSpace(opts.AppName) {
		return fmt.Errorf("%s: manifest source app %q does not match --app %q; update manifest source or pass the matching --app name", manifestPath, manifestApp, strings.TrimSpace(opts.AppName))
	}
	releaseManifest, releaseVersion, releaseArchives, err := collectReleaseArchivesFromDirsWithProgress([]string(opts.DistDirs), opts.Version)
	if err != nil {
		return err
	}
	if err := validateProviderPublishManifest(sourceManifest, releaseManifest, releaseVersion, opts.Version); err != nil {
		return err
	}
	if err := appregistry.ValidatePublishInputWithOptions(sourceManifest, opts.Version, sourceRef, appregistry.PublishValidationOptions{
		PublicationKind: appregistry.PublicationKindGitHub,
	}); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "gestalt-app-publish-*")
	if err != nil {
		return fmt.Errorf("create publish temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := writeProviderReleaseMetadata(tmpDir, releaseManifest, releaseVersion, releaseArchives, nil, false); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}

	releaseMetadataBytes, err := readProviderReleaseMetadata(filepath.Join(tmpDir, providerrelease.MetadataFile))
	if err != nil {
		return err
	}
	releaseMetadata, err := providerrelease.Decode(releaseMetadataBytes)
	if err != nil {
		return fmt.Errorf("decode provider release metadata: %w", err)
	}

	sourceInfo, err := providerPublishSourceRef(manifestPath, sourceRef)
	if err != nil {
		return err
	}

	storageRoot, err := registry.StorageURL()
	if err != nil {
		return err
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return err
	}

	writer := newAppRegistryWriter()
	publishStartedAt := appregistry.LoadPublishStartedAt(writer.Store, storageRoot, strings.TrimSpace(opts.AppName), strings.TrimSpace(opts.Version))

	plan, err := buildAppPublishManifest(buildAppPublishManifestInput{
		StorageRoot:  storageRoot,
		PublicRoot:   publicRoot,
		DisplayName:  sourceManifest.DisplayName,
		Description:  sourceManifest.Description,
		Version:      opts.Version,
		SourceRef:    sourceRef,
		ManifestPath: sourceInfo.ManifestPath,
		Publication:  publication,
		Manifest:     sourceManifest,
		Release:      releaseMetadata,
		Archives:     releaseArchives,
		StartedAt:    publishStartedAt,
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(plan.EntryObject.LocalPath) }()

	if opts.DryRun {
		return printAppPublishPlanJSON(plan)
	}
	progress := newAppPublishProgressReporter()
	req := appregistry.PublishRequest{Manifest: plan, SourceRef: sourceRef}
	if err := writer.Preflight(req, progress.progress()); err != nil {
		return err
	}
	return writer.Publish(req, progress.progress())
}

type buildAppPublishManifestInput struct {
	StorageRoot  string
	PublicRoot   string
	DisplayName  string
	Description  string
	Version      string
	SourceRef    string
	ManifestPath string
	Publication  *appregistry.Publication
	Manifest     *providermanifestv1.Manifest
	Release      *providerrelease.Metadata
	Archives     []releaseArchive
	StartedAt    time.Time
}

func buildAppPublishManifest(input buildAppPublishManifestInput) (appregistry.PublishManifest, error) {
	localArtifacts := make([]appregistry.LocalPublishArtifact, 0, len(input.Archives))
	for _, archive := range input.Archives {
		localArtifacts = append(localArtifacts, appregistry.LocalPublishArtifact{
			Target:    archive.Target,
			LocalPath: archive.Path,
		})
	}
	var hashProgress *commandProgress
	return appregistry.BuildPublishManifest(appregistry.BuildPublishManifestInput{
		StorageRoot: input.StorageRoot,
		PublicRoot:  input.PublicRoot,
		DisplayName: input.DisplayName,
		Description: input.Description,
		EntryInput: appregistry.BuildEntryInput{
			Manifest:         input.Manifest,
			Version:          input.Version,
			SourceRef:        input.SourceRef,
			ManifestPath:     input.ManifestPath,
			Publication:      input.Publication,
			PublicationKind:  appregistry.PublicationKindGitHub,
			Release:          input.Release,
			PublishStartedAt: input.StartedAt,
		},
		LocalArtifacts: localArtifacts,
		OnHashStart: func(fileCount int) {
			hashProgress = startCommandProgress("Hashing %d app publish files", fileCount)
		},
		OnHashDone: func(fileCount int) {
			if hashProgress != nil {
				hashProgress.done("Hashed %d app publish files", fileCount)
			}
		},
	})
}

func readProviderReleaseMetadata(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func readAppPublishManifest(manifestPath string) ([]byte, *providermanifestv1.Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	return data, manifest, nil
}

const appPublishManifestFile = "manifest.yaml"

func resolveAppPublishManifest(appName string) (string, error) {
	gitRoot, err := gitRootFromWorkingDirectory()
	if err != nil {
		return "", err
	}
	return resolveAppPublishManifestFromGitRoot(gitRoot, appName)
}

func resolveAppPublishManifestFromGitRoot(gitRoot, appName string) (string, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return "", fmt.Errorf("--app is required")
	}
	wantRel := filepath.ToSlash(filepath.Join("apps", appName, appPublishManifestFile))
	var matches []string
	err := filepath.WalkDir(gitRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != appPublishManifestFile {
			return nil
		}
		rel, err := filepath.Rel(gitRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == wantRel || strings.HasSuffix(rel, "/"+wantRel) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search apps/%s/manifest.yaml under %s: %w", appName, gitRoot, err)
	}
	switch len(matches) {
	case 0:
		msg := fmt.Sprintf("no apps/%s/manifest.yaml under git root %s", appName, gitRoot)
		if hint := appPublishManifestNotFoundHint(gitRoot, appName); hint != "" {
			msg += "; " + hint
		}
		return "", fmt.Errorf("%s", msg)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple apps/%s/manifest.yaml files under git root %s: %s; ensure only one matching app directory exists", appName, gitRoot, strings.Join(matches, ", "))
	}
}

func appPublishManifestNotFoundHint(gitRoot, appName string) string {
	wantDir := filepath.ToSlash(filepath.Join("apps", appName))
	var appDirs []string
	err := filepath.WalkDir(gitRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() || entry.Name() != appName {
			return nil
		}
		rel, err := filepath.Rel(gitRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == wantDir || strings.HasSuffix(rel, "/"+wantDir) {
			appDirs = append(appDirs, path)
		}
		return nil
	})
	if err != nil {
		return ""
	}
	switch len(appDirs) {
	case 0:
		return "verify --app and run from the repository checkout that contains apps/{app}/manifest.yaml"
	case 1:
		manifestYAML := filepath.Join(appDirs[0], appPublishManifestFile)
		if _, err := os.Stat(manifestYAML); err == nil {
			return ""
		}
		if _, err := os.Stat(filepath.Join(appDirs[0], "manifest.json")); err == nil {
			return fmt.Sprintf("found %s but app registry publish requires manifest.yaml", filepath.Join(appDirs[0], "manifest.json"))
		}
		return fmt.Sprintf("found app directory %s but no manifest.yaml inside it", appDirs[0])
	default:
		return "multiple app directories match this name; narrow the repository layout so only one apps/{app} directory exists"
	}
}

func gitRootFromWorkingDirectory() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	rootOut, err := runProviderPublishCommand("git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("app publish must run inside a git repository checkout (from %s): %w", cwd, err)
	}
	gitRoot := strings.TrimSpace(rootOut)
	if absGitRoot, err := filepath.Abs(gitRoot); err == nil {
		gitRoot = absGitRoot
	}
	if evaluatedGitRoot, err := filepath.EvalSymlinks(gitRoot); err == nil {
		gitRoot = evaluatedGitRoot
	}
	return gitRoot, nil
}

func printAppPublishPlanJSON(plan appregistry.PublishManifest) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

func printAppPublishUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app publish --bucket BUCKET --app APP --version VERSION --ref SHA --dist-dir DIR [--dist-dir DIR...] [publication flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Publish an installable app version to a GCS app registry bucket.")
	writeUsageLine(w, "Resolves the source manifest at apps/{app}/manifest.yaml under the git root.")
	writeUsageLine(w, "Builds immutable version metadata and artifacts under apps/{app}/ in the bucket.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --app        App name (manifest at apps/{app}/manifest.yaml)")
	writeUsageLine(w, "  --bucket     GCS bucket name or gs:// URL")
	writeUsageLine(w, "  --dist-dir   Directory containing release archives (repeatable)")
	writeUsageLine(w, "  --dry-run    Print the upload plan as JSON without writing")
	writeUsageLine(w, "  --ref        Full source commit SHA")
	writeUsageLine(w, "  --workflow-run-url      Publishing GitHub Actions run")
	writeUsageLine(w, "  --trigger-pr-number     Triggering pull request number")
	writeUsageLine(w, "  --trigger-pr-url        Triggering pull request URL")
	writeUsageLine(w, "  --trigger-pr-title      Triggering pull request title")
	writeUsageLine(w, "  --trigger-commit-sha    Triggering commit SHA")
	writeUsageLine(w, "  --trigger-commit-url    Triggering commit URL")
	writeUsageLine(w, "  --version    Semantic version guard")
}

func appPublishPublication(workflowRunURL string, triggerPRNumber int, triggerPRURL, triggerPRTitle, triggerCommitSHA, triggerCommitURL string) (*appregistry.Publication, error) {
	workflowRunURL = strings.TrimSpace(workflowRunURL)
	triggerPRURL = strings.TrimSpace(triggerPRURL)
	triggerPRTitle = strings.TrimSpace(triggerPRTitle)
	triggerCommitSHA = strings.ToLower(strings.TrimSpace(triggerCommitSHA))
	triggerCommitURL = strings.TrimSpace(triggerCommitURL)
	hasPR := triggerPRNumber != 0 || triggerPRURL != ""
	hasCommit := triggerCommitSHA != "" || triggerCommitURL != ""
	if workflowRunURL == "" && !hasPR && !hasCommit {
		return nil, nil
	}
	if workflowRunURL == "" {
		return nil, fmt.Errorf("--workflow-run-url is required with publication trigger flags")
	}
	if hasPR == hasCommit {
		return nil, fmt.Errorf("publication requires exactly one pull request or commit trigger")
	}
	publication := &appregistry.Publication{WorkflowRunURL: workflowRunURL}
	if hasPR {
		if triggerPRNumber <= 0 || triggerPRURL == "" {
			return nil, fmt.Errorf("--trigger-pr-number and --trigger-pr-url are required together")
		}
		publication.TriggerPullRequest = &appregistry.PublicationPullRequest{
			Number: triggerPRNumber,
			URL:    triggerPRURL,
			Title:  triggerPRTitle,
		}
		return publication, nil
	}
	if !fullGitSHARe.MatchString(triggerCommitSHA) || triggerCommitURL == "" {
		return nil, fmt.Errorf("--trigger-commit-sha must be a 40-character commit SHA and --trigger-commit-url is required")
	}
	publication.TriggerCommit = &appregistry.PublicationCommit{SHA: triggerCommitSHA, URL: triggerCommitURL}
	return publication, nil
}

type appRegistryCommandRunner struct{}

func (appRegistryCommandRunner) Run(name string, args ...string) (string, error) {
	return runProviderPublishCommand(name, args...)
}

func newAppRegistryWriter() *appregistry.Writer {
	return &appregistry.Writer{Store: appRegistryObjectStore()}
}

type appPublishProgressReporter struct {
	current *commandProgress
}

func newAppPublishProgressReporter() *appPublishProgressReporter {
	return &appPublishProgressReporter{}
}

func (r *appPublishProgressReporter) progress() appregistry.PublishProgress {
	return appregistry.PublishProgress{
		Start: func(format string, args ...any) {
			r.current = startCommandProgress(format, args...)
		},
		Status: func(format string, args ...any) {
			if r.current != nil {
				r.current.status(format, args...)
				return
			}
			progressStatus(format, args...)
		},
		Done: func(format string, args ...any) {
			if r.current != nil {
				r.current.done(format, args...)
				r.current = nil
			}
		},
		Log: func(line string) {
			_, _ = fmt.Fprintln(os.Stdout, line)
		},
	}
}
