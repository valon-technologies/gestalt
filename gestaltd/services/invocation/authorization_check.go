package invocation

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func SubjectAccessRequest(subjectID, action string, resource *proto.Resource) *proto.CheckAccessRequest {
	return &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: strings.TrimSpace(subjectID)},
		Action:   &proto.Action{Name: strings.TrimSpace(action)},
		Resource: resource,
	}
}

func CheckSubjectAccess(ctx context.Context, authorization core.AuthorizationProvider, req *proto.CheckAccessRequest) (bool, error) {
	resp, err := authorization.CheckAccess(ctx, req)
	if err != nil || resp == nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}
