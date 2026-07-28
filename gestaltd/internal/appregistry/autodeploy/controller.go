package autodeploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const Actor = "system:auto-deploy"

type AppConfig struct {
	Registry   string
	PublicRoot string
}

type RegistryReader interface {
	FetchAppIndexConditional(ctx context.Context, publicRoot, appName, ifNoneMatch string) (*appregistry.AppIndexFetchResult, error)
}

type Installer interface {
	Select(ctx context.Context, input appregistry.InstallInput) (*appregistry.InstallOutput, error)
}

type Controller struct {
	Settings       *coredata.AutoDeploySettingsService
	Rollouts       *coredata.AppRolloutService
	ChangeRequests *coredata.AppVersionChangeRequestService
	Reader         RegistryReader
	Installer      Installer
	Apps           map[string]AppConfig
	Interval       time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
	wake      chan string
	etags     map[string]string
}

func New(
	settings *coredata.AutoDeploySettingsService,
	rollouts *coredata.AppRolloutService,
	changeRequests *coredata.AppVersionChangeRequestService,
	reader RegistryReader,
	installer Installer,
	apps map[string]AppConfig,
	interval time.Duration,
) *Controller {
	if reader == nil {
		reader = &appregistry.RegistryReader{}
	}
	if interval <= 0 {
		interval = time.Minute
	}
	return &Controller{
		Settings:       settings,
		Rollouts:       rollouts,
		ChangeRequests: changeRequests,
		Reader:         reader,
		Installer:      installer,
		Apps:           cloneApps(apps),
		Interval:       interval,
		done:           make(chan struct{}),
		wake:           make(chan string, 32),
		etags:          make(map[string]string),
	}
}

func (c *Controller) Start(ctx context.Context) {
	if c == nil {
		return
	}
	c.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		c.cancel = cancel
		go c.run(runCtx)
	})
}

func (c *Controller) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		if c.cancel == nil {
			return
		}
		c.cancel()
		<-c.done
	})
}

// Notify requests prompt coalescing after a rollout reaches a terminal state.
// The periodic pass remains the source of eventual convergence if the signal
// is coalesced or dropped.
func (c *Controller) Notify(app string) {
	if c == nil {
		return
	}
	select {
	case c.wake <- strings.TrimSpace(app):
	default:
	}
}

func (c *Controller) run(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()

	c.reconcileAndLog(ctx, "")
	for {
		select {
		case <-ctx.Done():
			return
		case app := <-c.wake:
			c.reconcileAndLog(ctx, app)
		case <-ticker.C:
			c.reconcileAndLog(ctx, "")
		}
	}
}

func (c *Controller) reconcileAndLog(ctx context.Context, app string) {
	var err error
	if strings.TrimSpace(app) == "" {
		err = c.ReconcileAll(ctx)
	} else {
		err = c.Reconcile(ctx, app)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("app registry auto-deploy reconcile failed", "app", app, "error", err)
	}
}

func (c *Controller) ReconcileAll(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	settings, err := c.Settings.ListEnabled(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, setting := range settings {
		if setting == nil {
			continue
		}
		if err := c.Reconcile(ctx, setting.App); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", setting.App, err))
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) Reconcile(ctx context.Context, appName string) error {
	if err := c.validate(); err != nil {
		return err
	}
	appName = strings.TrimSpace(appName)
	app, ok := c.Apps[appName]
	if !ok {
		return nil
	}
	settings, err := c.Settings.Get(ctx, appName)
	if errors.Is(err, core.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}

	rollout, err := c.Rollouts.Get(ctx, appName)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return err
	}
	if rollout != nil && rollout.State == core.AppRolloutStateFailed {
		_, updateErr := c.Settings.Update(ctx, appName, func(current *core.AppAutoDeploySettings) error {
			current.Enabled = false
			current.PendingVersion = ""
			current.LastError = fmt.Sprintf("rollout for %s failed", rollout.Version)
			return nil
		})
		return updateErr
	}

	result, err := c.Reader.FetchAppIndexConditional(
		ctx,
		app.PublicRoot,
		appName,
		c.etags[appName],
	)
	if err != nil {
		return err
	}
	if !result.NotModified {
		c.etags[appName] = result.ETag
		versions := appregistry.VersionsFromIndex(result.Index, appName)
		if len(versions) > 0 {
			newest := versions[0].Version
			settings, err = c.Settings.Update(ctx, appName, func(current *core.AppAutoDeploySettings) error {
				if !current.Enabled {
					return nil
				}
				if current.LastSeenVersion != newest {
					current.LastSeenVersion = newest
					current.PendingVersion = newest
					current.LastError = ""
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}

	if !settings.Enabled {
		return nil
	}
	if rollout != nil && (rollout.State == core.AppRolloutStateEnrolling || rollout.State == core.AppRolloutStateRestarting) {
		if settings.LastError != "" {
			_, err = c.Settings.Update(ctx, appName, func(current *core.AppAutoDeploySettings) error {
				current.LastError = ""
				return nil
			})
			return err
		}
		return nil
	}
	if settings.PendingVersion == "" {
		return nil
	}
	known, err := c.ChangeRequests.ListKnownVersionsByApp(ctx, appName)
	if err != nil {
		return err
	}
	if coredata.LatestKnownVersion(known) == settings.PendingVersion {
		_, err = c.Settings.Update(ctx, appName, func(current *core.AppAutoDeploySettings) error {
			if current.PendingVersion == settings.PendingVersion {
				current.PendingVersion = ""
			}
			return nil
		})
		return err
	}

	_, err = c.Installer.Select(ctx, appregistry.InstallInput{
		Registry: app.Registry,
		App:      appName,
		Version:  settings.PendingVersion,
		Actor:    Actor,
	})
	switch {
	case err == nil, errors.Is(err, appregistry.ErrAppVersionAlreadyInstalled):
		_, updateErr := c.Settings.Update(ctx, appName, func(current *core.AppAutoDeploySettings) error {
			if current.PendingVersion == settings.PendingVersion {
				current.PendingVersion = ""
				current.LastError = ""
			}
			return nil
		})
		return updateErr
	case errors.Is(err, appregistry.ErrAppRolloutActive),
		errors.Is(err, appregistry.ErrInstallVersionLocked):
		return nil
	case isCandidateRejection(err):
		_, updateErr := c.Settings.Update(ctx, appName, func(current *core.AppAutoDeploySettings) error {
			if current.PendingVersion == settings.PendingVersion {
				current.PendingVersion = ""
				current.LastError = err.Error()
			}
			return nil
		})
		return updateErr
	default:
		return err
	}
}

func (c *Controller) validate() error {
	if c == nil || c.Settings == nil || c.Rollouts == nil || c.ChangeRequests == nil ||
		c.Reader == nil || c.Installer == nil {
		return fmt.Errorf("auto-deploy controller is not configured")
	}
	return nil
}

func isCandidateRejection(err error) bool {
	return errors.Is(err, appregistry.ErrInstallValidationFailed) ||
		errors.Is(err, appregistry.ErrAppVersionExpired) ||
		errors.Is(err, appregistry.ErrAppVersionLocked)
}

func cloneApps(apps map[string]AppConfig) map[string]AppConfig {
	out := make(map[string]AppConfig, len(apps))
	for name, app := range apps {
		out[strings.TrimSpace(name)] = AppConfig{
			Registry:   strings.TrimSpace(app.Registry),
			PublicRoot: strings.TrimRight(strings.TrimSpace(app.PublicRoot), "/"),
		}
	}
	return out
}
