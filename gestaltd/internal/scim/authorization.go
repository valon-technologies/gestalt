package scim

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type authorizationGate struct {
	underlying core.AuthorizationProvider
	users      *coredata.UserService
	scim       *Service
}

func WrapAuthorization(underlying core.AuthorizationProvider, users *coredata.UserService, service *Service) core.AuthorizationProvider {
	if underlying == nil || service == nil || len(service.domainOwners) == 0 && len(service.managed) == 0 {
		return underlying
	}
	return &authorizationGate{underlying: underlying, users: users, scim: service}
}

func (g *authorizationGate) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	eligible, err := g.requestEligible(ctx, req)
	if err != nil {
		// Eligibility datastore failures fail closed as an ordinary denial.
		eligible = false
	}
	if !eligible {
		return &proto.CheckAccessResponse{Allowed: false}, nil
	}
	return g.underlying.CheckAccess(ctx, req)
}

func (g *authorizationGate) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if req == nil || len(req.Requests) == 0 {
		return g.underlying.CheckAccessMany(ctx, req)
	}
	decisions := make([]*proto.CheckAccessResponse, len(req.Requests))
	forward := &proto.CheckAccessManyRequest{}
	indexes := make([]int, 0, len(req.Requests))
	for i, request := range req.Requests {
		eligible, err := g.requestEligible(ctx, request)
		if err != nil {
			// Eligibility datastore failures fail closed as an ordinary denial.
			eligible = false
		}
		if !eligible {
			decisions[i] = &proto.CheckAccessResponse{Allowed: false}
			continue
		}
		forward.Requests = append(forward.Requests, request)
		indexes = append(indexes, i)
	}
	if len(forward.Requests) > 0 {
		response, err := g.underlying.CheckAccessMany(ctx, forward)
		if err != nil {
			return nil, err
		}
		if len(response.Decisions) != len(indexes) {
			return nil, fmt.Errorf("authorization provider returned %d decisions for %d requests", len(response.Decisions), len(indexes))
		}
		for i, decision := range response.Decisions {
			decisions[indexes[i]] = decision
		}
	}
	return &proto.CheckAccessManyResponse{Decisions: decisions}, nil
}

func (g *authorizationGate) requestEligible(ctx context.Context, req *proto.CheckAccessRequest) (bool, error) {
	if req == nil || req.Subject == nil {
		return true, nil
	}
	userID := principal.UserIDFromSubjectID(strings.TrimSpace(req.Subject.Id))
	if principal.ClassifyUserSubjectValue(userID) != principal.UserSubjectFormCanonical {
		return true, nil
	}
	user, err := g.users.GetUser(ctx, userID)
	if err != nil {
		return false, err
	}
	return g.scim.IsEligible(ctx, user.ID, user.Email)
}

func (g *authorizationGate) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return g.underlying.ListRelationships(ctx, req)
}

func (g *authorizationGate) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if req != nil && req.Relationship != nil && req.Relationship.Tuple != nil && g.managed(req.Relationship.Tuple) {
		return nil, fmt.Errorf("relationship is managed by SCIM")
	}
	return g.underlying.AddRelationship(ctx, req)
}

func (g *authorizationGate) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if req != nil && req.RelationshipTuple != nil && g.managed(req.RelationshipTuple) {
		return nil, fmt.Errorf("relationship is managed by SCIM")
	}
	return g.underlying.DeleteRelationship(ctx, req)
}

func (g *authorizationGate) managed(tuple *proto.RelationshipTuple) bool {
	return tuple.Resource != nil && g.scim.ManagedRelationship(tuple.Resource.Type, tuple.Resource.Id, tuple.Relation)
}

func (g *authorizationGate) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return g.underlying.SetAuthorizationState(ctx, req)
}

func (g *authorizationGate) GetActiveModelRef(ctx context.Context) (*proto.GetActiveModelRefResponse, error) {
	return g.underlying.GetActiveModelRef(ctx)
}

func (g *authorizationGate) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return g.underlying.SetActiveModel(ctx, req)
}

func (g *authorizationGate) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return g.underlying.ListActiveModelResourceTypes(ctx, req)
}

func (g *authorizationGate) Ping(ctx context.Context) error { return g.underlying.Ping(ctx) }
func (g *authorizationGate) Close() error                   { return g.underlying.Close() }
