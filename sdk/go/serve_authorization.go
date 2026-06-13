package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServeAuthorizationProvider starts a gRPC server for an
// [AuthorizationProvider].
func ServeAuthorizationProvider(ctx context.Context, provider AuthorizationProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthorization, provider))
		proto.RegisterAuthorizationServer(srv, client.NewAuthorizationProviderServer(authorizationHandler{provider: provider}))
	})
}

// authorizationHandler bridges the ergonomic [AuthorizationProvider] facade
// onto the generated transport handler; wire conversion lives in the generated
// adapter. providerRPCError preserves root sentinel-error mapping.
type authorizationHandler struct {
	client.UnimplementedAuthorizationProvider
	provider AuthorizationProvider
}

func (h authorizationHandler) CheckAccess(ctx context.Context, req *client.CheckAccessRequest) (*client.CheckAccessResponse, error) {
	rootReq := &CheckAccessRequest{
		Subject:  clientSubjectToRoot(req.Subject),
		Action:   clientActionToRoot(req.Action),
		Resource: clientResourceToRoot(req.Resource),
	}
	resp, err := h.provider.CheckAccess(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization check access", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization check access")
	}
	return &client.CheckAccessResponse{Allowed: resp.Allowed, ModelID: resp.ModelId}, nil
}

func (h authorizationHandler) CheckAccessMany(ctx context.Context, req *client.CheckAccessManyRequest) (*client.CheckAccessManyResponse, error) {
	rootReq := &CheckAccessManyRequest{
		Requests: make([]*CheckAccessRequest, 0, len(req.Requests)),
	}
	for _, r := range req.Requests {
		rootReq.Requests = append(rootReq.Requests, &CheckAccessRequest{
			Subject:  clientSubjectToRoot(r.Subject),
			Action:   clientActionToRoot(r.Action),
			Resource: clientResourceToRoot(r.Resource),
		})
	}
	resp, err := h.provider.CheckAccessMany(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization check access many", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization check access many")
	}
	out := &client.CheckAccessManyResponse{
		Decisions: make([]*client.CheckAccessResponse, 0, len(resp.Decisions)),
	}
	for _, d := range resp.Decisions {
		if d == nil {
			out.Decisions = append(out.Decisions, nil)
			continue
		}
		out.Decisions = append(out.Decisions, &client.CheckAccessResponse{Allowed: d.Allowed, ModelID: d.ModelId})
	}
	return out, nil
}

func (h authorizationHandler) ListRelationships(ctx context.Context, req *client.ListRelationshipsRequest) (*client.ListRelationshipsResponse, error) {
	rootReq := &ListRelationshipsRequest{
		Filter:    clientRelationshipFilterToRoot(req.Filter),
		PageSize:  req.PageSize,
		PageToken: req.PageToken,
	}
	resp, err := h.provider.ListRelationships(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization list relationships", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization list relationships")
	}
	clientRelationships, convertErr := rootRelationshipsToClient(resp.Relationships)
	if convertErr != nil {
		return nil, rootAuthorizationConvertError("authorization list relationships", convertErr)
	}
	return &client.ListRelationshipsResponse{
		Relationships: clientRelationships,
		NextPageToken: resp.NextPageToken,
	}, nil
}

func (h authorizationHandler) AddRelationship(ctx context.Context, req *client.AddRelationshipRequest) (*client.AddRelationshipResponse, error) {
	rootReq := &AddRelationshipRequest{
		Relationship: clientRelationshipToRoot(req.Relationship),
	}
	resp, err := h.provider.AddRelationship(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization add relationship", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization add relationship")
	}
	clientRelationship, convertErr := rootRelationshipToClient(resp.Relationship)
	if convertErr != nil {
		return nil, rootAuthorizationConvertError("authorization add relationship", convertErr)
	}
	return &client.AddRelationshipResponse{Relationship: clientRelationship}, nil
}

