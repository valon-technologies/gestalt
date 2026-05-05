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

type postConnectProbeProvider struct {
	coretesting.StubIntegration
	supports bool
	called   bool
	err      error
}

func (p *postConnectProbeProvider) SupportsPostConnect() bool {
	return p.supports
}

func (p *postConnectProbeProvider) PostConnect(context.Context, *core.ExternalCredential) (map[string]string, error) {
	p.called = true
	if p.err != nil {
		return nil, p.err
	}
	return map[string]string{"ok": "true"}, nil
}

type postConnectSupportOnlyProvider struct {
	coretesting.StubIntegration
}

func (p *postConnectSupportOnlyProvider) SupportsPostConnect() bool {
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

	postConnect := &postConnectProbeProvider{
		StubIntegration: coretesting.StubIntegration{N: "post-connect"},
	}
	metadata, supported, err := core.PostConnect(context.Background(), postConnect, &core.ExternalCredential{})
	if err != nil {
		t.Fatalf("PostConnect: %v", err)
	}
	if supported {
		t.Fatal("expected post-connect support to be skipped")
	}
	if metadata != nil {
		t.Fatalf("expected nil metadata, got %#v", metadata)
	}
	if postConnect.called {
		t.Fatal("explicit false support should prevent PostConnect from being called")
	}

	unsupportedPostConnect := &postConnectProbeProvider{
		StubIntegration: coretesting.StubIntegration{N: "post-connect"},
		supports:        true,
		err:             core.ErrPostConnectUnsupported,
	}
	metadata, supported, err = core.PostConnect(context.Background(), unsupportedPostConnect, &core.ExternalCredential{})
	if err != nil {
		t.Fatalf("PostConnect unsupported connection: %v", err)
	}
	if supported {
		t.Fatal("expected provider-level unsupported post-connect to report unsupported")
	}
	if metadata != nil {
		t.Fatalf("expected nil metadata, got %#v", metadata)
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

	_, supported, err := core.PostConnect(context.Background(), &postConnectSupportOnlyProvider{
		StubIntegration: coretesting.StubIntegration{N: "post-connect"},
	}, &core.ExternalCredential{})
	if !supported {
		t.Fatal("expected advertised post-connect support")
	}
	if !errors.Is(err, core.ErrPostConnectUnsupported) {
		t.Fatalf("expected ErrPostConnectUnsupported, got %v", err)
	}
}
