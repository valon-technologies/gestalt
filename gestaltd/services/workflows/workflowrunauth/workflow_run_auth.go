package workflowrunauth

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type Resolver interface {
	ResolveWorkflowRun(ctx context.Context, providerName, runID string) (*coreworkflow.Run, error)
}

type TargetAuth struct {
	Operations  map[string]map[string]core.ConnectionMode
	Permissions principal.PermissionSet
}

type ResolvedInvocation struct {
	ProviderName string
	RunID        string
	Run          *coreworkflow.Run
	RunAs        *core.RunAsSubject
	Auth         TargetAuth
	Workflow     map[string]any
}

func ResolveInvocationFromWorkflowRun(ctx context.Context, resolver Resolver, workflow *structpb.Struct) (ResolvedInvocation, error) {
	if workflow == nil {
		return ResolvedInvocation{}, status.Error(codes.FailedPrecondition, "invocation token or workflow runAs is required")
	}
	if resolver == nil {
		return ResolvedInvocation{}, status.Error(codes.FailedPrecondition, "workflow run resolver is not configured")
	}
	workflowMap := invocation.CloneWorkflowContext(workflow.AsMap())
	providerName := strings.TrimSpace(invocation.WorkflowContextString(workflowMap, "providerName"))
	if providerName == "" {
		providerName = strings.TrimSpace(invocation.WorkflowContextString(workflowMap, "provider"))
	}
	runID := strings.TrimSpace(invocation.WorkflowContextString(workflowMap, "runId"))
	if providerName == "" || runID == "" {
		return ResolvedInvocation{}, status.Error(codes.FailedPrecondition, "workflow provider and run id are required for workflow runAs invocation")
	}
	run, err := resolver.ResolveWorkflowRun(ctx, providerName, runID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound {
			return ResolvedInvocation{}, status.Errorf(codes.FailedPrecondition, "workflow run %q was not found", runID)
		}
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return ResolvedInvocation{}, st.Err()
		}
		return ResolvedInvocation{}, status.Errorf(codes.FailedPrecondition, "resolve workflow run %q: %v", runID, err)
	}
	if run == nil {
		return ResolvedInvocation{}, status.Errorf(codes.FailedPrecondition, "workflow run %q was not found", runID)
	}
	runAs := core.NormalizeRunAsSubject(run.RunAs)
	if runAs == nil || strings.TrimSpace(runAs.SubjectID) == "" {
		return ResolvedInvocation{}, status.Errorf(codes.FailedPrecondition, "workflow run %q has no runAs subject", runID)
	}
	return ResolvedInvocation{
		ProviderName: providerName,
		RunID:        runID,
		Run:          run,
		RunAs:        runAs,
		Auth:         TargetInvocationAuth(run.Target),
		Workflow:     WorkflowContextWithPersistedRunAs(workflowMap, providerName, runID, runAs),
	}, nil
}

func TargetInvocationAuth(target coreworkflow.Target) TargetAuth {
	auth := TargetAuth{
		Operations:  map[string]map[string]core.ConnectionMode{},
		Permissions: principal.PermissionSet{},
	}
	for i := range target.Steps {
		step := &target.Steps[i]
		if step.App != nil {
			addOperationGrant(auth.Operations, step.App.Name, step.App.Operation, step.App.CredentialMode)
			addOperationPermission(auth.Permissions, step.App.Name, step.App.Operation)
		}
		if step.Agent != nil {
			if providerName := strings.TrimSpace(step.Agent.ProviderName); providerName != "" {
				auth.Permissions[providerName] = nil
			}
			for j := range step.Agent.ToolRefs {
				addToolRefAuth(auth.Operations, auth.Permissions, &step.Agent.ToolRefs[j])
			}
		}
	}
	return auth
}

func addToolRefAuth(operations map[string]map[string]core.ConnectionMode, perms principal.PermissionSet, ref *coreagent.ToolRef) {
	if ref == nil {
		return
	}
	addOperationGrant(operations, ref.App, ref.Operation, "")
	addOperationPermission(perms, ref.App, ref.Operation)
}

func addOperationGrant(operations map[string]map[string]core.ConnectionMode, appName, operation string, credentialMode core.ConnectionMode) {
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	if appName == "" || operation == "" {
		return
	}
	appOperations := operations[appName]
	if appOperations == nil {
		appOperations = map[string]core.ConnectionMode{}
	}
	appOperations[operation] = core.NormalizeOptionalConnectionMode(credentialMode)
	operations[appName] = appOperations
}

func addOperationPermission(perms principal.PermissionSet, appName, operation string) {
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	if appName == "" || operation == "" {
		return
	}
	if existing, ok := perms[appName]; ok && existing == nil {
		return
	}
	ops := perms[appName]
	if ops == nil {
		ops = map[string]struct{}{}
	}
	ops[operation] = struct{}{}
	perms[appName] = ops
}

func WorkflowContextWithPersistedRunAs(workflowMap map[string]any, providerName, runID string, runAs *core.RunAsSubject) map[string]any {
	out := invocation.CloneWorkflowContext(workflowMap)
	if out == nil {
		out = map[string]any{}
	}
	out["provider"] = strings.TrimSpace(providerName)
	out["providerName"] = strings.TrimSpace(providerName)
	out["runId"] = strings.TrimSpace(runID)
	if runAsMap := RunAsContext(runAs); len(runAsMap) > 0 {
		out["runAs"] = runAsMap
	} else {
		delete(out, "runAs")
	}
	return out
}

func RunAsContext(runAs *core.RunAsSubject) map[string]any {
	runAs = core.NormalizeRunAsSubject(runAs)
	if runAs == nil {
		return nil
	}
	out := map[string]any{}
	if runAs.SubjectID != "" {
		out["id"] = runAs.SubjectID
	}
	if runAs.CredentialSubjectID != "" {
		out["credentialSubjectId"] = runAs.CredentialSubjectID
	}
	return out
}
