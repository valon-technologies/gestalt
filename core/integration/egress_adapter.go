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

	if pgn, ok := b.Pagination[operation]; ok {
		return apiexec.DoPaginatedWithExecutor(ctx, b.httpClient(), req, pgn, executeEgressHTTP)
	}

	return executeEgressHTTP(ctx, b.httpClient(), req)
}

func executeEgressHTTP(ctx context.Context, client *http.Client, req apiexec.Request) (*core.OperationResult, error) {
	return egress.ExecuteHTTP(ctx, client, egressRequestFromAPIExec(req))
}

func egressRequestFromAPIExec(req apiexec.Request) egress.HTTPRequestSpec {
	credential := egress.CredentialMaterialization{}
	switch {
	case req.AuthHeader != "":
		credential.Authorization = req.AuthHeader
	case req.Token != "":
		credential.Authorization = core.BearerScheme + req.Token
	}

	return egress.HTTPRequestSpec{
		Target: egress.Target{
			Method: req.Method,
			Path:   req.Path,
		},
		BaseURL:     req.BaseURL,
		Params:      req.Params,
		Headers:     copyHeaders(req.CustomHeaders),
		Body:        req.Body,
		ContentType: req.ContentType,
		Check:       req.CheckResponse,
		MaxRetries:  req.MaxRetries,
		NoRetry:     req.NoRetry,
		Credential:  credential,
	}
}
