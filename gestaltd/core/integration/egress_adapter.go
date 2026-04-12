package integration

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/egress"
)

func (b *Base) executeREST(ctx context.Context, operation string, catOp *catalog.CatalogOperation, params map[string]any, token string) (*core.OperationResult, error) {
	if catOp == nil {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	method := strings.ToUpper(strings.TrimSpace(catOp.Method))
	if method == "" {
		return nil, fmt.Errorf("operation %q is missing method", operation)
	}
	if strings.TrimSpace(catOp.Path) == "" {
		return nil, fmt.Errorf("operation %q is missing path", operation)
	}
	bodyParams, queryParams, headerParams := partitionParams(catOp, params)

	baseURL, headers := b.resolvedURLAndHeaders(ctx)
	for k, v := range headerParams {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[k] = v
	}

	if err := b.checkEgressHost(baseURL); err != nil {
		return nil, err
	}

	credential, err := b.materializeCredential(token)
	if err != nil {
		return nil, err
	}
	headers = egress.ApplyHeaderMutations(headers, credential.Headers)

	req := apiexec.Request{
		Method:        method,
		BaseURL:       baseURL,
		Path:          catOp.Path,
		Params:        bodyParams,
		QueryParams:   queryParams,
		AuthHeader:    credential.Authorization,
		CustomHeaders: headers,
		CheckResponse: b.CheckResponse,
	}

	if pgn, ok := b.Pagination[operation]; ok {
		return apiexec.DoPaginated(ctx, b.httpClient(), req, pgn)
	}

	result, err := apiexec.Do(ctx, b.httpClient(), req)
	if err != nil {
		return nil, err
	}

	if b.ResponseMapping != nil {
		result = applyResponseMapping(result, b.ResponseMapping)
	}

	return result, nil
}

func (b *Base) executeGraphQL(ctx context.Context, operation string, query string, params map[string]any, token string) (*core.OperationResult, error) {
	gqlURL, headers := b.resolvedURLAndHeaders(ctx)

	if err := b.checkEgressHost(gqlURL); err != nil {
		return nil, err
	}

	credential, err := b.materializeCredential(token)
	if err != nil {
		return nil, err
	}
	headers = egress.ApplyHeaderMutations(headers, credential.Headers)

	gqlReq := apiexec.GraphQLRequest{
		URL:           gqlURL,
		Query:         query,
		Variables:     params,
		AuthHeader:    credential.Authorization,
		CustomHeaders: headers,
	}

	return apiexec.DoGraphQL(ctx, b.httpClient(), gqlReq)
}

func (b *Base) checkEgressHost(rawURL string) error {
	if b.CheckEgress == nil {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("egress check: parsing URL: %w", err)
	}
	return b.CheckEgress(parsed.Host)
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
	var wireNames map[string]string
	for _, p := range catOp.Parameters {
		if p.Location != "" {
			locations[p.Name] = p.Location
		}
		if p.WireName != "" {
			if wireNames == nil {
				wireNames = make(map[string]string)
			}
			wireNames[p.Name] = p.WireName
		}
	}
	if len(locations) == 0 {
		return params, nil, nil
	}

	body = make(map[string]any)
	query = make(map[string]any)
	headers = make(map[string]string)
	for k, v := range params {
		httpKey := k
		if wn, ok := wireNames[k]; ok {
			httpKey = wn
		}
		switch locations[k] {
		case "query":
			query[httpKey] = v
		case "header":
			headers[httpKey] = fmt.Sprintf("%v", v)
		case "path":
			body[httpKey] = v
		default:
			body[k] = v
		}
	}
	return body, query, headers
}
