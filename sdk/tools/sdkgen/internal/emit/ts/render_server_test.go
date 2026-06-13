package ts

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// TestRenderProviderSurface exercises the provider handler + dispatch adapter
// rendering in isolation, before it is wired into Emit(). It renders a small
// hand-built provider service and asserts the shape of the generated handler
// class, the Unimplemented defaults, and the wire dispatch adapter, so the
// rendering logic is reviewable on its own. Hermetic: no buf, no pipeline.
func TestRenderProviderSurface(t *testing.T) {
	t.Parallel()

	req := &model.Message{FullName: "gestalt.test.v1.FooRequest", Name: "FooRequest", ProtoFile: "test.proto"}
	resp := &model.Message{FullName: "gestalt.test.v1.FooResponse", Name: "FooResponse", ProtoFile: "test.proto"}
	svc := &model.Service{
		FullName:  "gestalt.test.v1.Thing",
		Name:      "Thing",
		Provider:  true,
		ProtoFile: "test.proto",
		Methods: []*model.Method{
			{Name: "Foo", Stream: model.Unary, Input: req, Output: resp},
			{Name: "Bar", Stream: model.Unary, Input: req, OutputIsEmpty: true},
		},
	}
	idx := &index{
		messages: map[string]*model.Message{req.FullName: req, resp.FullName: resp},
		enums:    map[string]*model.Enum{},
	}

	r := newServerRenderer(idx, "test")
	r.renderProviderHandler(svc)
	r.renderProviderService(svc)
	out := r.assemble()

	for _, want := range []string{
		// Handler class: named ThingProvider (not yet *Provider); default methods
		// throw GestaltError(Unimplemented) so providers only override what they need.
		"export abstract class ThingProvider {",
		// One method per RPC; transport-shaped (full request in, full response out).
		"async foo(request: FooRequest): Promise<FooResponse> {",
		// An Empty response collapses to Promise<void>.
		"async bar(request: FooRequest): Promise<void> {",
		// Unimplemented defaults carry the canonical error code and operation string.
		"GestaltErrorCode.Unimplemented",
		"thing foo is not implemented",
		// Wire dispatch factory: returns Partial<ServiceImpl<typeof wire.Thing>>.
		"export function createThingProviderService(",
		"): Partial<ServiceImpl<typeof wire.Thing>> {",
		// Codec functions are called to convert wire <-> native.
		"fromWireFooRequest(request)",
		"toWireFooResponse(response)",
		// Error mapping is delegated to statusError.
		`statusError("thing foo", error)`,
		// Empty-response path returns an empty object (connect-es convention).
		"return {};",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered provider surface missing %q\n---\n%s", want, out)
		}
	}

	// The server support file backs the statusError function.
	if !strings.Contains(serverSupportFile, "function statusError(operation: string, error: unknown): ConnectError") {
		t.Error("serverSupportFile missing statusError")
	}
}

// TestProviderNameSuffix verifies that a service already ending in Provider
// receives a Handler suffix to avoid colliding with the generated client name.
func TestProviderNameSuffix(t *testing.T) {
	t.Parallel()
	if got := providerName("AgentProvider"); got != "AgentProviderHandler" {
		t.Errorf("providerName(%q) = %q, want AgentProviderHandler", "AgentProvider", got)
	}
	if got := providerName("Cache"); got != "CacheProvider" {
		t.Errorf("providerName(%q) = %q, want CacheProvider", "Cache", got)
	}
}

// TestOperationString verifies the human-readable operation tag.
func TestOperationString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		svc, method, want string
	}{
		{"Workflow", "ApplyDefinition", "workflow apply definition"},
		{"Thing", "Foo", "thing foo"},
		{"Cache", "Get", "cache get"},
	}
	for _, tc := range cases {
		if got := operationString(tc.svc, tc.method); got != tc.want {
			t.Errorf("operationString(%q, %q) = %q, want %q", tc.svc, tc.method, got, tc.want)
		}
	}
}
