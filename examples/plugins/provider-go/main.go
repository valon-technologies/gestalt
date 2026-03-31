package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type exampleProvider struct {
	greeting    string
	startedName string
}

func (p *exampleProvider) Name() string        { return "example" }
func (p *exampleProvider) DisplayName() string { return "Example Provider" }
func (p *exampleProvider) Description() string {
	return "A minimal example provider built with the public SDK"
}
func (p *exampleProvider) ConnectionMode() gestalt.ConnectionMode {
	return gestalt.ConnectionModeNone
}

func (p *exampleProvider) ListOperations() []gestalt.Operation {
	return []gestalt.Operation{
		{
			Name:        "greet",
			Description: "Return a greeting message",
			Method:      http.MethodGet,
			Parameters: []gestalt.Parameter{
				{Name: "name", Type: "string", Description: "Name to greet", Required: true},
			},
		},
		{
			Name:        "echo",
			Description: "Echo back the input",
			Method:      http.MethodPost,
			Parameters: []gestalt.Parameter{
				{Name: "message", Type: "string", Description: "Message to echo", Required: true},
			},
		},
		{
			Name:        "status",
			Description: "Return provider startup state",
			Method:      http.MethodGet,
		},
	}
}

func (p *exampleProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*gestalt.OperationResult, error) {
	switch operation {
	case "greet":
		name, _ := params["name"].(string)
		if name == "" {
			name = "World"
		}
		greeting := p.greeting
		if greeting == "" {
			greeting = "Hello"
		}
		body, _ := json.Marshal(map[string]string{"message": fmt.Sprintf("%s, %s!", greeting, name)})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
	case "echo":
		msg, _ := params["message"].(string)
		body, _ := json.Marshal(map[string]string{"echo": msg})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
	case "status":
		body, _ := json.Marshal(map[string]string{
			"name":     p.startedName,
			"greeting": p.greeting,
		})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
	default:
		return &gestalt.OperationResult{Status: http.StatusNotFound, Body: `{"error":"unknown operation"}`}, nil
	}
}

func (p *exampleProvider) Start(_ context.Context, name string, config map[string]any) error {
	p.startedName = name
	if g, ok := config["greeting"].(string); ok {
		p.greeting = g
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := gestalt.ServeProvider(ctx, &exampleProvider{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