func (h authorizationHandler) DeleteRelationship(ctx context.Context, req *client.DeleteRelationshipRequest) (*client.DeleteRelationshipResponse, error) {
	rootReq := &DeleteRelationshipRequest{
		RelationshipTuple: clientRelationshipTupleToRoot(req.RelationshipTuple),
	}
	resp, err := h.provider.DeleteRelationship(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization delete relationship", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization delete relationship")
	}
	return &client.DeleteRelationshipResponse{}, nil
}

func (h authorizationHandler) SetAuthorizationState(ctx context.Context, req *client.SetAuthorizationStateRequest) (*client.SetAuthorizationStateResponse, error) {
	rootRelationships := make([]*Relationship, 0, len(req.Relationships))
	for _, r := range req.Relationships {
		rootRelationships = append(rootRelationships, clientRelationshipToRoot(r))
	}
	rootReq := &SetAuthorizationStateRequest{
		Model:         clientAuthorizationModelToRoot(req.Model),
		Relationships: rootRelationships,
	}
	resp, err := h.provider.SetAuthorizationState(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization set authorization state", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization set authorization state")
	}
	return &client.SetAuthorizationStateResponse{
		ActiveModel: rootAuthorizationModelRefToClient(resp.ActiveModel),
	}, nil
}

func (h authorizationHandler) GetActiveModelRef(ctx context.Context) (*client.GetActiveModelRefResponse, error) {
	resp, err := h.provider.GetActiveModelRef(ctx)
	if err != nil {
		return nil, providerRPCError("authorization get active model ref", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization get active model ref")
	}
	return &client.GetActiveModelRefResponse{
		Model: rootAuthorizationModelRefToClient(resp.Model),
	}, nil
}

func (h authorizationHandler) SetActiveModel(ctx context.Context, req *client.SetActiveModelRequest) (*client.SetActiveModelResponse, error) {
	rootReq := &SetActiveModelRequest{
		Model: clientAuthorizationModelToRoot(req.Model),
	}
	resp, err := h.provider.SetActiveModel(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization set active model", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization set active model")
	}
	return &client.SetActiveModelResponse{
		Model: rootAuthorizationModelRefToClient(resp.Model),
	}, nil
}

func (h authorizationHandler) ListActiveModelResourceTypes(ctx context.Context, req *client.ListActiveModelResourceTypesRequest) (*client.ListActiveModelResourceTypesResponse, error) {
	rootFilter := (*AuthorizationModelResourceTypeFilter)(nil)
	if f := req.Filter; f != nil {
		rootFilter = &AuthorizationModelResourceTypeFilter{
			Name:        f.Name,
			SourceLayer: SourceLayer(f.SourceLayer),
		}
	}
	rootReq := &ListActiveModelResourceTypesRequest{
		Filter:    rootFilter,
		PageSize:  req.PageSize,
		PageToken: req.PageToken,
	}
	resp, err := h.provider.ListActiveModelResourceTypes(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("authorization list active model resource types", err)
	}
	if resp == nil {
		return nil, rootAuthorizationNilError("authorization list active model resource types")
	}
	clientResourceTypes, convertErr := rootAuthorizationModelResourceTypesToClient(resp.ResourceTypes)
	if convertErr != nil {
		return nil, rootAuthorizationConvertError("authorization list active model resource types", convertErr)
	}
	return &client.ListActiveModelResourceTypesResponse{
		ResourceTypes: clientResourceTypes,
		NextPageToken: resp.NextPageToken,
		ModelID:       resp.ModelId,
	}, nil
}

// --- Conversion helpers ---

func rootAuthorizationNilError(op string) error {
	return status.Errorf(codes.Internal, "%s returned nil response", op)
}

func rootAuthorizationConvertError(op string, err error) error {
	return status.Errorf(codes.Internal, "%s returned invalid response: %v", op, err)
}

