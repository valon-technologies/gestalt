package integration

import (
	"context"
	"fmt"
	"net/http"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/apiexec"
	"github.com/valon-technologies/gestalt/internal/egress"
)

func (b *Base) executeREST(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	ep, ok := b.Endpoints[operation]
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	baseURL, headers := b.resolvedURLAndHeaders(ctx)
	req := apiexec.Request{
		Method:        ep.Method,
		BaseURL:       baseURL,
		Path:          ep.Path,
		Params:        params,
		CustomHeaders: headers,
		CheckResponse: b.CheckResponse,
	}

	if err := b.applyAuth(&req, token); err != nil {
		return nil, err
	}

	if b.RequestMutator != nil {
		if err := b.RequestMutator(operation, &req, params); err != nil {
			return nil, err
		}
	}

	resolved, err := b.resolveEgress(ctx, operation, req)
	if err != nil {
		return nil, err
	}
	req.CustomHeaders = egress.CopyHeaders(resolved.Headers)

	if pgn, ok := b.Pagination[operation]; ok {
		return apiexec.DoPaginatedWithExecutor(ctx, b.httpClient(), req, pgn, executeEgressHTTP)
	}

	return executeEgressHTTP(ctx, b.httpClient(), req)
}

func (b *Base) resolveEgress(ctx context.Context, operation string, req apiexec.Request) (egress.Resolution, error) {
	resolver := egress.Resolver{}
	if b.EgressResolver != nil {
		resolver = *b.EgressResolver
	}

	return resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider:  b.IntegrationName,
			Operation: operation,
			Method:    req.Method,
			Path:      apiexec.ExpandedPath(req.Method, req.Path, req.Params),
		},
		Headers:    egress.CopyHeaders(req.CustomHeaders),
		Credential: credentialFromAPIRequest(req),
	})
}

func credentialFromAPIRequest(req apiexec.Request) egress.CredentialMaterialization {
	switch {
	case req.AuthHeader != "":
		return egress.CredentialMaterialization{Authorization: req.AuthHeader}
	case req.Token != "":
		return egress.CredentialMaterialization{Authorization: core.BearerScheme + req.Token}
	default:
		return egress.CredentialMaterialization{}
	}
}

func executeEgressHTTP(ctx context.Context, client *http.Client, req apiexec.Request) (*core.OperationResult, error) {
	return egress.ExecuteHTTP(ctx, client, egressRequestFromAPIExec(req))
}

func egressRequestFromAPIExec(req apiexec.Request) egress.HTTPRequestSpec {
	return egress.HTTPRequestSpec{
		Target: egress.Target{
			Method: req.Method,
			Path:   req.Path,
		},
		BaseURL:     req.BaseURL,
		Params:      req.Params,
		Headers:     egress.CopyHeaders(req.CustomHeaders),
		Body:        req.Body,
		ContentType: req.ContentType,
		Check:       req.CheckResponse,
		MaxRetries:  req.MaxRetries,
		NoRetry:     req.NoRetry,
		Credential:  credentialFromAPIRequest(req),
	}
}
