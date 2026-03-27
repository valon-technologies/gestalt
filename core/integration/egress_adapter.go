package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/apiexec"
	"github.com/valon-technologies/gestalt/internal/egress"
)

func (b *Base) executeREST(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	ep, ok := b.Endpoints[operation]
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	catOp := findCatalogOp(b.catalog, operation)
	bodyParams, queryParams, headerParams := partitionParams(catOp, params)

	baseURL, headers := b.resolvedURLAndHeaders(ctx)
	for k, v := range headerParams {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[k] = v
	}

	req := apiexec.Request{
		Method:        ep.Method,
		BaseURL:       baseURL,
		Path:          ep.Path,
		Params:        bodyParams,
		QueryParams:   queryParams,
		CustomHeaders: headers,
		CheckResponse: b.CheckResponse,
	}

	if err := b.applyAuth(&req, token); err != nil {
		return nil, err
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
			Path:      apiexec.ExpandedPathWithQuery(req.Method, req.Path, req.Params, req.QueryParams),
		},
		Headers:    egress.CopyHeaders(req.CustomHeaders),
		Credential: credentialFromAPIRequest(req),
	})
}

func (b *Base) resolveGraphQLEgress(ctx context.Context, operation string, req apiexec.GraphQLRequest) (egress.Resolution, error) {
	resolver := egress.Resolver{}
	if b.EgressResolver != nil {
		resolver = *b.EgressResolver
	}

	parsed, err := url.Parse(req.URL)
	if err != nil {
		return egress.Resolution{}, fmt.Errorf("parsing graphql url: %w", err)
	}

	return resolver.Resolve(ctx, egress.ResolutionInput{
		Target: egress.Target{
			Provider:  b.IntegrationName,
			Operation: operation,
			Method:    http.MethodPost,
			Host:      parsed.Host,
			Path:      parsed.Path,
		},
		Headers:    egress.CopyHeaders(req.CustomHeaders),
		Credential: credentialFromGraphQLRequest(req),
	})
}

func credentialFromAuth(token, authHeader string) egress.CredentialMaterialization {
	switch {
	case authHeader != "":
		return egress.CredentialMaterialization{Authorization: authHeader}
	case token != "":
		return egress.CredentialMaterialization{Authorization: core.BearerScheme + token}
	default:
		return egress.CredentialMaterialization{}
	}
}

func credentialFromAPIRequest(req apiexec.Request) egress.CredentialMaterialization {
	return credentialFromAuth(req.Token, req.AuthHeader)
}

func credentialFromGraphQLRequest(req apiexec.GraphQLRequest) egress.CredentialMaterialization {
	return credentialFromAuth(req.Token, req.AuthHeader)
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
		QueryParams: req.QueryParams,
		Headers:     egress.CopyHeaders(req.CustomHeaders),
		Body:        req.Body,
		ContentType: req.ContentType,
		Check:       req.CheckResponse,
		MaxRetries:  req.MaxRetries,
		NoRetry:     req.NoRetry,
		Credential:  credentialFromAPIRequest(req),
	}
}

func findCatalogOp(cat *catalog.Catalog, id string) *catalog.CatalogOperation {
	if cat == nil {
		return nil
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == id {
			return &cat.Operations[i]
		}
	}
	return nil
}

func partitionParams(catOp *catalog.CatalogOperation, params map[string]any) (body map[string]any, query map[string]any, headers map[string]string) {
	if catOp == nil || len(catOp.Parameters) == 0 {
		return params, nil, nil
	}

	locations := make(map[string]string, len(catOp.Parameters))
	for _, p := range catOp.Parameters {
		if p.Location != "" {
			locations[p.Name] = p.Location
		}
	}
	if len(locations) == 0 {
		return params, nil, nil
	}

	body = make(map[string]any)
	query = make(map[string]any)
	headers = make(map[string]string)
	for k, v := range params {
		switch locations[k] {
		case "query":
			query[k] = v
		case "header":
			headers[k] = fmt.Sprintf("%v", v)
		case "path":
			body[k] = v
		default:
			body[k] = v
		}
	}
	return body, query, headers
}
