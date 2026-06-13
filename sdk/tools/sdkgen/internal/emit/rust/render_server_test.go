package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// TestRenderProviderSurface exercises the provider handler trait +
// Unimplemented defaults + wire dispatch adapter rendering in isolation,
// before it is wired into Emit(). It renders a small hand-built provider
// service and asserts the shape of the generated handler trait, the
// Unimplemented defaults, and the wire dispatch adapter, so the rendering
// logic is reviewable on its own.
// Hermetic: no buf, no pipeline (that would cause an import cycle).
func TestRenderProviderSurface(t *testing.T) {
	t.Parallel()

	req := &model.Message{
		FullName:  "gestalt.test.v1.FooRequest",
		Name:      "FooRequest",
		ProtoFile: "test.proto",
	}
	resp := &model.Message{
		FullName:  "gestalt.test.v1.FooResponse",
		Name:      "FooResponse",
		ProtoFile: "test.proto",
	}
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
		messages:     map[string]*model.Message{req.FullName: req, resp.FullName: resp},
		enums:        map[string]*model.Enum{},
		needToWire:   map[string]bool{},
		needFromWire: map[string]bool{},
	}

	r := newRenderer(idx, "test", modulePublic)
	r.renderProviderHandler(svc)
	r.renderProviderServer(svc)
	out := r.assemble()

	for _, want := range []string{
		// Handler trait: one method per RPC, transport-shaped (full native
		// request in, full native response out); an Empty response collapses
		// to Result<(), GestaltError>.
		"pub trait ThingProvider:",
		"async fn foo(&self, request: FooRequest) -> Result<FooResponse, GestaltError>",
		"async fn bar(&self, request: FooRequest) -> Result<(), GestaltError>",

		// Unimplemented defaults carrying the canonical error code + operation.
		"pub struct UnimplementedThingProvider",
		"gestalt_error_code::UNIMPLEMENTED",
		"thing foo is not implemented",
		"thing bar is not implemented",

		// Wire dispatch adapter struct and impl.
		"pub struct ThingProviderServer<P>",
		"impl<P: ThingProvider> ThingProviderServer<P>",

		// Codec calls in the dispatch body.
		"from_wire_foo_request(request.into_inner())",
		"to_wire_foo_response(response)",

		// Error mapping via status_error with operation string.
		`crate::server_support::status_error("thing foo", e)`,
		`crate::server_support::status_error("thing bar", e)`,

		// Empty-response path returns tonic::Response::new(()).
		"Ok(tonic::Response::new(()))",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered provider surface missing %q\n---\n%s", want, out)
		}
	}

	// Also verify the *Provider handler-name rule:
	// a service already named *Provider gets a Handler suffix.
	provSvc := &model.Service{
		FullName: "gestalt.test.v1.ThingProvider",
		Name:     "ThingProvider",
		Provider: true,
		Methods: []*model.Method{
			{Name: "Foo", Stream: model.Unary, Input: req, Output: resp},
		},
	}
	r2 := newRenderer(idx, "test", modulePublic)
	r2.renderProviderHandler(provSvc)
	out2 := r2.assemble()
	if !strings.Contains(out2, "pub trait ThingProviderHandler:") {
		t.Errorf("service already named *Provider should get Handler suffix, got:\n%s", out2)
	}
	if strings.Contains(out2, "pub trait ThingProviderProvider:") {
		t.Errorf("service named *Provider must NOT double-suffix to *ProviderProvider, got:\n%s", out2)
	}

	// The serverSupportFile backs the generated adapter; verify the key function.
	if !strings.Contains(serverSupportFile, "pub(crate) fn status_error(operation: &str, err: GestaltError) -> tonic::Status") {
		t.Error("serverSupportFile missing status_error")
	}
}
