package invocation

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// maxEvaluatedRelations bounds the work a single decision may push onto the
// server. A provider that returns an unbounded relation list can never make the
// server loop or allocate without limit.
const maxEvaluatedRelations = 64

// ErrAuthorizationUnavailable reports that no authorization evaluator is
// configured. Callers must deny.
var ErrAuthorizationUnavailable = errors.New("authorization provider unavailable")

// ErrMalformedAuthorizationDecision reports a provider response the server
// cannot interpret. Callers must deny.
var ErrMalformedAuthorizationDecision = errors.New("malformed authorization decision")

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

// ResourceAccessRequest describes one authorization question: may this subject
// perform this action on this resource, and if the caller restricts access to a
// role set, which of those roles authorized it.
type ResourceAccessRequest struct {
	SubjectID string
	Action    string
	Resource  *proto.Resource
	// AllowedRoles, when non-empty, requires the evaluator to report one of
	// these relations as the relation that authorized the action.
	AllowedRoles []string
	// SubjectProperties, when set, is the subject metadata the evaluator sees
	// (token scope, client ID, audience). Surfaces that carry it on the
	// single-decision path must carry the same value into a batched decision.
	SubjectProperties *structpb.Struct
}

// ResourceAccessDecision is the evaluator's answer. Role is the relation that
// authorized the action when the evaluator reported one.
type ResourceAccessDecision struct {
	Allowed bool
	Role    string
}

// CheckResourceAccess is the single server-side authorization decision helper.
// Every server surface reaches a decision through it, so direct grants, subject
// sets, default roles, and action precedence stay owned by the provider's
// evaluator instead of being re-derived from relationship listings.
//
// It fails closed: a nil provider, a transport error, or a response the server
// cannot interpret returns an error with Allowed false. No path returns
// Allowed true without an explicit allow from the evaluator.
func CheckResourceAccess(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	req ResourceAccessRequest,
) (ResourceAccessDecision, error) {
	if authorization == nil {
		return ResourceAccessDecision{}, ErrAuthorizationUnavailable
	}
	subjectID := strings.TrimSpace(req.SubjectID)
	action := strings.TrimSpace(req.Action)
	if subjectID == "" || action == "" || req.Resource == nil {
		return ResourceAccessDecision{}, nil
	}

	resp, err := authorization.CheckAccess(ctx, req.protoRequest())
	if err != nil {
		return ResourceAccessDecision{}, err
	}
	if resp == nil {
		return ResourceAccessDecision{}, ErrMalformedAuthorizationDecision
	}
	return resourceAccessDecision(resp, req.AllowedRoles), nil
}

// protoRequest builds the evaluator question for one resource access request.
// The batched and single-decision paths both go through it, so they can never
// ask the evaluator two different questions about the same access request.
func (req ResourceAccessRequest) protoRequest() *proto.CheckAccessRequest {
	out := SubjectAccessRequest(req.SubjectID, req.Action, req.Resource)
	if req.SubjectProperties != nil {
		out.Subject.Properties = req.SubjectProperties
	}
	return out
}

// resourceAccessDecision projects one evaluator response onto the caller's
// allowed-role restriction. It is the only place a CheckAccessResponse becomes
// an allow/deny answer, so a decision read out of a batched response and a
// decision read out of a single response are identical by construction.
func resourceAccessDecision(resp *proto.CheckAccessResponse, allowedRoles []string) ResourceAccessDecision {
	if !resp.GetAllowed() {
		return ResourceAccessDecision{}
	}
	roles := boundedRelations(resp.GetMatchedRelations())
	if len(allowedRoles) == 0 {
		role := ""
		if len(roles) > 0 {
			role = roles[0]
		}
		return ResourceAccessDecision{Allowed: true, Role: role}
	}
	role := matchedAllowedRole(roles, allowedRoles)
	if role == "" {
		return ResourceAccessDecision{}
	}
	return ResourceAccessDecision{Allowed: true, Role: role}
}

// boundedRelations normalizes and caps the relations reported by a decision.
// Blank entries are dropped and duplicates collapse, so a malformed response
// cannot inflate the work a caller performs.
func boundedRelations(relations []string) []string {
	if len(relations) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(relations), maxEvaluatedRelations))
	seen := make(map[string]struct{}, min(len(relations), maxEvaluatedRelations))
	for _, relation := range relations {
		relation = strings.TrimSpace(relation)
		if relation == "" {
			continue
		}
		if _, ok := seen[relation]; ok {
			continue
		}
		seen[relation] = struct{}{}
		out = append(out, relation)
		if len(out) == maxEvaluatedRelations {
			break
		}
	}
	return out
}

func matchedAllowedRole(matchedRelations, allowedRoles []string) string {
	matched := make(map[string]struct{}, len(matchedRelations))
	for _, relation := range matchedRelations {
		if relation = strings.TrimSpace(relation); relation != "" {
			matched[relation] = struct{}{}
		}
	}
	for _, role := range allowedRoles {
		role = strings.TrimSpace(role)
		if _, ok := matched[role]; ok {
			return role
		}
	}
	return ""
}
