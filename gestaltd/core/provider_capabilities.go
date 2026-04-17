package core

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

var ErrSessionCatalogUnavailable = errors.New("session catalog unavailable")

type sessionCatalogUnavailableError struct {
	cause error
}

func (e *sessionCatalogUnavailableError) Error() string {
	if e == nil || e.cause == nil {
		return ErrSessionCatalogUnavailable.Error()
	}
	return e.cause.Error()
}

func (e *sessionCatalogUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *sessionCatalogUnavailableError) Is(target error) bool {
	return target == ErrSessionCatalogUnavailable
}

func wrapSessionCatalogUnavailable(err error) error {
	if err == nil || errors.Is(err, ErrSessionCatalogUnavailable) {
		return err
	}
	return &sessionCatalogUnavailableError{cause: err}
}

func SupportsSessionCatalog(prov Provider) bool {
	_, ok := prov.(SessionCatalogProvider)
	return ok
}

func CatalogForRequest(ctx context.Context, prov Provider, token string) (*catalog.Catalog, bool, error) {
	scp, ok := prov.(SessionCatalogProvider)
	if !ok {
		return nil, false, nil
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	return cat, true, wrapSessionCatalogUnavailable(err)
}
