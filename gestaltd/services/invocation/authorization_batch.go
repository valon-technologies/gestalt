package invocation

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// MaxBatchedAccessChecks bounds one batched authorization call so a large
// catalog or app roster cannot turn a single listing request into an unbounded
// provider request.
const MaxBatchedAccessChecks = 1000

// ErrBatchedAccessTooLarge reports that a caller asked for more decisions in
// one batch than the server will send to the evaluator.
var ErrBatchedAccessTooLarge = fmt.Errorf("batched authorization check exceeds %d requests", MaxBatchedAccessChecks)

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
// allow. Listing callers propagate errors instead of treating them as denials
// or amplifying a failed batch into per-entry requests.
func CheckResourceAccessMany(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	reqs []ResourceAccessRequest,
) ([]ResourceAccessDecision, error) {
	if authorization == nil {
		return nil, ErrAuthorizationUnavailable
	}
	if len(reqs) > MaxBatchedAccessChecks {
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

// OperationAccessDecision carries the effective roles used for authorization,
// so discovery advertises the same permissions without a second policy read.
type OperationAccessDecision struct {
	Err          error
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
	) ([]OperationAccessDecision, error)
}

// CheckOperationAccessMany answers operation-access questions in bounded
// evaluator batches. Element i has no error when the operation is allowed
// and otherwise carries the same ErrAuthorizationDenied error CheckOperationAccess
// would return for that operation.
//
// Every answer runs through the same token-scope check, the same remote
// delegation check, and the same evaluator projection the single-decision path
// uses, so a listing answer and the invocation answer for the same operation
// cannot disagree. Allowed roles are applied here as well, matching
// authorizeOperation, so a tool the caller could not actually call is not
// listed.
//
// Provider errors propagate without retrying unresolved questions individually.
func (b *Broker) CheckOperationAccessMany(
	ctx context.Context,
	p *principal.Principal,
	queries []OperationAccessQuery,
) ([]OperationAccessDecision, error) {
	results := make([]OperationAccessDecision, len(queries))
	pending := make([]int, 0, len(queries))
	type providerAccess struct {
		policy          core.AppOperationPolicy
		profile         *core.AppAccessProfile
		profileErr      error
		delegatesRemote bool
		resource        *proto.Resource
	}
	accessByProvider := make(map[string]providerAccess)
	for i, query := range queries {
		if !principal.AllowsOperationPermission(p, query.Provider, query.Operation) {
			results[i].Err = operationAccessDenied(query)
			continue
		}
		access, ok := accessByProvider[query.Provider]
		if !ok {
			var err error
			access.policy, err = b.appOperationPolicy(ctx, query.Provider)
			if err != nil {
				return nil, err
			}
			access.profile, access.profileErr = b.appAccessProfile(ctx, p, query.Provider)
			access.delegatesRemote = b.providerDelegatesRemoteAuthorization(ctx, query.Provider)
			accessByProvider[query.Provider] = access
		}
		roles, allowed := access.policy.Resolve(query.Operation, query.AllowedRoles)
		results[i].AllowedRoles = roles
		if !allowed || access.profileErr != nil || !appAccessProfileAllows(access.profile, query.Operation) {
			results[i].Err = operationAccessDenied(query)
			continue
		}
		if !access.delegatesRemote {
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
			results[i].Err = fmt.Errorf("%w: %s.%s: %v",
				ErrAuthorizationDenied, queries[i].Provider, queries[i].Operation, err)
		}
		return results, nil
	}

	properties := subjectAccessProperties(p)
	reqs := make([]ResourceAccessRequest, 0, len(pending))
	for _, i := range pending {
		access := accessByProvider[queries[i].Provider]
		if access.resource == nil {
			access.resource = b.authorizationResource(ctx, queries[i].Provider)
			accessByProvider[queries[i].Provider] = access
		}
		reqs = append(reqs, ResourceAccessRequest{
			SubjectID:         subjectID,
			Action:            queries[i].Operation,
			Resource:          access.resource,
			AllowedRoles:      results[i].AllowedRoles,
			SubjectProperties: properties,
		})
	}

	for start := 0; start < len(reqs); start += MaxBatchedAccessChecks {
		end := min(start+MaxBatchedAccessChecks, len(reqs))
		decisions, err := CheckResourceAccessMany(ctx, b.authorization, reqs[start:end])
		if err != nil {
			return nil, err
		}
		for n, decision := range decisions {
			i := pending[start+n]
			results[i].Err = operationAccessResult(decision, queries[i])
		}
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
