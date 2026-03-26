package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

type exampleProvider struct {
	greeting    string
	startedName string
	startedMode string
}

func (p *exampleProvider) Name() string        { return "example" }
func (p *exampleProvider) DisplayName() string { return "Example Provider" }
func (p *exampleProvider) Description() string {
	return "A minimal example provider built with the public SDK"
}
func (p *exampleProvider) ConnectionMode() pluginsdk.ConnectionMode {
	return pluginsdk.ConnectionModeNone
}

func (p *exampleProvider) ListOperations() []pluginsdk.Operation {
	return []pluginsdk.Operation{
		{
			Name:        "greet",
			Description: "Return a greeting message",
			Method:      "GET",
			Parameters: []pluginsdk.Parameter{
				{Name: "name", Type: "string", Description: "Name to greet", Required: true},
			},
		},
		{
			Name:        "echo",
			Description: "Echo back the input",
			Method:      "POST",
			Parameters: []pluginsdk.Parameter{
				{Name: "message", Type: "string", Description: "Message to echo", Required: true},
			},
		},
		{
			Name:        "status",
			Description: "Return provider startup state",
			Method:      "GET",
		},
	}
}

func (p *exampleProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*pluginsdk.OperationResult, error) {
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
		return &pluginsdk.OperationResult{Status: 200, Body: string(body)}, nil
	case "echo":
		msg, _ := params["message"].(string)
		body, _ := json.Marshal(map[string]string{"echo": msg})
		return &pluginsdk.OperationResult{Status: 200, Body: string(body)}, nil
	case "status":
		body, _ := json.Marshal(map[string]string{
			"name":     p.startedName,
			"mode":     p.startedMode,
			"greeting": p.greeting,
		})
		return &pluginsdk.OperationResult{Status: 200, Body: string(body)}, nil
	default:
		return &pluginsdk.OperationResult{Status: 404, Body: `{"error":"unknown operation"}`}, nil
	}
}

func (p *exampleProvider) Start(_ context.Context, name string, config map[string]any, mode string) error {
	p.startedName = name
	p.startedMode = mode
	if g, ok := config["greeting"].(string); ok {
		p.greeting = g
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := pluginsdk.ServeProvider(ctx, &exampleProvider{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
