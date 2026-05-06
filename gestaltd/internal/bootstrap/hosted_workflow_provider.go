package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"gopkg.in/yaml.v3"
)

const hostedWorkflowRuntimeLifecycleSafetyMargin = 5 * time.Second

type hostedWorkflowProviderLaunch struct {
	name            string
	runtimeConfig   config.EffectiveHostedRuntime
	runtimeProvider pluginruntime.Provider
	runtimeOwned    bool
	runtimePlan     HostedRuntimePlan
	cfg             componentprovider.YAMLConfig
	allowedHosts    []string
	launch          hostedProcessLaunch
	cleanup         func()
}

type hostedWorkflowProviderInstance struct {
	provider         coreworkflow.Provider
	runtimeProvider  pluginruntime.Provider
	runtimeSessionID string
	runtimeSession   *pluginruntime.Session
}

func buildHostedWorkflowProvider(ctx context.Context, name string, entry *config.ProviderEntry, node yaml.Node, hostServices []runtimehost.HostService, deps Deps) (coreworkflow.Provider, error) {
	launch, err := prepareHostedWorkflowProviderLaunch(ctx, name, entry, node, deps)
	if err != nil {
		return nil, err
	}
	hostServices = appendRuntimeLogHostService(hostServices, launch.runtimeConfig, deps, launch.runtimePlan)
	publicHostServicesCleanup, err := registerPublicRuntimeHostServices(name, hostServices, deps, launch.runtimePlan, launch.runtimeProvider)
	if err != nil {
		launch.close()
		return nil, err
	}
	launch.cleanup = chainCleanup(launch.cleanup, publicHostServicesCleanup)

	runtimeCfg := entry.HostedRuntimeConfig()
	if runtimeCfg != nil && runtimeCfg.LifecyclePolicyFieldsSet() {
		policy, err := runtimeCfg.LifecyclePolicy()
		if err != nil {
			launch.close()
			return nil, fmt.Errorf("parse hosted workflow runtime lifecycle policy: %w", err)
		}
		return newHostedWorkflowProviderPool(ctx, launch, hostServices, deps, policy)
	}
	cleanup := launch.cleanup
	launch.cleanup = nil
	instance, err := startHostedWorkflowProviderInstance(ctx, launch, hostServices, deps, launch.runtimeOwned, cleanup, 0)
	if err != nil {
		launch.close()
		return nil, err
	}
	return instance.provider, nil
}

func (p *hostedWorkflowProviderLaunch) close() {
	if p == nil {
		return
	}
	if p.runtimeOwned && p.runtimeProvider != nil {
		_ = p.runtimeProvider.Close()
	}
	if p.cleanup != nil {
		p.cleanup()
		p.cleanup = nil
	}
}

func prepareHostedWorkflowProviderLaunch(ctx context.Context, name string, entry *config.ProviderEntry, node yaml.Node, deps Deps) (*hostedWorkflowProviderLaunch, error) {
	runtimeConfig, runtimeProvider, runtimeOwned, err := effectiveConfiguredHostedRuntime(ctx, "providers.workflow."+name, entry, deps)
	if err != nil {
		return nil, err
	}
	if runtimeProvider == nil {
		return nil, fmt.Errorf("workflow provider: runtime is required")
	}
	runtimeSupport, err := runtimeProvider.Support(ctx)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("query %s support: %w", hostedRuntimeLabel(runtimeConfig), err)
	}
	requiresHostnameEgress := deps.Egress.ProviderPolicy(entry).RequiresHostnameEnforcement()
	runtimePlan := buildHostedRuntimePlan(runtimeSupport, deps, true, requiresHostnameEgress)
	if err := runtimePlan.Validate(hostedRuntimeLabel(runtimeConfig)); err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}

	cfg, err := componentprovider.DecodeYAMLConfig(node, "workflow provider")
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup := func() {}
	if !hostedRuntimeUsesImageEntrypoint(runtimeConfig) {
		prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
			Kind:                 providermanifestv1.KindWorkflow,
			Subject:              "workflow provider",
			SourceMissingMessage: "no Go, Rust, Python, or TypeScript workflow provider source package found",
			Config:               cfg,
		})
		if err != nil {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, err
		}
		cfg = prepared.YAMLConfig
		cleanup = prepared.Cleanup
	}
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	launch, err := prepareHostedProcessLaunch(providermanifestv1.KindWorkflow, name, entry, cfg.Command, cfg.Args, cleanup, runtimeConfig)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup = launch.cleanup

	preparedLaunch := &hostedWorkflowProviderLaunch{
		name:            name,
		runtimeConfig:   runtimeConfig,
		runtimeProvider: runtimeProvider,
		runtimeOwned:    runtimeOwned,
		runtimePlan:     runtimePlan,
		cfg:             cfg,
		allowedHosts:    entry.EffectiveAllowedHosts(),
		launch:          launch,
		cleanup:         cleanup,
	}
	cleanup = nil
	return preparedLaunch, nil
}

