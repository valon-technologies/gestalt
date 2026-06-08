package workflowmanager

import (
	"context"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	workflowManagerOperationDefinitionsApply  = workflowauth.OperationDefinitionsApply
	workflowManagerOperationRunsStart         = workflowauth.OperationRunsStart
	workflowManagerOperationRunsSignalOrStart = workflowauth.OperationRunsSignalOrStart
	workflowManagerOperationTargetScopeOnly   = ""
)

func (m *Manager) authorizeAgentWorkflowTarget(ctx context.Context, p *principal.Principal, operation string, target coreworkflow.Target, caller invocation.CallerProvider) (*principal.Principal, error) {
	agent := invocation.AgentInvocationContextFromContext(ctx)
	if agent == (invocation.AgentInvocationContext{}) {
		return principal.Canonicalized(p), nil
	}
	if m == nil || m.agentManager == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent workflow invocation authorizer is required")
	}
	caller = workflowCaller(ctx, caller)
	reqContext, err := workflowProviderRequestContext(ctx, p, caller)
	if err != nil {
		return nil, err
	}
	authorized, err := m.agentManager.AuthorizeWorkflowInvocation(ctx, invocation.AgentWorkflowAuthorizationRequest{
		AgentProviderName: strings.TrimSpace(agent.ProviderName),
		CallerKind:        caller.Kind,
		CallerName:        caller.Name,
		Agent:             agent,
		Principal:         p,
		Operation:         operation,
		Target:            &target,
		RequestContext:    reqContext,
	})
	if err != nil {
		return nil, err
	}
	if authorized.Principal != nil {
		return authorized.Principal, nil
	}
	return principal.Canonicalized(p), nil
}
