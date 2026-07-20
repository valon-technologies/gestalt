package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

const providerStopTimeout = 10 * time.Second

// RegistryInstalledResolver resolves an isolated provider entry backed by a
// registry-materialized app package.
type RegistryInstalledResolver interface {
	ResolveInstalledApp(app string, entry *config.ProviderEntry, version string) (*config.ProviderEntry, error)
}

type pendingProviderClose struct {
	provider core.Provider
	done     <-chan error
}

type AppProviderRestarter struct {
	cfg              *config.Config
	deps             Deps
	providers        *registry.ProviderMap[core.Provider]
	authBuilds       []*preparedProviderBuilds
	lifecycles       *appProviderLifecycles
	registryResolver RegistryInstalledResolver
	held             map[string]func()
	closing          map[string]pendingProviderClose
	closeFailures    map[string]error
	runningVersions  map[string]string

	lifecycleMu sync.Mutex
}

type AppProviderRestarterConfig struct {
	Config           *config.Config
	Deps             Deps
	Providers        *registry.ProviderMap[core.Provider]
	AuthBuilds       []*preparedProviderBuilds
	Lifecycles       *appProviderLifecycles
	RegistryResolver RegistryInstalledResolver
}

func NewAppProviderRestarter(cfg AppProviderRestarterConfig) *AppProviderRestarter {
	return &AppProviderRestarter{
		cfg:              cfg.Config,
		deps:             cfg.Deps,
		providers:        cfg.Providers,
		authBuilds:       cfg.AuthBuilds,
		lifecycles:       cfg.Lifecycles,
		registryResolver: cfg.RegistryResolver,
		held:             make(map[string]func()),
		closing:          make(map[string]pendingProviderClose),
		closeFailures:    make(map[string]error),
		runningVersions:  make(map[string]string),
	}
}

func (r *AppProviderRestarter) Restartable(app string) (bool, error) {
	if r == nil {
		return false, fmt.Errorf("app provider restarter is not configured")
	}
	entry, err := r.appEntry(app)
	if err != nil {
		return false, err
	}
	return appProviderRestartable(r.cfg, entry), nil
}

func appProviderRestartable(cfg *config.Config, entry *config.ProviderEntry) bool {
	return entry != nil && !entry.DevActive && providerBuildsLocal(cfg, entry)
}

func (r *AppProviderRestarter) RunningVersion(app string) string {
	if r == nil {
		return ""
	}
	app = strings.TrimSpace(app)
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.providers == nil {
		return ""
	}
	if _, err := r.providers.Get(app); err != nil {
		delete(r.runningVersions, app)
		return ""
	}
	return r.runningVersions[app]
}

func (r *AppProviderRestarter) ValidateInstallation(installation *core.AppInstallation) error {
	if installation == nil {
		return fmt.Errorf("app installation is required")
	}
	entry, err := r.appEntry(strings.TrimSpace(installation.AppName))
	if err != nil {
		return err
	}
	if entry.Source.IsRegistry() && strings.TrimSpace(entry.Source.Registry) != strings.TrimSpace(installation.Registry) {
		return fmt.Errorf("app %q registry %q does not match configured registry %q", installation.AppName, installation.Registry, entry.Source.Registry)
	}
	return nil
}

func (r *AppProviderRestarter) StopApp(ctx context.Context, app string) error {
	if r == nil {
		return fmt.Errorf("stop app provider: restarter is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return fmt.Errorf("stop app provider: app is required")
	}
	entry, err := r.appEntry(app)
	if err != nil {
		return err
	}
	if !appProviderRestartable(r.cfg, entry) {
		return fmt.Errorf("stop app provider: %q is not catalog-restartable", app)
	}
	if r.providers == nil {
		return fmt.Errorf("stop app provider: provider registry is not configured")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if err := r.ensureLifecycleLease(ctx, app); err != nil {
		return fmt.Errorf("stop app provider: wait for provider lifecycle: %w", err)
	}
	if pending, ok := r.closing[app]; ok {
		return r.awaitProviderClose(ctx, app, pending)
	}
	if closeErr, ok := r.closeFailures[app]; ok {
		return fmt.Errorf("stop app provider %q: previous close failed: %w", app, closeErr)
	}

	provider, err := r.providers.Get(app)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			delete(r.runningVersions, app)
			return nil
		}
		r.releaseLifecycleLease(app)
		return fmt.Errorf("stop app provider: %w", err)
	}
	r.providers.Remove(app)
	delete(r.runningVersions, app)
	pending := pendingProviderClose{provider: provider, done: closeProviderForRestart(provider)}
	r.closing[app] = pending
	return r.awaitProviderClose(ctx, app, pending)
}