func startHostedWorkflowProviderInstance(ctx context.Context, launch *hostedWorkflowProviderLaunch, hostServices []runtimehost.HostService, deps Deps, closeRuntime bool, cleanup func(), stopTimeout time.Duration) (*hostedWorkflowProviderInstance, error) {
	if launch == nil {
		return nil, fmt.Errorf("hosted workflow launch is required")
	}
	runtimeProvider := launch.runtimeProvider
	if runtimeProvider == nil {
		return nil, fmt.Errorf("workflow provider: runtime is required")
	}
	name := launch.name
	session, err := runtimeProvider.StartSession(ctx, buildHostedRuntimeStartSessionRequest(providermanifestv1.KindWorkflow, name, launch.runtimeConfig))
	if err != nil {
		if closeRuntime {
			_ = runtimeProvider.Close()
		}
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("start workflow runtime session: %w", err)
	}
	sessionID := session.ID
	stopSession := true
	closeOnFailure := closeRuntime
	defer func() {
		if !stopSession {
			return
		}
		_ = stopPluginRuntimeSessionWithTimeout(runtimeProvider, sessionID, stopTimeout)
		if closeOnFailure {
			_ = runtimeProvider.Close()
		}
		if cleanup != nil {
			cleanup()
		}
	}()
	readySession, err := waitForPluginRuntimeSessionReady(ctx, runtimeProvider, sessionID)
	if err != nil {
		return nil, fmt.Errorf("wait for hosted workflow runtime session %q ready: %w", sessionID, err)
	}

	startEnv := withRuntimeSessionEnv(maps.Clone(launch.cfg.Env), sessionID)
	startEnv = withHostServiceTLSCAEnv(startEnv, deps)
	workflowAllowedHosts := launch.cfg.EgressPolicy("").AllowedHosts
	if len(workflowAllowedHosts) == 0 {
		workflowAllowedHosts = append([]string(nil), launch.allowedHosts...)
	}
	allowedHosts := hostedWorkflowAllowedHosts(workflowAllowedHosts, launch.runtimePlan)
	for _, hostService := range hostServiceBindingDescriptorsFromConfigured(hostServices) {
		bindingEnv, relayHost, err := buildHostedRuntimeHostServiceEnv(name, sessionID, hostService, deps)
		if err != nil {
			return nil, err
		}
		if len(bindingEnv) > 0 {
			if startEnv == nil {
				startEnv = make(map[string]string, len(bindingEnv))
			}
			maps.Copy(startEnv, bindingEnv)
		}
		if launch.runtimePlan.RequiresHostnameEgress {
			allowedHosts = appendAllowedHost(allowedHosts, relayHost)
		}
	}
	egressPlan, err := buildHostedRuntimeEgressLaunchPlan(name, sessionID, deps.Egress.Policy(workflowAllowedHosts), allowedHosts, launch.runtimePlan, deps)
	if err != nil {
		return nil, err
	}
	if len(egressPlan.Env) > 0 {
		if startEnv == nil {
			startEnv = make(map[string]string, len(egressPlan.Env))
		}
		maps.Copy(startEnv, egressPlan.Env)
	}

	hostedPlugin, err := runtimeProvider.StartPlugin(ctx, pluginruntime.StartPluginRequest{
		SessionID:  sessionID,
		PluginName: name,
		Command:    launch.launch.command,
		Args:       launch.launch.args,
		Env:        startEnv,
		Egress: pluginruntime.RuntimeEgressPolicy{
			AllowedHosts:  egressPlan.RuntimeAllowedHosts,
			DefaultAction: pluginruntime.PolicyAction(deps.Egress.DefaultAction),
		},
		HostBinary: launch.cfg.HostBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("start hosted workflow provider: %w", err)
	}
	conn, err := pluginruntime.DialHostedWorkflow(ctx, hostedPlugin.DialTarget,
		pluginruntime.WithProviderName(name),
		pluginruntime.WithTelemetry(deps.Telemetry),
	)
	if err != nil {
		return nil, fmt.Errorf("dial hosted workflow provider: %w", err)
	}
	provider, err := workflowservice.NewRemote(ctx, workflowservice.RemoteConfig{
		Client:  conn.Workflow(),
		Runtime: conn.Lifecycle(),
		Closer: &runtimeBackedHostedCloser{
			conn:         conn,
			runtime:      runtimeProvider,
			sessionID:    sessionID,
			closeRuntime: closeRuntime,
			cleanup:      cleanup,
			stopTimeout:  stopTimeout,
		},
		Config: launch.cfg.Config,
		Name:   name,
	})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	stopSession = false
	closeOnFailure = false
	cleanup = nil
	return &hostedWorkflowProviderInstance{
		provider:         provider,
		runtimeProvider:  runtimeProvider,
		runtimeSessionID: sessionID,
		runtimeSession:   readySession,
	}, nil
}

func hostedWorkflowAllowedHosts(configured []string, runtimePlan HostedRuntimePlan) []string {
	return hostedAgentAllowedHosts(configured, runtimePlan)
}

type hostedWorkflowProviderPool struct {
	coreworkflow.Provider

	executionRefs coreworkflow.ExecutionReferenceStore

	name         string
	launch       *hostedWorkflowProviderLaunch
	hostServices []runtimehost.HostService
	deps         Deps
	policy       config.HostedRuntimeLifecyclePolicy

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu             sync.Mutex
	nextID         int
	starting       int
	started        bool
	controlStarted bool
	closed         bool
	workers        []*hostedWorkflowWorker
}

type hostedWorkflowWorker struct {
	id               int
	provider         coreworkflow.Provider
	runtimeProvider  pluginruntime.Provider
	runtimeSessionID string
	runtimeSession   *pluginruntime.Session
	startedAt        time.Time
	runtimeDrainAt   *time.Time
	forceCloseAt     *time.Time
	active           int
	draining         bool
	closing          bool
	closed           bool
}

func newHostedWorkflowProviderPool(ctx context.Context, launch *hostedWorkflowProviderLaunch, hostServices []runtimehost.HostService, deps Deps, policy config.HostedRuntimeLifecyclePolicy) (coreworkflow.Provider, error) {
	if launch == nil {
		return nil, fmt.Errorf("hosted workflow launch is required")
	}
	control, err := startHostedWorkflowProviderInstance(ctx, launch, hostServices, deps, false, nil, 0)
	if err != nil {
		launch.close()
		return nil, err
	}
	executionRefs, _ := control.provider.(coreworkflow.ExecutionReferenceStore)
	poolCtx, cancel := context.WithCancel(context.Background())
	return &hostedWorkflowProviderPool{
		Provider:      control.provider,
		executionRefs: executionRefs,
		name:          launch.name,
		launch:        launch,
		hostServices:  append([]runtimehost.HostService(nil), hostServices...),
		deps:          deps,
		policy:        policy,
		ctx:           poolCtx,
		cancel:        cancel,
	}, nil
}

func (p *hostedWorkflowProviderPool) Start(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("hosted workflow provider %q is closed", p.name)
	}
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.mu.Unlock()

	if err := p.startControl(ctx); err != nil {
		p.markStartFailed()
		return err
	}
	if err := p.ensureMinReady(ctx); err != nil {
		p.markStartFailed()
		return err
	}
	if p.policy.RestartPolicy != config.HostedRuntimeRestartPolicyNever {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.healthLoop()
		}()
	} else {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.runtimeSessionLoop()
		}()
	}
	return nil
}