// clientSubjectToRoot converts client.Subject → root AuthorizationSubject.
func clientSubjectToRoot(in *client.Subject) *AuthorizationSubject {
	if in == nil {
		return nil
	}
	return &AuthorizationSubject{Type: in.Type, Id: in.ID, Properties: in.Properties}
}

// clientActionToRoot converts client.Action → root AuthorizationAction.
func clientActionToRoot(in *client.Action) *AuthorizationAction {
	if in == nil {
		return nil
	}
	return &AuthorizationAction{Name: in.Name, Properties: in.Properties}
}

// clientResourceToRoot converts client.Resource → root AuthorizationResource.
func clientResourceToRoot(in *client.Resource) *AuthorizationResource {
	if in == nil {
		return nil
	}
	return &AuthorizationResource{Type: in.Type, Id: in.ID, Properties: in.Properties}
}

func clientRelationshipFilterToRoot(in *client.RelationshipFilter) *RelationshipFilter {
	if in == nil {
		return nil
	}
	return &RelationshipFilter{
		Target:           clientRelationshipTargetToRoot(in.Target),
		Relation:         in.Relation,
		Resource:         clientResourceToRoot(in.Resource),
		TargetType:       RelationshipTargetType(in.TargetType),
		TargetEntityType: in.TargetEntityType,
		ResourceType:     in.ResourceType,
		SourceLayer:      SourceLayer(in.SourceLayer),
	}
}

func clientRelationshipTargetToRoot(in *client.RelationshipTarget) *RelationshipTarget {
	if in == nil {
		return nil
	}
	switch kind := in.Kind.(type) {
	case *client.RelationshipTargetKindSubject:
		return &RelationshipTarget{Subject: clientSubjectToRoot(kind.Value)}
	case *client.RelationshipTargetKindResource:
		return &RelationshipTarget{Resource: clientResourceToRoot(kind.Value)}
	case *client.RelationshipTargetKindSubjectSet:
		return &RelationshipTarget{SubjectSet: clientSubjectSetToRoot(kind.Value)}
	default:
		return &RelationshipTarget{}
	}
}

func clientSubjectSetToRoot(in *client.SubjectSet) *SubjectSet {
	if in == nil {
		return nil
	}
	return &SubjectSet{Resource: clientResourceToRoot(in.Resource), Relation: in.Relation}
}

func clientRelationshipToRoot(in *client.Relationship) *Relationship {
	if in == nil {
		return nil
	}
	return &Relationship{
		Tuple:       clientRelationshipTupleToRoot(in.Tuple),
		Properties:  in.Properties,
		SourceLayer: SourceLayer(in.SourceLayer),
	}
}

func clientRelationshipTupleToRoot(in *client.RelationshipTuple) *RelationshipTuple {
	if in == nil {
		return nil
	}
	return &RelationshipTuple{
		Target:   clientRelationshipTargetToRoot(in.Target),
		Relation: in.Relation,
		Resource: clientResourceToRoot(in.Resource),
	}
}

func clientAuthorizationModelToRoot(in *client.AuthorizationModel) *AuthorizationModel {
	if in == nil {
		return nil
	}
	return &AuthorizationModel{
		Id:            in.ID,
		Version:       in.Version,
		ResourceTypes: clientAuthorizationModelResourceTypesToRoot(in.ResourceTypes),
	}
}

func clientAuthorizationModelResourceTypesToRoot(in []*client.AuthorizationModelResourceType) []*AuthorizationModelResourceType {
	out := make([]*AuthorizationModelResourceType, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &AuthorizationModelResourceType{
			Name:                item.Name,
			Relations:           clientModelRelationsToRoot(item.Relations),
			Actions:             clientModelActionsToRoot(item.Actions),
			SourceLayer:         SourceLayer(item.SourceLayer),
			DefaultAccessPolicy: DefaultAccessPolicy(item.DefaultAccessPolicy),
		})
	}
	return out
}

