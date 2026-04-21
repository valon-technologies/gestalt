package provider

import (
	"context"
	"encoding/json"
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

type RequestContextInput struct{}

type InvokeRequestContextInput struct{}

type RequestContextSubject struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	AuthSource  string `json:"auth_source"`
}

type RequestContextCredential struct {
	Mode       string `json:"mode"`
	SubjectID  string `json:"subject_id"`
	Connection string `json:"connection"`
	Instance   string `json:"instance"`
}

type RequestContextAccess struct {
	Policy string `json:"policy"`
	Role   string `json:"role"`
}

type RequestContextOutput struct {
	Subject         RequestContextSubject    `json:"subject"`
	Credential      RequestContextCredential `json:"credential"`
	Access          RequestContextAccess     `json:"access"`
	InvocationToken string                   `json:"invocation_token"`
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
	requestContextOperation = gestalt.Operation[RequestContextInput, RequestContextOutput]{
		ID:          "request_context",
		Method:      http.MethodGet,
		Description: "Return the request context received by the provider",
		ReadOnly:    true,
	}
	invokeRequestContextOperation = gestalt.Operation[InvokeRequestContextInput, RequestContextOutput]{
		ID:          "invoke_request_context",
		Method:      http.MethodGet,
		Description: "Invoke request_context through InvokerFromContext",
		ReadOnly:    true,
	}
	Router = gestalt.MustRouter(
		gestalt.Register(greetOperation, (*Provider).greet),
		gestalt.Register(echoOperation, (*Provider).echo),
		gestalt.Register(statusOperation, (*Provider).status),
		gestalt.Register(requestContextOperation, (*Provider).requestContext),
		gestalt.Register(invokeRequestContextOperation, (*Provider).invokeRequestContext),
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

func (p *Provider) requestContext(_ context.Context, _ RequestContextInput, req gestalt.Request) (gestalt.Response[RequestContextOutput], error) {
	return gestalt.OK(RequestContextOutput{
		Subject: RequestContextSubject{
			ID:          req.Subject.ID,
			Kind:        req.Subject.Kind,
			DisplayName: req.Subject.DisplayName,
			AuthSource:  req.Subject.AuthSource,
		},
		Credential: RequestContextCredential{
			Mode:       req.Credential.Mode,
			SubjectID:  req.Credential.SubjectID,
			Connection: req.Credential.Connection,
			Instance:   req.Credential.Instance,
		},
		Access: RequestContextAccess{
			Policy: req.Access.Policy,
			Role:   req.Access.Role,
		},
		InvocationToken: req.InvocationToken(),
	}), nil
}

func (p *Provider) invokeRequestContext(ctx context.Context, _ InvokeRequestContextInput, _ gestalt.Request) (gestalt.Response[RequestContextOutput], error) {
	invoker, err := gestalt.InvokerFromContext(ctx)
	if err != nil {
		return gestalt.Response[RequestContextOutput]{}, err
	}
	defer func() { _ = invoker.Close() }()

	result, err := invoker.Invoke(ctx, "example", "request_context", nil, nil)
	if err != nil {
		return gestalt.Response[RequestContextOutput]{}, err
	}

	var out RequestContextOutput
	if err := json.Unmarshal([]byte(result.Body), &out); err != nil {
		return gestalt.Response[RequestContextOutput]{}, err
	}
	return gestalt.OK(out), nil
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
