package echo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/valon-technologies/toolshed/core"
)

const (
	providerName  = "echo"
	operationName = "echo"
)

var _ core.Provider = (*Provider)(nil)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string                        { return providerName }
func (p *Provider) DisplayName() string                 { return "Echo" }
func (p *Provider) Description() string                 { return "Echoes back the input parameters" }
func (p *Provider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeNone }

func (p *Provider) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        operationName,
			Description: "Echo back input params as JSON",
			Method:      http.MethodPost,
		},
	}
}

func (p *Provider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
	if operation != operationName {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}

	return &core.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}
