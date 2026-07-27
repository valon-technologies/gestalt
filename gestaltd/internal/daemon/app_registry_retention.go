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
	appName     *string
	dryRun      *bool
}

func newAppRegistryRetentionFlags(fs *flag.FlagSet) appRegistryRetentionFlags {
	return appRegistryRetentionFlags{
		configPaths: new(repeatedStringFlag),
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
	if len(*flags.configPaths) == 0 {
		return fmt.Errorf("--config is required")
	}
	appName := strings.TrimSpace(*flags.appName)
	if appName == "" {
		return fmt.Errorf("--app is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return fmt.Errorf("--app: %w", err)
	}

	cfg, err := config.LoadPaths([]string(*flags.configPaths))
	if err != nil {
		return err
	}
	entry, ok := cfg.Apps[appName]
	if !ok || entry == nil || !entry.Source.IsRegistry() {
		return fmt.Errorf("app %q is not registry-managed in deploy config", appName)
	}
	registryName := strings.TrimSpace(entry.Source.Registry)
	registry, okRegistry := cfg.AppRegistries[registryName]
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
	changeSvc := env.Result.Services.AppVersionChangeRequests
	if changeSvc == nil {
		return fmt.Errorf("app version change request service is unavailable")
	}
	lockSvc := env.Result.Services.AppVersionInstallLocks
	known, err := changeSvc.ListKnownVersionsByApp(context.Background(), appName)
	if err != nil {
		return fmt.Errorf("list known versions: %w", err)
	}
	desired := coredata.LatestKnownVersion(known)

	return pruneAppRegistryRetention(registry, appName, desired, changeSvc, lockSvc, *flags.dryRun)
}

func pruneAppRegistryRetention(registry config.AppRegistryConfig, appName, desiredVersion string, changeSvc *coredata.AppVersionChangeRequestService, lockSvc *coredata.AppVersionInstallLockService, dryRun bool) error {
	if changeSvc == nil {
		return fmt.Errorf("app version change request service is required")
	}
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

	unused, deployed, err := registry.RetentionPolicy()
	if err != nil {
		return err
	}
	policy := appregistry.RetentionPolicy{UnusedRetention: unused, DeployedRetention: deployed}
	now := time.Now().UTC()

	requests, err := changeSvc.ListRequestsByApp(context.Background(), appName)
	if err != nil {
		return fmt.Errorf("list change requests: %w", err)
	}
	chain := appregistry.VersionDeploymentChainFromChangeRequests(requests)

	for attempt := 1; attempt <= appRegistryCatalogUpdateAttempts; attempt++ {
		indexGen, indexData, err := downloadAppRegistryObject(indexURL)
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

		actions := appregistry.EvaluateRetentionPrune(index, appName, desiredVersion, chain, policy, now)
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
		for _, action := range actions {
			if _, deployed := chain.Deployed[action.Version]; deployed && action.Kind == appregistry.RetentionPruneDeleteUnused {
				continue
			}
			if appregistry.ApplyRetentionPruneAction(index, appName, action) {
				indexChanged = true
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

		if !indexChanged {
			return nil
		}
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
		return nil
	}
	return fmt.Errorf("prune retention for %s: exceeded retry limit after concurrent updates", appName)
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
	writeUsageLine(w, "  prune     Delete or lock eligible published versions")
}

func printAppRegistryRetentionPruneUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry retention prune --config CONFIG --app APP [--dry-run]")
}
