package python

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// TestRenderProviderSurface exercises the provider handler ABC and dispatch
// servicer rendering in isolation, before it is wired into Emit(). It renders
// a small hand-built provider service and asserts the shape of the generated
// handler ABC, the Unimplemented defaults, and the wire dispatch servicer, so
// the rendering logic is reviewable on its own. Hermetic: no buf, no pipeline.
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

	r := newServerRenderer(idx, "test")
	r.renderProviderHandler(svc)
	r.renderProviderServer(svc)
	out := r.assembleServer("test")

	for _, want := range []string{
		// Handler ABC: one abstract method per RPC, transport-shaped (full
		// request in, full response out); an Empty response collapses to None.
		// Types are qualified with the _native module alias for static analysis.
		"class ThingHandler(ABC):",
		"@abstractmethod",
		"def foo(self, request: _native.FooRequest) -> _native.FooResponse:",
		"def bar(self, request: _native.FooRequest) -> None:",
		// Unimplemented defaults carrying the canonical error code + operation.
		"class UnimplementedThingHandler(ThingHandler):",
		"GestaltErrorCode.UNIMPLEMENTED",
		"thing foo is not implemented",
		"thing bar is not implemented",
		// Wire dispatch servicer over the existing codecs + error mapping.
		"def new_thing_handler_server(handler: ThingHandler)",
		"class _ThingHandlerServicer(_test_pb2_grpc.ThingServicer):",
		"from_wire_foo_request(request)",
		"to_wire_foo_response(response)",
		`status_error(context, "thing foo", err)`,
		`status_error(context, "thing bar", err)`,
		"_empty_pb2.Empty()",
		// Imports
		"from abc import ABC, abstractmethod",
		"from ..rpc_support import GestaltError, GestaltErrorCode",
		"from .support import status_error",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered provider surface missing %q\n---\n%s", want, out)
		}
	}

	// The provider error-mapping support file backs the generated adapters.
	if !strings.Contains(serverSupportFile, "def status_error(") {
		t.Error("serverSupportFile missing status_error")
	}
}

// TestRenderProviderSurface_EmptyInput exercises the empty-input path: methods
// with no request take no request parameter and still map errors through
// status_error.
func TestRenderProviderSurface_EmptyInput(t *testing.T) {
	t.Parallel()

	resp := &model.Message{FullName: "gestalt.test.v1.PingResponse", Name: "PingResponse", ProtoFile: "test.proto"}
	svc := &model.Service{
		FullName: "gestalt.test.v1.Pinger",
		Name:     "Pinger",
		Provider: true,
		Methods: []*model.Method{
			// Empty input + normal output
			{Name: "Ping", Stream: model.Unary, InputIsEmpty: true, Output: resp},
			// Empty input + empty output
			{Name: "Reset", Stream: model.Unary, InputIsEmpty: true, OutputIsEmpty: true},
		},
	}
	idx := &index{
		messages: map[string]*model.Message{resp.FullName: resp},
		enums:    map[string]*model.Enum{},
	}

	r := newServerRenderer(idx, "test")
	r.renderProviderHandler(svc)
	r.renderProviderServer(svc)
	out := r.assembleServer("test")

	for _, want := range []string{
		// Empty-input handler methods take no request parameter.
		// Types are qualified with the _native module alias for static analysis.
		"def ping(self) -> _native.PingResponse:",
		"def reset(self) -> None:",
		// Unimplemented defaults still compile with no request arg.
		"class UnimplementedPingerHandler(PingerHandler):",
		"pinger ping is not implemented",
		// Dispatch adapter accepts wire Empty, does not forward it.
		"class _PingerHandlerServicer(_test_pb2_grpc.PingerServicer):",
		"self._handler.ping()",
		"self._handler.reset()",
		"_empty_pb2.Empty()",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered empty-input provider surface missing %q\n---\n%s", want, out)
		}
	}
}

// TestOperationString verifies the operation tag format used in error messages.
func TestOperationString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		svc, method, want string
	}{
		{"Workflow", "ApplyDefinition", "workflow apply definition"},
		{"Thing", "Foo", "thing foo"},
		{"Cache", "GetMany", "cache get many"},
	}
	for _, tc := range cases {
		got := operationString(tc.svc, tc.method)
		if got != tc.want {
			t.Errorf("operationString(%q, %q) = %q, want %q", tc.svc, tc.method, got, tc.want)
		}
	}
}
