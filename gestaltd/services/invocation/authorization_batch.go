package invocation

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// maxBatchedAccessChecks bounds one batched authorization call so a large
// catalog or app roster cannot turn a single listing request into an unbounded
// provider request.
const maxBatchedAccessChecks = 1000

// ErrBatchedAccessTooLarge reports that a caller asked for more decisions in
// one batch than the server will send to the evaluator.
var ErrBatchedAccessTooLarge = fmt.Errorf("batched authorization check exceeds %d requests", maxBatchedAccessChecks)

// CheckResourceAccessMany answers many authorization questions with ONE
// provider call. Listing surfaces use it so a catalog or app roster costs one
// decision round trip instead of one per entry.
//
// Every answer is built by the same helpers CheckResourceAccess uses: the same
// question builder and the same response projection. A batched decision and a
// single decision for the same (subject, action, resource, allowed roles) are
// therefore identical by construction, not by convention.
//
// It fails closed exactly like CheckResourceAccess: a nil provider, a transport
// error, or a response the server cannot interpret returns an error and no
// allow. Callers that must not hide entries on failure are expected to fall
// back to CheckResourceAccess per entry rather than treat the error as a deny.
func CheckResourceAccessMany(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	reqs []ResourceAccessRequest,
) ([]ResourceAccessDecision, error) {
	if authorization == nil {
		return nil, ErrAuthorizationUnavailable
	}
	if len(reqs) > maxBatchedAccessChecks {
		return nil, ErrBatchedAccessTooLarge
	}

	decisions := make([]ResourceAccessDecision, len(reqs))
	batch := &proto.CheckAccessManyRequest{Requests: make([]*proto.CheckAccessRequest, 0, len(reqs))}
	indexes := make([]int, 0, len(reqs))
	for i, req := range reqs {
		question := req.protoRequest()
		if question.GetSubject().GetId() == "" || question.GetAction().GetName() == "" || question.GetResource() == nil {
			// CheckResourceAccess answers a degenerate question with a deny and
			// no provider call; the batch does the same and stays aligned.
			continue
		}
		indexes = append(indexes, i)
		batch.Requests = append(batch.Requests, question)
	}
	if len(batch.Requests) == 0 {
		return decisions, nil
	}

	resp, err := authorization.CheckAccessMany(ctx, batch)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.GetDecisions()) != len(batch.Requests) {
		return nil, ErrMalformedAuthorizationDecision
	}
	for n, decision := range resp.GetDecisions() {
		if decision == nil {
			return nil, ErrMalformedAuthorizationDecision
		}
		i := indexes[n]
		decisions[i] = resourceAccessDecision(decision, reqs[i].AllowedRoles)
	}
	return decisions, nil
}

// OperationAccessQuery is one operation-listing question: may this principal
// invoke this operation on this app.
type OperationAccessQuery struct {
	Provider     string
	Operation    string
	AllowedRoles []string
}

// OperationAccessChecker answers many operation-access questions with one
// batched evaluator call. Catalog and MCP listing depend on this interface so
// they can reach exactly the decisions invocation reaches.
type OperationAccessChecker interface {
	CheckOperationAccessMany(
		ctx context.Context,
		p *principal.Principal,
		queries []OperationAccessQuery,
	) ([]error, error)
}

// CheckOperationAccessMany answers many operation-access questions with one
// batched evaluator call. Element i is nil when the operation is allowed and
// otherwise carries the same ErrAuthorizationDenied error CheckOperationAccess
// would return for that operation.
//
// Every answer runs through the same token-scope check, the same remote
// delegation check, and the same evaluator projection the single-decision path
// uses, so a listing answer and the invocation answer for the same operation
// cannot disagree. Allowed roles are applied here as well, matching
// authorizeOperation, so a tool the caller could not actually call is not
// listed.
//
// When the provider cannot serve the batch - a transport failure, an
// unimplemented batch RPC, or a response the server cannot interpret - each
// unresolved question falls back to the single-decision path instead of being
// reported as denied. Listing then costs more calls, never fewer results.
func (b *Broker) CheckOperationAccessMany(
	ctx context.Context,
	p *principal.Principal,
	queries []OperationAccessQuery,
) ([]error, error) {
	results := make([]error, len(queries))
	pending := make([]int, 0, len(queries))
	for i, query := range queries {
		switch {
		case !principal.AllowsOperationPermission(p, query.Provider, query.Operation):
			results[i] = operationAccessDenied(query)
		case b.providerDelegatesRemoteAuthorization(ctx, query.Provider):
			// The remote app owns this decision, exactly as at invoke time.
		default:
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 || b == nil || b.authorization == nil {
		// A server with no authorization provider allows every operation its
		// token scope allows; that is what CheckOperationAccess does today.
		return results, nil
	}

	subjectID, err := principal.ResolveCredentialSubjectID(ctx, b.users, p)
	if err != nil {
		for _, i := range pending {
			results[i] = fmt.Errorf("%w: %s.%s: %v",
				ErrAuthorizationDenied, queries[i].Provider, queries[i].Operation, err)
		}
		return results, nil
	}

	properties := subjectAccessProperties(p)
	reqs := make([]ResourceAccessRequest, 0, len(pending))
	for _, i := range pending {
		reqs = append(reqs, ResourceAccessRequest{
			SubjectID:         subjectID,
			Action:            queries[i].Operation,
			Resource:          b.authorizationResource(queries[i].Provider),
			AllowedRoles:      queries[i].AllowedRoles,
			SubjectProperties: properties,
		})
	}

	decisions, batchErr := CheckResourceAccessMany(ctx, b.authorization, reqs)
	if batchErr != nil {
		for n, i := range pending {
			decision, singleErr := CheckResourceAccess(ctx, b.authorization, reqs[n])
			if singleErr != nil {
				return nil, singleErr
			}
			results[i] = operationAccessResult(decision, queries[i])
		}
		return results, nil
	}
	for n, i := range pending {
		results[i] = operationAccessResult(decisions[n], queries[i])
	}
	return results, nil
}

func operationAccessResult(decision ResourceAccessDecision, query OperationAccessQuery) error {
	if !decision.Allowed {
		return operationAccessDenied(query)
	}
	return nil
}

func operationAccessDenied(query OperationAccessQuery) error {
	return fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, query.Provider, query.Operation)
}
