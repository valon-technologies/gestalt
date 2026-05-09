package authorization

import (
	"context"
	"reflect"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRemoteAuthorizationProviderOptionalCapabilitiesFollowMetadata(t *testing.T) {
	t.Parallel()

	base := newRemoteAuthorizationProvider(nil, nil, nil, "remote", nil)
	if _, ok := base.(core.AuthorizationProviderEffectiveSearch); ok {
		t.Fatal("remote provider without effective search capabilities unexpectedly implements effective search")
	}
	if _, ok := base.(core.AuthorizationProviderExpansion); ok {
		t.Fatal("remote provider without expansion capability unexpectedly implements expansion")
	}
	if got, want := authorizationHostCapabilities(base), []string{capabilitySearchSubjects}; !reflect.DeepEqual(got, want) {
		t.Fatalf("host capabilities without remote optional caps = %#v, want %#v", got, want)
	}

	effectiveOnly := newRemoteAuthorizationProvider(nil, nil, nil, "remote", []string{capabilityEffectiveSearchResources, capabilityEffectiveSearchSubjects})
	if _, ok := effectiveOnly.(core.AuthorizationProviderEffectiveSearch); !ok {
		t.Fatal("remote provider with effective search capabilities does not implement effective search")
	}
	if _, ok := effectiveOnly.(core.AuthorizationProviderExpansion); ok {
		t.Fatal("remote provider without expansion capability unexpectedly implements expansion")
	}

	allOptional := newRemoteAuthorizationProvider(nil, nil, nil, "remote", []string{
		capabilityEffectiveSearchResources,
		capabilityEffectiveSearchSubjects,
		capabilityExpand,
	})
	if _, ok := allOptional.(core.AuthorizationProviderEffectiveSearch); !ok {
		t.Fatal("remote provider with effective search capabilities does not implement effective search")
	}
	if _, ok := allOptional.(core.AuthorizationProviderExpansion); !ok {
		t.Fatal("remote provider with expansion capability does not implement expansion")
	}
}

func TestProviderServerPreservesOptionalStatusCodes(t *testing.T) {
	t.Parallel()

	provider := fakeEffectiveAuthorizationProvider{
		fakeAuthorizationProvider: fakeAuthorizationProvider{name: "fake"},
		err:                       status.Error(codes.Unimplemented, "optional unsupported"),
	}
	server := NewProviderServer(provider)

	_, err := server.EffectiveSearchResources(context.Background(), &proto.ResourceSearchRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("EffectiveSearchResources code = %v, want %v (err=%v)", got, codes.Unimplemented, err)
	}
	_, err = server.EffectiveSearchSubjects(context.Background(), &proto.EffectiveSubjectSearchRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("EffectiveSearchSubjects code = %v, want %v (err=%v)", got, codes.Unimplemented, err)
	}
	_, err = server.Expand(context.Background(), &proto.ExpandRequest{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("Expand code = %v, want %v (err=%v)", got, codes.Unimplemented, err)
	}
}

type fakeAuthorizationProvider struct {
	name string
}

func (p fakeAuthorizationProvider) Name() string {
	return p.name
}

func (fakeAuthorizationProvider) Evaluate(context.Context, *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	return &core.AccessDecision{}, nil
}

func (fakeAuthorizationProvider) EvaluateMany(context.Context, *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	return &core.AccessEvaluationsResponse{}, nil
}

func (fakeAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return &core.ResourceSearchResponse{}, nil
}

func (fakeAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}

func (fakeAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}

func (fakeAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}

func (fakeAuthorizationProvider) ReadRelationships(context.Context, *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	return &core.ReadRelationshipsResponse{}, nil
}

func (fakeAuthorizationProvider) WriteRelationships(context.Context, *core.WriteRelationshipsRequest) error {
	return nil
}

func (fakeAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{}, nil
}

func (fakeAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}

func (fakeAuthorizationProvider) WriteModel(context.Context, *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	return &core.AuthorizationModelRef{}, nil
}

type fakeEffectiveAuthorizationProvider struct {
	fakeAuthorizationProvider
	err error
}

func (p fakeEffectiveAuthorizationProvider) EffectiveSearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return nil, p.err
}

func (p fakeEffectiveAuthorizationProvider) EffectiveSearchSubjects(context.Context, *core.EffectiveSubjectSearchRequest) (*core.EffectiveSubjectSearchResponse, error) {
	return nil, p.err
}

func (p fakeEffectiveAuthorizationProvider) Expand(context.Context, *core.ExpandRequest) (*core.ExpandResponse, error) {
	return nil, p.err
}
