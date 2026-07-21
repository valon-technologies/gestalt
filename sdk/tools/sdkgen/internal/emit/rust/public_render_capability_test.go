package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func TestMixedServiceCapabilityProjection(t *testing.T) {
	t.Parallel()

	svc := &model.Service{
		FullName:  "gestalt.provider.v1.Mixed",
		Name:      "Mixed",
		ProtoFile: "sdk/proto/v1/mixed.proto",
		Methods: []*model.Method{
			{
				Name:       "RestMethod",
				FullMethod: "/gestalt.provider.v1.Mixed/RestMethod",
				Stream:     model.Unary,
				HTTP:       &model.HTTPRule{Verb: "GET", Path: "/api/v2/mixed/items/{id}"},
				Input:      &model.Message{FullName: "gestalt.provider.v1.RestMethodRequest", ProtoFile: "sdk/proto/v1/mixed.proto"},
				Output:     &model.Message{FullName: "gestalt.provider.v1.RestMethodResponse", ProtoFile: "sdk/proto/v1/mixed.proto"},
			},
			{
				Name:       "GrpcOnlyMethod",
				FullMethod: "/gestalt.provider.v1.Mixed/GrpcOnlyMethod",
				Stream:     model.Unary,
				Input:      &model.Message{FullName: "gestalt.provider.v1.GrpcOnlyMethodRequest", ProtoFile: "sdk/proto/v1/mixed.proto"},
				Output:     &model.Message{FullName: "gestalt.provider.v1.GrpcOnlyMethodResponse", ProtoFile: "sdk/proto/v1/mixed.proto"},
			},
		},
	}

	r := newRenderer(&index{
		messages: map[string]*model.Message{
			"gestalt.provider.v1.RestMethodRequest":       svc.Methods[0].Input,
			"gestalt.provider.v1.RestMethodResponse":      svc.Methods[0].Output,
			"gestalt.provider.v1.GrpcOnlyMethodRequest":   svc.Methods[1].Input,
			"gestalt.provider.v1.GrpcOnlyMethodResponse":  svc.Methods[1].Output,
		},
	}, "mixed", "mixed", modulePublic, true, nil, nil)
	r.renderAppClient(svc)
	out := r.assembleGenerated()

	restImplStart := strings.Index(out, "impl<T: UnaryTransport> MixedClient<T> {\n    pub async fn rest_method")
	if restImplStart < 0 {
		t.Fatalf("REST method missing from UnaryTransport impl:\n%s", out)
	}
	restImpl := between(out, "impl<T: UnaryTransport> MixedClient<T> {\n    pub async fn rest_method", "\n}\n\n")
	if strings.Contains(restImpl, "grpc_only_method") {
		t.Fatalf("gRPC-only method leaked into UnaryTransport impl:\n%s", restImpl)
	}
	grpcImpl := between(out, "impl<T: crate::public::generated::unary_transport::GrpcCapable> MixedClient<T>", "\n}\n\n")
	if !strings.Contains(grpcImpl, "pub async fn grpc_only_method(") {
		t.Fatalf("gRPC-only method missing from GrpcCapable impl:\n%s", grpcImpl)
	}
	if strings.Contains(grpcImpl, "rest_method") {
		t.Fatalf("REST method leaked into GrpcCapable impl:\n%s", grpcImpl)
	}
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}
