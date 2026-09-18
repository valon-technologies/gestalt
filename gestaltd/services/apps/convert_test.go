package plugins

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

func TestCatalogProtoRoundTripPreservesOperationExposure(t *testing.T) {
	t.Parallel()

	api := catalog.APIExposurePrivate
	mcp := true
	want := &catalog.Catalog{
		Name: "wire",
		Operations: []catalog.CatalogOperation{{
			ID:     "restricted",
			Method: "POST",
			API:    &api,
			MCP:    &mcp,
		}},
	}

	wire, err := gproto.Marshal(catalogToProto(want))
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	var decoded proto.Catalog
	if err := gproto.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	got, err := catalogFromProto(&decoded)
	if err != nil {
		t.Fatalf("catalogFromProto: %v", err)
	}
	if len(got.Operations) != 1 {
		t.Fatalf("operations = %+v, want one operation", got.Operations)
	}
	op := got.Operations[0]
	if op.API == nil || *op.API != api || op.MCP == nil || *op.MCP != mcp {
		t.Fatalf("surface flags = api:%v mcp:%v, want api:%v mcp:%v", op.API, op.MCP, api, mcp)
	}
}

func TestCatalogProtoRoundTripPreservesBrowserSessionAndDowngradeDeny(t *testing.T) {
	t.Parallel()
	mode := catalog.APIExposureBrowserSession
	wire := catalogToProto(&catalog.Catalog{Name: "wire", Operations: []catalog.CatalogOperation{{ID: "browser", API: &mode}}})
	if wire.Operations[0].Api == nil || *wire.Operations[0].Api {
		t.Fatalf("wire api = %v, want explicit false fallback", wire.Operations[0].Api)
	}
	if wire.Operations[0].ApiMode == nil || *wire.Operations[0].ApiMode != proto.APIExposureMode_API_EXPOSURE_MODE_BROWSER_SESSION {
		t.Fatalf("wire api mode = %v, want browser session", wire.Operations[0].ApiMode)
	}
	got, err := catalogFromProto(wire)
	if err != nil {
		t.Fatalf("catalogFromProto: %v", err)
	}
	if got.Operations[0].API == nil || !got.Operations[0].API.IsBrowserSession() {
		t.Fatalf("round trip api = %v, want browserSession", got.Operations[0].API)
	}

	// An older peer ignores api_mode but still sees api=false, which remains
	// denied by the legacy surface check.
	legacyWire := gproto.Clone(wire).(*proto.Catalog)
	legacyWire.Operations[0].ApiMode = nil
	legacyCat, err := catalogFromProto(legacyWire)
	if err != nil {
		t.Fatalf("legacy catalogFromProto: %v", err)
	}
	if catalog.OperationExposedOnAPI(legacyCat.Operations[0]) {
		t.Fatal("legacy api=false fallback was exposed")
	}
}
