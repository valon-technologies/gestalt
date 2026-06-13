package golang

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// TestRenderProviderSurface exercises the provider handler + dispatch adapter
// rendering in isolation, before it is wired into Emit(). It renders a small
// hand-built provider service and asserts the shape of the generated handler
// interface, the Unimplemented defaults, and the wire dispatch adapter, so the
// rendering logic is reviewable on its own. Hermetic: no buf, no pipeline.
func TestRenderProviderSurface(t *testing.T) {
	t.Parallel()

	req := &model.Message{FullName: "gestalt.test.v1.FooRequest", Name: "FooRequest", ProtoFile: "test.proto"}
	resp := &model.Message{FullName: "gestalt.test.v1.FooResponse", Name: "FooResponse", ProtoFile: "test.proto"}
	svc := &model.Service{
		FullName: "gestalt.test.v1.Thing",
		Name:     "Thing",
		Provider: true,
		Methods: []*model.Method{
			{Name: "Foo", Stream: model.Unary, Input: req, Output: resp},
			{Name: "Bar", Stream: model.Unary, Input: req, OutputIsEmpty: true},
		},
	}
	idx := &index{
		messages: map[string]*model.Message{req.FullName: req, resp.FullName: resp},
		enums:    map[string]*model.Enum{},
	}

	r := newRenderer(idx)
	r.renderProviderHandler(svc)
	r.renderProviderServer(svc)
	out := r.assemble()

	for _, want := range []string{
		// Handler interface: one method per RPC, transport-shaped (full request
		// in, full response out); an Empty response collapses to error-only.
		"type ThingProvider interface {",
		"Foo(ctx context.Context, request *FooRequest) (*FooResponse, error)",
		"Bar(ctx context.Context, request *FooRequest) error",
		// Unimplemented defaults carrying the canonical error code + operation.
		"type UnimplementedThingProvider struct{}",
		"GestaltErrorCodeUnimplemented",
		"thing foo is not implemented",
		// Wire dispatch adapter over the existing codecs + error mapping.
		"func NewThingProviderServer(provider ThingProvider) proto.ThingServer {",
		"fromWireFooRequest(request)",
		"toWireFooResponse(response)",
		`statusError("thing foo", err)`,
		"return &emptypb.Empty{}, nil",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered provider surface missing %q\n---\n%s", want, out)
		}
	}

	// The provider error-mapping support file backs the generated adapter.
	if !strings.Contains(serverSupportFile, "func statusError(operation string, err error) error") {
		t.Error("serverSupportFile missing statusError")
	}
}
