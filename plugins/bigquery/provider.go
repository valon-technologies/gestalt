package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

const (
	providerName        = "bigquery"
	providerDisplayName = "BigQuery"
	providerDescription = "Google BigQuery data warehouse"
)

type Provider struct {
	runner queryRunner
}

var _ gestalt.Provider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{runner: sdkQueryRunner{}}
}

func (p *Provider) Name() string                           { return providerName }
func (p *Provider) DisplayName() string                    { return providerDisplayName }
func (p *Provider) Description() string                    { return providerDescription }
func (p *Provider) ConnectionMode() gestalt.ConnectionMode { return gestalt.ConnectionModeUser }

func (p *Provider) Catalog() *gestalt.Catalog {
	return &gestalt.Catalog{
		Name:        providerName,
		DisplayName: providerDisplayName,
		Description: providerDescription,
		Operations: []gestalt.CatalogOperation{
			{
				ID:          queryOperationName,
				Description: "Execute a BigQuery SQL query",
				Method:      http.MethodPost,
				Parameters: []gestalt.CatalogParameter{
					{Name: queryParamProjectID, Type: "string", Required: true, Description: "GCP project ID"},
					{Name: queryParamSQL, Type: "string", Required: true, Description: "SQL query to execute"},
					{Name: queryParamMaxResults, Type: "integer", Description: "Maximum number of rows to return", Default: defaultQueryMaxResults},
					{Name: queryParamTimeoutMs, Type: "integer", Description: "Query timeout in milliseconds", Default: defaultQueryTimeoutMs},
					{Name: queryParamUseLegacySQL, Type: "boolean", Description: "Use legacy SQL syntax", Default: defaultQueryUseLegacySQL},
				},
			},
		},
	}
}

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*gestalt.OperationResult, error) {
	if operation != queryOperationName {
		return nil, fmt.Errorf("unknown operation %q", operation)
	}

	projectID, _ := params[queryParamProjectID].(string)
	if projectID == "" {
		return nil, fmt.Errorf("%s is required", queryParamProjectID)
	}

	sql, _ := params[queryParamSQL].(string)
	if sql == "" {
		return nil, fmt.Errorf("%s is required", queryParamSQL)
	}

	maxResults := intParam(params, queryParamMaxResults, defaultQueryMaxResults)
	if maxResults < 0 {
		maxResults = 0
	}

	iter, err := p.runner.Run(ctx, projectID, token, sql, queryOptions{
		Timeout:      timeDurationMs(intParam(params, queryParamTimeoutMs, defaultQueryTimeoutMs)),
		UseLegacySQL: boolParam(params, queryParamUseLegacySQL, defaultQueryUseLegacySQL),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	rows, err := readRows(iter, maxResults)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(queryResult{
		Schema:      convertSchema(iter.Schema()),
		Rows:        rows,
		TotalRows:   iter.TotalRows(),
		JobComplete: true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}

	return &gestalt.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}