func (p *hostedWorkflowProviderPool) startControl(ctx context.Context) error {
	p.mu.Lock()
	if p.controlStarted {
		p.mu.Unlock()
		return nil
	}
	provider := p.Provider
	p.mu.Unlock()

	if provider == nil {
		return fmt.Errorf("hosted workflow provider %q has no control provider", p.name)
	}
	if starter, ok := provider.(startableWorkflowProvider); ok {
		if err := starter.Start(ctx); err != nil {
			return fmt.Errorf("start hosted workflow control provider: %w", err)
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("hosted workflow provider %q is closed", p.name)
	}
	p.controlStarted = true
	return nil
}

func (p *hostedWorkflowProviderPool) markStartFailed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.started = false
	}
}

func (p *hostedWorkflowProviderPool) Ping(ctx context.Context) error {
	var joined []error
	if p.Provider == nil {
		joined = append(joined, fmt.Errorf("hosted workflow provider %q has no control provider", p.name))
	} else if err := p.Provider.Ping(ctx); err != nil {
		joined = append(joined, fmt.Errorf("control provider: %w", err))
	}
	if !p.isStarted() {
		return errors.Join(joined...)
	}
	workers := p.readyWorkers()
	if len(workers) == 0 {
		joined = append(joined, fmt.Errorf("hosted workflow provider %q has no ready runtime workers", p.name))
		return errors.Join(joined...)
	}
	errs := make(chan error, len(workers))
	var wg sync.WaitGroup
	for _, worker := range workers {
		wg.Add(1)
		go func(worker *hostedWorkflowWorker) {
			defer wg.Done()
			if !p.acquireWorker(worker) {
				return
			}
			if err := worker.provider.Ping(ctx); err != nil {
				errs <- fmt.Errorf("runtime worker %d: %w", worker.id, err)
			}
			p.releaseWorker(worker)
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}

func (p *hostedWorkflowProviderPool) PutExecutionReference(ctx context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	store, err := p.executionReferenceStore()
	if err != nil {
		return nil, err
	}
	return store.PutExecutionReference(ctx, ref)
}

func (p *hostedWorkflowProviderPool) GetExecutionReference(ctx context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	store, err := p.executionReferenceStore()
	if err != nil {
		return nil, err
	}
	return store.GetExecutionReference(ctx, id)
}

func (p *hostedWorkflowProviderPool) ListExecutionReferences(ctx context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	store, err := p.executionReferenceStore()
	if err != nil {
		return nil, err
	}
	return store.ListExecutionReferences(ctx, subjectID)
}

func (p *hostedWorkflowProviderPool) executionReferenceStore() (coreworkflow.ExecutionReferenceStore, error) {
	if p == nil || p.executionRefs == nil {
		name := ""
		if p != nil {
			name = p.name
		}
		return nil, fmt.Errorf("hosted workflow provider %q does not expose execution references", name)
	}
	return p.executionRefs, nil
}

func (p *hostedWorkflowProviderPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	workers := append([]*hostedWorkflowWorker(nil), p.workers...)
	for _, worker := range workers {
		if worker != nil && !worker.closed {
			worker.draining = true
		}
	}
	p.mu.Unlock()
	p.cancel()

	errs := make(chan error, len(workers))
	var wg sync.WaitGroup
	for _, worker := range workers {
		wg.Add(1)
		go func(worker *hostedWorkflowWorker) {
			defer wg.Done()
			errs <- p.drainAndCloseWorker(worker)
		}(worker)
	}
	wg.Wait()
	close(errs)
	p.wg.Wait()
	var closeErrs []error
	for err := range errs {
		if err != nil {
			closeErrs = append(closeErrs, err)
		}
	}
	p.mu.Lock()
	p.workers = nil
	p.mu.Unlock()
	if p.Provider != nil {
		closeErrs = append(closeErrs, p.Provider.Close())
	}
	if p.launch != nil {
		p.launch.close()
	}
	return errors.Join(closeErrs...)
}

func (p *hostedWorkflowProviderPool) isStarted() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started
}

func (p *hostedWorkflowProviderPool) startWorker(ctx context.Context) (*hostedWorkflowWorker, error) {
	startCtx, cancel := context.WithTimeout(ctx, p.policy.StartupTimeout)
	defer cancel()
	instance, err := startHostedWorkflowProviderInstance(startCtx, p.launch, p.hostServices, p.deps, false, nil, p.policy.DrainTimeout)
	if err != nil {
		return nil, err
	}
	if starter, ok := instance.provider.(startableWorkflowProvider); ok {
		if err := starter.Start(startCtx); err != nil {
			_ = instance.provider.Close()
			return nil, fmt.Errorf("start hosted workflow worker: %w", err)
		}
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = instance.provider.Close()
		return nil, fmt.Errorf("hosted workflow provider %q is closed", p.name)
	}
	p.nextID++
	now := time.Now().UTC()
	worker := &hostedWorkflowWorker{
		id:               p.nextID,
		provider:         instance.provider,
		runtimeProvider:  instance.runtimeProvider,
		runtimeSessionID: instance.runtimeSessionID,
		runtimeSession:   instance.runtimeSession,
		startedAt:        now,
		runtimeDrainAt:   p.runtimeSessionDrainAt(instance.runtimeSession, now),
		forceCloseAt:     runtimeSessionExpiresAt(instance.runtimeSession),
	}
	p.workers = append(p.workers, worker)
	p.mu.Unlock()
	return worker, nil
}

func (p *hostedWorkflowProviderPool) ensureMinReady(ctx context.Context) error {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil
		}
		ready := 0
		now := time.Now().UTC()
		for _, worker := range p.workers {
			if p.workerAvailableLocked(worker, now) {
				ready++
			}
		}
		if ready+p.starting >= p.policy.MinReadyInstances {
			p.mu.Unlock()
			return nil
		}
		p.starting++
		p.mu.Unlock()

		_, err := p.startWorker(ctx)
		p.mu.Lock()
		p.starting--
		p.mu.Unlock()
		if err != nil {
			slog.Warn("failed to start hosted workflow runtime worker", "provider", p.name, "error", err)
			return err
		}
	}
}

