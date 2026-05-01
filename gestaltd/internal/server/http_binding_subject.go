package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) resolveHTTPBindingPrincipal(ctx context.Context, binding MountedHTTPBinding, r *http.Request, verified *verifiedHTTPBindingSender, parsed *parsedHTTPBindingRequest) (*principal.Principal, error) {
	bindingPrincipal := s.httpBindingPrincipal(binding, verified)

	prov, err := s.providers.Get(binding.PluginName)
	if err != nil {
		return nil, err
	}
	resolveCtx := principal.WithPrincipal(ctx, bindingPrincipal)
	resolveCtx = invocation.WithAccessContext(resolveCtx, s.providerAccessContextWithContext(resolveCtx, bindingPrincipal, binding.PluginName))
	resolveCtx = invocation.WithWorkflowContext(resolveCtx, httpBindingContextValue(binding, verified, parsed))
	resolveCtx = invocation.WithInvocationSurface(resolveCtx, invocation.InvocationSurfaceHTTP)
	resolveCtx = invocation.WithHTTPBinding(resolveCtx, binding.Name)

	resolved, supported, err := core.ResolveHTTPSubject(resolveCtx, prov, &core.HTTPSubjectResolveRequest{
		Binding:         binding.Name,
		Method:          requestMethod(r, binding),
		Path:            requestPath(r, binding),
		ContentType:     parsedContentType(parsed),
		Headers:         requestHeaders(r),
		Query:           requestQuery(r),
		Params:          parsedParams(parsed),
		RawBody:         parsedRawBody(parsed),
		SecurityScheme:  verifiedScheme(verified),
		VerifiedSubject: verifiedSubject(verified),
		VerifiedClaims:  verifiedClaims(verified),
	})
	if !supported {
		return bindingPrincipal, nil
	}
	if err != nil {
		var resolveErr *core.HTTPSubjectResolveError
		if errors.As(err, &resolveErr) {
			status := resolveErr.Status
			if status < http.StatusBadRequest {
				status = http.StatusBadRequest
			}
			return nil, newHTTPBindingRequestError(status, resolveErr.Message, err)
		}
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "failed to resolve http subject", err)
	}
	if resolved == nil || strings.TrimSpace(resolved.ID) == "" {
		return bindingPrincipal, nil
	}
	if principal.IsSystemSubjectID(strings.TrimSpace(resolved.ID)) {
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "invalid resolved http subject", errors.New("resolved subject uses reserved system namespace"))
	}

	p := &principal.Principal{
		SubjectID:   strings.TrimSpace(resolved.ID),
		DisplayName: strings.TrimSpace(resolved.DisplayName),
	}
	principal.SetAuthSource(p, resolved.AuthSource)
	if kind := strings.TrimSpace(resolved.Kind); kind != "" {
		p.Kind = principal.Kind(kind)
	}
	return principal.Canonicalize(p), nil
}

func requestMethod(r *http.Request, binding MountedHTTPBinding) string {
	if r != nil && strings.TrimSpace(r.Method) != "" {
		return r.Method
	}
	return binding.Method
}

func requestPath(r *http.Request, binding MountedHTTPBinding) string {
	if r != nil && r.URL != nil && strings.TrimSpace(r.URL.Path) != "" {
		return r.URL.Path
	}
	return binding.Path
}

func requestHeaders(r *http.Request) http.Header {
	if r == nil {
		return nil
	}
	return r.Header.Clone()
}

func requestQuery(r *http.Request) url.Values {
	if r == nil || r.URL == nil {
		return nil
	}
	return cloneURLValues(r.URL.Query())
}

func parsedContentType(parsed *parsedHTTPBindingRequest) string {
	if parsed == nil {
		return ""
	}
	return parsed.ContentType
}

func parsedParams(parsed *parsedHTTPBindingRequest) map[string]any {
	if parsed == nil {
		return nil
	}
	return cloneAnyMap(parsed.Params)
}

func parsedRawBody(parsed *parsedHTTPBindingRequest) []byte {
	if parsed == nil || len(parsed.RawBody) == 0 {
		return nil
	}
	body := make([]byte, len(parsed.RawBody))
	copy(body, parsed.RawBody)
	return body
}

func verifiedScheme(verified *verifiedHTTPBindingSender) string {
	if verified == nil {
		return ""
	}
	return verified.Scheme
}

func verifiedSubject(verified *verifiedHTTPBindingSender) string {
	if verified == nil {
		return ""
	}
	return verified.Subject
}

func verifiedClaims(verified *verifiedHTTPBindingSender) map[string]string {
	if verified == nil || len(verified.Claims) == 0 {
		return nil
	}
	claims := make(map[string]string, len(verified.Claims))
	for key, value := range verified.Claims {
		claims[key] = value
	}
	return claims
}

func cloneURLValues(src url.Values) url.Values {
	if len(src) == 0 {
		return nil
	}
	dst := make(url.Values, len(src))
	for key, values := range src {
		copied := make([]string, len(values))
		copy(copied, values)
		dst[key] = copied
	}
	return dst
}
