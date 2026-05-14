package modelmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type recordingModelProvider struct {
	generate func(context.Context, coremodel.GenerateRequest) (*coremodel.GenerateResponse, error)
}

func (p *recordingModelProvider) Generate(ctx context.Context, req coremodel.GenerateRequest) (*coremodel.GenerateResponse, error) {
	if p.generate != nil {
		return p.generate(ctx, req)
	}
	return &coremodel.GenerateResponse{OutputText: "ok"}, nil
}

func (p *recordingModelProvider) GetCapabilities(context.Context, coremodel.GetCapabilitiesRequest) (*coremodel.ProviderCapabilities, error) {
	return &coremodel.ProviderCapabilities{}, nil
}

func (p *recordingModelProvider) Ping(context.Context) error { return nil }

func (p *recordingModelProvider) Close() error { return nil }

func TestManagerGenerateUsesDefaultProviderAndThreadsCallerContext(t *testing.T) {
	t.Parallel()

	provider := &recordingModelProvider{
		generate: func(_ context.Context, req coremodel.GenerateRequest) (*coremodel.GenerateResponse, error) {
			if req.ProviderName != "default" {
				t.Fatalf("ProviderName = %q, want default", req.ProviderName)
			}
			if req.Model != "claude-test" {
				t.Fatalf("Model = %q, want claude-test", req.Model)
			}
			if req.CallerPluginName != "valonSats" {
				t.Fatalf("CallerPluginName = %q, want valonSats", req.CallerPluginName)
			}
			if req.Subject.SubjectID != "user:123" || req.Subject.SubjectKind != "user" || req.Subject.AuthSource != "session" {
				t.Fatalf("Subject = %#v, want user principal context", req.Subject)
			}
			if got := req.Messages[0].Text; got != "grade this" {
				t.Fatalf("message text = %q, want grade this", got)
			}
			if got := req.ResponseSchema["type"]; got != "object" {
				t.Fatalf("response schema type = %#v, want object", got)
			}
			if got := req.ModelOptions["temperature"]; got != 0.2 {
				t.Fatalf("model option temperature = %#v, want 0.2", got)
			}
			if got := req.Metadata["attempt_id"]; got != "attempt-1" {
				t.Fatalf("metadata attempt_id = %#v, want attempt-1", got)
			}
			return &coremodel.GenerateResponse{
				OutputText:       "graded",
				StructuredOutput: map[string]any{"score": 1.0},
			}, nil
		},
	}
	manager := New(Config{
		Providers:       map[string]coremodel.Provider{"default": provider},
		DefaultProvider: "default",
	})
	p := &principal.Principal{
		SubjectID: "user:123",
		Kind:      principal.KindUser,
		Source:    principal.SourceSession,
	}

	resp, err := manager.Generate(context.Background(), p, coremodel.ManagerGenerateRequest{
		Model:            " claude-test ",
		Messages:         []coremodel.Message{{Role: "user", Text: "grade this"}},
		ResponseSchema:   map[string]any{"type": "object"},
		ModelOptions:     map[string]any{"temperature": 0.2},
		Metadata:         map[string]any{"attempt_id": "attempt-1"},
		CallerPluginName: " valonSats ",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.OutputText != "graded" || resp.StructuredOutput["score"] != 1.0 {
		t.Fatalf("response = %#v, want structured graded response", resp)
	}
}

func TestManagerGenerateValidationAndProviderErrors(t *testing.T) {
	t.Parallel()

	manager := New(Config{Providers: map[string]coremodel.Provider{
		"default": &recordingModelProvider{
			generate: func(context.Context, coremodel.GenerateRequest) (*coremodel.GenerateResponse, error) {
				return &coremodel.GenerateResponse{OutputText: "missing structured output"}, nil
			},
		},
	}, DefaultProvider: "default"})

	if _, err := manager.Generate(context.Background(), nil, coremodel.ManagerGenerateRequest{}); !errors.Is(err, ErrInvalidGenerateRequest) {
		t.Fatalf("Generate without messages error = %v, want ErrInvalidGenerateRequest", err)
	}
	if _, err := manager.Generate(context.Background(), nil, coremodel.ManagerGenerateRequest{
		Messages:       []coremodel.Message{{Role: "user", Text: "hello"}},
		ResponseSchema: map[string]any{"type": "array"},
	}); !errors.Is(err, ErrInvalidGenerateRequest) {
		t.Fatalf("Generate with array schema error = %v, want ErrInvalidGenerateRequest", err)
	}
	if _, err := manager.Generate(context.Background(), nil, coremodel.ManagerGenerateRequest{
		ProviderName: "missing",
		Messages:     []coremodel.Message{{Role: "user", Text: "hello"}},
	}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Generate missing provider error = %v, want ErrNotFound", err)
	}
	if _, err := manager.Generate(context.Background(), nil, coremodel.ManagerGenerateRequest{
		Messages:       []coremodel.Message{{Role: "user", Text: "hello"}},
		ResponseSchema: map[string]any{"type": "object"},
	}); err == nil || err.Error() != "model provider did not return structured output" {
		t.Fatalf("Generate missing structured output error = %v", err)
	}
}