func clientModelRelationsToRoot(in []*client.ModelRelation) []*ModelRelation {
	out := make([]*ModelRelation, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &ModelRelation{
			Name:           item.Name,
			AllowedTargets: clientModelAllowedTargetsToRoot(item.AllowedTargets),
		})
	}
	return out
}

func clientModelAllowedTargetsToRoot(in []*client.ModelAllowedTarget) []*ModelAllowedTarget {
	out := make([]*ModelAllowedTarget, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		switch kind := item.Kind.(type) {
		case *client.ModelAllowedTargetKindSubjectType:
			out = append(out, &ModelAllowedTarget{SubjectType: kind.Value})
		case *client.ModelAllowedTargetKindResourceType:
			out = append(out, &ModelAllowedTarget{ResourceType: kind.Value})
		case *client.ModelAllowedTargetKindSubjectSetType:
			if kind.Value != nil {
				out = append(out, &ModelAllowedTarget{SubjectSetType: &SubjectSetType{
					ResourceType: kind.Value.ResourceType,
					Relation:     kind.Value.Relation,
				}})
			} else {
				out = append(out, &ModelAllowedTarget{})
			}
		default:
			out = append(out, &ModelAllowedTarget{})
		}
	}
	return out
}

func clientModelActionsToRoot(in []*client.ModelAction) []*ModelAction {
	out := make([]*ModelAction, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &ModelAction{
			Name:      item.Name,
			Relations: append([]string(nil), item.Relations...),
		})
	}
	return out
}

// --- Root → Client conversions ---

func rootAuthorizationModelRefToClient(in *AuthorizationModelRef) *client.AuthorizationModelRef {
	if in == nil {
		return nil
	}
	out := &client.AuthorizationModelRef{
		ID:      in.Id,
		Version: in.Version,
	}
	if !in.CreatedAt.IsZero() {
		t := in.CreatedAt
		out.CreatedAt = &t
	}
	return out
}

func rootRelationshipToClient(in *Relationship) (*client.Relationship, error) {
	if in == nil {
		return nil, nil
	}
	tuple, err := rootRelationshipTupleToClient(in.Tuple)
	if err != nil {
		return nil, err
	}
	return &client.Relationship{
		Tuple:       tuple,
		Properties:  in.Properties,
		SourceLayer: client.SourceLayer(in.SourceLayer),
	}, nil
}

func rootRelationshipsToClient(in []*Relationship) ([]*client.Relationship, error) {
	out := make([]*client.Relationship, 0, len(in))
	for i, item := range in {
		r, err := rootRelationshipToClient(item)
		if err != nil {
			return nil, &indexedError{index: i, err: err}
		}
		out = append(out, r)
	}
	return out, nil
}

type indexedError struct {
	index int
	err   error
}

func (e *indexedError) Error() string {
	return e.err.Error()
}

func rootRelationshipTupleToClient(in *RelationshipTuple) (*client.RelationshipTuple, error) {
	if in == nil {
		return nil, nil
	}
	target, err := rootRelationshipTargetToClient(in.Target)
	if err != nil {
		return nil, err
	}
	resource, err := rootResourceToClient(in.Resource)
	if err != nil {
		return nil, err
	}
	return &client.RelationshipTuple{
		Target:   target,
		Relation: in.Relation,
		Resource: resource,
	}, nil
}

func rootRelationshipTargetToClient(in *RelationshipTarget) (*client.RelationshipTarget, error) {
	if in == nil {
		return nil, nil
	}
	if in.Subject != nil {
		subject, err := rootSubjectToClient(in.Subject)
		if err != nil {
			return nil, err
		}
		return &client.RelationshipTarget{Kind: &client.RelationshipTargetKindSubject{Value: subject}}, nil
	}
	if in.Resource != nil {
		resource, err := rootResourceToClient(in.Resource)
		if err != nil {
			return nil, err
		}
		return &client.RelationshipTarget{Kind: &client.RelationshipTargetKindResource{Value: resource}}, nil
	}
	if in.SubjectSet != nil {
		ss, err := rootSubjectSetToClient(in.SubjectSet)
		if err != nil {
			return nil, err
		}
		return &client.RelationshipTarget{Kind: &client.RelationshipTargetKindSubjectSet{Value: ss}}, nil
	}
	return &client.RelationshipTarget{}, nil
}

