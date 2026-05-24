package testutil

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func SDKGoModulePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(RepoRootPath(t), "sdk", "go")
}

func RepoRootPath(t *testing.T) string {
	t.Helper()
	root, ok := repoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return root
}

func ExampleProviderPluginPath(t *testing.T) string {
	t.Helper()
	return MustExampleProviderPluginPath()
}

func ExampleProviderAppPath(t *testing.T) string {
	t.Helper()
	return MustExampleProviderAppPath()
}

func MustExampleProviderPluginPath() string {
	root, ok := repoRoot()
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-go")
}

func MustExampleProviderAppPath() string {
	return MustExampleProviderPluginPath()
}

func MustSDKTestProviderPath(name string) string {
	root, ok := repoRoot()
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "testproviders", name)
}

func BuildSDKTestMainBinary(srcDir, output string) error {
	root, ok := repoRoot()
	if !ok {
		return errors.New("runtime.Caller failed")
	}
	moduleDir, err := os.MkdirTemp("", "gestalt-sdk-test-provider-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(moduleDir) }()

	if err := copyDir(srcDir, moduleDir); err != nil {
		return err
	}
	goMod := "module github.com/valon-technologies/gestalt/testdata/" + filepath.Base(srcDir) + "\n\n" +
		"go 1.26\n\n" +
		"require github.com/valon-technologies/gestalt/sdk/go v0.0.0\n\n" +
		"replace github.com/valon-technologies/gestalt/sdk/go => " + filepath.ToSlash(filepath.Join(root, "sdk", "go")) + "\n" +
		"replace github.com/valon-technologies/gestalt/server/rpc => " + filepath.ToSlash(filepath.Join(root, "gestaltd", "rpc")) + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return err
	}
	if err := runGo(moduleDir, "mod", "tidy"); err != nil {
		return err
	}
	return runGo(moduleDir, "build", "-o", output, ".")
}

func CopyExampleProviderPlugin(t *testing.T, dst string) {
	t.Helper()

	src := ExampleProviderPluginPath(t)
	if err := copyDir(src, dst); err != nil {
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
	updated = rewriteModuleLine(
		t,
		updated,
		"replace github.com/valon-technologies/gestalt/server/rpc => ",
		"replace github.com/valon-technologies/gestalt/server/rpc => "+filepath.ToSlash(filepath.Join(RepoRootPath(t), "gestaltd", "rpc")),
	)
	if err := os.WriteFile(goModPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write %s: %v", goModPath, err)
	}
}

func CopyExampleProviderApp(t *testing.T, dst string) {
	t.Helper()
	CopyExampleProviderPlugin(t, dst)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
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
		if d.Type()&fs.ModeSymlink != 0 {
			return &os.PathError{Op: "copy", Path: path, Err: fs.ErrInvalid}
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
	})
}

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

func (p *Provider) BeginLogin(_ context.Context, req *gestalt.BeginLoginRequest) (*gestalt.BeginLoginResponse, error) {
	return &gestalt.BeginLoginResponse{
		AuthorizationUrl: "https://auth.example.test/login?state=idp-state&prompt=consent",
	}, nil
}

func (p *Provider) CompleteLogin(_ context.Context, req *gestalt.CompleteLoginRequest) (*gestalt.AuthenticatedUser, error) {
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

func (p *Provider) ValidateExternalToken(_ context.Context, token string) (*gestalt.AuthenticatedUser, error) {
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}
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
		Resources: []*gestalt.AuthorizationResource{{Type: "app", Id: "github"}},
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
			Resource: &gestalt.AuthorizationResource{Type: "app", Id: "github"},
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
)

type Provider struct {
	gestalt.UnimplementedWorkflowProvider
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

func (p *Provider) StartRun(context.Context, *gestalt.StartWorkflowProviderRunRequest) (*gestalt.BoundWorkflowRun, error) {
	return &gestalt.BoundWorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValuePending}, nil
}

func (p *Provider) GetRun(context.Context, *gestalt.GetWorkflowProviderRunRequest) (*gestalt.BoundWorkflowRun, error) {
	return &gestalt.BoundWorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValuePending}, nil
}

func (p *Provider) ListRuns(context.Context, *gestalt.ListWorkflowProviderRunsRequest) (*gestalt.ListWorkflowProviderRunsResponse, error) {
	return &gestalt.ListWorkflowProviderRunsResponse{}, nil
}

