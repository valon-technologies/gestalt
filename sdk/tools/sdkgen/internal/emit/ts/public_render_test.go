package ts

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestRenderPublicConvertersAlwaysEmitWireMessages(t *testing.T) {
	t.Parallel()

	input := &model.Message{
		FullName:  "gestalt.provider.v1.AppInvokeRequest",
		Name:      "AppInvokeRequest",
		ProtoFile: "sdk/proto/v1/app.proto",
		Fields: []*model.Field{
			{Name: "app", JSONName: "app", Kind: model.KindScalar, Scalar: model.ScalarString},
			{Name: "operation", JSONName: "operation", Kind: model.KindScalar, Scalar: model.ScalarString},
		},
	}
	view := &publicsurface.View{
		Services: []*publicsurface.Service{{
			Service: &model.Service{
				FullName:  "gestalt.provider.v1.App",
				Name:      "App",
				ProtoFile: "sdk/proto/v1/app.proto",
			},
			PublicMethods: []*model.Method{{
				Name:  "Invoke",
				Input: input,
			}},
		}},
	}

	out := renderPublicConverters(view, ServerPublicImports())
	if !strings.Contains(out, "export function toWireAppInvokeRequest") {
		t.Fatalf("missing wire converter:\n%s", out)
	}
	if !strings.Contains(out, "internal/codec/") {
		t.Fatalf("converter must delegate to internal codec:\n%s", out)
	}
	if strings.Contains(out, "create(") {
		t.Fatalf("converter must not copy fields directly:\n%s", out)
	}
}

func TestRenderPublicAppClientAlwaysUsesWireConverter(t *testing.T) {
	t.Parallel()

	services := []*model.Service{{
		FullName:  "gestalt.provider.v1.App",
		Name:      "App",
		ProtoFile: "sdk/proto/v1/app.proto",
		Methods: []*model.Method{{
			Name: "Invoke",
			Input: &model.Message{
				FullName:  "gestalt.provider.v1.AppInvokeRequest",
				Name:      "AppInvokeRequest",
				ProtoFile: "sdk/proto/v1/app.proto",
			},
			Output: &model.Message{
				FullName:  "gestalt.provider.v1.OperationResult",
				Name:      "OperationResult",
				ProtoFile: "sdk/proto/v1/other.proto",
			},
		}},
	}}

	out := renderPublicAppClient(services, ServerPublicImports())
	if !strings.Contains(out, "toWireAppInvokeRequest(request)") {
		t.Fatalf("app client must call wire converter:\n%s", out)
	}
	if strings.Contains(out, "\n        request,\n") {
		t.Fatalf("app client must not pass plain request objects to transport:\n%s", out)
	}
	if !strings.Contains(out, "v1/other_pb.ts") {
		t.Fatalf("app client must import output schema from output proto file:\n%s", out)
	}
}

func TestRenderPublicGatewayErrorServerImports(t *testing.T) {
	t.Parallel()

	out := renderPublicGatewayError(ServerPublicImports())
	if !strings.Contains(out, `from "../../rpc_support.ts"`) {
		t.Fatalf("server gateway_error must import package rpc_support:\n%s", out)
	}
	if !strings.Contains(out, "export function parseGatewayError") {
		t.Fatalf("missing parseGatewayError:\n%s", out)
	}
	if strings.Contains(out, "__RPC_SUPPORT_IMPORT__") {
		t.Fatalf("import placeholder must be substituted:\n%s", out)
	}
}

func TestRenderPublicGatewayErrorWebImports(t *testing.T) {
	t.Parallel()

	out := renderPublicGatewayError(WebPublicImports())
	if !strings.Contains(out, `from "../runtime/rpc_support.ts"`) {
		t.Fatalf("web gateway_error must import runtime rpc_support:\n%s", out)
	}
}

func TestRenderPublicRestRequestMappingUsesPublicMethodMetadata(t *testing.T) {
	t.Parallel()

	out := renderPublicRestRequestMapping()
	for _, want := range []string{
		`from "./methods.ts"`,
		"export function buildRestPath",
		"export function buildRestQuery",
		"export function buildRestBody",
		`Pick<PublicMethod, "http" | "fill" | "reject">`,
		"...method.fill, ...method.reject",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in rest_request_mapping:\n%s", want, out)
		}
	}
}

func TestRenderPublicUnaryTransportIncludesCallOptions(t *testing.T) {
	t.Parallel()

	out := renderPublicUnaryTransport()
	for _, want := range []string{
		"export interface PublicUnaryCallOptions",
		"callOptions?: PublicUnaryCallOptions",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in unary_transport:\n%s", want, out)
		}
	}
}

func TestRenderPublicTransportSupportServerImports(t *testing.T) {
	t.Parallel()

	out := renderPublicTransportSupport(ServerPublicImports())
	for _, want := range []string{
		`from "../../rpc_support.ts"`,
		`from "./unary_transport.ts"`,
		"export function resolveEffectiveAbortSignal",
		"export async function raceWithAbort",
		"export function toTransportGestaltError",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in transport_support:\n%s", want, out)
		}
	}
}

func TestRenderPublicTransportSupportWebImports(t *testing.T) {
	t.Parallel()

	out := renderPublicTransportSupport(WebPublicImports())
	if !strings.Contains(out, `from "../runtime/rpc_support.ts"`) {
		t.Fatalf("web transport_support must import runtime rpc_support:\n%s", out)
	}
}

func TestRenderPublicTransportKernelImports(t *testing.T) {
	t.Parallel()

	server := renderPublicTransportKernel(ServerPublicImports())
	if !strings.Contains(server, `from "../../rpc_support.ts"`) {
		t.Fatalf("server transport kernel must import package rpc_support:\n%s", server)
	}
	for _, want := range []string{
		`from "./gateway_error.ts"`,
		`from "./rest_request_mapping.ts"`,
		"export function prepareRestRequest",
		"export function decodeRestResponse",
	} {
		if !strings.Contains(server, want) {
			t.Fatalf("missing %q in transport_kernel:\n%s", want, server)
		}
	}
	for _, forbidden := range []string{"from \"node:", "from 'node:", "from \"http\"", "fetch("} {
		if strings.Contains(server, forbidden) {
			t.Fatalf("transport kernel must not use %s", forbidden)
		}
	}

	web := renderPublicTransportKernel(WebPublicImports())
	if !strings.Contains(web, `from "../runtime/rpc_support.ts"`) {
		t.Fatalf("web transport kernel must import runtime rpc_support:\n%s", web)
	}
}
