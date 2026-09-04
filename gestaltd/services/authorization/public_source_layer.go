package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

// Public relationship writes that omit source_layer are runtime grants from the
// Members UI and other public surfaces. Stamp RUNTIME so roster projection does
// not treat them as locked deploy-config policy.
func stampPublicRuntimeAddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) *proto.AddRelationshipRequest {
	if _, ok := publicrpc.PublicOriginFromContext(ctx); !ok {
		return req
	}
	relationship := req.GetRelationship()
	if relationship == nil || relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
		return req
	}
	cloned := gproto.Clone(req).(*proto.AddRelationshipRequest)
	cloned.Relationship.SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME
	return cloned
}

func stampPublicRuntimeWriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) *proto.WriteRelationshipsRequest {
	if _, ok := publicrpc.PublicOriginFromContext(ctx); !ok {
		return req
	}
	needsStamp := false
	for _, update := range req.GetUpdates() {
		if update.GetOperation() != proto.RelationshipUpdate_OPERATION_TOUCH {
			continue
		}
		relationship := update.GetRelationship()
		if relationship != nil && relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
			needsStamp = true
			break
		}
	}
	if !needsStamp {
		return req
	}
	cloned := gproto.Clone(req).(*proto.WriteRelationshipsRequest)
	for _, update := range cloned.GetUpdates() {
		if update.GetOperation() != proto.RelationshipUpdate_OPERATION_TOUCH {
			continue
		}
		relationship := update.GetRelationship()
		if relationship != nil && relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
			relationship.SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME
		}
	}
	return cloned
}
