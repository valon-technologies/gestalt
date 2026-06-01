package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

var ErrSessionCatalogUnavailable = errors.New("session catalog unavailable")
var ErrSessionCatalogUnsupported = errors.New("session catalog unsupported")

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

type sessionCatalogSupporter interface {
	SupportsSessionCatalog() bool
}

type httpSubjectSupporter interface {
	SupportsHTTPSubject() bool
}

// supportsOptionalProviderFeature checks optional provider capabilities.
// When tryExplicit returns handled=false, hasFallback is used instead.
func supportsOptionalProviderFeature(
	prov Provider,
	tryExplicit func(Provider) (handled bool, supported bool),
	hasFallback func(Provider) bool,
) bool {
	if prov == nil {
		return false
	}
	if handled, supported := tryExplicit(prov); handled {
		return supported
	}
	return hasFallback(prov)
}

func SupportsSessionCatalog(prov Provider) bool {
	return supportsOptionalProviderFeature(prov,
		func(p Provider) (bool, bool) {
			aware, ok := p.(sessionCatalogSupporter)
			if !ok {
				return false, false
			}
			return true, aware.SupportsSessionCatalog()
		},
		func(p Provider) bool {
			_, ok := p.(SessionCatalogProvider)
			return ok
		},
	)
}

func CatalogForRequest(ctx context.Context, prov Provider, token string) (*catalog.Catalog, bool, error) {
	if !SupportsSessionCatalog(prov) {
		return nil, false, nil
	}
	scp, ok := prov.(SessionCatalogProvider)
	if !ok {
		return nil, true, WrapSessionCatalogUnsupported(fmt.Errorf("provider %q advertises session catalog support but does not implement session catalogs", prov.Name()))
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	return cat, true, wrapSessionCatalogUnavailable(err)
}

func SupportsHTTPSubject(prov Provider) bool {
	return supportsOptionalProviderFeature(prov,
		func(p Provider) (bool, bool) {
			aware, ok := p.(httpSubjectSupporter)
			if !ok {
				return false, false
			}
			return true, aware.SupportsHTTPSubject()
		},
		func(p Provider) bool {
			_, ok := p.(HTTPSubjectResolver)
			return ok
		},
	)
}

func ResolveHTTPSubject(ctx context.Context, prov Provider, req *HTTPSubjectResolveRequest) (*HTTPResolvedSubject, bool, error) {
	if !SupportsHTTPSubject(prov) {
		return nil, false, nil
	}
	resolver, ok := prov.(HTTPSubjectResolver)
	if !ok {
		return nil, false, nil
	}
	subject, err := resolver.ResolveHTTPSubject(ctx, req)
	return subject, true, err
}
