//go:build functionaltest

package bigquery

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	cloudbigquery "cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"google.golang.org/api/iterator"
)

func NewQueryProviderForFunctionalTest(scenario string) (*QueryProvider, error) {
	switch scenario {
	case "rows":
		return &QueryProvider{runner: functionalTestQueryRunner{scenario: scenario}}, nil
	case "non_terminating_decimal":
		return &QueryProvider{runner: functionalTestQueryRunner{scenario: scenario}}, nil
	case "backend_error":
		return &QueryProvider{runner: functionalTestQueryRunner{scenario: scenario}}, nil
	default:
		return nil, fmt.Errorf("unknown functional test scenario %q", scenario)
	}
}

type functionalTestQueryRunner struct {
	scenario string
}

func (r functionalTestQueryRunner) Run(_ context.Context, projectID, token, sql string, opts queryOptions) (queryIterator, error) {
	switch r.scenario {
	case "rows":
		if projectID != "sample" {
			return nil, fmt.Errorf("unexpected project_id %q", projectID)
		}
		_ = token
		if sql != "SELECT 1" {
			return nil, fmt.Errorf("unexpected query %q", sql)
		}
		if opts.Timeout != 1500*time.Millisecond {
			return nil, fmt.Errorf("unexpected timeout %s", opts.Timeout)
		}
		if !opts.UseLegacySQL {
			return nil, errors.New("expected legacy SQL flag")
		}

		return &functionalTestQueryIterator{
			schema: cloudbigquery.Schema{
				{Name: "value", Type: cloudbigquery.NumericFieldType},
				{Name: "stamp", Type: cloudbigquery.DateTimeFieldType},
				{Name: "items", Type: cloudbigquery.StringFieldType, Repeated: true},
				{Name: "nested", Type: cloudbigquery.RecordFieldType},
			},
			totalRows:      2,
			delayTotalRows: true,
			rows: []map[string]cloudbigquery.Value{
				{
					"value": big.NewRat(2469, 200),
					"stamp": civil.DateTime{
						Date: civil.Date{Year: 2026, Month: 3, Day: 25},
						Time: civil.Time{Hour: 9, Minute: 30, Second: 0},
					},
					"items": []cloudbigquery.Value{"x", big.NewRat(1, 8)},
					"nested": map[string]cloudbigquery.Value{
						"ratio": big.NewRat(1001, 1000),
					},
				},
				{
					"value": big.NewRat(-5, 2),
				},
			},
		}, nil
	case "non_terminating_decimal":
		if projectID != "sample" {
			return nil, fmt.Errorf("unexpected project_id %q", projectID)
		}
		_ = token
		if sql != "SELECT 1" {
			return nil, fmt.Errorf("unexpected query %q", sql)
		}

		return &functionalTestQueryIterator{
			schema: cloudbigquery.Schema{
				{Name: "value", Type: cloudbigquery.NumericFieldType},
			},
			totalRows: 1,
			rows: []map[string]cloudbigquery.Value{
				{
					"value": big.NewRat(1, 3),
				},
			},
		}, nil
	case "backend_error":
		return nil, errors.New("backend unavailable")
	default:
		return nil, fmt.Errorf("unknown functional test scenario %q", r.scenario)
	}
}

type functionalTestQueryIterator struct {
	schema             cloudbigquery.Schema
	totalRows          uint64
	delayTotalRows     bool
	totalRowsAvailable bool
	rows               []map[string]cloudbigquery.Value
	index              int
	closed             bool
}

func (f *functionalTestQueryIterator) Schema() cloudbigquery.Schema { return f.schema }
func (f *functionalTestQueryIterator) TotalRows() uint64 {
	if f.delayTotalRows && !f.totalRowsAvailable {
		return 0
	}
	return f.totalRows
}

func (f *functionalTestQueryIterator) Next(row *map[string]cloudbigquery.Value) error {
	if f.closed {
		return errors.New("iterator used after close")
	}
	if f.index >= len(f.rows) {
		return iterator.Done
	}
	*row = f.rows[f.index]
	f.index++
	f.totalRowsAvailable = true
	return nil
}

func (f *functionalTestQueryIterator) Close() error {
	f.closed = true
	return nil
}
