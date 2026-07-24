package daemon

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

const appRegistryCatalogUpdateAttempts = 5

type appRegistryPendingFlags struct {
	bucket         *string
	appName        *string
	version        *string
	ref            *string
	workflowRunURL *string
	triggerPRNumber *int
	triggerPRURL   *string
	triggerPRTitle *string
	triggerCommitSHA *string
	triggerCommitURL *string
}

func newAppRegistryPendingFlags(fs *flag.FlagSet) appRegistryPendingFlags {
	return appRegistryPendingFlags{
		bucket:           fs.String("bucket", "", "GCS bucket name for registry uploads"),
		appName:          fs.String("app", "", "app name under apps/{app}/manifest.yaml"),
		version:          fs.String("version", "", "semantic version guard"),
		ref:              fs.String("ref", "", "full source commit SHA"),
		workflowRunURL:   fs.String("workflow-run-url", "", "GitHub Actions workflow run URL"),
		triggerPRNumber:  fs.Int("trigger-pr-number", 0, "pull request number that triggered publication"),
		triggerPRURL:     fs.String("trigger-pr-url", "", "pull request URL that triggered publication"),
		triggerPRTitle:   fs.String("trigger-pr-title", "", "pull request title that triggered publication"),
		triggerCommitSHA: fs.String("trigger-commit-sha", "", "commit SHA that triggered publication"),
		triggerCommitURL: fs.String("trigger-commit-url", "", "commit URL that triggered publication"),
	}
}

func runAppRegistryPending(args []string) error {
	if len(args) == 0 {
		printAppRegistryPendingUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printAppRegistryPendingUsage(os.Stderr)
		return flag.ErrHelp
	case "set":
		return runAppRegistryPendingSet(args[1:])
	case "clear":
		return runAppRegistryPendingClear(args[1:])
	case "fail":
		return runAppRegistryPendingFail(args[1:])
	default:
		return fmt.Errorf("unknown app registry pending command %q", args[0])
	}
}

func runAppRegistryPendingSet(args []string) error {
	fs := flag.NewFlagSet("gestaltd app registry pending set", flag.ContinueOnError)
	fs.Usage = func() { printAppRegistryPendingSetUsage(fs.Output()) }
	flags := newAppRegistryPendingFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	appName, sourceRef, registry, publication, err := parseAppRegistryPendingWriteFlags(flags, true)
	if err != nil {
		return err
	}

	manifestPath, err := resolveAppPublishManifest(appName)
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
	if manifestApp != appName {
		return fmt.Errorf("%s: manifest source app %q does not match --app %q", manifestPath, manifestApp, appName)
	}
	if err := appregistry.ValidatePublishInput(sourceManifest, *flags.version, sourceRef); err != nil {
		return err
	}
	repository, err := appregistry.RepositoryFromManifestSource(sourceManifest.Source)
	if err != nil {
		return fmt.Errorf("%s: invalid manifest source: %w", manifestPath, err)
	}

	now := time.Now().UTC()
	pendingVersion := appregistry.PendingVersion{
		Version:     *flags.version,
		SourceRef:   sourceRef,
		Repository:  repository,
		Publication: publication,
	}

	return mutateAppRegistryPendingCatalog(registry, appName, sourceRef, func(state *appRegistryPendingCatalogState) error {
		appregistry.PrunePendingIndex(state.pending, state.failed, state.published, now)
		appregistry.PruneFailedIndex(state.failed, state.published, now)
		state.pending, _ = appregistry.UpsertPendingVersion(state.pending, appName, pendingVersion, now)
		state.pendingChanged = true
		return nil
	})
}

func runAppRegistryPendingClear(args []string) error {
	fs := flag.NewFlagSet("gestaltd app registry pending clear", flag.ContinueOnError)
	fs.Usage = func() { printAppRegistryPendingClearUsage(fs.Output()) }
	flags := newAppRegistryPendingFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	appName, sourceRef, registry, _, err := parseAppRegistryPendingWriteFlags(flags, false)
	if err != nil {
		return err
	}

	return mutateAppRegistryPendingCatalog(registry, appName, sourceRef, func(state *appRegistryPendingCatalogState) error {
		if _, ok := appregistry.RemovePendingVersion(state.pending, *flags.version); ok {
			state.pendingChanged = true
		}
		return nil
	})
}