func rootSubjectToClient(in *AuthorizationSubject) (*client.Subject, error) {
	if in == nil {
		return nil, nil
	}
	return &client.Subject{Type: in.Type, ID: in.Id, Properties: in.Properties}, nil
}

func rootResourceToClient(in *AuthorizationResource) (*client.Resource, error) {
	if in == nil {
		return nil, nil
	}
	return &client.Resource{Type: in.Type, ID: in.Id, Properties: in.Properties}, nil
}

func rootSubjectSetToClient(in *SubjectSet) (*client.SubjectSet, error) {
	if in == nil {
		return nil, nil
	}
	resource, err := rootResourceToClient(in.Resource)
	if err != nil {
		return nil, err
	}
	return &client.SubjectSet{Resource: resource, Relation: in.Relation}, nil
}

func rootAuthorizationModelResourceTypesToClient(in []*AuthorizationModelResourceType) ([]*client.AuthorizationModelResourceType, error) {
	out := make([]*client.AuthorizationModelResourceType, 0, len(in))
	for i, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		relations, err := rootModelRelationsToClient(item.Relations)
		if err != nil {
			return nil, &indexedError{index: i, err: err}
		}
		out = append(out, &client.AuthorizationModelResourceType{
			Name:                item.Name,
			Relations:           relations,
			Actions:             rootModelActionsToClient(item.Actions),
			SourceLayer:         client.SourceLayer(item.SourceLayer),
			DefaultAccessPolicy: client.DefaultAccessPolicy(item.DefaultAccessPolicy),
		})
	}
	return out, nil
}

func rootModelRelationsToClient(in []*ModelRelation) ([]*client.ModelRelation, error) {
	out := make([]*client.ModelRelation, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		targets, err := rootModelAllowedTargetsToClient(item.AllowedTargets)
		if err != nil {
			return nil, err
		}
		out = append(out, &client.ModelRelation{Name: item.Name, AllowedTargets: targets})
	}
	return out, nil
}

func rootModelAllowedTargetsToClient(in []*ModelAllowedTarget) ([]*client.ModelAllowedTarget, error) {
	out := make([]*client.ModelAllowedTarget, 0, len(in))
	for _, item := range in {
		out = append(out, rootModelAllowedTargetToClient(item))
	}
	return out, nil
}

func rootModelAllowedTargetToClient(in *ModelAllowedTarget) *client.ModelAllowedTarget {
	if in == nil {
		return nil
	}
	if in.SubjectType != "" {
		return &client.ModelAllowedTarget{Kind: &client.ModelAllowedTargetKindSubjectType{Value: in.SubjectType}}
	}
	if in.ResourceType != "" {
		return &client.ModelAllowedTarget{Kind: &client.ModelAllowedTargetKindResourceType{Value: in.ResourceType}}
	}
	if in.SubjectSetType != nil {
		return &client.ModelAllowedTarget{Kind: &client.ModelAllowedTargetKindSubjectSetType{Value: &client.SubjectSetType{
			ResourceType: in.SubjectSetType.ResourceType,
			Relation:     in.SubjectSetType.Relation,
		}}}
	}
	return &client.ModelAllowedTarget{}
}

func rootModelActionsToClient(in []*ModelAction) []*client.ModelAction {
	out := make([]*client.ModelAction, 0, len(in))
	for _, item := range in {
		if item == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &client.ModelAction{Name: item.Name, Relations: append([]string(nil), item.Relations...)})
	}
	return out
}

