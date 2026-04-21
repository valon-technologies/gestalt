package bootstrap

import (
	"context"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func workflowHostStartupContextFunc(runtime *workflowRuntime, providerName string) func(context.Context, coreworkflow.InvokeOperationRequest) context.Context {
	providerName = strings.TrimSpace(providerName)
	return func(ctx context.Context, req coreworkflow.InvokeOperationRequest) context.Context {
		if runtime == nil || providerName == "" || strings.TrimSpace(req.ExecutionRef) != "" || principal.FromContext(ctx) != nil {
			return ctx
		}
		if !runtime.StartupPendingProvider(providerName) {
			return ctx
		}
		return principal.WithPrincipal(ctx, workflowStartupPrincipal(req))
	}
}
