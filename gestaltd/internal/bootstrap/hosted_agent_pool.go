package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const hostedAgentRuntimeLifecycleSafetyMargin = 5 * time.Second

type hostedAgentProviderPool struct {
	name         string
	launch       *hostedAgentProviderLaunch
	hostServices []runtimehost.HostService
	deps         Deps
	policy       config.HostedRuntimeLifecyclePolicy

	ctx         context.Context
	cancel      context.CancelFunc
	lifecycleWG sync.WaitGroup

	mu                  sync.Mutex
	nextID              int
	nextPick            int
	starting            int
	closed              bool
	backends            []*hostedAgentPoolBackend
	sessionBackends     map[string]*hostedAgentPoolBackend
	turnBackends        map[string]*hostedAgentPoolBackend
	interactionBackends map[string]*hostedAgentPoolBackend
}

type hostedAgentPoolBackend struct {
	id               int
	provider         coreagent.Provider
	runtimeProvider  pluginruntime.Provider
	runtimeSessionID string
	runtimeSession   *pluginruntime.Session
	startedAt        time.Time
	runtimeDrainAt   *time.Time
	forceCloseAt     *time.Time
	active           int
	liveTurns        map[string]struct{}
	draining         bool
	replacing        bool
	closing          bool
	closed           bool
}

func newHostedAgentProviderPool(ctx context.Context, launch *hostedAgentProviderLaunch, hostServices []runtimehost.HostService, deps Deps, policy config.HostedRuntimeLifecyclePolicy) (coreagent.Provider, error) {
	if launch == nil {
		return nil, fmt.Errorf("hosted agent launch is required")
	}
	poolCtx, cancel := context.WithCancel(context.Background())
	pool := &hostedAgentProviderPool{
		name:                launch.name,
		launch:              launch,
		hostServices:        append([]runtimehost.HostService(nil), hostServices...),
		deps:                deps,
		policy:              policy,
		ctx:                 poolCtx,
		cancel:              cancel,
		sessionBackends:     map[string]*hostedAgentPoolBackend{},
		turnBackends:        map[string]*hostedAgentPoolBackend{},
		interactionBackends: map[string]*hostedAgentPoolBackend{},
	}
	for i := 0; i < policy.MinReadyInstances; i++ {
		if _, err := pool.startBackend(ctx); err != nil {
			_ = pool.Close()
			return nil, err
		}
	}
	if policy.RestartPolicy != config.HostedRuntimeRestartPolicyNever {
		pool.lifecycleWG.Add(1)
		go func() {
			defer pool.lifecycleWG.Done()
			pool.healthLoop()
		}()
	}
	return pool, nil
}

func (p *hostedAgentProviderPool) CreateSession(ctx context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	preferred := p.sessionBackend(req.SessionID)
	backend, release, err := p.acquireBackendForNewWork(ctx, preferred, preferred != nil)
	if err != nil {
		return nil, err
	}
	session, err := backend.provider.CreateSession(ctx, req)
	release()
	p.maybeProbeAfterCallError(backend, err)
	if err != nil {
		return nil, err
	}
	if session != nil {
		p.recordSession(session, backend)
	} else {
		p.recordSessionBackend(req.SessionID, backend)
	}
	return session, nil
}

