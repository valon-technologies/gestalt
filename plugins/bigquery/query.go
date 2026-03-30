package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	cloudbigquery "cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	queryOperationName = "query"

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
	Close() error
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
	schema cloudbigquery.Schema
	iter   *cloudbigquery.RowIterator
	client *cloudbigquery.Client
	cancel context.CancelFunc
}

func (sdkQueryRunner) Run(ctx context.Context, projectID, token, sql string, opts queryOptions) (queryIterator, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client, err := cloudbigquery.NewClient(ctx, projectID, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("creating bigquery client: %w", err)
	}

	query := client.Query(sql)
	query.UseLegacySQL = opts.UseLegacySQL

	queryCtx := ctx
	cancel := func() {}
	if opts.Timeout > 0 {
		queryCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}

	iter, err := query.Read(queryCtx)
	if err != nil {
		cancel()
		_ = client.Close()
		return nil, fmt.Errorf("executing query: %w", err)
	}
	return sdkQueryIterator{
		schema: iter.Schema,
		iter:   iter,
		client: client,
		cancel: cancel,
	}, nil
}

func (it sdkQueryIterator) Schema() cloudbigquery.Schema { return it.schema }
func (it sdkQueryIterator) TotalRows() uint64            { return it.iter.TotalRows }

func (it sdkQueryIterator) Next(row *map[string]cloudbigquery.Value) error {
	return it.iter.Next(row)
}

func (it sdkQueryIterator) Close() error {
	if it.cancel != nil {
		it.cancel()
	}
	if it.client != nil {
		return it.client.Close()
	}
	return nil
}

func readRows(iter queryIterator, maxResults int) ([]map[string]any, error) {
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
	return rows, nil
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
	scale, ok := finiteDecimalScale(den)
	if !ok {
		return sign + new(big.Rat).SetFrac(num, den).RatString()
	}

	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaledNum := new(big.Int).Mul(num, pow10)
	scaledNum.Quo(scaledNum, den)

	intPart := new(big.Int)
	fracPart := new(big.Int)
	intPart.QuoRem(scaledNum, pow10, fracPart)
	if scale == 0 {
		return sign + intPart.String()
	}
	return sign + intPart.String() + "." + zeroPadDecimal(fracPart.String(), scale)
}

func finiteDecimalScale(den *big.Int) (int, bool) {
	if den == nil || den.Sign() == 0 {
		return 0, false
	}

	remaining := new(big.Int).Set(den)
	two := big.NewInt(2)
	five := big.NewInt(5)
	zero := big.NewInt(0)

	twos := 0
	for new(big.Int).Mod(remaining, two).Cmp(zero) == 0 {
		remaining.Quo(remaining, two)
		twos++
	}

	fives := 0
	for new(big.Int).Mod(remaining, five).Cmp(zero) == 0 {
		remaining.Quo(remaining, five)
		fives++
	}

	if remaining.Cmp(big.NewInt(1)) != 0 {
		return 0, false
	}
	if twos > fives {
		return twos, true
	}
	return fives, true
}

func zeroPadDecimal(value string, width int) string {
	if len(value) >= width {
		return value
	}
	buf := make([]byte, width)
	offset := width - len(value)
	for i := 0; i < offset; i++ {
		buf[i] = '0'
	}
	copy(buf[offset:], value)
	return string(buf)
}

func timeDurationMs(ms int) time.Duration {
	return time.Duration(ms) * time.Millisecond
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
