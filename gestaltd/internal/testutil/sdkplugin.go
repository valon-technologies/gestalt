package testutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func SDKGoModulePath(t *testing.T) string {
	t.Helper()
	root, ok := repoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "sdk", "go")
}

func ExampleProviderPluginPath(t *testing.T) string {
	t.Helper()
	return MustExampleProviderPluginPath()
}

func MustExampleProviderPluginPath() string {
	root, ok := repoRoot()
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-go")
}

func MustExampleDatastoreProviderPath() string {
	root, ok := repoRoot()
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-rust-datastore")
}

func CopyExampleProviderPlugin(t *testing.T, dst string) {
	t.Helper()

	src := ExampleProviderPluginPath(t)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dst, err)
	}
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}); err != nil {
		t.Fatalf("copy example provider plugin: %v", err)
	}

	goModPath := filepath.Join(dst, "go.mod")
	goMod, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read %s: %v", goModPath, err)
	}
	updated := rewriteModuleLine(
		t,
		string(goMod),
		"replace github.com/valon-technologies/gestalt/sdk/go => ",
		"replace github.com/valon-technologies/gestalt/sdk/go => "+SDKGoModulePath(t),
	)
	if err := os.WriteFile(goModPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write %s: %v", goModPath, err)
	}
}

func repoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), true
}

func GeneratedProviderPackageSource() string {
	return `package provider

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) generatedOp(context.Context, struct{}, gestalt.Request) (gestalt.Response[map[string]any], error) {
	return gestalt.OK(map[string]any{}), nil
}

var Router = gestalt.MustRouter(
	gestalt.Register(gestalt.Operation[struct{}, map[string]any]{ID: "generated_op"}, (*Provider).generatedOp),
)
`
}

func GeneratedAuthPackageSource() string {
	return `package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAuthentication,
		Name:        "generated-auth",
		DisplayName: "Generated Auth",
	}
}

func (p *Provider) BeginAuthentication(_ context.Context, req *gestalt.BeginAuthenticationRequest) (*gestalt.BeginAuthenticationResponse, error) {
	return &gestalt.BeginAuthenticationResponse{
		AuthorizationUrl: "https://auth.example.test/login?state=idp-state&prompt=consent",
	}, nil
}

func (p *Provider) CompleteAuthentication(_ context.Context, req *gestalt.CompleteAuthenticationRequest) (*gestalt.AuthenticatedUser, error) {
	if req.GetQuery()["state"] != "idp-state" {
		return nil, fmt.Errorf("unexpected state %q", req.GetQuery()["state"])
	}
	if req.GetQuery()["prompt"] != "consent" {
		return nil, fmt.Errorf("unexpected prompt %q", req.GetQuery()["prompt"])
	}
	return &gestalt.AuthenticatedUser{
		Email:       "generated-auth@example.com",
		DisplayName: "Generated Auth User",
	}, nil
}

func (p *Provider) Authenticate(_ context.Context, req *gestalt.AuthenticateRequest) (*gestalt.AuthenticatedUser, error) {
	token := req.GetToken().GetToken()
	if token != "" {
		if strings.Count(token, ".") == 2 {
			return &gestalt.AuthenticatedUser{
				Email:       "jwt@example.com",
				DisplayName: "Validated JWT User",
			}, nil
		}
		return &gestalt.AuthenticatedUser{
			Email:       token + "@example.com",
			DisplayName: "Validated User",
		}, nil
	}
	if req.GetHttp() != nil {
		return &gestalt.AuthenticatedUser{
			Email:       req.GetHttp().GetHeaders()["x-test-user"],
			DisplayName: "Validated HTTP User",
		}, nil
	}
	return nil, fmt.Errorf("authentication input is required")
}

func (p *Provider) SessionTTL() time.Duration { return 90 * time.Minute }
`
}

