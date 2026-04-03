package integration

import (
	"encoding/json"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
)

type ResponseMappingConfig struct {
	DataPath              string
	PaginationHasMorePath string
	PaginationCursorPath  string
}

func applyResponseMapping(result *core.OperationResult, cfg *ResponseMappingConfig) *core.OperationResult {
	if result == nil || cfg == nil || result.Status >= http.StatusBadRequest {
		return result
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(result.Body), &raw); err != nil {
		return result
	}

	output := make(map[string]any)

	if cfg.DataPath != "" {
		if data, ok := apiexec.ExtractJSONPath(raw, cfg.DataPath); ok {
			output["data"] = data
		} else {
			return result
		}
	}

	if cfg.PaginationHasMorePath != "" || cfg.PaginationCursorPath != "" {
		pgn := make(map[string]any)
		if cfg.PaginationHasMorePath != "" {
			if v, ok := apiexec.ExtractJSONPath(raw, cfg.PaginationHasMorePath); ok {
				pgn["has_more"] = v
			}
		}
		if cfg.PaginationCursorPath != "" {
			if v, ok := apiexec.ExtractJSONPath(raw, cfg.PaginationCursorPath); ok {
				pgn["cursor"] = v
			}
		}
		if len(pgn) > 0 {
			output["pagination"] = pgn
		}
	}

	body, err := json.Marshal(output)
	if err != nil {
		return result
	}

	return &core.OperationResult{
		Status:  result.Status,
		Headers: result.Headers,
		Body:    string(body),
	}
}