func (p *Provider) CancelRun(context.Context, *gestalt.CancelWorkflowProviderRunRequest) (*gestalt.BoundWorkflowRun, error) {
	return &gestalt.BoundWorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValueCanceled}, nil
}

func (p *Provider) UpsertSchedule(context.Context, *gestalt.UpsertWorkflowProviderScheduleRequest) (*gestalt.BoundWorkflowSchedule, error) {
	return &gestalt.BoundWorkflowSchedule{ID: "generated-schedule"}, nil
}

func (p *Provider) GetSchedule(context.Context, *gestalt.GetWorkflowProviderScheduleRequest) (*gestalt.BoundWorkflowSchedule, error) {
	return &gestalt.BoundWorkflowSchedule{ID: "generated-schedule"}, nil
}

func (p *Provider) ListSchedules(context.Context, *gestalt.ListWorkflowProviderSchedulesRequest) (*gestalt.ListWorkflowProviderSchedulesResponse, error) {
	return &gestalt.ListWorkflowProviderSchedulesResponse{}, nil
}

func (p *Provider) DeleteSchedule(context.Context, *gestalt.DeleteWorkflowProviderScheduleRequest) error {
	return nil
}

func (p *Provider) PauseSchedule(context.Context, *gestalt.PauseWorkflowProviderScheduleRequest) (*gestalt.BoundWorkflowSchedule, error) {
	return &gestalt.BoundWorkflowSchedule{ID: "generated-schedule", Paused: true}, nil
}

func (p *Provider) ResumeSchedule(context.Context, *gestalt.ResumeWorkflowProviderScheduleRequest) (*gestalt.BoundWorkflowSchedule, error) {
	return &gestalt.BoundWorkflowSchedule{ID: "generated-schedule"}, nil
}

func (p *Provider) UpsertEventTrigger(context.Context, *gestalt.UpsertWorkflowProviderEventTriggerRequest) (*gestalt.BoundWorkflowEventTrigger, error) {
	return &gestalt.BoundWorkflowEventTrigger{ID: "generated-trigger"}, nil
}

func (p *Provider) GetEventTrigger(context.Context, *gestalt.GetWorkflowProviderEventTriggerRequest) (*gestalt.BoundWorkflowEventTrigger, error) {
	return &gestalt.BoundWorkflowEventTrigger{ID: "generated-trigger"}, nil
}

func (p *Provider) ListEventTriggers(context.Context, *gestalt.ListWorkflowProviderEventTriggersRequest) (*gestalt.ListWorkflowProviderEventTriggersResponse, error) {
	return &gestalt.ListWorkflowProviderEventTriggersResponse{}, nil
}

func (p *Provider) DeleteEventTrigger(context.Context, *gestalt.DeleteWorkflowProviderEventTriggerRequest) error {
	return nil
}

func (p *Provider) PauseEventTrigger(context.Context, *gestalt.PauseWorkflowProviderEventTriggerRequest) (*gestalt.BoundWorkflowEventTrigger, error) {
	return &gestalt.BoundWorkflowEventTrigger{ID: "generated-trigger", Paused: true}, nil
}

func (p *Provider) ResumeEventTrigger(context.Context, *gestalt.ResumeWorkflowProviderEventTriggerRequest) (*gestalt.BoundWorkflowEventTrigger, error) {
	return &gestalt.BoundWorkflowEventTrigger{ID: "generated-trigger"}, nil
}

func (p *Provider) PublishEvent(context.Context, *gestalt.PublishWorkflowProviderEventRequest) (*gestalt.WorkflowEvent, error) {
	return &gestalt.WorkflowEvent{ID: "generated-event"}, nil
}
`
}

func GeneratedExternalCredentialPackageSource() string {
	return `package externalcredential

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Provider struct {
	mu          sync.Mutex
	credentials map[string]*gestalt.ExternalCredential
	lookupByID  map[string]string
}

func New() *Provider {
	return &Provider{
		credentials: map[string]*gestalt.ExternalCredential{},
		lookupByID:  map[string]string{},
	}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindExternalCredential,
		Name:        "generated-external-credentials",
		DisplayName: "Generated External Credentials",
	}
}

