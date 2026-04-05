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

func (p *exampleProvider) Configure(_ context.Context, name string, config map[string]any) error {
	p.startedName = name
	if g, ok := config["greeting"].(string); ok {
		p.greeting = g
	}
	return nil
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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := gestalt.ServeProvider(ctx, &exampleProvider{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
