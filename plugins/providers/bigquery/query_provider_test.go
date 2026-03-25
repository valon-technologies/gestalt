package bigquery

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	cloudbigquery "cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"google.golang.org/api/iterator"
)

type fakeQueryRunner struct {
	runFn func(context.Context, string, string, string, queryOptions) (queryIterator, error)
}

func (f fakeQueryRunner) Run(ctx context.Context, projectID, token, sql string, opts queryOptions) (queryIterator, error) {
	return f.runFn(ctx, projectID, token, sql, opts)
}

type fakeQueryIterator struct {
	schema    cloudbigquery.Schema
	totalRows uint64
	rows      []map[string]cloudbigquery.Value
	index     int
}

func (f *fakeQueryIterator) Schema() cloudbigquery.Schema { return f.schema }
func (f *fakeQueryIterator) TotalRows() uint64            { return f.totalRows }

func (f *fakeQueryIterator) Next(row *map[string]cloudbigquery.Value) error {
	if f.index >= len(f.rows) {
		return iterator.Done
	}
	*row = f.rows[f.index]
	f.index++
	return nil
}

func TestQueryProviderExecuteShapesResults(t *testing.T) {
	t.Parallel()

	runner := fakeQueryRunner{
		runFn: func(ctx context.Context, projectID, token, sql string, opts queryOptions) (queryIterator, error) {
			if projectID != "analytics-prod" {
				t.Fatalf("projectID = %q", projectID)
			}
			if token != "access-token" {
				t.Fatalf("token = %q", token)
			}
			if sql != "SELECT * FROM report" {
				t.Fatalf("sql = %q", sql)
			}
			if !opts.UseLegacySQL {
				t.Fatal("expected UseLegacySQL to be true")
			}
			if opts.Timeout != 1500*time.Millisecond {
				t.Fatalf("timeout = %s", opts.Timeout)
			}

			return &fakeQueryIterator{
				schema: cloudbigquery.Schema{
					{Name: "amount", Type: cloudbigquery.NumericFieldType},
					{Name: "created_at", Type: cloudbigquery.DateTimeFieldType},
					{Name: "tags", Type: cloudbigquery.StringFieldType, Repeated: true},
					{Name: "details", Type: cloudbigquery.RecordFieldType},
				},
				totalRows: 2,
				rows: []map[string]cloudbigquery.Value{
					{
						"amount":     big.NewRat(2469, 200),
						"created_at": civil.DateTime{Date: civil.Date{Year: 2026, Month: 3, Day: 25}, Time: civil.Time{Hour: 9, Minute: 30, Second: 0}},
						"tags":       []cloudbigquery.Value{"a", big.NewRat(1, 8)},
						"details": map[string]cloudbigquery.Value{
							"exact": big.NewRat(1001, 1000),
						},
					},
					{
						"amount": big.NewRat(-5, 2),
					},
				},
			}, nil
		},
	}

	provider := newQueryProviderWithRunner(runner)
	result, err := provider.Execute(context.Background(), queryOperationName, map[string]any{
		queryParamProjectID:    "analytics-prod",
		queryParamSQL:          "SELECT * FROM report",
		queryParamMaxResults:   1,
		queryParamTimeoutMs:    1500,
		queryParamUseLegacySQL: true,
	}, "access-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var body struct {
		Schema      []querySchemaField `json:"schema"`
		Rows        []map[string]any   `json:"rows"`
		TotalRows   uint64             `json:"total_rows"`
		JobComplete bool               `json:"job_complete"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if result.Status != 200 {
		t.Fatalf("status = %d", result.Status)
	}
	if body.TotalRows != 2 || !body.JobComplete {
		t.Fatalf("body metadata = %+v", body)
	}
	if len(body.Rows) != 1 {
		t.Fatalf("rows = %d", len(body.Rows))
	}
	if got := body.Rows[0]["amount"]; got != "12.345" {
		t.Fatalf("amount = %#v, want exact decimal string", got)
	}
	if got := body.Rows[0]["created_at"]; got != "2026-03-25T09:30:00" {
		t.Fatalf("created_at = %#v", got)
	}
	tags, ok := body.Rows[0]["tags"].([]any)
	if !ok {
		t.Fatalf("tags type = %T", body.Rows[0]["tags"])
	}
	if len(tags) != 2 || tags[1] != "0.125" {
		t.Fatalf("tags = %#v", tags)
	}
	details, ok := body.Rows[0]["details"].(map[string]any)
	if !ok {
		t.Fatalf("details type = %T", body.Rows[0]["details"])
	}
	if details["exact"] != "1.001" {
		t.Fatalf("details.exact = %#v", details["exact"])
	}
	if len(body.Schema) != 4 || body.Schema[2].Mode != fieldModeRepeated {
		t.Fatalf("schema = %+v", body.Schema)
	}
}

func TestQueryProviderExecuteReturnsMeaningfulErrors(t *testing.T) {
	t.Parallel()

	provider := newQueryProviderWithRunner(fakeQueryRunner{
		runFn: func(context.Context, string, string, string, queryOptions) (queryIterator, error) {
			return nil, errors.New("backend failed")
		},
	})

	cases := []struct {
		name      string
		operation string
		params    map[string]any
		wantErr   string
	}{
		{
			name:      "unknown operation",
			operation: "other",
			params:    map[string]any{},
			wantErr:   "unknown operation",
		},
		{
			name:      "missing project",
			operation: queryOperationName,
			params:    map[string]any{queryParamSQL: "SELECT 1"},
			wantErr:   queryParamProjectID + " is required",
		},
		{
			name:      "missing query",
			operation: queryOperationName,
			params:    map[string]any{queryParamProjectID: "analytics-prod"},
			wantErr:   queryParamSQL + " is required",
		},
		{
			name:      "runner error",
			operation: queryOperationName,
			params: map[string]any{
				queryParamProjectID: "analytics-prod",
				queryParamSQL:       "SELECT 1",
			},
			wantErr: "backend failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := provider.Execute(context.Background(), tc.operation, tc.params, "access-token")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
