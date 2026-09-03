package scim

import (
	"context"
	"fmt"
	"sort"
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
	if underlying == nil || !service.Enabled() {
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
		if response == nil {
			return nil, fmt.Errorf("authorization provider returned an empty batch response")
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
	if g.scim != nil && g.scim.compact != nil {
		return g.scim.compact.IsEligible(ctx, user.ID, user.Email)
	}
	return g.scim.IsEligible(ctx, user.ID, user.Email)
}

func (g *authorizationGate) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return g.underlying.ListRelationships(ctx, req)
}

func (g *authorizationGate) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	if req == nil {
		return g.underlying.WriteRelationships(ctx, req)
	}
	affected := map[string]map[string]struct{}{}
	runtimeChanges := map[string]struct{}{}
	for _, update := range req.Updates {
		if update == nil || update.Relationship == nil {
			continue
		}
		relationship := update.Relationship
		affectsRuntime := relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_RUNTIME
		if update.Operation == proto.RelationshipUpdate_OPERATION_DELETE && relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
			var err error
			affectsRuntime, err = g.scim.compact.relationshipPresent(ctx, relationship.GetTuple())
			if err != nil {
				return nil, err
			}
		}
		if !affectsRuntime {
			continue
		}
		runtimeChanges[tupleKey(relationship.GetTuple())] = struct{}{}
		if update.Operation == proto.RelationshipUpdate_OPERATION_DELETE {
			if err := g.scim.compact.captureRelationshipAffectedUsers(ctx, relationship.GetTuple(), affected); err != nil {
				return nil, err
			}
		}
	}
	response, err := g.underlying.WriteRelationships(ctx, req)
	if err != nil {
		return response, err
	}
	ids := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, update := range req.Updates {
		if update == nil || update.Relationship == nil {
			continue
		}
		tuple := update.Relationship.GetTuple()
		key := tupleKey(tuple)
		if _, affectsRuntime := runtimeChanges[key]; !affectsRuntime {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		touch, err := g.scim.compact.relationshipTouchIDs(ctx, tuple)
		if err != nil {
			return response, err
		}
		for id := range touch {
			ids[id] = struct{}{}
		}
	}
	if err := g.scim.compact.collectClientCoreUserTouchIDs(ctx, affected, ids); err != nil {
		return response, err
	}
	if err := g.scim.compact.touchRows(ctx, ids); err != nil {
		return response, err
	}
	return response, nil
}

func (g *authorizationGate) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	relationship := req.GetRelationship()
	_, err := g.WriteRelationships(ctx, &proto.WriteRelationshipsRequest{Updates: []*proto.RelationshipUpdate{{
		Operation:    proto.RelationshipUpdate_OPERATION_TOUCH,
		Relationship: relationship,
	}}})
	if err != nil {
		return nil, err
	}
	return &proto.AddRelationshipResponse{Relationship: relationship}, nil
}

func (g *authorizationGate) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	_, err := g.WriteRelationships(ctx, &proto.WriteRelationshipsRequest{Updates: []*proto.RelationshipUpdate{{
		Operation: proto.RelationshipUpdate_OPERATION_DELETE,
		Relationship: &proto.Relationship{
			Tuple: req.GetRelationshipTuple(),
		},
	}}})
	if err != nil {
		return nil, err
	}
	return &proto.DeleteRelationshipResponse{}, nil
}

func (g *authorizationGate) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	var before []*proto.Relationship
	if g.scim != nil && g.scim.compact != nil {
		var err error
		before, err = g.scim.compact.listRelationships(ctx, nil, 100)
		if err != nil {
			return nil, err
		}
	}
	changed := changedRuntimeRelationships(before, req.GetRelationships())
	affected := map[string]map[string]struct{}{}
	if g.scim != nil && g.scim.compact != nil {
		for _, relationship := range changed.removed {
			if err := g.scim.compact.captureRelationshipAffectedUsers(ctx, relationship.GetTuple(), affected); err != nil {
				return nil, err
			}
		}
	}
	response, err := g.underlying.SetAuthorizationState(ctx, req)
	if err != nil {
		return response, err
	}
	if g.scim != nil && g.scim.compact != nil {
		ids := map[string]struct{}{}
		for _, relationship := range changed.added {
			touch, touchErr := g.scim.compact.relationshipTouchIDs(ctx, relationship.GetTuple())
			if touchErr != nil {
				return response, touchErr
			}
			for id := range touch {
				ids[id] = struct{}{}
			}
		}
		for _, relationship := range changed.removed {
			touch, touchErr := g.scim.compact.relationshipTouchIDs(ctx, relationship.GetTuple())
			if touchErr != nil {
				return response, touchErr
			}
			for id := range touch {
				ids[id] = struct{}{}
			}
		}
		if touchErr := g.scim.compact.collectClientCoreUserTouchIDs(ctx, affected, ids); touchErr != nil {
			return response, touchErr
		}
		if touchErr := g.scim.compact.touchRows(ctx, ids); touchErr != nil {
			return response, touchErr
		}
	}
	return response, nil
}

type relationshipChanges struct {
	added, removed []*proto.Relationship
}

func changedRuntimeRelationships(before, after []*proto.Relationship) relationshipChanges {
	left := make(map[string]*proto.Relationship, len(before))
	right := make(map[string]*proto.Relationship, len(after))
	for _, relationship := range before {
		if relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_RUNTIME {
			left[relationshipKeyWithLayer(relationship)] = relationship
		}
	}
	for _, relationship := range after {
		if relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_RUNTIME {
			right[relationshipKeyWithLayer(relationship)] = relationship
		}
	}
	changes := relationshipChanges{}
	for key, relationship := range left {
		if _, ok := right[key]; !ok {
			changes.removed = append(changes.removed, relationship)
		}
	}
	for key, relationship := range right {
		if _, ok := left[key]; !ok {
			changes.added = append(changes.added, relationship)
		}
	}
	sort.Slice(changes.added, func(i, j int) bool {
		return relationshipKeyWithLayer(changes.added[i]) < relationshipKeyWithLayer(changes.added[j])
	})
	sort.Slice(changes.removed, func(i, j int) bool {
		return relationshipKeyWithLayer(changes.removed[i]) < relationshipKeyWithLayer(changes.removed[j])
	})
	return changes
}

func relationshipKeyWithLayer(relationship *proto.Relationship) string {
	if relationship == nil {
		return ""
	}
	return tupleKey(relationship.GetTuple()) + "\x00" + relationship.GetSourceLayer().String()
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