func GeneratedAuthorizationPackageSource() string {
	return `package authorization

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAuthorization,
		Name:        "generated-authorization",
		DisplayName: "Generated Authorization",
	}
}

func (p *Provider) Evaluate(_ context.Context, _ *gestalt.AccessEvaluationRequest) (*gestalt.AccessDecision, error) {
	return &gestalt.AccessDecision{Allowed: true, ModelId: "model-v1"}, nil
}

func (p *Provider) EvaluateMany(_ context.Context, req *gestalt.AccessEvaluationsRequest) (*gestalt.AccessEvaluationsResponse, error) {
	decisions := make([]*gestalt.AccessDecision, 0, len(req.GetRequests()))
	for range req.GetRequests() {
		decisions = append(decisions, &gestalt.AccessDecision{Allowed: true, ModelId: "model-v1"})
	}
	return &gestalt.AccessEvaluationsResponse{Decisions: decisions}, nil
}

func (p *Provider) SearchResources(_ context.Context, _ *gestalt.ResourceSearchRequest) (*gestalt.ResourceSearchResponse, error) {
	return &gestalt.ResourceSearchResponse{
		Resources: []*gestalt.AuthorizationResource{{Type: "plugin", Id: "github"}},
		ModelId:   "model-v1",
	}, nil
}

func (p *Provider) SearchSubjects(_ context.Context, _ *gestalt.SubjectSearchRequest) (*gestalt.SubjectSearchResponse, error) {
	return &gestalt.SubjectSearchResponse{
		Subjects: []*gestalt.AuthorizationSubject{{Type: "user", Id: "generated-user"}},
		ModelId:  "model-v1",
	}, nil
}

func (p *Provider) SearchActions(_ context.Context, _ *gestalt.ActionSearchRequest) (*gestalt.ActionSearchResponse, error) {
	return &gestalt.ActionSearchResponse{
		Actions: []*gestalt.AuthorizationAction{{Name: "invoke"}},
		ModelId: "model-v1",
	}, nil
}

func (p *Provider) GetMetadata(context.Context) (*gestalt.AuthorizationMetadata, error) {
	return &gestalt.AuthorizationMetadata{
		Capabilities: []string{"evaluate", "relationships", "models"},
		ActiveModelId: "model-v1",
	}, nil
}

func (p *Provider) ReadRelationships(_ context.Context, _ *gestalt.ReadRelationshipsRequest) (*gestalt.ReadRelationshipsResponse, error) {
	return &gestalt.ReadRelationshipsResponse{
		Relationships: []*gestalt.Relationship{{
			Subject:  &gestalt.AuthorizationSubject{Type: "user", Id: "generated-user"},
			Relation: "viewer",
			Resource: &gestalt.AuthorizationResource{Type: "plugin", Id: "github"},
		}},
		ModelId: "model-v1",
	}, nil
}

func (p *Provider) WriteRelationships(context.Context, *gestalt.WriteRelationshipsRequest) error { return nil }

func (p *Provider) GetActiveModel(context.Context) (*gestalt.GetActiveModelResponse, error) {
	return &gestalt.GetActiveModelResponse{
		Model: &gestalt.AuthorizationModelRef{Id: "model-v1", Version: "v1"},
	}, nil
}

func (p *Provider) ListModels(_ context.Context, _ *gestalt.ListModelsRequest) (*gestalt.ListModelsResponse, error) {
	return &gestalt.ListModelsResponse{
		Models: []*gestalt.AuthorizationModelRef{{Id: "model-v1", Version: "v1"}},
	}, nil
}

func (p *Provider) WriteModel(context.Context, *gestalt.WriteModelRequest) (*gestalt.AuthorizationModelRef, error) {
	return &gestalt.AuthorizationModelRef{Id: "model-v2", Version: "v2"}, nil
}
`
}

func GeneratedSecretsPackageSource() string {
	return `package secrets

import (
	"context"
	"fmt"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct {
	secrets map[string]string
}

func New() *Provider {
	return &Provider{
		secrets: map[string]string{
			"generated-secret": "generated-secret-value",
			"source-token":     "ghp_inline_auth_source_token",
		},
	}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindSecrets,
		Name:        "generated-secrets",
		DisplayName: "Generated Secrets",
	}
}

func (p *Provider) GetSecret(_ context.Context, name string) (string, error) {
	if value, ok := p.secrets[name]; ok {
		return value, nil
	}
	return "", fmt.Errorf("secret %q not found", name)
}
`
}

func GeneratedWorkflowPackageSource() string {
	return `package workflow

import (
	"context"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Provider struct {
	proto.UnimplementedWorkflowProviderServer
}

func New() *Provider { return &Provider{} }

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindWorkflow,
		Name:        "generated-workflow",
		DisplayName: "Generated Workflow",
	}
}

func (p *Provider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return &proto.BoundWorkflowRun{Id: "generated-run", Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING}, nil
}

func (p *Provider) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return &proto.BoundWorkflowRun{Id: "generated-run", Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING}, nil
}

func (p *Provider) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return &proto.ListWorkflowProviderRunsResponse{}, nil
}

func (p *Provider) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return &proto.BoundWorkflowRun{Id: "generated-run", Status: proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED}, nil
}

func (p *Provider) UpsertSchedule(context.Context, *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return &proto.BoundWorkflowSchedule{Id: "generated-schedule"}, nil
}

func (p *Provider) GetSchedule(context.Context, *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return &proto.BoundWorkflowSchedule{Id: "generated-schedule"}, nil
}

func (p *Provider) ListSchedules(context.Context, *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	return &proto.ListWorkflowProviderSchedulesResponse{}, nil
}

func (p *Provider) DeleteSchedule(context.Context, *proto.DeleteWorkflowProviderScheduleRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (p *Provider) PauseSchedule(context.Context, *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return &proto.BoundWorkflowSchedule{Id: "generated-schedule", Paused: true}, nil
}

func (p *Provider) ResumeSchedule(context.Context, *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return &proto.BoundWorkflowSchedule{Id: "generated-schedule"}, nil
}

func (p *Provider) UpsertEventTrigger(context.Context, *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return &proto.BoundWorkflowEventTrigger{Id: "generated-trigger"}, nil
}

func (p *Provider) GetEventTrigger(context.Context, *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return &proto.BoundWorkflowEventTrigger{Id: "generated-trigger"}, nil
}

func (p *Provider) ListEventTriggers(context.Context, *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	return &proto.ListWorkflowProviderEventTriggersResponse{}, nil
}

func (p *Provider) DeleteEventTrigger(context.Context, *proto.DeleteWorkflowProviderEventTriggerRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (p *Provider) PauseEventTrigger(context.Context, *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return &proto.BoundWorkflowEventTrigger{Id: "generated-trigger", Paused: true}, nil
}

func (p *Provider) ResumeEventTrigger(context.Context, *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return &proto.BoundWorkflowEventTrigger{Id: "generated-trigger"}, nil
}

func (p *Provider) PublishEvent(context.Context, *proto.PublishWorkflowProviderEventRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
`
}

