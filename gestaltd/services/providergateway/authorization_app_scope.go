package providergateway

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	gproto "google.golang.org/protobuf/proto"
)

const (
	appAuthorizationResourceType = "app"
	appAdminRelation               = "admin"
)

func relationshipTupleFromAuthorizationRequest(fullMethod string, req gproto.Message) (*proto.RelationshipTuple, bool) {
	if req == nil {
		return nil, false
	}
	_, method := splitFullMethod(fullMethod)
	switch method {
	case "AddRelationship":
		addReq, ok := req.(*proto.AddRelationshipRequest)
		if !ok || addReq.GetRelationship() == nil {
			return nil, false
		}
		return addReq.GetRelationship().GetTuple(), true
	case "DeleteRelationship":
		deleteReq, ok := req.(*proto.DeleteRelationshipRequest)
		if !ok {
			return nil, false
		}
		return deleteReq.GetRelationshipTuple(), true
	default:
		return nil, false
	}
}

func allowsAppScopedRelationshipMutation(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	subjectID string,
	tuple *proto.RelationshipTuple,
) (bool, error) {
	if authorization == nil || tuple == nil {
		return false, nil
	}
	if strings.TrimSpace(tuple.GetResource().GetType()) != appAuthorizationResourceType {
		return false, nil
	}
	appID := strings.TrimSpace(tuple.GetResource().GetId())
	if appID == "" {
		return false, nil
	}
	if strings.TrimSpace(tuple.GetRelation()) == "" {
		return false, nil
	}
	if !relationshipTupleHasDelegableTarget(tuple) {
		return false, nil
	}
	decision, err := invocation.CheckResourceAccess(ctx, authorization, invocation.ResourceAccessRequest{
		SubjectID:    subjectID,
		Action:       appID,
		Resource:     &proto.Resource{Type: appAuthorizationResourceType, Id: appID},
		AllowedRoles: []string{appAdminRelation},
	})
	if err != nil {
		return false, err
	}
	return decision.Allowed && decision.Role == appAdminRelation, nil
}

func relationshipTupleHasDelegableTarget(tuple *proto.RelationshipTuple) bool {
	switch tuple.GetTarget().GetKind().(type) {
	case *proto.RelationshipTarget_Subject:
		return strings.TrimSpace(tuple.GetTarget().GetSubject().GetId()) != ""
	case *proto.RelationshipTarget_SubjectSet:
		subjectSet := tuple.GetTarget().GetSubjectSet()
		return strings.TrimSpace(subjectSet.GetResource().GetType()) != "" &&
			strings.TrimSpace(subjectSet.GetResource().GetId()) != ""
	default:
		return false
	}
}