func (p *hostedAgentProviderPool) GetSession(ctx context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	var retryableErr error
	var lastNotFound error
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := p.sessionBackend(req.SessionID); backend != nil {
		tried[backend] = struct{}{}
		session, err := p.withBackendSession(ctx, backend, req)
		switch {
		case err == nil:
			p.recordSession(session, backend)
			return session, nil
		case errors.Is(err, core.ErrNotFound):
			p.deleteSessionBackend(req.SessionID)
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		session, err := p.withBackendSession(ctx, backend, req)
		if err == nil {
			p.recordSession(session, backend)
			return session, nil
		}
		if errors.Is(err, core.ErrNotFound) {
			lastNotFound = err
			continue
		}
		if isHostedAgentReadRetryableError(err) {
			retryableErr = err
			continue
		}
		return nil, err
	}
	if retryableErr != nil {
		return nil, retryableErr
	}
	if lastNotFound != nil {
		return nil, lastNotFound
	}
	return nil, core.ErrNotFound
}

func (p *hostedAgentProviderPool) ListSessions(ctx context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	var out []*coreagent.Session
	seenSessionIDs := map[string]struct{}{}
	var retryableErr error
	succeeded := false
	for _, backend := range p.availableBackends(true) {
		acquired, release, err := p.acquireBackend(ctx, backend, true)
		if err != nil {
			if isHostedAgentReadRetryableError(err) {
				retryableErr = err
				continue
			}
			return nil, err
		}
		sessions, err := acquired.provider.ListSessions(ctx, req)
		release()
		p.maybeProbeAfterCallError(acquired, err)
		if err != nil {
			if isHostedAgentReadRetryableError(err) {
				retryableErr = err
				continue
			}
			return nil, err
		}
		succeeded = true
		for _, session := range sessions {
			sessionID := strings.TrimSpace(sessionIDForSession(session))
			if sessionID != "" {
				if _, ok := seenSessionIDs[sessionID]; ok {
					continue
				}
				seenSessionIDs[sessionID] = struct{}{}
			}
			p.recordSession(session, acquired)
			out = append(out, session)
		}
	}
	if !succeeded && retryableErr != nil {
		return nil, retryableErr
	}
	return out, nil
}

func (p *hostedAgentProviderPool) UpdateSession(ctx context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	backend := p.sessionBackend(req.SessionID)
	if backend == nil {
		session, err := p.GetSession(ctx, coreagent.GetSessionRequest{SessionID: req.SessionID})
		if err != nil {
			return nil, err
		}
		backend = p.sessionBackend(sessionIDForSession(session))
	}
	if backend == nil {
		return nil, core.ErrNotFound
	}
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	session, err := acquired.provider.UpdateSession(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err == nil {
		p.recordSession(session, acquired)
	}
	return session, err
}

func (p *hostedAgentProviderPool) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	preferred := p.turnBackend(req.TurnID)
	allowDraining := preferred != nil
	if preferred == nil {
		preferred = p.sessionBackend(req.SessionID)
	}
	backend, release, err := p.acquireBackendForNewWork(ctx, preferred, allowDraining)
	if err != nil {
		return nil, err
	}
	turn, err := backend.provider.CreateTurn(ctx, req)
	release()
	p.maybeProbeAfterCallError(backend, err)
	if err != nil {
		return nil, err
	}
	if turn != nil {
		p.recordTurn(turn, backend)
	} else {
		p.recordTurnBackend(req.TurnID, backend)
	}
	return turn, nil
}

func (p *hostedAgentProviderPool) GetTurn(ctx context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	var retryableErr error
	var lastNotFound error
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := p.turnBackend(req.TurnID); backend != nil {
		tried[backend] = struct{}{}
		turn, err := p.withBackendTurn(ctx, backend, req)
		switch {
		case err == nil:
			p.recordTurn(turn, backend)
			return turn, nil
		case errors.Is(err, core.ErrNotFound):
			p.deleteTurnBackend(req.TurnID)
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		turn, err := p.withBackendTurn(ctx, backend, req)
		if err == nil {
			p.recordTurn(turn, backend)
			return turn, nil
		}
		if errors.Is(err, core.ErrNotFound) {
			lastNotFound = err
			continue
		}
		if isHostedAgentReadRetryableError(err) {
			retryableErr = err
			continue
		}
		return nil, err
	}
	if retryableErr != nil {
		return nil, retryableErr
	}
	if lastNotFound != nil {
		return nil, lastNotFound
	}
	return nil, core.ErrNotFound
}

func (p *hostedAgentProviderPool) ListTurns(ctx context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	var retryableErr error
	var lastNotFound error
	succeeded := false
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := p.sessionBackend(req.SessionID); backend != nil {
		tried[backend] = struct{}{}
		turns, err := p.listTurnsFromBackend(ctx, backend, req)
		if err == nil {
			return turns, nil
		}
		switch {
		case errors.Is(err, core.ErrNotFound):
			p.deleteSessionBackend(req.SessionID)
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	var out []*coreagent.Turn
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		turns, err := p.listTurnsFromBackend(ctx, backend, req)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				lastNotFound = err
				continue
			}
			if isHostedAgentReadRetryableError(err) {
				retryableErr = err
				continue
			}
			return nil, err
		}
		succeeded = true
		out = append(out, turns...)
	}
	if !succeeded && retryableErr != nil {
		return nil, retryableErr
	}
	if !succeeded && lastNotFound != nil {
		return nil, lastNotFound
	}
	return out, nil
}

func (p *hostedAgentProviderPool) CancelTurn(ctx context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	backend := p.turnBackend(req.TurnID)
	if backend == nil {
		turn, err := p.GetTurn(ctx, coreagent.GetTurnRequest{TurnID: req.TurnID})
		if err != nil {
			return nil, err
		}
		backend = p.turnBackend(turnIDForTurn(turn))
	}
	if backend == nil {
		return nil, core.ErrNotFound
	}
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	turn, err := acquired.provider.CancelTurn(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err == nil {
		p.recordTurn(turn, acquired)
	}
	return turn, err
}

func (p *hostedAgentProviderPool) ListTurnEvents(ctx context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	var retryableErr error
	var lastNotFound error
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := p.turnBackend(req.TurnID); backend != nil {
		tried[backend] = struct{}{}
		events, err := p.listTurnEventsFromBackend(ctx, backend, req)
		if err == nil {
			return events, nil
		}
		switch {
		case errors.Is(err, core.ErrNotFound):
			p.deleteTurnBackend(req.TurnID)
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		events, err := p.listTurnEventsFromBackend(ctx, backend, req)
		if err == nil {
			return events, nil
		}
		if errors.Is(err, core.ErrNotFound) {
			lastNotFound = err
			continue
		}
		if isHostedAgentReadRetryableError(err) {
			retryableErr = err
			continue
		}
		return nil, err
	}
	if retryableErr != nil {
		return nil, retryableErr
	}
	if lastNotFound != nil {
		return nil, lastNotFound
	}
	return nil, core.ErrNotFound
}

func (p *hostedAgentProviderPool) GetInteraction(ctx context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	var retryableErr error
	var lastNotFound error
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := p.interactionBackend(req.InteractionID); backend != nil {
		tried[backend] = struct{}{}
		interaction, err := p.getInteractionFromBackend(ctx, backend, req)
		switch {
		case err == nil:
			p.recordInteraction(interaction, backend)
			return interaction, nil
		case errors.Is(err, core.ErrNotFound):
			p.deleteInteractionBackend(req.InteractionID)
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		interaction, err := p.getInteractionFromBackend(ctx, backend, req)
		if err == nil {
			p.recordInteraction(interaction, backend)
			return interaction, nil
		}
		if errors.Is(err, core.ErrNotFound) {
			lastNotFound = err
			continue
		}
		if isHostedAgentReadRetryableError(err) {
			retryableErr = err
			continue
		}
		return nil, err
	}
	if retryableErr != nil {
		return nil, retryableErr
	}
	if lastNotFound != nil {
		return nil, lastNotFound
	}
	return nil, core.ErrNotFound
}

func (p *hostedAgentProviderPool) ListInteractions(ctx context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	var retryableErr error
	var lastNotFound error
	succeeded := false
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := p.turnBackend(req.TurnID); backend != nil {
		tried[backend] = struct{}{}
		interactions, err := p.listInteractionsFromBackend(ctx, backend, req)
		if err == nil {
			return interactions, nil
		}
		switch {
		case errors.Is(err, core.ErrNotFound):
			p.deleteTurnBackend(req.TurnID)
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	var out []*coreagent.Interaction
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		interactions, err := p.listInteractionsFromBackend(ctx, backend, req)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				lastNotFound = err
				continue
			}
			if isHostedAgentReadRetryableError(err) {
				retryableErr = err
				continue
			}
			return nil, err
		}
		succeeded = true
		out = append(out, interactions...)
	}
	if !succeeded && retryableErr != nil {
		return nil, retryableErr
	}
	if !succeeded && lastNotFound != nil {
		return nil, lastNotFound
	}
	return out, nil
}

func (p *hostedAgentProviderPool) ResolveInteraction(ctx context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	backend := p.interactionBackend(req.InteractionID)
	if backend == nil {
		interaction, err := p.GetInteraction(ctx, coreagent.GetInteractionRequest{InteractionID: req.InteractionID})
		if err != nil {
			return nil, err
		}
		backend = p.interactionBackend(interactionIDForInteraction(interaction))
	}
	if backend == nil {
		return nil, core.ErrNotFound
	}
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	interaction, err := acquired.provider.ResolveInteraction(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err == nil {
		p.recordInteraction(interaction, acquired)
	}
	return interaction, err
}

func (p *hostedAgentProviderPool) GetCapabilities(ctx context.Context, req coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	backend, release, err := p.acquireBackend(ctx, nil, false)
	if err != nil {
		return nil, err
	}
	capabilities, err := backend.provider.GetCapabilities(ctx, req)
	release()
	p.maybeProbeAfterCallError(backend, err)
	return capabilities, err
}

func (p *hostedAgentProviderPool) Ping(ctx context.Context) error {
	backends := p.readyBackends()
	if len(backends) == 0 {
		return p.unavailableError(fmt.Errorf("hosted agent provider %q has no ready runtime instances", p.name))
	}
	errs := make(chan error, len(backends))
	var wg sync.WaitGroup
	for _, backend := range backends {
		wg.Add(1)
		go func(backend *hostedAgentPoolBackend) {
			defer wg.Done()
			acquired, release, err := p.acquireBackend(ctx, backend, true)
			if err != nil {
				errs <- err
				return
			}
			defer release()
			err = acquired.provider.Ping(ctx)
			if err != nil {
				errs <- fmt.Errorf("runtime instance %d: %w", acquired.id, err)
			}
		}(backend)
	}
	wg.Wait()
	close(errs)
	var joined []error
	for err := range errs {
		joined = append(joined, err)
	}
	return errors.Join(joined...)
}

func (p *hostedAgentProviderPool) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	backends := append([]*hostedAgentPoolBackend(nil), p.backends...)
	for _, backend := range backends {
		if backend != nil && !backend.closed {
			backend.draining = true
		}
	}
	ready, starting, draining := p.instanceCountsLocked()
	p.mu.Unlock()
	p.recordInstanceCounts(context.Background(), ready, starting, draining)
	p.cancel()

	errs := make(chan error, len(backends))
	var wg sync.WaitGroup
	for _, backend := range backends {
		wg.Add(1)
		go func(backend *hostedAgentPoolBackend) {
			defer wg.Done()
			errs <- p.drainAndCloseBackend(backend)
		}(backend)
	}
	wg.Wait()
	close(errs)
	p.lifecycleWG.Wait()
	var closeErrs []error
	for err := range errs {
		if err != nil {
			closeErrs = append(closeErrs, err)
		}
	}
	p.mu.Lock()
	p.backends = nil
	p.sessionBackends = map[string]*hostedAgentPoolBackend{}
	p.turnBackends = map[string]*hostedAgentPoolBackend{}
	p.interactionBackends = map[string]*hostedAgentPoolBackend{}
	p.mu.Unlock()
	if p.launch != nil {
		p.launch.close()
	}
	return errors.Join(closeErrs...)
}

func (p *hostedAgentProviderPool) withBackendSession(ctx context.Context, backend *hostedAgentPoolBackend, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	session, err := acquired.provider.GetSession(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	return session, err
}

func (p *hostedAgentProviderPool) withBackendTurn(ctx context.Context, backend *hostedAgentPoolBackend, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	turn, err := acquired.provider.GetTurn(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	return turn, err
}

func (p *hostedAgentProviderPool) listTurnsFromBackend(ctx context.Context, backend *hostedAgentPoolBackend, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	turns, err := acquired.provider.ListTurns(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err == nil {
		for _, turn := range turns {
			p.recordTurn(turn, acquired)
		}
	}
	return turns, err
}

func (p *hostedAgentProviderPool) listTurnEventsFromBackend(ctx context.Context, backend *hostedAgentPoolBackend, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	events, err := acquired.provider.ListTurnEvents(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	return events, err
}

func (p *hostedAgentProviderPool) getInteractionFromBackend(ctx context.Context, backend *hostedAgentPoolBackend, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	interaction, err := acquired.provider.GetInteraction(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	return interaction, err
}

func (p *hostedAgentProviderPool) listInteractionsFromBackend(ctx context.Context, backend *hostedAgentPoolBackend, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	interactions, err := acquired.provider.ListInteractions(ctx, req)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err == nil {
		for _, interaction := range interactions {
			p.recordInteraction(interaction, acquired)
		}
	}
	return interactions, err
}

func (p *hostedAgentProviderPool) acquireBackend(ctx context.Context, preferred *hostedAgentPoolBackend, allowDraining bool) (*hostedAgentPoolBackend, func(), error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, nil, fmt.Errorf("hosted agent provider %q is closed", p.name)
		}
		if preferred != nil {
			if p.backendAvailableLocked(preferred, allowDraining) {
				preferred.active++
				p.mu.Unlock()
				return preferred, p.releaseBackend(preferred), nil
			}
			p.mu.Unlock()
			return nil, nil, p.unavailableError(fmt.Errorf("hosted agent provider %q runtime instance is not ready", p.name))
		}
		if backend := p.pickReadyBackendLocked(); backend != nil {
			var ready, starting, draining int
			var scaled bool
			if backend.active > 0 && p.canScaleOutLocked() {
				ready, starting, draining, scaled = p.startScaleOutLocked()
			}
			backend.active++
			p.mu.Unlock()
			if scaled {
				p.recordInstanceCounts(ctx, ready, starting, draining)
			}
			return backend, p.releaseBackend(backend), nil
		}
		if p.canScaleOutLocked() {
			p.starting++
			ready, starting, draining := p.instanceCountsLocked()
			p.mu.Unlock()
			p.recordInstanceCounts(ctx, ready, starting, draining)
			started, startErr := p.startBackend(ctx)
			p.mu.Lock()
			p.starting--
			ready, starting, draining = p.instanceCountsLocked()
			if startErr == nil && p.backendAcceptsNewWorkLocked(started, time.Now().UTC()) {
				started.active++
				p.mu.Unlock()
				p.recordInstanceCounts(ctx, ready, starting, draining)
				return started, p.releaseBackend(started), nil
			}
			p.mu.Unlock()
			p.recordInstanceCounts(ctx, ready, starting, draining)
			if startErr != nil {
				return nil, nil, p.unavailableError(startErr)
			}
			continue
		}
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (p *hostedAgentProviderPool) acquireBackendForNewWork(ctx context.Context, preferred *hostedAgentPoolBackend, allowDraining bool) (*hostedAgentPoolBackend, func(), error) {
	if preferred != nil {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, nil, fmt.Errorf("hosted agent provider %q is closed", p.name)
		}
		now := time.Now().UTC()
		if p.backendAcceptsNewWorkLocked(preferred, now) || (allowDraining && p.backendAvailableLocked(preferred, true) && !p.backendRuntimeDrainDueLocked(preferred, now)) {
			preferred.active++
			p.mu.Unlock()
			return preferred, p.releaseBackend(preferred), nil
		}
		p.mu.Unlock()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
	}
	return p.acquireBackend(ctx, nil, false)
}

func (p *hostedAgentProviderPool) releaseBackend(backend *hostedAgentPoolBackend) func() {
	return func() {
		p.mu.Lock()
		if backend.active > 0 {
			backend.active--
		}
		p.mu.Unlock()
	}
}

func (p *hostedAgentProviderPool) pickReadyBackendLocked() *hostedAgentPoolBackend {
	now := time.Now().UTC()
	ready := make([]*hostedAgentPoolBackend, 0, len(p.backends))
	for _, backend := range p.backends {
		if p.backendAcceptsNewWorkLocked(backend, now) {
			ready = append(ready, backend)
		}
	}
	if len(ready) == 0 {
		return nil
	}
	idle := ready[:0]
	for _, backend := range ready {
		if backend.active == 0 {
			idle = append(idle, backend)
		}
	}
	if len(idle) > 0 {
		ready = idle
	}
	idx := p.nextPick % len(ready)
	p.nextPick++
	return ready[idx]
}

func (p *hostedAgentProviderPool) canScaleOutLocked() bool {
	if p.policy.MaxReadyInstances <= 0 {
		return false
	}
	now := time.Now().UTC()
	ready := 0
	for _, backend := range p.backends {
		if p.backendAcceptsNewWorkLocked(backend, now) {
			ready++
		}
	}
	return ready+p.starting < p.policy.MaxReadyInstances
}

func (p *hostedAgentProviderPool) startScaleOutLocked() (ready, starting, draining int, scaled bool) {
	if p.closed || !p.canScaleOutLocked() {
		return 0, 0, 0, false
	}
	p.starting++
	ready, starting, draining = p.instanceCountsLocked()
	p.lifecycleWG.Add(1)
	go func() {
		defer p.lifecycleWG.Done()
		_, err := p.startBackend(p.ctx)
		p.mu.Lock()
		p.starting--
		ready, starting, draining := p.instanceCountsLocked()
		p.mu.Unlock()
		p.recordInstanceCounts(p.ctx, ready, starting, draining)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("failed to scale out hosted agent runtime instance", "provider", p.name, "error", err)
		}
	}()
	return ready, starting, draining, true
}

func (p *hostedAgentProviderPool) backendAvailableLocked(backend *hostedAgentPoolBackend, allowDraining bool) bool {
	if backend == nil || backend.closed || backend.provider == nil {
		return false
	}
	if (backend.draining || backend.closing) && !allowDraining {
		return false
	}
	for _, candidate := range p.backends {
		if candidate == backend {
			return true
		}
	}
	return false
}

func (p *hostedAgentProviderPool) backendAcceptsNewWorkLocked(backend *hostedAgentPoolBackend, now time.Time) bool {
	return p.backendAvailableLocked(backend, false) && !p.backendRuntimeDrainDueLocked(backend, now)
}

func (p *hostedAgentProviderPool) backendRuntimeDrainDueLocked(backend *hostedAgentPoolBackend, now time.Time) bool {
	if backend == nil || backend.runtimeDrainAt == nil {
		return false
	}
	drainAt := backend.runtimeDrainAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return !now.Before(drainAt)
}

func (p *hostedAgentProviderPool) readyBackends() []*hostedAgentPoolBackend {
	return p.availableBackends(false)
}

func (p *hostedAgentProviderPool) availableBackends(allowDraining bool) []*hostedAgentPoolBackend {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*hostedAgentPoolBackend, 0, len(p.backends))
	for _, backend := range p.backends {
		if p.backendAvailableLocked(backend, allowDraining) {
			out = append(out, backend)
		}
	}
	return out
}

func (p *hostedAgentProviderPool) unavailableError(err error) error {
	if err == nil {
		return nil
	}
	return hostedAgentProviderUnavailableError{providerName: p.name, cause: err}
}

type hostedAgentProviderUnavailableError struct {
	providerName string
	cause        error
}

func (e hostedAgentProviderUnavailableError) Error() string {
	return fmt.Sprintf("%s: %v", agentmanager.NewAgentProviderNotAvailableError(e.providerName), e.cause)
}

func (e hostedAgentProviderUnavailableError) Unwrap() []error {
	return []error{agentmanager.NewAgentProviderNotAvailableError(e.providerName), e.cause}
}

func (p *hostedAgentProviderPool) instanceCountsLocked() (ready, starting, draining int) {
	starting = p.starting
	now := time.Now().UTC()
	for _, backend := range p.backends {
		if backend == nil || backend.closed {
			continue
		}
		if backend.draining || backend.closing || p.backendRuntimeDrainDueLocked(backend, now) {
			draining++
			continue
		}
		if backend.provider != nil {
			ready++
		}
	}
	return ready, starting, draining
}

func isHostedAgentReadRetryableError(err error) bool {
	if err == nil || errors.Is(err, core.ErrNotFound) || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, agentmanager.ErrAgentProviderNotAvailable) {
		return true
	}
	switch grpcstatus.Code(err) {
	case codes.DeadlineExceeded, codes.Unavailable:
		return true
	default:
		return false
	}
}

func (p *hostedAgentProviderPool) recordInstanceCounts(ctx context.Context, ready, starting, draining int) {
	recordHostedAgentRuntimeInstances(ctx, p.name, ready, starting, draining)
}

func (p *hostedAgentProviderPool) sessionBackend(sessionID string) *hostedAgentPoolBackend {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionBackends[sessionID]
}

func (p *hostedAgentProviderPool) turnBackend(turnID string) *hostedAgentPoolBackend {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.turnBackends[turnID]
}

func (p *hostedAgentProviderPool) interactionBackend(interactionID string) *hostedAgentPoolBackend {
	interactionID = strings.TrimSpace(interactionID)
	if interactionID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.interactionBackends[interactionID]
}

func (p *hostedAgentProviderPool) recordSessionBackend(sessionID string, backend *hostedAgentPoolBackend) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || backend == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.backendAvailableLocked(backend, true) {
		p.sessionBackends[sessionID] = backend
	}
}

func (p *hostedAgentProviderPool) recordSession(session *coreagent.Session, backend *hostedAgentPoolBackend) {
	if session == nil {
		return
	}
	sessionID := strings.TrimSpace(session.ID)
	if sessionID == "" {
		return
	}
	if session.State == coreagent.SessionStateArchived && !p.backendDraining(backend) {
		p.deleteSessionBackend(sessionID)
		return
	}
	p.recordSessionBackend(sessionID, backend)
}

func (p *hostedAgentProviderPool) deleteSessionBackend(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessionBackends, sessionID)
}

func (p *hostedAgentProviderPool) recordTurnBackend(turnID string, backend *hostedAgentPoolBackend) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" || backend == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.backendAvailableLocked(backend, true) {
		p.turnBackends[turnID] = backend
	}
}

