package models

import (
	"context"
	"testing"

	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/models/modelgrants"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type recordingModelManagerService struct {
	generate func(context.Context, *principal.Principal, coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error)
}

func (s *recordingModelManagerService) Generate(ctx context.Context, p *principal.Principal, req coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error) {
	if s.generate != nil {
		return s.generate(ctx, p, req)
	}
	return &coremodel.GenerateResponse{OutputText: "ok"}, nil
}

func TestManagerServerEmptyModelGrantsDenyGenerate(t *testing.T) {
	t.Parallel()

	tokens, err := plugininvokerservice.NewInvocationTokenManager([]byte("model-manager-deny-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token, err := tokens.MintRootTokenWithManagerGrants(
		principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:123", Kind: principal.KindUser}),
		"caller",
		nil,
		nil,
		modelgrants.Grants{},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithManagerGrants: %v", err)
	}
	server := NewManagerServer("caller", &recordingModelManagerService{
		generate: func(context.Context, *principal.Principal, coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error) {
			t.Fatal("Generate service should not be called without model grant")
			return nil, nil
		},
	}, tokens)

	_, err = server.Generate(context.Background(), &proto.ModelManagerGenerateRequest{InvocationToken: token})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Generate error = %v, want PermissionDenied", err)
	}
}

func TestManagerServerGenerateRestoresPrincipalAndRequest(t *testing.T) {
	t.Parallel()

	tokens, err := plugininvokerservice.NewInvocationTokenManager([]byte("model-manager-generate-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	token, err := tokens.MintRootTokenWithManagerGrants(
		principal.WithPrincipal(context.Background(), &principal.Principal{
			SubjectID: "user:123",
			Kind:      principal.KindUser,
			Source:    principal.SourceSession,
		}),
		"caller",
		nil,
		nil,
		modelgrants.Grants{modelgrants.OperationGenerate: {}},
	)
	if err != nil {
		t.Fatalf("MintRootTokenWithManagerGrants: %v", err)
	}
	responseSchema := mustStruct(t, map[string]any{"type": "object"})
	modelOptions := mustStruct(t, map[string]any{"temperature": 0.1})
	metadata := mustStruct(t, map[string]any{"attempt_id": "attempt-1"})
	server := NewManagerServer(" caller ", &recordingModelManagerService{
		generate: func(_ context.Context, p *principal.Principal, req coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error) {
			if p == nil || p.SubjectID != "user:123" || p.Kind != principal.KindUser {
				t.Fatalf("principal = %#v, want restored user", p)
			}
			if req.ProviderName != "claude" || req.Model != "claude-test" || req.CallerPluginName != "caller" {
				t.Fatalf("manager request routing fields = %#v", req)
			}
			if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Text != "grade" {
				t.Fatalf("messages = %#v, want single user message", req.Messages)
			}
			if req.ResponseSchema["type"] != "object" || req.ModelOptions["temperature"] != 0.1 || req.Metadata["attempt_id"] != "attempt-1" {
				t.Fatalf("request maps = %#v %#v %#v", req.ResponseSchema, req.ModelOptions, req.Metadata)
			}
			return &coremodel.GenerateResponse{
				OutputText:       "done",
				StructuredOutput: map[string]any{"score": 1.0},
			}, nil
		},
	}, tokens)

	resp, err := server.Generate(context.Background(), &proto.ModelManagerGenerateRequest{
		InvocationToken: token,
		ProviderName:    " claude ",
		Model:           " claude-test ",
		Messages:        []*proto.ModelMessage{{Role: "user", Text: "grade"}},
		ResponseSchema:  responseSchema,
		ModelOptions:    modelOptions,
		Metadata:        metadata,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.GetOutputText() != "done" || resp.GetStructuredOutput().GetFields()["score"].GetNumberValue() != 1.0 {
		t.Fatalf("response = %#v, want encoded structured response", resp)
	}
}

func mustStruct(t *testing.T, value map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(value)
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	return out
}