func (r *AppProviderRestarter) StartApp(ctx context.Context, app, version string) error {
	if r == nil {
		return fmt.Errorf("start app provider: restarter is not configured")
	}
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	if app == "" {
		return fmt.Errorf("start app provider: app is required")
	}
	entry, err := r.appEntry(app)
	if err != nil {
		return err
	}
	if !appProviderRestartable(r.cfg, entry) {
		return fmt.Errorf("start app provider: %q is not catalog-restartable", app)
	}
	if r.providers == nil {
		return fmt.Errorf("start app provider: provider registry is not configured")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if err := r.ensureLifecycleLease(ctx, app); err != nil {
		return fmt.Errorf("start app provider: wait for provider lifecycle: %w", err)
	}

	if _, getErr := r.providers.Get(app); getErr == nil {
		r.releaseLifecycleLease(app)
		return nil
	} else if !errors.Is(getErr, core.ErrNotFound) {
		return fmt.Errorf("start app provider: %w", getErr)
	}

	entry, err = r.resolveRegistryInstall(app, entry, version)
	if err != nil {
		return err
	}
	buildCtx, cancel := context.WithTimeout(ctx, providerInstallTimeout)
	defer cancel()
	buildCtx = invocation.WithCallerProvider(buildCtx, invocation.ProviderKindApp, app)
	result, err := buildProvider(buildCtx, app, entry, r.deps)
	if errors.Is(err, providerdev.ErrFrontendOnlyDevApp) {
		r.storeConnectionAuth(app, &ProviderBuildResult{})
		if r.deps.AppWorkflowDeclarations != nil {
			r.deps.AppWorkflowDeclarations.Set(app, []*proto.WorkflowDefinitionSpec{})
		}
		r.releaseLifecycleLease(app)
		return nil
	}
	if err != nil {
		return fmt.Errorf("start app provider %q: %w", app, err)
	}
	if err := validateProviderConnectionMode(app, result.Provider.ConnectionMode()); err != nil {
		closeIfPossible(result.Provider)
		return fmt.Errorf("start app provider %q: %w", app, err)
	}
	if err := r.providers.Register(app, result.Provider); err != nil {
		closeIfPossible(result.Provider)
		return fmt.Errorf("start app provider %q: %w", app, err)
	}
	if version != "" {
		r.runningVersions[app] = version
	}
	r.storeConnectionAuth(app, result)
	if r.deps.AppWorkflowDeclarations != nil {
		decls := result.WorkflowDeclarations
		if decls == nil {
			decls = []*proto.WorkflowDefinitionSpec{}
		}
		r.deps.AppWorkflowDeclarations.Set(app, decls)
	}
	r.releaseLifecycleLease(app)
	return nil
}

func (r *AppProviderRestarter) AbortRestarts() {
	if r == nil {
		return
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	for app := range r.held {
		r.releaseLifecycleLease(app)
	}
}

func (r *AppProviderRestarter) ensureLifecycleLease(ctx context.Context, app string) error {
	if _, ok := r.held[app]; ok {
		return nil
	}
	if r.lifecycles == nil {
		r.held[app] = func() {}
		return nil
	}
	release, err := r.lifecycles.acquire(ctx, app)
	if err != nil {
		return err
	}
	r.held[app] = release
	return nil
}

func (r *AppProviderRestarter) releaseLifecycleLease(app string) {
	release, ok := r.held[app]
	if !ok {
		return
	}
	delete(r.held, app)
	release()
}

func closeProviderForRestart(provider core.Provider) <-chan error {
	done := make(chan error, 1)
	closer, ok := provider.(interface{ Close() error })
	if !ok {
		done <- nil
		return done
	}
	go func() { done <- closer.Close() }()
	return done
}

func (r *AppProviderRestarter) awaitProviderClose(ctx context.Context, app string, pending pendingProviderClose) error {
	waitCtx, cancel := context.WithTimeout(ctx, providerStopTimeout)
	defer cancel()
	select {
	case err := <-pending.done:
		delete(r.closing, app)
		if err == nil {
			return nil
		}
		r.closeFailures[app] = err
		return fmt.Errorf("stop app provider %q: %w", app, err)
	case <-waitCtx.Done():
		return fmt.Errorf("stop app provider %q: close did not finish: %w", app, waitCtx.Err())
	}
}

func (r *AppProviderRestarter) appEntry(app string) (*config.ProviderEntry, error) {
	if r.cfg == nil || r.cfg.Apps == nil {
		return nil, fmt.Errorf("app %q is not configured", app)
	}
	entry := r.cfg.Apps[app]
	if entry == nil {
		return nil, fmt.Errorf("app %q is not configured", app)
	}
	return entry, nil
}

func (r *AppProviderRestarter) resolveRegistryInstall(app string, entry *config.ProviderEntry, version string) (*config.ProviderEntry, error) {
	if r == nil || r.registryResolver == nil {
		return entry, nil
	}
	resolved, err := r.registryResolver.ResolveInstalledApp(app, entry, version)
	if err != nil {
		return nil, fmt.Errorf("mount registry installed app %q@%s: %w", app, version, err)
	}
	if resolved == nil {
		return nil, fmt.Errorf("mount registry installed app %q@%s: resolver returned nil provider entry", app, version)
	}
	return resolved, nil
}

func (r *AppProviderRestarter) storeConnectionAuth(app string, result *ProviderBuildResult) {
	for _, builds := range r.authBuilds {
		builds.storeConnectionAuth(app, result)
	}
}
