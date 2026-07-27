package daemon

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

type appRegistryRetentionFlags struct {
	configPaths *repeatedStringFlag
	bucket      *string
	appName     *string
	dryRun      *bool
}

func newAppRegistryRetentionFlags(fs *flag.FlagSet) appRegistryRetentionFlags {
	return appRegistryRetentionFlags{
		configPaths: new(repeatedStringFlag),
		bucket:      fs.String("bucket", "", "GCS bucket name for registry objects"),
		appName:     fs.String("app", "", "app name under apps/{app}/"),
		dryRun:      fs.Bool("dry-run", false, "report prune actions without writing"),
	}
}

func runAppRegistryRetention(args []string) error {
	if len(args) == 0 {
		printAppRegistryRetentionUsage(os.Stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "-h", "--help", "help":
		printAppRegistryRetentionUsage(os.Stderr)
		return flag.ErrHelp
	case "prune":
		return runAppRegistryRetentionPrune(args[1:])
	default:
		return fmt.Errorf("unknown app registry retention command %q", args[0])
	}
}

func runAppRegistryRetentionPrune(args []string) error {
	fs := flag.NewFlagSet("gestaltd app registry retention prune", flag.ContinueOnError)
	fs.Usage = func() { printAppRegistryRetentionPruneUsage(fs.Output()) }
	flags := newAppRegistryRetentionFlags(fs)
	fs.Var(flags.configPaths, "config", "path to config file (repeat to layer overrides)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	appName := strings.TrimSpace(*flags.appName)
	if appName == "" {
		return fmt.Errorf("--app is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return fmt.Errorf("--app: %w", err)
	}

	var (
		registry  config.AppRegistryConfig
		changeSvc *coredata.AppVersionChangeRequestService
		lockSvc   *coredata.AppVersionInstallLockService
		desired   string
	)
	switch {
	case len(*flags.configPaths) > 0:
		cfg, err := config.LoadPaths([]string(*flags.configPaths))
		if err != nil {
			return err
		}
		entry, ok := cfg.Apps[appName]
		if !ok || entry == nil || !entry.Source.IsRegistry() {
			return fmt.Errorf("app %q is not registry-managed in deploy config", appName)
		}
		registryName := strings.TrimSpace(entry.Source.Registry)
		var okRegistry bool
		registry, okRegistry = cfg.AppRegistries[registryName]
		if !okRegistry {
			return fmt.Errorf("registry %q is not configured", registryName)
		}
		env, err := setupBootstrapWithConfigPaths([]string(*flags.configPaths), "", cfg.Server.ArtifactsDir, true, true, "", "")
		if err != nil {
			return err
		}
		defer env.Stop()
		if env.Result == nil || env.Result.Services == nil {
			return fmt.Errorf("bootstrap services are unavailable")
		}
		changeSvc = env.Result.Services.AppVersionChangeRequests
		lockSvc = env.Result.Services.AppVersionInstallLocks
		known, err := changeSvc.ListKnownVersionsByApp(context.Background(), appName)
		if err != nil {
			return fmt.Errorf("list known versions: %w", err)
		}
		desired = coredata.LatestKnownVersion(known)
	case strings.TrimSpace(*flags.bucket) == "":
		return fmt.Errorf("--config or --bucket is required")
	default:
		var err error
		registry, err = config.NewGCSAppRegistry(*flags.bucket)
		if err != nil {
			return fmt.Errorf("--bucket: %w", err)
		}
	}

	return pruneAppRegistryRetention(registry, appName, desired, changeSvc, lockSvc, *flags.dryRun)
}

func pruneAppRegistryRetention(registry config.AppRegistryConfig, appName, desiredVersion string, changeSvc *coredata.AppVersionChangeRequestService, lockSvc *coredata.AppVersionInstallLockService, dryRun bool) error {
	if lockSvc != nil {
		lockHolder := uuid.NewString()
		ctx := context.Background()
		if err := lockSvc.Acquire(ctx, appName, desiredVersion, lockHolder, coredata.DefaultAppVersionInstallLockTTL); err != nil {
			return fmt.Errorf("claim app version install lock: %w", err)
		}
		defer func() { _ = lockSvc.Release(context.WithoutCancel(ctx), appName, desiredVersion, lockHolder) }()
	}

	storageRoot, err := registry.StorageURL()
	if err != nil {
		return err
	}
	indexURL := appregistry.StorageURL(storageRoot, appregistry.AppIndexPath(appName))
	retentionURL := appregistry.StorageURL(storageRoot, appregistry.AppRetentionPath(appName))

	unused, deployed, err := registry.RetentionPolicy()
	if err != nil {
		return err
	}
	policy := appregistry.RetentionPolicy{UnusedRetention: unused, DeployedRetention: deployed}
	now := time.Now().UTC()

	deployedVersions := map[string]struct{}{}
	if changeSvc != nil {
		requests, err := changeSvc.ListRequestsByApp(context.Background(), appName)
		if err != nil {
			return fmt.Errorf("list change requests: %w", err)
		}
		deployedVersions = appregistry.DeployedVersionsFromChangeRequests(requests)
	}

	for attempt := 1; attempt <= appRegistryCatalogUpdateAttempts; attempt++ {
		indexGen, indexData, err := downloadAppRegistryObject(indexURL)
		if err != nil {
			return err
		}
		retentionGen, retentionData, err := downloadAppRegistryObject(retentionURL)
		if err != nil {
			return err
		}
		var index *appregistry.Index
		if len(indexData) == 0 {
			index = appregistry.NewEmptyIndex()
		} else {
			index, err = appregistry.DecodeIndex(indexData)
			if err != nil {
				return fmt.Errorf("decode index: %w", err)
			}
		}
		var retention *appregistry.RetentionIndex
		if len(retentionData) == 0 {
			retention = appregistry.NewEmptyRetentionIndex()
		} else {
			retention, err = appregistry.DecodeRetentionIndex(retentionData)
			if err != nil {
				return fmt.Errorf("decode retention: %w", err)
			}
		}

		actions := appregistry.EvaluateRetentionPrune(index, retention, appName, desiredVersion, deployedVersions, policy, now)
		if dryRun {
			for _, action := range actions {
				_, _ = fmt.Fprintf(os.Stdout, "would %s %s\n", action.Kind, action.Version)
			}
			return nil
		}
		if len(actions) == 0 {
			return nil
		}

		indexChanged := false
		retentionChanged := false
		for _, action := range actions {
			if deployedVersions != nil {
				if _, deployed := deployedVersions[action.Version]; deployed && action.Kind == appregistry.RetentionPruneDeleteUnused {
					continue
				}
			}
			entry, ok, err := freshRetentionEntryForPrune(retentionURL, action.Version)
			if err != nil {
				return err
			}
			if !ok || !appregistry.ShouldApplyRetentionPruneAction(action, entry, now) {
				continue
			}
			if appregistry.ApplyRetentionPruneAction(index, retention, appName, action, now) {
				if action.Kind == appregistry.RetentionPruneDeleteUnused {
					retentionChanged = true
					indexChanged = true
				}
			}
			if action.Kind == appregistry.RetentionPruneDeleteUnused {
				entryPath := appregistry.StorageURL(storageRoot, appregistry.AppVersionEntryPath(appName, action.Version))
				if err := deleteAppRegistryObject(entryPath); err != nil {
					return err
				}
				artifactPrefix := appregistry.StorageURL(storageRoot, appregistry.AppArtifactPrefix(appName, action.Version))
				if err := deleteAppRegistryPrefix(artifactPrefix); err != nil {
					return err
				}
			}
			if action.Kind == appregistry.RetentionPruneDeleteArtifact {
				artifactPrefix := appregistry.StorageURL(storageRoot, appregistry.AppArtifactPrefix(appName, action.Version))
				if err := deleteAppRegistryPrefix(artifactPrefix); err != nil {
					return err
				}
			}
		}

		if retentionChanged {
			data, err := json.MarshalIndent(retention, "", "  ")
			if err != nil {
				return err
			}
			tmpPath, err := writeTempJSON("gestalt-app-retention-*", append(data, '\n'))
			if err != nil {
				return err
			}
			if err := uploadAppRegistryIndexFile(tmpPath, retentionURL, "retention-prune", retentionGen); err != nil {
				_ = os.Remove(tmpPath)
				if appPublishPreconditionFailed(err) && attempt < appRegistryCatalogUpdateAttempts {
					continue
				}
				return err
			}
			_ = os.Remove(tmpPath)
		}
		if indexChanged {
			data, err := json.MarshalIndent(index, "", "  ")
			if err != nil {
				return err
			}
			tmpPath, err := writeTempJSON("gestalt-app-index-*", append(data, '\n'))
			if err != nil {
				return err
			}
			if err := uploadAppRegistryIndexFile(tmpPath, indexURL, "retention-prune", indexGen); err != nil {
				_ = os.Remove(tmpPath)
				if appPublishPreconditionFailed(err) && attempt < appRegistryCatalogUpdateAttempts {
					continue
				}
				return err
			}
			_ = os.Remove(tmpPath)
		}
		return nil
	}
	return fmt.Errorf("prune retention for %s: exceeded retry limit after concurrent updates", appName)
}

func freshRetentionEntryForPrune(retentionURL, version string) (appregistry.RetentionVersion, bool, error) {
	_, retentionData, err := downloadAppRegistryObject(retentionURL)
	if err != nil {
		return appregistry.RetentionVersion{}, false, err
	}
	if len(retentionData) == 0 {
		return appregistry.RetentionVersion{}, false, nil
	}
	retention, err := appregistry.DecodeRetentionIndex(retentionData)
	if err != nil {
		return appregistry.RetentionVersion{}, false, fmt.Errorf("decode retention: %w", err)
	}
	entry, ok := retention.Versions[version]
	return entry, ok, nil
}

func deleteAppRegistryObject(storageURL string) error {
	_, err := runProviderPublishCommand("gcloud", "storage", "rm", storageURL)
	return err
}

func deleteAppRegistryPrefix(prefix string) error {
	_, err := runProviderPublishCommand("gcloud", "storage", "rm", "--recursive", prefix+"/**")
	return err
}

func printAppRegistryRetentionUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry retention <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  prune     Delete eligible published versions and prune locked artifacts")
}

func printAppRegistryRetentionPruneUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry retention prune --config CONFIG --app APP [--dry-run]")
	writeUsageLine(w, "  gestaltd app registry retention prune --bucket BUCKET --app APP [--dry-run]")
}
