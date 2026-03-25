package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	providerName   = "bigquery"
	operationQuery = "query"

	paramProjectID    = "project_id"
	paramQuery        = "query"
	paramMaxResults   = "max_results"
	paramTimeoutMs    = "timeout_ms"
	paramUseLegacySQL = "use_legacy_sql"

	defaultMaxResults   = 500
	defaultTimeoutMs    = 60000
	defaultUseLegacySQL = false

	modeRepeated = "REPEATED"
	modeRequired = "REQUIRED"
	modeNullable = "NULLABLE"
)

var _ core.Provider = (*bigQueryProvider)(nil)

type bigQueryProvider struct{}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return pluginapi.ServeProvider(ctx, &bigQueryProvider{})
}

func (p *bigQueryProvider) Name() string                        { return providerName }
func (p *bigQueryProvider) DisplayName() string                 { return "BigQuery Query" }
func (p *bigQueryProvider) Description() string                 { return "BigQuery SQL query execution" }
func (p *bigQueryProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }

func (p *bigQueryProvider) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        operationQuery,
			Description: "Execute a BigQuery SQL query",
			Method:      http.MethodPost,
			Parameters: []core.Parameter{
				{Name: paramProjectID, Type: "string", Required: true, Description: "GCP project ID"},
				{Name: paramQuery, Type: "string", Required: true, Description: "SQL query to execute"},
				{Name: paramMaxResults, Type: "integer", Description: "Maximum number of rows to return", Default: defaultMaxResults},
				{Name: paramTimeoutMs, Type: "integer", Description: "Query timeout in milliseconds", Default: defaultTimeoutMs},
				{Name: paramUseLegacySQL, Type: "boolean", Description: "Use legacy SQL syntax", Default: defaultUseLegacySQL},
			},
		},
	}
}

func (p *bigQueryProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if operation != operationQuery {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	projectID, _ := params[paramProjectID].(string)
	if projectID == "" {
		return nil, fmt.Errorf("%s is required", paramProjectID)
	}

	sql, _ := params[paramQuery].(string)
	if sql == "" {
		return nil, fmt.Errorf("%s is required", paramQuery)
	}

	maxResults := intParam(params, paramMaxResults, defaultMaxResults)
	timeoutMs := intParam(params, paramTimeoutMs, defaultTimeoutMs)
	useLegacySQL := boolParam(params, paramUseLegacySQL, defaultUseLegacySQL)

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client, err := bigquery.NewClient(ctx, projectID, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("creating bigquery client: %w", err)
	}
	defer func() { _ = client.Close() }()

	q := client.Query(sql)
	q.UseLegacySQL = useLegacySQL

	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	it, err := q.Read(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}

	schema := convertSchema(it.Schema)

	rows := make([]map[string]any, 0)
	for i := 0; i < maxResults; i++ {
		row := make(map[string]bigquery.Value)
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}
		rows = append(rows, sanitizeRow(row))
	}

	result := queryResult{
		Schema:      schema,
		Rows:        rows,
		TotalRows:   it.TotalRows,
		JobComplete: true,
	}

	body, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}

	return &core.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}

type queryResult struct {
	Schema      []schemaField    `json:"schema"`
	Rows        []map[string]any `json:"rows"`
	TotalRows   uint64           `json:"total_rows"`
	JobComplete bool             `json:"job_complete"`
}

type schemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

func convertSchema(schema bigquery.Schema) []schemaField {
	fields := make([]schemaField, len(schema))
	for i, f := range schema {
		fields[i] = schemaField{
			Name: f.Name,
			Type: string(f.Type),
			Mode: fieldMode(f),
		}
	}
	return fields
}

func fieldMode(f *bigquery.FieldSchema) string {
	if f.Repeated {
		return modeRepeated
	}
	if f.Required {
		return modeRequired
	}
	return modeNullable
}

func sanitizeRow(row map[string]bigquery.Value) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = sanitizeValue(v)
	}
	return out
}

// sanitizeValue converts BigQuery SDK types that don't serialize cleanly
// to JSON-safe equivalents. In particular, NUMERIC/BIGNUMERIC columns
// are represented as *big.Rat which marshals as rational notation ("a/b").
func sanitizeValue(v bigquery.Value) any {
	switch val := v.(type) {
	case *big.Rat:
		f, _ := val.Float64()
		return f
	case []bigquery.Value:
		out := make([]any, len(val))
		for i, elem := range val {
			out[i] = sanitizeValue(elem)
		}
		return out
	case map[string]bigquery.Value:
		return sanitizeRow(val)
	case civil.Date:
		return val.String()
	case civil.Time:
		return val.String()
	case civil.DateTime:
		return val.String()
	default:
		return v
	}
}

func intParam(params map[string]any, key string, defaultVal int) int {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return defaultVal
		}
		return int(i)
	default:
		return defaultVal
	}
}

func boolParam(params map[string]any, key string, defaultVal bool) bool {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}