func runAppRegistryPendingFail(args []string) error {
	fs := flag.NewFlagSet("gestaltd app registry pending fail", flag.ContinueOnError)
	fs.Usage = func() { printAppRegistryPendingFailUsage(fs.Output()) }
	flags := newAppRegistryPendingFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	appName, sourceRef, registry, _, err := parseAppRegistryPendingWriteFlags(flags, false)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return mutateAppRegistryPendingCatalog(registry, appName, sourceRef, func(state *appRegistryPendingCatalogState) error {
		removed, ok := appregistry.RemovePendingVersion(state.pending, *flags.version)
		if !ok {
			return nil
		}
		state.pendingChanged = true
		if updated, changed := appregistry.RecordFailedVersion(state.failed, appName, *removed, now, appregistry.FailedReasonWorkflowFailed); changed {
			state.failed = updated
			state.failedChanged = true
		}
		return nil
	})
}

func parseAppRegistryPendingWriteFlags(flags appRegistryPendingFlags, requireRef bool) (appName, sourceRef string, registry config.AppRegistryConfig, publication *appregistry.Publication, err error) {
	if strings.TrimSpace(*flags.bucket) == "" {
		return "", "", config.AppRegistryConfig{}, nil, fmt.Errorf("--bucket is required")
	}
	appName = strings.TrimSpace(*flags.appName)
	if appName == "" {
		return "", "", config.AppRegistryConfig{}, nil, fmt.Errorf("--app is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return "", "", config.AppRegistryConfig{}, nil, fmt.Errorf("--app: %w", err)
	}
	if strings.TrimSpace(*flags.version) == "" {
		return "", "", config.AppRegistryConfig{}, nil, fmt.Errorf("--version is required")
	}
	sourceRef = strings.ToLower(strings.TrimSpace(*flags.ref))
	if requireRef {
		if !fullGitSHARe.MatchString(sourceRef) {
			return "", "", config.AppRegistryConfig{}, nil, fmt.Errorf("--ref must be a 40-character commit SHA")
		}
		publication, err = appPublishPublication(
			*flags.workflowRunURL,
			*flags.triggerPRNumber,
			*flags.triggerPRURL,
			*flags.triggerPRTitle,
			*flags.triggerCommitSHA,
			*flags.triggerCommitURL,
		)
		if err != nil {
			return "", "", config.AppRegistryConfig{}, nil, err
		}
	}
	registry, err = config.NewGCSAppRegistry(*flags.bucket)
	if err != nil {
		return "", "", config.AppRegistryConfig{}, nil, fmt.Errorf("--bucket: %w", err)
	}
	return appName, sourceRef, registry, publication, nil
}

type appRegistryPendingCatalogState struct {
	pending        *appregistry.PendingIndex
	failed         *appregistry.FailedIndex
	published      *appregistry.Index
	pendingGen     int64
	failedGen      int64
	pendingChanged bool
	failedChanged  bool
}

func mutateAppRegistryPendingCatalog(registry config.AppRegistryConfig, appName, sourceRef string, mutate func(*appRegistryPendingCatalogState) error) error {
	storageRoot, err := registry.StorageURL()
	if err != nil {
		return err
	}
	pendingURL := appregistry.StorageURL(storageRoot, appregistry.AppPendingPath(appName))
	failedURL := appregistry.StorageURL(storageRoot, appregistry.AppFailedPath(appName))
	indexURL := appregistry.StorageURL(storageRoot, appregistry.AppIndexPath(appName))

	for attempt := 1; attempt <= appRegistryCatalogUpdateAttempts; attempt++ {
		if attempt > 1 {
			startCommandProgress("").status("App registry pending catalog changed concurrently; retrying attempt %d/%d", attempt, appRegistryCatalogUpdateAttempts)
		}
		state, err := loadAppRegistryPendingCatalog(pendingURL, failedURL, indexURL, appName)
		if err != nil {
			return err
		}
		state.pendingChanged = false
		state.failedChanged = false
		if err := mutate(state); err != nil {
			return err
		}
		if !state.pendingChanged && !state.failedChanged {
			return nil
		}
		if state.failedChanged {
			if err := uploadAppRegistryCatalogDocument(failedURL, sourceRef, state.failedGen, state.failed); err != nil {
				if appPublishPreconditionFailed(err) && attempt < appRegistryCatalogUpdateAttempts {
					continue
				}
				return err
			}
		}
		if state.pendingChanged {
			if err := uploadAppRegistryCatalogDocument(pendingURL, sourceRef, state.pendingGen, state.pending); err != nil {
				if appPublishPreconditionFailed(err) && attempt < appRegistryCatalogUpdateAttempts {
					continue
				}
				return err
			}
		}
		if state.pendingChanged {
			_, _ = fmt.Fprintf(os.Stdout, "updated %s\n", pendingURL)
		}
		if state.failedChanged {
			_, _ = fmt.Fprintf(os.Stdout, "updated %s\n", failedURL)
		}
		return nil
	}
	return fmt.Errorf("update pending catalog for %s: exceeded retry limit after concurrent updates", appName)
}

func loadAppRegistryPendingCatalog(pendingURL, failedURL, indexURL, appName string) (*appRegistryPendingCatalogState, error) {
	pendingGen, pendingData, err := downloadAppRegistryObject(pendingURL)
	if err != nil {
		return nil, err
	}
	failedGen, failedData, err := downloadAppRegistryObject(failedURL)
	if err != nil {
		return nil, err
	}
	_, indexData, err := downloadAppRegistryObject(indexURL)
	if err != nil {
		return nil, err
	}

	state := &appRegistryPendingCatalogState{}
	if len(pendingData) == 0 {
		state.pending = appregistry.NewEmptyPendingIndex(appName)
	} else {
		state.pending, err = appregistry.DecodePendingIndex(pendingData)
		if err != nil {
			return nil, fmt.Errorf("decode pending catalog: %w", err)
		}
	}
	if len(failedData) == 0 {
		state.failed = appregistry.NewEmptyFailedIndex(appName)
	} else {
		state.failed, err = appregistry.DecodeFailedIndex(failedData)
		if err != nil {
			return nil, fmt.Errorf("decode failed catalog: %w", err)
		}
	}
	if len(indexData) == 0 {
		state.published = appregistry.NewEmptyIndex()
	} else {
		state.published, err = appregistry.DecodeIndex(indexData)
		if err != nil {
			return nil, fmt.Errorf("decode published index: %w", err)
		}
	}
	state.pendingGen = pendingGen
	state.failedGen = failedGen
	return state, nil
}

func uploadAppRegistryCatalogDocument(storageURL, sourceRef string, generation int64, document any) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	tmpPath, err := writeTempJSON("gestalt-app-catalog-*", append(data, '\n'))
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := uploadAppRegistryIndexFile(tmpPath, storageURL, sourceRef, generation); err != nil {
		return err
	}
	return nil
}

func printAppRegistryPendingUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry pending <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  set     Record an in-flight publish in pending.json")
	writeUsageLine(w, "  clear   Remove a pending publish on success")
	writeUsageLine(w, "  fail    Record a failed publish in failed.json")
}

func printAppRegistryPendingSetUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry pending set --bucket BUCKET --app APP --version VERSION --ref SHA [publication flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Prune stuck pending and old failed entries, then upsert pending.json for VERSION.")
}

func printAppRegistryPendingClearUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry pending clear --bucket BUCKET --app APP --version VERSION")
}

func printAppRegistryPendingFailUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry pending fail --bucket BUCKET --app APP --version VERSION")
}
