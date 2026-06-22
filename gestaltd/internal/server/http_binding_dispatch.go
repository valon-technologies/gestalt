package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) httpBindingPrincipal(binding MountedHTTPBinding, verified *verifiedHTTPBindingSender) *principal.Principal {
	permissions := principal.CompilePermissions([]core.AccessPermission{{
		App:        binding.AppName,
		Operations: []string{binding.Target},
	}})
	displayName := binding.AppName + "/" + binding.Name
	if verified != nil && strings.TrimSpace(verified.Subject) != "" {
		displayName = strings.TrimSpace(verified.Subject)
	}
	return principal.Canonicalize(&principal.Principal{
		SubjectID:   "system:http_binding:" + binding.AppName + ":" + binding.Name,
		DisplayName: displayName,
		Scopes:      principal.ScopeStringsFromPermissionSet(permissions),
	})
}

func httpBindingContextValue(binding MountedHTTPBinding, r *http.Request, verified *verifiedHTTPBindingSender, parsed *parsedHTTPBindingRequest) map[string]any {
	value := map[string]any{
		"name":   binding.Name,
		"app":    binding.AppName,
		"path":   binding.Path,
		"method": binding.Method,
		"target": binding.Target,
	}
	if headers := requestHeaderValues(r); len(headers) > 0 {
		value["headers"] = headers
	}
	if parsed != nil && len(parsed.RawBody) > 0 {
		value["rawBodyBase64"] = base64.StdEncoding.EncodeToString(parsed.RawBody)
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

func (s *Server) httpBindingOperationInvocation(ctx context.Context, binding MountedHTTPBinding, r *http.Request, p *principal.Principal, verified *verifiedHTTPBindingSender, parsed *parsedHTTPBindingRequest) (*core.OperationResult, error) {
	params := map[string]any{}
	if parsed != nil && parsed.Params != nil {
		params = cloneAnyMap(parsed.Params)
	}
	if p == nil {
		p = s.httpBindingPrincipal(binding, verified)
	}
	ctx = principal.WithPrincipal(ctx, p)
	ctx = invocation.WithWorkflowContext(ctx, httpBindingContextValue(binding, r, verified, parsed))
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceHTTPBinding)
	ctx = invocation.WithEntry(ctx, invocation.EntryHTTP)
	ctx = invocation.WithHTTPBinding(ctx, binding.Name)
	if binding.CredentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, binding.CredentialMode)
	}
	return s.invoker.Invoke(ctx, p, binding.AppName, "", binding.Target, params)
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

func requestHeaderValues(r *http.Request) map[string]any {
	if r == nil || len(r.Header) == 0 {
		return nil
	}
	headers := make(map[string]any, len(r.Header))
	for key, values := range r.Header {
		copied := make([]string, len(values))
		copy(copied, values)
		headers[key] = copied
	}
	return headers
}
