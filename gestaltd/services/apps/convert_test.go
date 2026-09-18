package plugins

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

func TestCatalogProtoRoundTripPreservesOperationExposure(t *testing.T) {
	t.Parallel()

	api := false
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
