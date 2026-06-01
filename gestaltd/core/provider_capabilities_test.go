package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

type sessionCatalogProbeProvider struct {
	coretesting.StubIntegration
	supports bool
	called   bool
}

func (p *sessionCatalogProbeProvider) SupportsSessionCatalog() bool {
	return p.supports
}

func (p *sessionCatalogProbeProvider) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	p.called = true
	return &catalog.Catalog{Name: "probe"}, nil
}

type sessionCatalogSupportOnlyProvider struct {
	coretesting.StubIntegration
}

func (p *sessionCatalogSupportOnlyProvider) SupportsSessionCatalog() bool {
	return true
}

func TestCapabilityHelpersRespectExplicitSupportFlags(t *testing.T) {
	t.Parallel()

	session := &sessionCatalogProbeProvider{
		StubIntegration: coretesting.StubIntegration{N: "session"},
	}
	cat, attempted, err := core.CatalogForRequest(context.Background(), session, "token")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if attempted {
		t.Fatal("expected session catalog attempt to be skipped")
	}
	if cat != nil {
		t.Fatalf("expected nil catalog, got %#v", cat)
	}
	if session.called {
		t.Fatal("explicit false support should prevent CatalogForRequest from being called")
	}
}

func TestCapabilityHelpersReportAdvertisedSupportWithoutMethod(t *testing.T) {
	t.Parallel()

	_, attempted, err := core.CatalogForRequest(context.Background(), &sessionCatalogSupportOnlyProvider{
		StubIntegration: coretesting.StubIntegration{N: "session"},
	}, "token")
	if !attempted {
		t.Fatal("expected advertised session catalog support to be attempted")
	}
	if !errors.Is(err, core.ErrSessionCatalogUnsupported) {
		t.Fatalf("expected ErrSessionCatalogUnsupported, got %v", err)
	}
}
