package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func (s *Server) webhookPrincipal(mounted MountedWebhook, verified *verifiedWebhookSender) *principal.Principal {
	subjectID := "system:webhook:" + mounted.PluginName + ":" + mounted.Name
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     mounted.PluginName,
		Operations: []string{strings.TrimSpace(mounted.Target.Operation)},
	}})
	p := &principal.Principal{
		SubjectID:        subjectID,
		Source:           principal.SourceWebhook,
		Scopes:           principal.PermissionPlugins(permissions),
		TokenPermissions: permissions,
	}
	if verified != nil {
		p.DisplayName = strings.TrimSpace(verified.Subject)
	}
	return principal.Canonicalize(p)
}

func webhookContextValue(mounted MountedWebhook, verified *verifiedWebhookSender) map[string]any {
	value := map[string]any{
		"name":   mounted.Name,
		"plugin": mounted.PluginName,
	}
	if verified == nil {
		return map[string]any{"webhook": value}
	}
	if verified.Scheme != "" {
		value["scheme"] = verified.Scheme
	}
	if verified.Subject != "" {
		value["subject"] = verified.Subject
	}
	if verified.DeliveryID != "" {
		value["deliveryId"] = verified.DeliveryID
	}
	if len(verified.Claims) > 0 {
		claims := make(map[string]any, len(verified.Claims))
		for key, item := range verified.Claims {
			claims[key] = item
		}
		value["claims"] = claims
	}
	return map[string]any{"webhook": value}
}

func (s *Server) webhookOperationInvocation(ctx context.Context, mounted MountedWebhook, verified *verifiedWebhookSender, params map[string]any) (*core.OperationResult, error) {
	p := s.webhookPrincipal(mounted, verified)
	instance := ""
	if raw, ok := params[httpInstanceParam].(string); ok {
		instance = strings.TrimSpace(raw)
		delete(params, httpInstanceParam)
	}
	connection := ""
	if raw, ok := params[httpConnectionParam].(string); ok {
		connection = strings.TrimSpace(raw)
		delete(params, httpConnectionParam)
	}
	if instance != "" {
		if !safeParamValue.MatchString(instance) {
			return nil, fmt.Errorf("instance name contains invalid characters")
		}
	}
	if connection != "" {
		if !safeParamValue.MatchString(connection) {
			return nil, fmt.Errorf("connection name contains invalid characters")
		}
		connection = config.ResolveConnectionAlias(connection)
	}

	ctx = principal.WithPrincipal(ctx, p)
	ctx = invocation.WithAccessContext(ctx, s.providerAccessContextWithContext(ctx, p, mounted.PluginName))
	ctx = invocation.WithWorkflowContext(ctx, webhookContextValue(mounted, verified))
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceHTTP)
	if connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	return s.invoker.Invoke(ctx, p, mounted.PluginName, instance, mounted.Target.Operation, params)
}

func (s *Server) startWebhookWorkflowRun(ctx context.Context, mounted MountedWebhook, verified *verifiedWebhookSender, params map[string]any) (*coreworkflow.Run, error) {
	if s.workflow == nil {
		return nil, fmt.Errorf("workflow runtime is not configured")
	}
	target := mounted.Target.Workflow
	if target == nil {
		return nil, fmt.Errorf("workflow target is not configured")
	}
	_, provider, err := s.workflow.ResolveProviderSelection(strings.TrimSpace(target.Provider))
	if err != nil {
		return nil, err
	}
	input := make(map[string]any, len(target.Input)+len(params)+1)
	for key, value := range params {
		input[key] = value
	}
	for key, value := range target.Input {
		input[key] = value
	}
	input["_gestaltWebhook"] = webhookContextValue(mounted, verified)["webhook"]
	return provider.StartRun(ctx, coreworkflow.StartRunRequest{
		Target: coreworkflow.Target{
			PluginName: strings.TrimSpace(target.Plugin),
			Operation:  strings.TrimSpace(target.Operation),
			Connection: strings.TrimSpace(target.Connection),
			Instance:   strings.TrimSpace(target.Instance),
			Input:      input,
		},
		IdempotencyKey: strings.TrimSpace(verifiedDeliveryID(verified)),
		CreatedBy: coreworkflow.Actor{
			SubjectID:   "system:webhook:" + mounted.PluginName + ":" + mounted.Name,
			SubjectKind: string(principal.KindWorkload),
			DisplayName: verifiedDisplayName(verified),
			AuthSource:  principal.SourceWebhook.String(),
		},
		ExecutionRef: "",
	})
}

func verifiedDeliveryID(verified *verifiedWebhookSender) string {
	if verified == nil {
		return ""
	}
	return verified.DeliveryID
}

func verifiedDisplayName(verified *verifiedWebhookSender) string {
	if verified == nil {
		return ""
	}
	return verified.Subject
}

func (s *Server) dispatchWebhookAsync(mounted MountedWebhook, verified *verifiedWebhookSender, params map[string]any) {
	go func() {
		ctx := invocation.WithRequestMeta(context.Background(), invocation.RequestMeta{})
		switch {
		case mounted.Target != nil && strings.TrimSpace(mounted.Target.Operation) != "":
			if _, err := s.webhookOperationInvocation(ctx, mounted, verified, cloneMap(params)); err != nil {
				slog.Error("webhook async operation failed", "plugin", mounted.PluginName, "webhook", mounted.Name, "operation", mounted.Target.Operation, "error", err)
			}
		case mounted.Target != nil && mounted.Target.Workflow != nil:
			if _, err := s.startWebhookWorkflowRun(ctx, mounted, verified, cloneMap(params)); err != nil {
				slog.Error("webhook async workflow failed", "plugin", mounted.PluginName, "webhook", mounted.Name, "workflow_plugin", mounted.Target.Workflow.Plugin, "workflow_operation", mounted.Target.Workflow.Operation, "error", err)
			}
		default:
			slog.Error("webhook async dispatch skipped because target is missing", "plugin", mounted.PluginName, "webhook", mounted.Name)
		}
	}()
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
