package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

const (
	providerName        = "bigquery"
	providerDisplayName = "BigQuery"
	providerDescription = "Google BigQuery data warehouse"

	bigqueryBaseURL = "https://bigquery.googleapis.com/bigquery/v2"

	httpTimeout          = 30 * time.Second
	maxResponseBodyBytes = 50 << 20 // 50 MB
)

var restClient = &http.Client{Timeout: httpTimeout}

type Provider struct {
	runner queryRunner
}

var _ pluginsdk.Provider = (*Provider)(nil)

func NewProvider() *Provider {
	return &Provider{runner: sdkQueryRunner{}}
}

func (p *Provider) Name() string                              { return providerName }
func (p *Provider) DisplayName() string                       { return providerDisplayName }
func (p *Provider) Description() string                       { return providerDescription }
func (p *Provider) ConnectionMode() pluginsdk.ConnectionMode { return pluginsdk.ConnectionModeUser }

func (p *Provider) ListOperations() []pluginsdk.Operation {
	return []pluginsdk.Operation{
		{
			Name:        "list_datasets",
			Description: "List datasets in a project",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "project_id", Type: "string", Required: true, Description: "GCP project ID"},
				{Name: "maxResults", Type: "integer", Description: "Maximum results"},
			},
		},
		{
			Name:        "get_dataset",
			Description: "Get dataset metadata",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "project_id", Type: "string", Required: true, Description: "GCP project ID"},
				{Name: "dataset_id", Type: "string", Required: true, Description: "Dataset ID"},
			},
		},
		{
			Name:        "list_tables",
			Description: "List tables in a dataset",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "project_id", Type: "string", Required: true, Description: "GCP project ID"},
				{Name: "dataset_id", Type: "string", Required: true, Description: "Dataset ID"},
				{Name: "maxResults", Type: "integer", Description: "Maximum results"},
			},
		},
		{
			Name:        "get_table",
			Description: "Get table metadata",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "project_id", Type: "string", Required: true, Description: "GCP project ID"},
				{Name: "dataset_id", Type: "string", Required: true, Description: "Dataset ID"},
				{Name: "table_id", Type: "string", Required: true, Description: "Table ID"},
			},
		},
		{
			Name:        "list_routines",
			Description: "List routines in a dataset",
			Method:      http.MethodGet,
			Parameters: []pluginsdk.Parameter{
				{Name: "project_id", Type: "string", Required: true, Description: "GCP project ID"},
				{Name: "dataset_id", Type: "string", Required: true, Description: "Dataset ID"},
				{Name: "maxResults", Type: "integer", Description: "Maximum results"},
			},
		},
		{
			Name:        queryOperationName,
			Description: "Execute a BigQuery SQL query",
			Method:      http.MethodPost,
			Parameters: []pluginsdk.Parameter{
				{Name: queryParamProjectID, Type: "string", Required: true, Description: "GCP project ID"},
				{Name: queryParamSQL, Type: "string", Required: true, Description: "SQL query to execute"},
				{Name: queryParamMaxResults, Type: "integer", Description: "Maximum number of rows to return", Default: defaultQueryMaxResults},
				{Name: queryParamTimeoutMs, Type: "integer", Description: "Query timeout in milliseconds", Default: defaultQueryTimeoutMs},
				{Name: queryParamUseLegacySQL, Type: "boolean", Description: "Use legacy SQL syntax", Default: defaultQueryUseLegacySQL},
			},
		},
	}
}

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	if operation == queryOperationName {
		return p.executeQuery(ctx, params, token)
	}
	return p.executeREST(ctx, operation, params, token)
}

func (p *Provider) executeREST(ctx context.Context, operation string, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
	projectID, _ := params["project_id"].(string)
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	var path string
	switch operation {
	case "list_datasets":
		path = fmt.Sprintf("/projects/%s/datasets", projectID)
	case "get_dataset":
		datasetID, _ := params["dataset_id"].(string)
		if datasetID == "" {
			return nil, fmt.Errorf("dataset_id is required")
		}
		path = fmt.Sprintf("/projects/%s/datasets/%s", projectID, datasetID)
	case "list_tables":
		datasetID, _ := params["dataset_id"].(string)
		if datasetID == "" {
			return nil, fmt.Errorf("dataset_id is required")
		}
		path = fmt.Sprintf("/projects/%s/datasets/%s/tables", projectID, datasetID)
	case "get_table":
		datasetID, _ := params["dataset_id"].(string)
		tableID, _ := params["table_id"].(string)
		if datasetID == "" || tableID == "" {
			return nil, fmt.Errorf("dataset_id and table_id are required")
		}
		path = fmt.Sprintf("/projects/%s/datasets/%s/tables/%s", projectID, datasetID, tableID)
	case "list_routines":
		datasetID, _ := params["dataset_id"].(string)
		if datasetID == "" {
			return nil, fmt.Errorf("dataset_id is required")
		}
		path = fmt.Sprintf("/projects/%s/datasets/%s/routines", projectID, datasetID)
	default:
		return &pluginsdk.OperationResult{
			Status: http.StatusNotFound,
			Body:   `{"error":"unknown operation"}`,
		}, nil
	}

	reqURL := bigqueryBaseURL + path
	query := make(url.Values)
	if maxResults, ok := params["maxResults"]; ok {
		query.Set("maxResults", fmt.Sprintf("%v", maxResults))
	}
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := restClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing REST request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return &pluginsdk.OperationResult{
		Status: resp.StatusCode,
		Body:   string(body),
	}, nil
}

func (p *Provider) executeQuery(ctx context.Context, params map[string]any, token string) (*pluginsdk.OperationResult, error) {
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

	return &pluginsdk.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}

