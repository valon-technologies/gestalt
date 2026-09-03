package authorizationstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

// Apply merges the requested model and relationships into the active runtime
// authorization store without replacing unrelated runtime-only state.
func Apply(
	ctx context.Context,
	provider core.AuthorizationProvider,
	req *proto.SetAuthorizationStateRequest,
) (*proto.SetAuthorizationStateResponse, error) {
	if provider == nil {
		return nil, fmt.Errorf("authorization provider is required")
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	model := gproto.Clone(req.GetModel()).(*proto.AuthorizationModel)
	if model == nil {
		model = &proto.AuthorizationModel{}
	}
	runtimeResourceTypes, err := listRuntimeModelResourceTypes(ctx, provider)
	if err != nil {
		return nil, err
	}
	model.ResourceTypes = mergeModelResourceTypes(model.GetResourceTypes(), runtimeResourceTypes)
	if err := stampModel(model, time.Now()); err != nil {
		return nil, err
	}
	if err := ensureRelationships(ctx, provider, req.GetRelationships()); err != nil {
		return nil, err
	}
	resp, err := provider.SetActiveModel(ctx, &proto.SetActiveModelRequest{Model: model})
	if err != nil {
		return nil, fmt.Errorf("set active model: %w", err)
	}
	return &proto.SetAuthorizationStateResponse{ActiveModel: resp.GetModel()}, nil
}

func ensureRelationships(ctx context.Context, provider core.AuthorizationProvider, desired []*proto.Relationship) error {
	existing, err := listAllRelationships(ctx, provider)
	if err != nil {
		return err
	}
	existingKeys := make(map[string]struct{}, len(existing))
	for _, relationship := range existing {
		key, err := relationshipTupleKey(relationship.GetTuple())
		if err != nil {
			return err
		}
		existingKeys[key] = struct{}{}
	}
	for _, relationship := range desired {
		if relationship == nil || relationship.GetTuple() == nil {
			continue
		}
		key, err := relationshipTupleKey(relationship.GetTuple())
		if err != nil {
			return err
		}
		if _, ok := existingKeys[key]; ok {
			continue
		}
		if _, err := provider.AddRelationship(ctx, &proto.AddRelationshipRequest{
			Relationship: gproto.Clone(relationship).(*proto.Relationship),
		}); err != nil {
			return fmt.Errorf("add relationship %q: %w", key, err)
		}
		existingKeys[key] = struct{}{}
	}
	return nil
}

func listAllRelationships(ctx context.Context, provider core.AuthorizationProvider) ([]*proto.Relationship, error) {
	var out []*proto.Relationship
	pageToken := ""
	for {
		resp, err := provider.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list relationships: %w", err)
		}
		out = append(out, resp.GetRelationships()...)
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}

func listRuntimeModelResourceTypes(ctx context.Context, provider core.AuthorizationProvider) ([]*proto.AuthorizationModelResourceType, error) {
	var out []*proto.AuthorizationModelResourceType
	pageToken := ""
	for {
		resp, err := provider.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
			Filter: &proto.AuthorizationModelResourceTypeFilter{
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list runtime model resource types: %w", err)
		}
		for _, resourceType := range resp.GetResourceTypes() {
			if resourceType.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
				continue
			}
			out = append(out, gproto.Clone(resourceType).(*proto.AuthorizationModelResourceType))
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}

func mergeModelResourceTypes(static, runtime []*proto.AuthorizationModelResourceType) []*proto.AuthorizationModelResourceType {
	out := make([]*proto.AuthorizationModelResourceType, 0, len(static)+len(runtime))
	staticNames := make(map[string]struct{}, len(static))
	for _, resourceType := range static {
		name := strings.TrimSpace(resourceType.GetName())
		staticNames[name] = struct{}{}
		out = append(out, resourceType)
	}
	for _, resourceType := range runtime {
		name := strings.TrimSpace(resourceType.GetName())
		if _, ok := staticNames[name]; ok {
			continue
		}
		out = append(out, resourceType)
	}
	return out
}

func stampModel(model *proto.AuthorizationModel, now time.Time) error {
	id, err := modelContentHash(model)
	if err != nil {
		return err
	}
	model.Id = id
	model.Version = strconv.FormatInt(now.Unix(), 10)
	return nil
}

func modelContentHash(model *proto.AuthorizationModel) (string, error) {
	content := gproto.Clone(model).(*proto.AuthorizationModel)
	content.Id = ""
	content.Version = ""
	data, err := gproto.MarshalOptions{Deterministic: true}.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("hash authorization model content: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func relationshipTupleKey(tuple *proto.RelationshipTuple) (string, error) {
	if tuple == nil {
		return "", fmt.Errorf("relationship tuple is required")
	}
	data, err := gproto.MarshalOptions{Deterministic: true}.Marshal(tuple)
	if err != nil {
		return "", fmt.Errorf("marshal relationship tuple: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