func (p *hostedAgentProviderPool) recordTurn(turn *coreagent.Turn, backend *hostedAgentPoolBackend) {
	if turn == nil {
		return
	}
	turnID := strings.TrimSpace(turn.ID)
	if turnID == "" {
		return
	}
	p.recordSessionBackend(turn.SessionID, backend)
	p.mu.Lock()
	defer p.mu.Unlock()
	if backend == nil || backend.liveTurns == nil || !p.backendAvailableLocked(backend, true) {
		return
	}
	switch {
	case coreagent.ExecutionStatusIsLive(turn.Status):
		p.turnBackends[turnID] = backend
		backend.liveTurns[turnID] = struct{}{}
		return
	case backend.draining:
		p.turnBackends[turnID] = backend
	default:
		delete(p.turnBackends, turnID)
	}
	delete(backend.liveTurns, turnID)
}

func (p *hostedAgentProviderPool) deleteTurnBackend(turnID string) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.turnBackends, turnID)
	for _, backend := range p.backends {
		if backend.liveTurns != nil {
			delete(backend.liveTurns, turnID)
		}
	}
}

func (p *hostedAgentProviderPool) recordInteraction(interaction *coreagent.Interaction, backend *hostedAgentPoolBackend) {
	interactionID := interactionIDForInteraction(interaction)
	if interactionID == "" || backend == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.backendAvailableLocked(backend, true) {
		if (interaction.State == coreagent.InteractionStateResolved || interaction.State == coreagent.InteractionStateCanceled) && !backend.draining {
			delete(p.interactionBackends, interactionID)
		} else {
			p.interactionBackends[interactionID] = backend
		}
		if turnID := strings.TrimSpace(interaction.TurnID); turnID != "" {
			p.turnBackends[turnID] = backend
		}
		if sessionID := strings.TrimSpace(interaction.SessionID); sessionID != "" {
			p.sessionBackends[sessionID] = backend
		}
	}
}

