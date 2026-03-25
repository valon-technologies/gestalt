package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

const defaultGreeting = "Hello"

type exampleProvider struct {
	greeting string
}

func (p *exampleProvider) Name() string                                { return "example" }
func (p *exampleProvider) DisplayName() string                         { return "Example Provider" }
func (p *exampleProvider) Description() string                         { return "A minimal provider plugin demonstrating the Gestalt plugin SDK." }
func (p *exampleProvider) ConnectionMode() pluginsdk.ConnectionMode    { return pluginsdk.ConnectionModeNone }

func (p *exampleProvider) ListOperations() []pluginsdk.Operation {
	return []pluginsdk.Operation{
		{
			Name:        "greet",
			Description: "Returns a greeting for the given name.",
			Method:      "GET",
			Parameters: []pluginsdk.Parameter{
				{Name: "name", Type: "string", Description: "Name to greet.", Required: true},
			},
		},
		{
			Name:        "echo",
			Description: "Echoes back the input message.",
			Method:      "POST",
			Parameters: []pluginsdk.Parameter{
				{Name: "message", Type: "string", Description: "Message to echo.", Required: true},
			},
		},
		{
			Name:        "list_items",
			Description: "Returns a static list of sample items.",
			Method:      "GET",
		},
	}
}

func (p *exampleProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*pluginsdk.OperationResult, error) {
	switch operation {
	case "greet":
		name, _ := params["name"].(string)
		if name == "" {
			return &pluginsdk.OperationResult{Status: 400, Body: `{"error":"name is required"}`}, nil
		}
		greeting := p.greeting
		if greeting == "" {
			greeting = defaultGreeting
		}
		return jsonResult(200, map[string]string{"greeting": fmt.Sprintf("%s, %s!", greeting, name)})

	case "echo":
		message, _ := params["message"].(string)
		if message == "" {
			return &pluginsdk.OperationResult{Status: 400, Body: `{"error":"message is required"}`}, nil
		}
		return jsonResult(200, map[string]string{"echo": message})

	case "list_items":
		items := []map[string]any{
			{"id": 1, "name": "alpha"},
			{"id": 2, "name": "bravo"},
			{"id": 3, "name": "charlie"},
		}
		return jsonResult(200, map[string]any{"items": items})

	default:
		return &pluginsdk.OperationResult{Status: 404, Body: fmt.Sprintf(`{"error":"unknown operation %q"}`, operation)}, nil
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	provider := &exampleProvider{greeting: defaultGreeting}

	log.Println("starting example provider plugin")
	if err := pluginsdk.ServeProvider(ctx, provider); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func jsonResult(status int, v any) (*pluginsdk.OperationResult, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}
	return &pluginsdk.OperationResult{Status: status, Body: string(body)}, nil
}