func (p *Provider) UpsertCredential(ctx context.Context, req *gestalt.UpsertExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return nil, err
	} else if ok {
		defer func() { _ = client.Close() }()
		return client.UpsertCredential(ctx, req)
	}
	if req == nil || req.GetCredential() == nil {
		return nil, fmt.Errorf("credential is required")
	}

	value := cloneExternalCredential(req.GetCredential())
	key := externalCredentialLookupKey(value.GetSubjectId(), value.GetConnectionId(), value.GetInstance())
	now := time.Now().UTC()

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing := p.credentials[key]; existing != nil {
		value.ID = existing.GetId()
		if value.GetCreatedAt() == nil {
			value.CreatedAt = cloneTime(existing.GetCreatedAt())
		}
	} else {
		if value.GetId() == "" {
			value.ID = "cred-" + value.GetConnectionId() + "-" + value.GetInstance()
		}
		if value.GetCreatedAt() == nil {
			value.CreatedAt = timePtr(now)
		}
	}
	if value.GetUpdatedAt() == nil {
		value.UpdatedAt = timePtr(now)
	}

	p.credentials[key] = cloneExternalCredential(value)
	p.lookupByID[value.GetId()] = key
	return cloneExternalCredential(value), nil
}

func (p *Provider) GetCredential(ctx context.Context, req *gestalt.GetExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return nil, err
	} else if ok {
		defer func() { _ = client.Close() }()
		return client.GetCredential(ctx, req)
	}
	if req == nil || req.GetLookup() == nil {
		return nil, fmt.Errorf("lookup is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	value, ok := p.credentials[externalCredentialLookupKey(
		req.GetLookup().GetSubjectId(),
		req.GetLookup().GetConnectionId(),
		req.GetLookup().GetInstance(),
	)]
	if !ok {
		return nil, gestalt.ErrExternalCredentialNotFound
	}
	return cloneExternalCredential(value), nil
}

func (p *Provider) ListCredentials(ctx context.Context, req *gestalt.ListExternalCredentialsRequest) (*gestalt.ListExternalCredentialsResponse, error) {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return nil, err
	} else if ok {
		defer func() { _ = client.Close() }()
		return client.ListCredentials(ctx, req)
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	credentials := make([]*gestalt.ExternalCredential, 0, len(p.credentials))
	for _, value := range p.credentials {
		if req.GetSubjectId() != "" && value.GetSubjectId() != req.GetSubjectId() {
			continue
		}
		if req.GetConnectionId() != "" && value.GetConnectionId() != req.GetConnectionId() {
			continue
		}
		if req.GetInstance() != "" && value.GetInstance() != req.GetInstance() {
			continue
		}
		credentials = append(credentials, cloneExternalCredential(value))
	}
	sort.Slice(credentials, func(i, j int) bool {
		return credentials[i].GetId() < credentials[j].GetId()
	})
	return &gestalt.ListExternalCredentialsResponse{Credentials: credentials}, nil
}