func (p *hostedAgentProviderPool) backendDraining(backend *hostedAgentPoolBackend) bool {
	if backend == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return backend.draining && !backend.closed
}

func (p *hostedAgentProviderPool) deleteInteractionBackend(interactionID string) {
	interactionID = strings.TrimSpace(interactionID)
	if interactionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.interactionBackends, interactionID)
}

func (p *hostedAgentProviderPool) healthLoop() {
	ticker := time.NewTicker(p.policy.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		}
		_ = p.ensureMinReady(p.ctx)
		for _, backend := range p.readyBackends() {
			session, drainAt, err := p.refreshBackendRuntimeSession(backend)
			if err != nil {
				slog.Warn("hosted agent runtime session refresh failed", "provider", p.name, "instance", backend.id, "error", err)
				p.replaceBackend(backend)
				continue
			}
			if reason := p.runtimeSessionRetirementReason(session, drainAt, time.Now().UTC()); reason != "" {
				slog.Info("retiring hosted agent runtime instance", "provider", p.name, "instance", backend.id, "reason", reason)
				p.replaceBackend(backend)
				continue
			}
			if err := p.pingBackend(backend); err != nil {
				slog.Warn("hosted agent runtime instance failed health check", "provider", p.name, "instance", backend.id, "error", err)
				p.replaceBackend(backend)
				continue
			}
			if reason := p.runtimeSessionProactiveReplacementReason(backend, session, drainAt, time.Now().UTC()); reason != "" {
				p.startProactiveReplacement(backend, reason)
			}
		}
	}
}

