package apiexec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/core"
)

const (
	PaginationStyleCursor = "cursor"
	PaginationStyleOffset = "offset"
	PaginationStylePage   = "page"

	defaultMaxPages    = 10
	defaultStartPage   = 1
	defaultStartOffset = 0
)

type PaginationConfig struct {
	Style        string
	CursorParam  string
	CursorPath   string
	LimitParam   string
	DefaultLimit int
	ResultsPath  string
	MaxPages     int
}

func (p PaginationConfig) maxPages() int {
	if p.MaxPages > 0 {
		return p.MaxPages
	}
	return defaultMaxPages
}

// DoPaginated fetches all pages of a paginated API endpoint and combines the
// results into a single JSON array. It delegates each individual request to Do.
func DoPaginated(ctx context.Context, client *http.Client, req Request, pgn PaginationConfig) (*core.OperationResult, error) {
	params := copyParams(req.Params)
	if params == nil {
		params = make(map[string]any)
	}

	if pgn.LimitParam != "" && pgn.DefaultLimit > 0 {
		if _, set := params[pgn.LimitParam]; !set {
			params[pgn.LimitParam] = pgn.DefaultLimit
		}
	}

	if pgn.Style == PaginationStylePage {
		if _, set := params[pgn.CursorParam]; !set {
			params[pgn.CursorParam] = defaultStartPage
		}
	}

	var allResults []any
	var lastStatus int
	maxPages := pgn.maxPages()

	for page := 0; page < maxPages; page++ {
		pageReq := req
		pageReq.Params = copyParams(params)

		result, err := Do(ctx, client, pageReq)
		if err != nil {
			return nil, err
		}
		lastStatus = result.Status

		var parsed any
		if err := json.Unmarshal([]byte(result.Body), &parsed); err != nil {
			return nil, fmt.Errorf("parsing paginated response: %w", err)
		}

		pageResults, ok := extractJSONPath(parsed, pgn.ResultsPath)
		if !ok {
			break
		}
		items, ok := pageResults.([]any)
		if !ok || len(items) == 0 {
			break
		}
		allResults = append(allResults, items...)

		switch pgn.Style {
		case PaginationStyleCursor:
			cursor, ok := extractJSONPath(parsed, pgn.CursorPath)
			if !ok || cursor == nil {
				return combinedResult(lastStatus, allResults)
			}
			cursorStr := cursorToString(cursor)
			if cursorStr == "" {
				return combinedResult(lastStatus, allResults)
			}
			params[pgn.CursorParam] = cursorStr

		case PaginationStyleOffset:
			currentOffset := defaultStartOffset
			if v, ok := params[pgn.CursorParam]; ok {
				currentOffset = toInt(v)
			}
			pageSize := len(items)
			if pgn.DefaultLimit > 0 {
				pageSize = pgn.DefaultLimit
			}
			if v, ok := params[pgn.LimitParam]; ok {
				pageSize = toInt(v)
			}
			params[pgn.CursorParam] = currentOffset + pageSize

		case PaginationStylePage:
			currentPage := defaultStartPage
			if v, ok := params[pgn.CursorParam]; ok {
				currentPage = toInt(v)
			}
			params[pgn.CursorParam] = currentPage + 1

		default:
			return nil, fmt.Errorf("unsupported pagination style: %q", pgn.Style)
		}
	}

	return combinedResult(lastStatus, allResults)
}

func combinedResult(status int, results []any) (*core.OperationResult, error) {
	if results == nil {
		results = []any{}
	}
	data, err := json.Marshal(results)
	if err != nil {
		return nil, fmt.Errorf("marshaling combined results: %w", err)
	}
	return &core.OperationResult{
		Status: status,
		Body:   string(data),
	}, nil
}

// extractJSONPath traverses a parsed JSON value using a dotted path (e.g. "data.items").
func extractJSONPath(data any, path string) (any, bool) {
	if path == "" {
		return data, true
	}
	parts := strings.Split(path, ".")
	current := data
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func cursorToString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case float64:
		if c == 0 {
			return ""
		}
		return fmt.Sprintf("%v", c)
	case bool:
		return ""
	default:
		s := fmt.Sprintf("%v", v)
		if s == "<nil>" {
			return ""
		}
		return s
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
		return 0
	default:
		return 0
	}
}
