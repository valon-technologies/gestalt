package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func TestRenderPublicMetadataWireJSONCallbacks(t *testing.T) {
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
	output := &model.Message{
		FullName:  "gestalt.provider.v1.OperationResult",
		Name:      "OperationResult",
		ProtoFile: "sdk/proto/v1/app.proto",
		Fields: []*model.Field{
			{Name: "status", JSONName: "status", Kind: model.KindScalar, Scalar: model.ScalarInt32},
			{Name: "body", JSONName: "body", Kind: model.KindBytes},
		},
	}
	methods := []publicsurface.PublicMethod{{
		Service:    "gestalt.provider.v1.App",
		Method:     "Invoke",
		FullMethod: "/gestalt.provider.v1.App/Invoke",
		Input:      input,
		Output:     output,
		REST: &publicsurface.RESTRule{
			Verb:         "POST",
			PathTemplate: "/api/v2/app/{app}/operations/{operation}",
			Body:         publicsurface.BodyStar,
			PathFields: []publicsurface.PublicField{
				{Name: "app", JSONName: "app"},
				{Name: "operation", JSONName: "operation"},
			},
		},
	}}

	meta := newRenderer(&index{}, "metadata", "metadata", modulePublic, true)
	meta.renderPublicMetadata(methods)
	out := meta.assembleGenerated()

	for _, want := range []string{
		"pub type EncodeRequestJson",
		"pub type DecodeResponseJson",
		"encode_request_json: Some(encode_invoke_request_json)",
		"decode_response_json: Some(decode_invoke_response_json)",
		"encode_wire_app_invoke_request_json",
		"decode_wire_operation_result_json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metadata output missing %q:\n%s", want, out)
		}
	}
}