func (p *hostedWorkflowProviderPool) healthLoop() {
	ticker := time.NewTicker(p.policy.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		}
		_ = p.ensureMinReady(p.ctx)
		for _, worker := range p.readyWorkers() {
			session, drainAt, err := p.refreshWorkerRuntimeSession(worker)
			if err != nil {
				slog.Warn("hosted workflow runtime session refresh failed", "provider", p.name, "worker", worker.id, "error", err)
				p.replaceWorker(worker)
				continue
			}
			if reason := p.runtimeSessionRetirementReason(session, drainAt, time.Now().UTC()); reason != "" {
				slog.Info("retiring hosted workflow runtime worker", "provider", p.name, "worker", worker.id, "reason", reason)
				p.replaceWorker(worker)
				continue
			}
			if !p.acquireWorker(worker) {
				continue
			}
			err = worker.provider.Ping(p.ctx)
			p.releaseWorker(worker)
			if err != nil {
				slog.Warn("hosted workflow runtime worker failed health check", "provider", p.name, "worker", worker.id, "error", err)
				p.replaceWorker(worker)
			}
		}
	}
}

func (p *hostedWorkflowProviderPool) runtimeSessionLoop() {
	interval := p.policy.HealthCheckInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		}
		_ = p.ensureMinReady(p.ctx)
		for _, worker := range p.runtimeManagedWorkers() {
			session, drainAt, err := p.refreshWorkerRuntimeSession(worker)
			if err != nil {
				slog.Warn("hosted workflow runtime session refresh failed", "provider", p.name, "worker", worker.id, "error", err)
				p.replaceWorkerAllowNever(worker, err.Error(), true)
				continue
			}
			if reason := p.runtimeSessionRetirementReason(session, drainAt, time.Now().UTC()); reason != "" {
				slog.Info("retiring hosted workflow runtime worker", "provider", p.name, "worker", worker.id, "reason", reason)
				p.replaceWorkerAllowNever(worker, reason, true)
			}
		}
	}
}

