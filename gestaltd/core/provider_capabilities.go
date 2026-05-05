package core

import (
	"context"
	"errors"
	"fmt"

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

type sessionCatalogSupporter interface {
	SupportsSessionCatalog() bool
}

type postConnectSupporter interface {
	SupportsPostConnect() bool
}

type httpSubjectSupporter interface {
	SupportsHTTPSubject() bool
}

func SupportsSessionCatalog(prov Provider) bool {
	if prov == nil {
		return false
	}
	if aware, ok := prov.(sessionCatalogSupporter); ok {
		return aware.SupportsSessionCatalog()
	}
	_, ok := prov.(SessionCatalogProvider)
	return ok
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

func SupportsPostConnect(prov Provider) bool {
	if prov == nil {
		return false
	}
	if aware, ok := prov.(postConnectSupporter); ok {
		return aware.SupportsPostConnect()
	}
	_, ok := prov.(PostConnectCapable)
	return ok
}

func PostConnect(ctx context.Context, prov Provider, token *ExternalCredential) (map[string]string, bool, error) {
	if !SupportsPostConnect(prov) {
		return nil, false, nil
	}
	pcp, ok := prov.(PostConnectCapable)
	if !ok {
		return nil, true, fmt.Errorf("%w: provider %q advertises post-connect support but does not implement post-connect", ErrPostConnectUnsupported, prov.Name())
	}
	metadata, err := pcp.PostConnect(ctx, token)
	if err != nil {
		if errors.Is(err, ErrPostConnectUnsupported) {
			return nil, false, nil
		}
		return nil, true, err
	}
	return metadata, true, nil
}

func SupportsHTTPSubject(prov Provider) bool {
	if prov == nil {
		return false
	}
	if aware, ok := prov.(httpSubjectSupporter); ok {
		return aware.SupportsHTTPSubject()
	}
	_, ok := prov.(HTTPSubjectResolver)
	return ok
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
