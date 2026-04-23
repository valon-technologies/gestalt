package core

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

var ErrSessionCatalogUnavailable = errors.New("session catalog unavailable")
var ErrSessionCatalogUnsupported = errors.New("session catalog unsupported")
var ErrPostConnectUnsupported = errors.New("post connect unsupported")

type sessionCatalogUnavailableError struct {
	cause       error
	unsupported bool
}

func (e *sessionCatalogUnavailableError) Error() string {
	if e == nil {
		return ErrSessionCatalogUnavailable.Error()
	}
	if e.cause == nil {
		if e.unsupported {
			return ErrSessionCatalogUnsupported.Error()
		}
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
	if target == ErrSessionCatalogUnavailable {
		return true
	}
	return e.unsupported && target == ErrSessionCatalogUnsupported
}

func wrapSessionCatalogUnavailable(err error) error {
	if err == nil || errors.Is(err, ErrSessionCatalogUnavailable) {
		return err
	}
	return &sessionCatalogUnavailableError{cause: err}
}

func WrapSessionCatalogUnsupported(err error) error {
	if err == nil {
		err = ErrSessionCatalogUnsupported
	}
	if errors.Is(err, ErrSessionCatalogUnavailable) && errors.Is(err, ErrSessionCatalogUnsupported) {
		return err
	}
	return &sessionCatalogUnavailableError{
		cause:       err,
		unsupported: true,
	}
}

func SupportsSessionCatalog(prov Provider) bool {
	if aware, ok := prov.(interface{ SupportsSessionCatalog() bool }); ok {
		return aware.SupportsSessionCatalog()
	}
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

func SupportsPostConnect(prov Provider) bool {
	if aware, ok := prov.(interface{ SupportsPostConnect() bool }); ok {
		return aware.SupportsPostConnect()
	}
	_, ok := prov.(PostConnectCapable)
	return ok
}

func PostConnect(ctx context.Context, prov Provider, token *IntegrationToken) (map[string]string, bool, error) {
	pcp, ok := prov.(PostConnectCapable)
	if !ok {
		return nil, false, nil
	}
	metadata, err := pcp.PostConnect(ctx, token)
	if err != nil {
		if errors.Is(err, ErrPostConnectUnsupported) {
			return nil, true, nil
		}
		return nil, true, err
	}
	return metadata, true, nil
}