func (p *hostedWorkflowProviderPool) readyWorkers() []*hostedWorkflowWorker {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	out := make([]*hostedWorkflowWorker, 0, len(p.workers))
	for _, worker := range p.workers {
		if p.workerAvailableLocked(worker, now) {
			out = append(out, worker)
		}
	}
	return out
}

func (p *hostedWorkflowProviderPool) runtimeManagedWorkers() []*hostedWorkflowWorker {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*hostedWorkflowWorker, 0, len(p.workers))
	for _, worker := range p.workers {
		if worker != nil && worker.provider != nil && !worker.closed && !worker.closing {
			out = append(out, worker)
		}
	}
	return out
}

func (p *hostedWorkflowProviderPool) workerAvailableLocked(worker *hostedWorkflowWorker, now time.Time) bool {
	if worker == nil || worker.provider == nil || worker.closed || worker.closing || worker.draining {
		return false
	}
	if hostedRuntimeSessionCompatibilityReason(worker.runtimeSession) != "" {
		return false
	}
	if worker.forceCloseAt != nil && !now.Before(*worker.forceCloseAt) {
		return false
	}
	return true
}

func (p *hostedWorkflowProviderPool) acquireWorker(worker *hostedWorkflowWorker) bool {
	if worker == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.workerAvailableLocked(worker, time.Now().UTC()) {
		return false
	}
	worker.active++
	return true
}

