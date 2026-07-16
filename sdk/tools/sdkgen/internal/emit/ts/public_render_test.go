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

	out := renderPublicConverters(view)
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

	out := renderPublicAppClient(services)
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