func GeneratedCachePackageSource() string {
	return `package cache

import (
	"context"
	"sync"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Provider struct {
	proto.UnimplementedCacheServer
	mu     sync.Mutex
	values map[string][]byte
}

func New() *Provider {
	return &Provider{values: map[string][]byte{}}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindCache,
		Name:        "generated-cache",
		DisplayName: "Generated Cache",
	}
}

func (p *Provider) Get(_ context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	value, ok := p.values[req.GetKey()]
	if !ok {
		return &proto.CacheGetResponse{}, nil
	}
	return &proto.CacheGetResponse{Found: true, Value: append([]byte(nil), value...)}, nil
}

func (p *Provider) GetMany(_ context.Context, req *proto.CacheGetManyRequest) (*proto.CacheGetManyResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries := make([]*proto.CacheResult, 0, len(req.GetKeys()))
	for _, key := range req.GetKeys() {
		entry := &proto.CacheResult{Key: key}
		if value, ok := p.values[key]; ok {
			entry.Found = true
			entry.Value = append([]byte(nil), value...)
		}
		entries = append(entries, entry)
	}
	return &proto.CacheGetManyResponse{Entries: entries}, nil
}

func (p *Provider) Set(_ context.Context, req *proto.CacheSetRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.values[req.GetKey()] = append([]byte(nil), req.GetValue()...)
	return &emptypb.Empty{}, nil
}

func (p *Provider) SetMany(_ context.Context, req *proto.CacheSetManyRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range req.GetEntries() {
		p.values[entry.GetKey()] = append([]byte(nil), entry.GetValue()...)
	}
	return &emptypb.Empty{}, nil
}

func (p *Provider) Delete(_ context.Context, req *proto.CacheDeleteRequest) (*proto.CacheDeleteResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.values[req.GetKey()]
	delete(p.values, req.GetKey())
	return &proto.CacheDeleteResponse{Deleted: ok}, nil
}

func (p *Provider) DeleteMany(_ context.Context, req *proto.CacheDeleteManyRequest) (*proto.CacheDeleteManyResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var deleted int64
	for _, key := range req.GetKeys() {
		if _, ok := p.values[key]; ok {
			delete(p.values, key)
			deleted++
		}
	}
	return &proto.CacheDeleteManyResponse{Deleted: deleted}, nil
}

func (p *Provider) Touch(_ context.Context, req *proto.CacheTouchRequest) (*proto.CacheTouchResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.values[req.GetKey()]
	return &proto.CacheTouchResponse{Touched: ok}, nil
}
`
}

func GeneratedProviderModuleSource(t *testing.T, module string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ExampleProviderPluginPath(t), "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(example go.mod): %v", err)
	}
	source := rewriteModuleLine(t, string(data), "module ", "module "+module)
	return rewriteModuleLine(
		t,
		source,
		"replace github.com/valon-technologies/gestalt/sdk/go => ",
		"replace github.com/valon-technologies/gestalt/sdk/go => "+SDKGoModulePath(t),
	)
}

func GeneratedProviderModuleSum(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ExampleProviderPluginPath(t), "go.sum"))
	if err != nil {
		t.Fatalf("ReadFile(example go.sum): %v", err)
	}
	return data
}

func rewriteModuleLine(t *testing.T, source, prefix, replacement string) string {
	t.Helper()
	lines := strings.Split(source, "\n")
	for i := range lines {
		if strings.HasPrefix(lines[i], prefix) {
			lines[i] = replacement
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("missing line prefix %q", prefix)
	return ""
}