func (p *hostedWorkflowProviderPool) releaseWorker(worker *hostedWorkflowWorker) {
	if worker == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if worker.active > 0 {
		worker.active--
	}
}

func (p *hostedWorkflowProviderPool) refreshWorkerRuntimeSession(worker *hostedWorkflowWorker) (*pluginruntime.Session, *time.Time, error) {
	if worker == nil {
		return nil, nil, fmt.Errorf("runtime worker is unavailable")
	}
	p.mu.Lock()
	runtimeProvider := worker.runtimeProvider
	sessionID := worker.runtimeSessionID
	p.mu.Unlock()
	if runtimeProvider == nil || sessionID == "" {
		return nil, nil, nil
	}
	timeout := p.policy.HealthCheckInterval
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()
	session, err := runtimeProvider.GetSession(ctx, pluginruntime.GetSessionRequest{SessionID: sessionID})
	if err != nil {
		return nil, nil, fmt.Errorf("get runtime session %q: %w", sessionID, err)
	}
	drainAt := p.runtimeSessionDrainAt(session, time.Now().UTC())
	p.mu.Lock()
	if p.workerAvailableLocked(worker, time.Now().UTC()) {
		if worker.runtimeDrainAt != nil && (drainAt == nil || worker.runtimeDrainAt.Before(*drainAt)) {
			drainAt = cloneTime(worker.runtimeDrainAt)
		}
		worker.runtimeSession = session
		worker.runtimeDrainAt = cloneTime(drainAt)
		worker.forceCloseAt = runtimeSessionExpiresAt(session)
	}
	p.mu.Unlock()
	return session, drainAt, nil
}

func (p *hostedWorkflowProviderPool) replaceWorker(worker *hostedWorkflowWorker) {
	p.replaceWorkerAllowNever(worker, "", false)
}

func (p *hostedWorkflowProviderPool) replaceWorkerAllowNever(worker *hostedWorkflowWorker, reason string, allowRestartPolicyNever bool) {
	if (p.policy.RestartPolicy == config.HostedRuntimeRestartPolicyNever && !allowRestartPolicyNever) || !p.markWorkerDraining(worker) {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.ensureMinReady(p.ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("failed to replace hosted workflow runtime worker", "provider", p.name, "worker", worker.id, "reason", reason, "error", err)
		}
		if err := p.drainAndCloseWorker(worker); err != nil {
			slog.Warn("failed to close hosted workflow runtime worker", "provider", p.name, "worker", worker.id, "reason", reason, "error", err)
		}
	}()
}

func (p *hostedWorkflowProviderPool) markWorkerDraining(worker *hostedWorkflowWorker) bool {
	if worker == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || worker.draining || worker.closed {
		return false
	}
	worker.draining = true
	return true
}

