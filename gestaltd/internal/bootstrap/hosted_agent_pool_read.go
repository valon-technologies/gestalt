package bootstrap

import (
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
)

func getHostedAgentValue[T any](
	p *hostedAgentProviderPool,
	preferred *hostedAgentPoolBackend,
	read func(*hostedAgentPoolBackend) (*T, error),
	forget func(),
	record func(*T, *hostedAgentPoolBackend),
) (*T, error) {
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
			return nil, err
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
