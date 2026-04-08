package provider

import (
	"context"
	"fmt"
	"net/http"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct {
	greeting    string
	startedName string
}

type GreetInput struct {
	Name string `json:"name,omitempty" doc:"Name to greet"`
}

type GreetOutput struct {
	Message string `json:"message"`
}

type EchoInput struct {
	Message string `json:"message" doc:"Message to echo"`
}

type EchoOutput struct {
	Echo string `json:"echo"`
}

type StatusInput struct{}

type StatusOutput struct {
	Name     string `json:"name"`
	Greeting string `json:"greeting"`
}

var (
	greetOperation = gestalt.Operation[GreetInput, GreetOutput]{
		ID:          "greet",
		Method:      http.MethodGet,
		Description: "Return a greeting message",
	}
	echoOperation = gestalt.Operation[EchoInput, EchoOutput]{
		ID:          "echo",
		Method:      http.MethodPost,
		Description: "Echo back the input",
	}
	statusOperation = gestalt.Operation[StatusInput, StatusOutput]{
		ID:          "status",
		Method:      http.MethodGet,
		Description: "Return provider startup state",
		ReadOnly:    true,
	}
	Router = gestalt.MustRouter(
		gestalt.Register(greetOperation, (*Provider).greet),
		gestalt.Register(echoOperation, (*Provider).echo),
		gestalt.Register(statusOperation, (*Provider).status),
	)
)

func New() *Provider {
	return &Provider{}
}

func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {
	p.startedName = name
	if g, ok := config["greeting"].(string); ok {
		p.greeting = g
	}
	return nil
}

func (p *Provider) greet(_ context.Context, input GreetInput, _ gestalt.Request) (gestalt.Response[GreetOutput], error) {
	return p.greetResult(input.Name), nil
}

func (p *Provider) echo(_ context.Context, input EchoInput, _ gestalt.Request) (gestalt.Response[EchoOutput], error) {
	return p.echoResult(input.Message), nil
}

func (p *Provider) status(_ context.Context, _ StatusInput, _ gestalt.Request) (gestalt.Response[StatusOutput], error) {
	return gestalt.OK(StatusOutput{
		Name:     p.startedName,
		Greeting: p.greeting,
	}), nil
}

func (p *Provider) greetingFor(name string) string {
	greeting := p.greeting
	if greeting == "" {
		greeting = "Hello"
	}
	if name == "" {
		name = "World"
	}
	return fmt.Sprintf("%s, %s!", greeting, name)
}

func (p *Provider) greetResult(name string) gestalt.Response[GreetOutput] {
	return gestalt.OK(GreetOutput{Message: p.greetingFor(name)})
}

func (p *Provider) echoResult(message string) gestalt.Response[EchoOutput] {
	return gestalt.OK(EchoOutput{Echo: message})
}