func (p *hostedWorkflowProviderPool) drainAndCloseWorker(worker *hostedWorkflowWorker) error {
	if worker == nil {
		return nil
	}
	p.mu.Lock()
	if worker.closed || worker.closing {
		p.mu.Unlock()
		return nil
	}
	worker.closing = true
	worker.draining = true
	p.mu.Unlock()

	deadline := time.Now().Add(p.policy.DrainTimeout)
	p.mu.Lock()
	if worker.forceCloseAt != nil && worker.forceCloseAt.Before(deadline) {
		deadline = worker.forceCloseAt.UTC()
	}
	p.mu.Unlock()

	for {
		p.mu.Lock()
		active := worker.active
		if active == 0 || time.Now().After(deadline) {
			worker.closed = true
			p.removeWorkerLocked(worker)
			p.mu.Unlock()
			break
		}
		p.mu.Unlock()
		time.Sleep(25 * time.Millisecond)
	}

	return worker.provider.Close()
}

func (p *hostedWorkflowProviderPool) removeWorkerLocked(worker *hostedWorkflowWorker) {
	for i, candidate := range p.workers {
		if candidate == worker {
			p.workers = append(p.workers[:i], p.workers[i+1:]...)
			return
		}
	}
}

func (p *hostedWorkflowProviderPool) runtimeSessionDrainAt(session *pluginruntime.Session, now time.Time) *time.Time {
	if session == nil || session.Lifecycle == nil {
		return nil
	}
	var drainAt *time.Time
	if session.Lifecycle.RecommendedDrainAt != nil {
		recommended := session.Lifecycle.RecommendedDrainAt.UTC()
		drainAt = &recommended
	}
	if session.Lifecycle.ExpiresAt != nil {
		expiryDrain := p.runtimeSessionExpiryDrainAt(session.Lifecycle, now)
		if drainAt == nil || expiryDrain.Before(*drainAt) {
			drainAt = &expiryDrain
		}
	}
	return drainAt
}

func (p *hostedWorkflowProviderPool) runtimeSessionExpiryDrainAt(lifecycle *pluginruntime.SessionLifecycle, now time.Time) time.Time {
	expiresAt := lifecycle.ExpiresAt.UTC()
	reserve := p.policy.StartupTimeout + p.policy.DrainTimeout + p.policy.HealthCheckInterval + hostedWorkflowRuntimeLifecycleSafetyMargin
	drainAt := expiresAt.Add(-reserve).UTC()
	if lifecycle.StartedAt != nil {
		startedAt := lifecycle.StartedAt.UTC()
		lifetime := expiresAt.Sub(startedAt)
		if lifetime > 0 {
			minDrainAt := startedAt.Add(lifetime / 2).UTC()
			if drainAt.Before(minDrainAt) {
				return minDrainAt
			}
		}
	}
	if !now.IsZero() && expiresAt.After(now) && drainAt.Before(now) {
		return now.Add(expiresAt.Sub(now) / 2).UTC()
	}
	return drainAt
}

func (p *hostedWorkflowProviderPool) runtimeSessionRetirementReason(session *pluginruntime.Session, drainAt *time.Time, now time.Time) string {
	if session == nil {
		return ""
	}
	switch session.State {
	case pluginruntime.SessionStateFailed, pluginruntime.SessionStateStopped:
		return fmt.Sprintf("runtime session entered %q state", session.State)
	}
	if reason := hostedRuntimeSessionCompatibilityReason(session); reason != "" {
		return reason
	}
	if drainAt == nil || now.Before(*drainAt) {
		return ""
	}
	if expiresAt := runtimeSessionExpiresAt(session); expiresAt != nil && !now.Before(*expiresAt) {
		return fmt.Sprintf("runtime session expired at %s", expiresAt.Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("runtime session reached drain deadline %s", drainAt.Format(time.RFC3339Nano))
}

var _ coreworkflow.Provider = (*hostedWorkflowProviderPool)(nil)
var _ coreworkflow.ExecutionReferenceStore = (*hostedWorkflowProviderPool)(nil)
