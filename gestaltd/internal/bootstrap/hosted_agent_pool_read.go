package bootstrap

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func getHostedAgentValue[T any](
	p *hostedAgentProviderPool,
	preferred *hostedAgentPoolBackend,
	read func(*hostedAgentPoolBackend) (T, error),
	forget func(),
	record func(T, *hostedAgentPoolBackend),
) (T, error) {
	var zero T
	var retryableErr error
	var lastNotFound error
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := preferred; backend != nil {
		tried[backend] = struct{}{}
		value, err := read(backend)
		switch {
		case err == nil:
			record(value, backend)
			return value, nil
		case errors.Is(err, core.ErrNotFound):
			forget()
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return zero, err
		}
	}
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		value, err := read(backend)
		if err == nil {
			record(value, backend)
			return value, nil
		}
		if errors.Is(err, core.ErrNotFound) {
			lastNotFound = err
			continue
		}
		if isHostedAgentReadRetryableError(err) {
			retryableErr = err
			continue
		}
		return zero, err
	}
	if retryableErr != nil {
		return zero, retryableErr
	}
	if lastNotFound != nil {
		return zero, lastNotFound
	}
	return zero, core.ErrNotFound
}

func listHostedAgentValues[T any](
	p *hostedAgentProviderPool,
	preferred *hostedAgentPoolBackend,
	read func(*hostedAgentPoolBackend) ([]*T, error),
	forget func(),
) ([]*T, error) {
	var retryableErr error
	var lastNotFound error
	succeeded := false
	tried := map[*hostedAgentPoolBackend]struct{}{}
	if backend := preferred; backend != nil {
		tried[backend] = struct{}{}
		values, err := read(backend)
		if err == nil {
			return values, nil
		}
		switch {
		case errors.Is(err, core.ErrNotFound):
			forget()
			lastNotFound = err
		case isHostedAgentReadRetryableError(err):
			retryableErr = err
		default:
			return nil, err
		}
	}
	var out []*T
	for _, backend := range p.availableBackends(true) {
		if _, ok := tried[backend]; ok {
			continue
		}
		values, err := read(backend)
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
		out = append(out, values...)
	}
	if !succeeded && retryableErr != nil {
		return nil, retryableErr
	}
	if !succeeded && lastNotFound != nil {
		return nil, lastNotFound
	}
	return out, nil
}

func mutateHostedAgentValue[T any](
	ctx context.Context,
	p *hostedAgentProviderPool,
	backend *hostedAgentPoolBackend,
	call func(coreagent.Provider) (*T, error),
	record func(*T, *hostedAgentPoolBackend),
) (*T, error) {
	if backend == nil {
		return nil, core.ErrNotFound
	}
	acquired, release, err := p.acquireBackend(ctx, backend, true)
	if err != nil {
		return nil, err
	}
	value, err := call(acquired.provider)
	release()
	p.maybeProbeAfterCallError(acquired, err)
	if err == nil {
		record(value, acquired)
	}
	return value, err
}