func (p *Provider) DeleteCredential(ctx context.Context, req *gestalt.DeleteExternalCredentialRequest) error {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return err
	} else if ok {
		defer func() { _ = client.Close() }()
		return client.DeleteCredential(ctx, req)
	}
	if req == nil || req.GetId() == "" {
		return fmt.Errorf("credential id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key, ok := p.lookupByID[req.GetId()]
	if !ok {
		return gestalt.ErrExternalCredentialNotFound
	}
	delete(p.lookupByID, req.GetId())
	delete(p.credentials, key)
	return nil
}

func (p *Provider) ValidateCredentialConfig(ctx context.Context, req *gestalt.ValidateExternalCredentialConfigRequest) error {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return err
	} else if ok {
		defer func() { _ = client.Close() }()
		if err := client.ValidateCredentialConfig(ctx, req); err != nil {
			if externalCredentialHostServiceMissing(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (p *Provider) ResolveCredential(ctx context.Context, req *gestalt.ResolveExternalCredentialRequest) (*gestalt.ResolveExternalCredentialResponse, error) {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return nil, err
	} else if ok {
		defer func() { _ = client.Close() }()
		return client.ResolveCredential(ctx, req)
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var credential *gestalt.ExternalCredential
	for _, value := range p.credentials {
		if value.GetSubjectId() != req.GetCredentialSubjectId() || value.GetConnectionId() != req.GetConnectionId() {
			continue
		}
		if req.GetInstance() != "" && value.GetInstance() != req.GetInstance() {
			continue
		}
		if credential != nil {
			return nil, fmt.Errorf("ambiguous external credential")
		}
		credential = value
	}
	if credential == nil {
		return nil, gestalt.ErrExternalCredentialNotFound
	}
	return &gestalt.ResolveExternalCredentialResponse{
		Token:        credential.GetAccessToken(),
		ExpiresAt:    credential.GetExpiresAt(),
		MetadataJSON: credential.GetMetadataJson(),
		Credential:   cloneExternalCredential(credential),
	}, nil
}

func (p *Provider) ExchangeCredential(ctx context.Context, req *gestalt.ExchangeExternalCredentialRequest) (*gestalt.ExchangeExternalCredentialResponse, error) {
	if client, ok, err := externalCredentialHostClient(); err != nil {
		return nil, err
	} else if ok {
		defer func() { _ = client.Close() }()
		return client.ExchangeCredential(ctx, req)
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	return &gestalt.ExchangeExternalCredentialResponse{TokenResponse: &gestalt.ExternalCredentialTokenResponse{
		AccessToken:   req.GetCredentialJson(),
		RefreshSource: req.GetCredentialJson(),
	}}, nil
}

func cloneExternalCredential(src *gestalt.ExternalCredential) *gestalt.ExternalCredential {
	if src == nil {
		return nil
	}
	value := *src
	value.ExpiresAt = cloneTime(src.ExpiresAt)
	value.LastRefreshedAt = cloneTime(src.LastRefreshedAt)
	value.CreatedAt = cloneTime(src.CreatedAt)
	value.UpdatedAt = cloneTime(src.UpdatedAt)
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func cloneTime(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func externalCredentialLookupKey(subjectID, connectionID, instance string) string {
	return subjectID + "\x00" + connectionID + "\x00" + instance
}

func externalCredentialHostClient() (*gestalt.ExternalCredentialClient, bool, error) {
	if os.Getenv(gestalt.EnvHostServiceSocket) == "" {
		return nil, false, nil
	}
	client, err := gestalt.ExternalCredentials()
	if err != nil {
		return nil, false, err
	}
	return client, true, nil
}

func externalCredentialHostServiceMissing(err error) bool {
	if status.Code(err) != codes.Unimplemented {
		return false
	}
	return strings.Contains(status.Convert(err).Message(), "unknown service gestalt.provider.v1.ExternalCredentialProvider")
}
`
}

func GeneratedCachePackageSource() string {
	return `package cache

import (
	"context"
	"sync"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

type Provider struct {
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

func (p *Provider) Get(_ context.Context, key string) ([]byte, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	value, ok := p.values[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), value...), true, nil
}

func (p *Provider) GetMany(_ context.Context, keys []string) (map[string][]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries := make(map[string][]byte)
	for _, key := range keys {
		if value, ok := p.values[key]; ok {
			entries[key] = append([]byte(nil), value...)
		}
	}
	return entries, nil
}

func (p *Provider) Set(_ context.Context, key string, value []byte, _ gestalt.CacheSetOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.values[key] = append([]byte(nil), value...)
	return nil
}

func (p *Provider) SetMany(_ context.Context, entries []gestalt.CacheEntry, _ gestalt.CacheSetOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range entries {
		p.values[entry.Key] = append([]byte(nil), entry.Value...)
	}
	return nil
}

func (p *Provider) Delete(_ context.Context, key string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.values[key]
	delete(p.values, key)
	return ok, nil
}

func (p *Provider) DeleteMany(_ context.Context, keys []string) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var deleted int64
	for _, key := range keys {
		if _, ok := p.values[key]; ok {
			delete(p.values, key)
			deleted++
		}
	}
	return deleted, nil
}

func (p *Provider) Touch(_ context.Context, key string, _ time.Duration) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok := p.values[key]
	return ok, nil
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
	source = rewriteModuleLine(
		t,
		source,
		"replace github.com/valon-technologies/gestalt/sdk/go => ",
		"replace github.com/valon-technologies/gestalt/sdk/go => "+SDKGoModulePath(t),
	)
	source = rewriteModuleLine(
		t,
		source,
		"replace github.com/valon-technologies/gestalt/server/rpc => ",
		"replace github.com/valon-technologies/gestalt/server/rpc => "+filepath.ToSlash(filepath.Join(RepoRootPath(t), "gestaltd", "rpc")),
	)
	return source
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