func (p *hostedAgentProviderPool) refreshBackendRuntimeSession(backend *hostedAgentPoolBackend) (*pluginruntime.Session, *time.Time, error) {
	if backend == nil {
		return nil, nil, fmt.Errorf("runtime instance is unavailable")
	}
	p.mu.Lock()
	runtimeProvider := backend.runtimeProvider
	sessionID := strings.TrimSpace(backend.runtimeSessionID)
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
	if p.backendAvailableLocked(backend, true) {
		if backend.runtimeDrainAt != nil && (drainAt == nil || backend.runtimeDrainAt.Before(*drainAt)) {
			drainAt = cloneTime(backend.runtimeDrainAt)
		}
		backend.runtimeSession = session
		backend.runtimeDrainAt = cloneTime(drainAt)
		backend.forceCloseAt = runtimeSessionExpiresAt(session)
	}
	p.mu.Unlock()
	return session, drainAt, nil
}

func (p *hostedAgentProviderPool) runtimeSessionRetirementReason(session *pluginruntime.Session, drainAt *time.Time, now time.Time) string {
	if session == nil {
		return ""
	}
	switch session.State {
	case pluginruntime.SessionStateFailed, pluginruntime.SessionStateStopped:
		return fmt.Sprintf("runtime session entered %q state", session.State)
	}
	if drainAt == nil || now.Before(*drainAt) {
		return ""
	}
	if expiresAt := runtimeSessionExpiresAt(session); expiresAt != nil && !now.Before(*expiresAt) {
		return fmt.Sprintf("runtime session expired at %s", expiresAt.Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("runtime session reached drain deadline %s", drainAt.Format(time.RFC3339Nano))
}

func (p *hostedAgentProviderPool) runtimeSessionProactiveReplacementReason(backend *hostedAgentPoolBackend, session *pluginruntime.Session, drainAt *time.Time, now time.Time) string {
	if session == nil {
		return ""
	}
	switch session.State {
	case pluginruntime.SessionStateFailed, pluginruntime.SessionStateStopped:
		return ""
	}
	if drainAt == nil {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	drainDeadline := drainAt.UTC()
	if !now.Before(drainDeadline) {
		return ""
	}
	startAt := drainDeadline.Add(-p.runtimeSessionReplacementLeadTime())
	if backend != nil && !backend.startedAt.IsZero() && backend.startedAt.Before(drainDeadline) {
		minStartAt := backend.startedAt.UTC().Add(drainDeadline.Sub(backend.startedAt.UTC()) / 2)
		if startAt.Before(minStartAt) {
			startAt = minStartAt
		}
	}
	if now.Before(startAt) {
		return ""
	}
	return fmt.Sprintf("runtime session approaching drain deadline %s", drainDeadline.Format(time.RFC3339Nano))
}

func (p *hostedAgentProviderPool) runtimeSessionReplacementLeadTime() time.Duration {
	lead := p.policy.StartupTimeout + p.policy.HealthCheckInterval
	if lead <= 0 {
		lead = p.policy.HealthCheckInterval
	}
	if lead <= 0 {
		return time.Second
	}
	return lead
}

func (p *hostedAgentProviderPool) runtimeSessionDrainAt(session *pluginruntime.Session, now time.Time) *time.Time {
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

func (p *hostedAgentProviderPool) runtimeSessionExpiryDrainAt(lifecycle *pluginruntime.SessionLifecycle, now time.Time) time.Time {
	expiresAt := lifecycle.ExpiresAt.UTC()
	reserve := p.policy.StartupTimeout + p.policy.DrainTimeout + p.policy.HealthCheckInterval + hostedAgentRuntimeLifecycleSafetyMargin
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

func runtimeSessionExpiresAt(session *pluginruntime.Session) *time.Time {
	if session == nil || session.Lifecycle == nil || session.Lifecycle.ExpiresAt == nil {
		return nil
	}
	expiresAt := session.Lifecycle.ExpiresAt.UTC()
	return &expiresAt
}

func cloneTime(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	out := src.UTC()
	return &out
}

func (p *hostedAgentProviderPool) maybeProbeAfterCallError(backend *hostedAgentPoolBackend, callErr error) {
	if callErr == nil || backend == nil || errors.Is(callErr, core.ErrNotFound) || p.policy.RestartPolicy == config.HostedRuntimeRestartPolicyNever {
		return
	}
	go func() {
		if err := p.pingBackend(backend); err != nil {
			slog.Warn("hosted agent runtime instance failed after provider call error", "provider", p.name, "instance", backend.id, "call_error", callErr, "ping_error", err)
			p.replaceBackend(backend)
		}
	}()
}

func (p *hostedAgentProviderPool) pingBackend(backend *hostedAgentPoolBackend) error {
	startedAt := time.Now()
	if backend == nil || backend.provider == nil {
		err := fmt.Errorf("runtime instance is unavailable")
		recordHostedAgentRuntimeHealthCheck(p.ctx, p.name, startedAt, err)
		return err
	}
	timeout := p.policy.HealthCheckInterval
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()
	err := backend.provider.Ping(ctx)
	recordHostedAgentRuntimeHealthCheck(p.ctx, p.name, startedAt, err)
	return err
}

func (p *hostedAgentProviderPool) replaceBackend(backend *hostedAgentPoolBackend) {
	if p.policy.RestartPolicy == config.HostedRuntimeRestartPolicyNever || !p.markBackendDraining(backend) {
		return
	}
	if !p.addLifecycleWork() {
		return
	}
	go func() {
		defer p.lifecycleWG.Done()
		// Replacement restores ready capacity. Session/turn recovery after the
		// drained backend closes depends on the agent provider's durable store.
		startErr := p.ensureMinReady(p.ctx)
		recordHostedAgentRuntimeReplacement(p.ctx, p.name, startErr)
		if startErr != nil && !errors.Is(startErr, context.Canceled) {
			slog.Warn("failed to replace hosted agent runtime instance", "provider", p.name, "instance", backend.id, "error", startErr)
		}
		if err := p.drainAndCloseBackend(backend); err != nil {
			slog.Warn("failed to close unhealthy hosted agent runtime instance", "provider", p.name, "instance", backend.id, "error", err)
		}
	}()
}

func (p *hostedAgentProviderPool) startProactiveReplacement(backend *hostedAgentPoolBackend, reason string) {
	if backend == nil || p.policy.RestartPolicy == config.HostedRuntimeRestartPolicyNever {
		return
	}
	p.mu.Lock()
	if p.closed || backend.draining || backend.closing || backend.closed || backend.replacing || !p.backendAvailableLocked(backend, false) || !p.canScaleOutLocked() {
		p.mu.Unlock()
		return
	}
	backend.replacing = true
	p.starting++
	ready, starting, draining := p.instanceCountsLocked()
	p.lifecycleWG.Add(1)
	p.mu.Unlock()
	p.recordInstanceCounts(p.ctx, ready, starting, draining)

	go func() {
		defer p.lifecycleWG.Done()
		slog.Info("starting proactive hosted agent runtime replacement", "provider", p.name, "instance", backend.id, "reason", reason)
		_, startErr := p.startBackend(p.ctx)
		recordHostedAgentRuntimeReplacement(p.ctx, p.name, startErr)
		p.mu.Lock()
		p.starting--
		if p.backendAvailableLocked(backend, true) {
			if startErr == nil {
				backend.draining = true
			}
			backend.replacing = false
		}
		ready, starting, draining := p.instanceCountsLocked()
		p.mu.Unlock()
		p.recordInstanceCounts(p.ctx, ready, starting, draining)
		if startErr != nil {
			if !errors.Is(startErr, context.Canceled) {
				slog.Warn("failed to proactively replace hosted agent runtime instance", "provider", p.name, "instance", backend.id, "error", startErr)
			}
			return
		}
		if err := p.drainAndCloseBackend(backend); err != nil {
			slog.Warn("failed to close proactively replaced hosted agent runtime instance", "provider", p.name, "instance", backend.id, "error", err)
		}
	}()
}

func (p *hostedAgentProviderPool) addLifecycleWork() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.lifecycleWG.Add(1)
	return true
}

func (p *hostedAgentProviderPool) ensureMinReady(ctx context.Context) error {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil
		}
		ready := 0
		now := time.Now().UTC()
		for _, backend := range p.backends {
			if p.backendAcceptsNewWorkLocked(backend, now) {
				ready++
			}
		}
		if ready+p.starting >= p.policy.MinReadyInstances {
			p.mu.Unlock()
			return nil
		}
		p.starting++
		ready, starting, draining := p.instanceCountsLocked()
		p.mu.Unlock()
		p.recordInstanceCounts(ctx, ready, starting, draining)

		_, err := p.startBackend(ctx)
		p.mu.Lock()
		p.starting--
		ready, starting, draining = p.instanceCountsLocked()
		p.mu.Unlock()
		p.recordInstanceCounts(ctx, ready, starting, draining)
		if err != nil {
			slog.Warn("failed to start hosted agent runtime instance", "provider", p.name, "error", err)
			return err
		}
	}
}

func (p *hostedAgentProviderPool) startBackend(ctx context.Context) (*hostedAgentPoolBackend, error) {
	startCtx, cancel := context.WithTimeout(ctx, p.policy.StartupTimeout)
	defer cancel()
	instance, err := startHostedAgentProviderInstance(startCtx, p.launch, p.hostServices, p.deps, false, nil)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = instance.provider.Close()
		return nil, fmt.Errorf("hosted agent provider %q is closed", p.name)
	}
	p.nextID++
	now := time.Now().UTC()
	backend := &hostedAgentPoolBackend{
		id:               p.nextID,
		provider:         instance.provider,
		runtimeProvider:  instance.runtimeProvider,
		runtimeSessionID: instance.runtimeSessionID,
		runtimeSession:   instance.runtimeSession,
		startedAt:        now,
		runtimeDrainAt:   p.runtimeSessionDrainAt(instance.runtimeSession, now),
		forceCloseAt:     runtimeSessionExpiresAt(instance.runtimeSession),
		liveTurns:        map[string]struct{}{},
	}
	p.backends = append(p.backends, backend)
	ready, starting, draining := p.instanceCountsLocked()
	p.mu.Unlock()
	p.recordInstanceCounts(ctx, ready, starting, draining)
	return backend, nil
}

func (p *hostedAgentProviderPool) markBackendDraining(backend *hostedAgentPoolBackend) bool {
	if backend == nil {
		return false
	}
	p.mu.Lock()
	if backend.draining || backend.closed {
		p.mu.Unlock()
		return false
	}
	backend.draining = true
	ready, starting, draining := p.instanceCountsLocked()
	p.mu.Unlock()
	p.recordInstanceCounts(p.ctx, ready, starting, draining)
	return true
}

func (p *hostedAgentProviderPool) drainAndCloseBackend(backend *hostedAgentPoolBackend) error {
	if backend == nil {
		return nil
	}
	p.mu.Lock()
	if backend.closed || backend.closing {
		p.mu.Unlock()
		return nil
	}
	backend.closing = true
	backend.draining = true
	p.mu.Unlock()

	deadline := time.Now().Add(p.policy.DrainTimeout)
	p.mu.Lock()
	if backend.forceCloseAt != nil && backend.forceCloseAt.Before(deadline) {
		deadline = backend.forceCloseAt.UTC()
	}
	p.mu.Unlock()
	for {
		p.mu.Lock()
		active := backend.active
		liveTurns := len(backend.liveTurns)
		if (active == 0 && liveTurns == 0) || time.Now().After(deadline) {
			backend.closed = true
			p.removeBackendLocked(backend)
			ready, starting, draining := p.instanceCountsLocked()
			p.mu.Unlock()
			p.recordInstanceCounts(p.ctx, ready, starting, draining)
			break
		}
		p.mu.Unlock()
		time.Sleep(25 * time.Millisecond)
	}
	return backend.provider.Close()
}

func (p *hostedAgentProviderPool) removeBackendLocked(backend *hostedAgentPoolBackend) {
	for i, candidate := range p.backends {
		if candidate == backend {
			p.backends = append(p.backends[:i], p.backends[i+1:]...)
			break
		}
	}
	for key, candidate := range p.sessionBackends {
		if candidate == backend {
			delete(p.sessionBackends, key)
		}
	}
	for key, candidate := range p.turnBackends {
		if candidate == backend {
			delete(p.turnBackends, key)
		}
	}
	for key, candidate := range p.interactionBackends {
		if candidate == backend {
			delete(p.interactionBackends, key)
		}
	}
}

func sessionIDForSession(session *coreagent.Session) string {
	if session == nil {
		return ""
	}
	return session.ID
}

func turnIDForTurn(turn *coreagent.Turn) string {
	if turn == nil {
		return ""
	}
	return turn.ID
}

func interactionIDForInteraction(interaction *coreagent.Interaction) string {
	if interaction == nil {
		return ""
	}
	return interaction.ID
}
