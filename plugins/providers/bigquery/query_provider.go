package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	cloudbigquery "cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"github.com/valon-technologies/gestalt/core"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	queryProviderDisplayName = "BigQuery Query"
	queryProviderDescription = "BigQuery SQL query execution"
	queryOperationName       = "query"

	queryParamProjectID    = "project_id"
	queryParamSQL          = "query"
	queryParamMaxResults   = "max_results"
	queryParamTimeoutMs    = "timeout_ms"
	queryParamUseLegacySQL = "use_legacy_sql"

	defaultQueryMaxResults   = 500
	defaultQueryTimeoutMs    = 60000
	defaultQueryUseLegacySQL = false

	fieldModeRepeated = "REPEATED"
	fieldModeRequired = "REQUIRED"
	fieldModeNullable = "NULLABLE"
)

type QueryProvider struct {
	runner queryRunner
}

type queryOptions struct {
	Timeout      time.Duration
	UseLegacySQL bool
}

type queryRunner interface {
	Run(context.Context, string, string, string, queryOptions) (queryIterator, error)
}

type queryIterator interface {
	Schema() cloudbigquery.Schema
	TotalRows() uint64
	Next(*map[string]cloudbigquery.Value) error
}

type queryResult struct {
	Schema      []querySchemaField `json:"schema"`
	Rows        []map[string]any   `json:"rows"`
	TotalRows   uint64             `json:"total_rows"`
	JobComplete bool               `json:"job_complete"`
}

type querySchemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type sdkQueryRunner struct{}

type sdkQueryIterator struct {
	schema    cloudbigquery.Schema
	totalRows uint64
	iter      *cloudbigquery.RowIterator
}

var _ core.Provider = (*QueryProvider)(nil)

func NewQueryProvider() *QueryProvider {
	return &QueryProvider{runner: sdkQueryRunner{}}
}

func newQueryProviderWithRunner(runner queryRunner) *QueryProvider {
	return &QueryProvider{runner: runner}
}

func (p *QueryProvider) Name() string                        { return "bigquery" }
func (p *QueryProvider) DisplayName() string                 { return queryProviderDisplayName }
func (p *QueryProvider) Description() string                 { return queryProviderDescription }
func (p *QueryProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }

func (p *QueryProvider) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        queryOperationName,
			Description: "Execute a BigQuery SQL query",
			Method:      http.MethodPost,
			Parameters: []core.Parameter{
				{Name: queryParamProjectID, Type: "string", Required: true, Description: "GCP project ID"},
				{Name: queryParamSQL, Type: "string", Required: true, Description: "SQL query to execute"},
				{Name: queryParamMaxResults, Type: "integer", Description: "Maximum number of rows to return", Default: defaultQueryMaxResults},
				{Name: queryParamTimeoutMs, Type: "integer", Description: "Query timeout in milliseconds", Default: defaultQueryTimeoutMs},
				{Name: queryParamUseLegacySQL, Type: "boolean", Description: "Use legacy SQL syntax", Default: defaultQueryUseLegacySQL},
			},
		},
	}
}

func (p *QueryProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if operation != queryOperationName {
		return nil, fmt.Errorf("unknown operation: %s", operation)
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
	iter, err := p.runner.Run(ctx, projectID, token, sql, queryOptions{
		Timeout:      time.Duration(intParam(params, queryParamTimeoutMs, defaultQueryTimeoutMs)) * time.Millisecond,
		UseLegacySQL: boolParam(params, queryParamUseLegacySQL, defaultQueryUseLegacySQL),
	})
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0, maxResults)
	for i := 0; i < maxResults; i++ {
		row := make(map[string]cloudbigquery.Value)
		err := iter.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}
		rows = append(rows, sanitizeRow(row))
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

	return &core.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}

func (sdkQueryRunner) Run(ctx context.Context, projectID, token, sql string, opts queryOptions) (queryIterator, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client, err := cloudbigquery.NewClient(ctx, projectID, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("creating bigquery client: %w", err)
	}
	defer func() { _ = client.Close() }()

	query := client.Query(sql)
	query.UseLegacySQL = opts.UseLegacySQL

	queryCtx := ctx
	cancel := func() {}
	if opts.Timeout > 0 {
		queryCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}
	defer cancel()

	iter, err := query.Read(queryCtx)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	return sdkQueryIterator{
		schema:    iter.Schema,
		totalRows: iter.TotalRows,
		iter:      iter,
	}, nil
}

func (it sdkQueryIterator) Schema() cloudbigquery.Schema { return it.schema }
func (it sdkQueryIterator) TotalRows() uint64            { return it.totalRows }

func (it sdkQueryIterator) Next(row *map[string]cloudbigquery.Value) error {
	return it.iter.Next(row)
}

func convertSchema(schema cloudbigquery.Schema) []querySchemaField {
	fields := make([]querySchemaField, len(schema))
	for i, f := range schema {
		fields[i] = querySchemaField{
			Name: f.Name,
			Type: string(f.Type),
			Mode: fieldMode(f),
		}
	}
	return fields
}

func fieldMode(f *cloudbigquery.FieldSchema) string {
	if f.Repeated {
		return fieldModeRepeated
	}
	if f.Required {
		return fieldModeRequired
	}
	return fieldModeNullable
}

func sanitizeRow(row map[string]cloudbigquery.Value) map[string]any {
	out := make(map[string]any, len(row))
	for key, value := range row {
		out[key] = sanitizeValue(value)
	}
	return out
}

func sanitizeValue(v cloudbigquery.Value) any {
	switch val := v.(type) {
	case *big.Rat:
		return rationalDecimalString(val)
	case []cloudbigquery.Value:
		out := make([]any, len(val))
		for i, elem := range val {
			out[i] = sanitizeValue(elem)
		}
		return out
	case map[string]cloudbigquery.Value:
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

func rationalDecimalString(r *big.Rat) string {
	if r == nil {
		return ""
	}
	if r.IsInt() {
		return r.Num().String()
	}

	num := new(big.Int).Set(r.Num())
	den := new(big.Int).Set(r.Denom())
	sign := ""
	if num.Sign() < 0 {
		sign = "-"
		num.Abs(num)
	}

	intPart := new(big.Int)
	remainder := new(big.Int)
	intPart.QuoRem(num, den, remainder)

	var frac strings.Builder
	ten := big.NewInt(10)
	for remainder.Sign() != 0 {
		remainder.Mul(remainder, ten)
		digit := new(big.Int)
		nextRemainder := new(big.Int)
		digit.QuoRem(remainder, den, nextRemainder)
		frac.WriteByte(byte('0' + digit.Int64()))
		remainder = nextRemainder
	}

	return sign + intPart.String() + "." + frac.String()
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
