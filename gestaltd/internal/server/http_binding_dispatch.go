package server

import (
	"context"
	"log/slog"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) httpBindingPrincipal(binding MountedHTTPBinding, verified *verifiedHTTPBindingSender) *principal.Principal {
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     binding.PluginName,
		Operations: []string{binding.Target},
	}})
	displayName := binding.PluginName + "/" + binding.Name
	if verified != nil && strings.TrimSpace(verified.Subject) != "" {
		displayName = strings.TrimSpace(verified.Subject)
	}
	return principal.Canonicalize(&principal.Principal{
		SubjectID:        "system:http_binding:" + binding.PluginName + ":" + binding.Name,
		DisplayName:      displayName,
		Scopes:           principal.PermissionPlugins(permissions),
		TokenPermissions: permissions,
	})
}

func httpBindingContextValue(binding MountedHTTPBinding, verified *verifiedHTTPBindingSender, parsed *parsedHTTPBindingRequest) map[string]any {
	value := map[string]any{
		"name":   binding.Name,
		"plugin": binding.PluginName,
		"path":   binding.Path,
		"method": binding.Method,
		"target": binding.Target,
	}
	if parsed != nil && parsed.ContentType != "" {
		value["contentType"] = parsed.ContentType
	}
	if verified != nil {
		if verified.Scheme != "" {
			value["security"] = verified.Scheme
		}
		if verified.Subject != "" {
			value["subject"] = verified.Subject
		}
		if len(verified.Claims) > 0 {
			claims := make(map[string]any, len(verified.Claims))
			for key, item := range verified.Claims {
				claims[key] = item
			}
			value["claims"] = claims
		}
	}
	return map[string]any{"http": value}
}

func (s *Server) httpBindingOperationInvocation(ctx context.Context, binding MountedHTTPBinding, p *principal.Principal, verified *verifiedHTTPBindingSender, parsed *parsedHTTPBindingRequest) (*core.OperationResult, error) {
	params := map[string]any{}
	if parsed != nil && parsed.Params != nil {
		params = cloneAnyMap(parsed.Params)
	}
	if p == nil {
		p = s.httpBindingPrincipal(binding, verified)
	}
	ctx = principal.WithPrincipal(ctx, p)
	ctx = invocation.WithAccessContext(ctx, s.providerAccessContextWithContext(ctx, p, binding.PluginName))
	ctx = invocation.WithWorkflowContext(ctx, httpBindingContextValue(binding, verified, parsed))
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceHTTPBinding)
	ctx = invocation.WithHTTPBinding(ctx, binding.Name)
	if binding.CredentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, binding.CredentialMode)
	}
	return s.invoker.Invoke(ctx, p, binding.PluginName, "", binding.Target, params)
}

func (s *Server) dispatchHTTPBindingAsync(binding MountedHTTPBinding, p *principal.Principal, verified *verifiedHTTPBindingSender, parsed *parsedHTTPBindingRequest, requestMeta invocation.RequestMeta) {
	go func() {
		ctx := metricutil.WithMeterProvider(context.Background(), s.meterProvider)
		ctx = invocation.WithRequestMeta(ctx, requestMeta)
		if _, err := s.httpBindingOperationInvocation(ctx, binding, p, verified, parsed); err != nil {
			slog.Error("http binding async operation failed", "plugin", binding.PluginName, "binding", binding.Name, "operation", binding.Target, "error", err)
		}
	}()
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
