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

func (p *Provider) CheckAccess(_ context.Context, _ *gestalt.CheckAccessRequest) (*gestalt.CheckAccessResponse, error) {
	return &gestalt.CheckAccessResponse{Allowed: true, ModelId: "model-v1"}, nil
}

func (p *Provider) CheckAccessMany(_ context.Context, req *gestalt.CheckAccessManyRequest) (*gestalt.CheckAccessManyResponse, error) {
	decisions := make([]*gestalt.CheckAccessResponse, 0, len(req.Requests))
	for range req.Requests {
		decisions = append(decisions, &gestalt.CheckAccessResponse{Allowed: true, ModelId: "model-v1"})
	}
	return &gestalt.CheckAccessManyResponse{Decisions: decisions}, nil
}

func (p *Provider) ListRelationships(_ context.Context, _ *gestalt.ListRelationshipsRequest) (*gestalt.ListRelationshipsResponse, error) {
	return &gestalt.ListRelationshipsResponse{
		Relationships: []*gestalt.Relationship{{
			Tuple: &gestalt.RelationshipTuple{
				Target:   &gestalt.RelationshipTarget{Subject: &gestalt.AuthorizationSubject{Type: "user", Id: "generated-user"}},
				Relation: "viewer",
				Resource: &gestalt.AuthorizationResource{Type: "app", Id: "github"},
			},
			SourceLayer: gestalt.SourceLayerRuntime,
		}},
	}, nil
}

func (p *Provider) AddRelationship(_ context.Context, req *gestalt.AddRelationshipRequest) (*gestalt.AddRelationshipResponse, error) {
	return &gestalt.AddRelationshipResponse{Relationship: req.Relationship}, nil
}

func (p *Provider) DeleteRelationship(context.Context, *gestalt.DeleteRelationshipRequest) (*gestalt.DeleteRelationshipResponse, error) {
	return &gestalt.DeleteRelationshipResponse{}, nil
}

func (p *Provider) SetAuthorizationState(_ context.Context, req *gestalt.SetAuthorizationStateRequest) (*gestalt.SetAuthorizationStateResponse, error) {
	return &gestalt.SetAuthorizationStateResponse{
		ActiveModel: &gestalt.AuthorizationModelRef{Id: req.Model.Id, Version: req.Model.Version},
	}, nil
}

func (p *Provider) GetActiveModelRef(context.Context) (*gestalt.GetActiveModelRefResponse, error) {
	return &gestalt.GetActiveModelRefResponse{
		Model: &gestalt.AuthorizationModelRef{Id: "model-v1", Version: "v1"},
	}, nil
}

func (p *Provider) SetActiveModel(_ context.Context, req *gestalt.SetActiveModelRequest) (*gestalt.SetActiveModelResponse, error) {
	return &gestalt.SetActiveModelResponse{Model: &gestalt.AuthorizationModelRef{Id: req.Model.Id, Version: req.Model.Version}}, nil
}

func (p *Provider) ListActiveModelResourceTypes(context.Context, *gestalt.ListActiveModelResourceTypesRequest) (*gestalt.ListActiveModelResourceTypesResponse, error) {
	return &gestalt.ListActiveModelResourceTypesResponse{
		ResourceTypes: []*gestalt.AuthorizationModelResourceType{{
			Name: "app",
			Actions: []*gestalt.ModelAction{{
				Name:      "invoke",
				Relations: []string{"viewer"},
			}},
			SourceLayer:         gestalt.SourceLayerRuntime,
			DefaultAccessPolicy: gestalt.DefaultAccessPolicyDeny,
		}},
		ModelId: "model-v1",
	}, nil
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

func (p *Provider) ApplyDefinition(context.Context, *gestalt.ApplyWorkflowProviderDefinitionRequest) (*gestalt.WorkflowDefinition, error) {
	return &gestalt.WorkflowDefinition{ID: "generated-definition", Generation: 1}, nil
}

func (p *Provider) GetDefinition(context.Context, *gestalt.GetWorkflowProviderDefinitionRequest) (*gestalt.WorkflowDefinition, error) {
	return &gestalt.WorkflowDefinition{ID: "generated-definition", Generation: 1}, nil
}

func (p *Provider) ListDefinitions(context.Context, *gestalt.ListWorkflowProviderDefinitionsRequest) (*gestalt.ListWorkflowProviderDefinitionsResponse, error) {
	return &gestalt.ListWorkflowProviderDefinitionsResponse{}, nil
}

func (p *Provider) SetDefinitionPaused(context.Context, *gestalt.SetWorkflowProviderDefinitionPausedRequest) (*gestalt.WorkflowDefinition, error) {
	return &gestalt.WorkflowDefinition{ID: "generated-definition", Generation: 1, Paused: true}, nil
}

func (p *Provider) SetActivationPaused(context.Context, *gestalt.SetWorkflowProviderActivationPausedRequest) (*gestalt.WorkflowDefinition, error) {
	return &gestalt.WorkflowDefinition{ID: "generated-definition", Generation: 1}, nil
}

func (p *Provider) DeleteDefinition(context.Context, *gestalt.DeleteWorkflowProviderDefinitionRequest) error {
	return nil
}

func (p *Provider) StartRun(context.Context, *gestalt.StartWorkflowProviderRunRequest) (*gestalt.WorkflowRun, error) {
	return &gestalt.WorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValuePending}, nil
}

func (p *Provider) GetRun(context.Context, *gestalt.GetWorkflowProviderRunRequest) (*gestalt.WorkflowRun, error) {
	return &gestalt.WorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValuePending}, nil
}

func (p *Provider) ListRuns(context.Context, *gestalt.ListWorkflowProviderRunsRequest) (*gestalt.ListWorkflowProviderRunsResponse, error) {
	return &gestalt.ListWorkflowProviderRunsResponse{}, nil
}

func (p *Provider) GetRunEvents(context.Context, *gestalt.GetWorkflowProviderRunEventsRequest) (*gestalt.GetWorkflowProviderRunEventsResponse, error) {
	return &gestalt.GetWorkflowProviderRunEventsResponse{}, nil
}

func (p *Provider) GetRunOutput(context.Context, *gestalt.GetWorkflowProviderRunOutputRequest) (*gestalt.GetWorkflowProviderRunOutputResponse, error) {
	return &gestalt.GetWorkflowProviderRunOutputResponse{}, nil
}

func (p *Provider) CancelRun(context.Context, *gestalt.CancelWorkflowProviderRunRequest) (*gestalt.WorkflowRun, error) {
	return &gestalt.WorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValueCanceled}, nil
}

func (p *Provider) SignalRun(_ context.Context, req *gestalt.SignalWorkflowProviderRunRequest) (*gestalt.SignalWorkflowRunResponse, error) {
	return &gestalt.SignalWorkflowRunResponse{Run: &gestalt.WorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValuePending}, Signal: req.Signal}, nil
}

func (p *Provider) SignalOrStartRun(_ context.Context, req *gestalt.SignalOrStartWorkflowProviderRunRequest) (*gestalt.SignalWorkflowRunResponse, error) {
	return &gestalt.SignalWorkflowRunResponse{Run: &gestalt.WorkflowRun{ID: "generated-run", Status: gestalt.WorkflowRunStatusValuePending}, Signal: req.Signal, StartedRun: true, WorkflowKey: req.WorkflowKey}, nil
}

func (p *Provider) DeliverEvent(_ context.Context, req *gestalt.DeliverWorkflowProviderEventRequest) (*gestalt.WorkflowEvent, error) {
	if req.Event != nil {
		return req.Event, nil
	}
	return &gestalt.WorkflowEvent{ID: "generated-event"}, nil
}
`
}

func GeneratedExternalCredentialPackageSource() string {
	return `package externalcredential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	sdkclient "github.com/valon-technologies/gestalt/sdk/go/client"
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

func (p *Provider) CreateCredential(ctx context.Context, req *gestalt.CreateExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return nil, err
	} else if ok {
		resp, err := hostClient.CreateCredentialRaw(ctx, &sdkclient.CreateExternalCredentialRequest{
			Credential: externalCredentialToClient(req.GetCredential()),
		})
		if err != nil {
			return nil, err
		}
		return externalCredentialFromClient(resp), nil
	}
	return p.storeCredential(req.GetCredential(), true)
}

func (p *Provider) UpsertCredential(ctx context.Context, req *gestalt.UpsertExternalCredentialRequest) (*gestalt.ExternalCredential, error) {
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return nil, err
	} else if ok {
		resp, err := hostClient.UpsertCredentialRaw(ctx, &sdkclient.UpsertExternalCredentialRequest{
			Credential: externalCredentialToClient(req.GetCredential()),
		})
		if err != nil {
			return nil, err
		}
		return externalCredentialFromClient(resp), nil
	}
	return p.storeCredential(req.GetCredential(), false)
}

func (p *Provider) storeCredential(credential *gestalt.ExternalCredential, insertOnly bool) (*gestalt.ExternalCredential, error) {
	if credential == nil {
		return nil, fmt.Errorf("credential is required")
	}

	value := cloneExternalCredential(credential)
	key := externalCredentialKey(value.GetSubject(), value.GetAudience(), value.GetQualifier())
	now := time.Now().UTC()

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing := p.credentials[key]; existing != nil {
		if insertOnly {
			return nil, gestalt.ErrAlreadyExists
		}
		value.ID = existing.GetId()
		if value.GetCreatedAt() == nil {
			value.CreatedAt = cloneTime(existing.GetCreatedAt())
		}
	} else {
		if value.GetId() == "" {
			value.ID = "cred-" + value.GetAudience() + "-" + value.GetQualifier()
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
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return nil, err
	} else if ok {
		resp, err := hostClient.GetCredentialRaw(ctx, &sdkclient.GetExternalCredentialRequest{
			Subject:   req.GetSubject(),
			Audience:  req.GetAudience(),
			Qualifier: req.GetQualifier(),
		})
		if externalCredentialHostServiceMissing(err) {
			return nil, gestalt.ErrExternalCredentialNotFound
		}
		if err != nil {
			return nil, err
		}
		return externalCredentialFromClient(resp), nil
	}
	if req == nil || req.GetSubject() == "" {
		return nil, fmt.Errorf("subject is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	value, ok := p.credentials[externalCredentialKey(req.GetSubject(), req.GetAudience(), req.GetQualifier())]
	if !ok {
		return nil, gestalt.ErrExternalCredentialNotFound
	}
	return cloneExternalCredential(value), nil
}

func (p *Provider) ListCredentials(ctx context.Context, req *gestalt.ListExternalCredentialsRequest) (*gestalt.ListExternalCredentialsResponse, error) {
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return nil, err
	} else if ok {
		resp, err := hostClient.ListCredentialsRaw(ctx, &sdkclient.ListExternalCredentialsRequest{
			Subject:  req.GetSubject(),
			Audience: req.GetAudience(),
		})
		if externalCredentialHostServiceMissing(err) {
			return &gestalt.ListExternalCredentialsResponse{}, nil
		}
		if err != nil {
			return nil, err
		}
		out := &gestalt.ListExternalCredentialsResponse{}
		for _, credential := range resp.Credentials {
			out.Credentials = append(out.Credentials, externalCredentialFromClient(credential))
		}
		return out, nil
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	credentials := make([]*gestalt.ExternalCredential, 0, len(p.credentials))
	for _, value := range p.credentials {
		if req.GetSubject() != "" && value.GetSubject() != req.GetSubject() {
			continue
		}
		if req.GetAudience() != "" && value.GetAudience() != req.GetAudience() {
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
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return err
	} else if ok {
		return hostClient.DeleteCredentialRaw(ctx, &sdkclient.DeleteExternalCredentialRequest{Id: req.GetId()})
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
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return err
	} else if ok {
		if err := hostClient.ValidateCredentialConfigRaw(ctx, &sdkclient.ValidateExternalCredentialConfigRequest{
			Provider:         req.GetProvider(),
			Connection:       req.GetConnection(),
			ConnectionId:     req.GetConnectionId(),
			Mode:             req.GetMode(),
			Auth:             externalCredentialAuthConfigToClient(req.GetAuth()),
			ConnectionParams: req.GetConnectionParams(),
		}); err != nil {
			if externalCredentialHostServiceMissing(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (p *Provider) ResolveCredential(ctx context.Context, req *gestalt.ResolveExternalCredentialRequest) (*gestalt.ResolveExternalCredentialResponse, error) {
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return nil, err
	} else if ok {
		resp, err := hostClient.ResolveCredentialRaw(ctx, &sdkclient.ResolveExternalCredentialRequest{
			Provider:            req.GetProvider(),
			Connection:          req.GetConnection(),
			ConnectionId:        req.GetConnectionId(),
			Mode:                req.GetMode(),
			CredentialSubjectId: req.GetCredentialSubjectId(),
			ActorSubjectId:      req.GetActorSubjectId(),
			Instance:            req.GetInstance(),
			Auth:                externalCredentialAuthConfigToClient(req.GetAuth()),
			ConnectionParams:    req.GetConnectionParams(),
		})
		if externalCredentialHostServiceMissing(err) {
			return nil, gestalt.ErrExternalCredentialNotFound
		}
		if err != nil {
			return nil, err
		}
		return &gestalt.ResolveExternalCredentialResponse{
			Token:        resp.Token,
			ExpiresAt:    cloneTime(resp.ExpiresAt),
			MetadataJSON: resp.MetadataJson,
			Params:       resp.Params,
			Credential:   externalCredentialFromClient(resp.Credential),
		}, nil
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var credential *gestalt.ExternalCredential
	for _, value := range p.credentials {
		if value.GetSubject() != req.GetCredentialSubjectId() || value.GetAudience() != req.GetConnectionId() {
			continue
		}
		if req.GetInstance() != "" && value.GetQualifier() != req.GetInstance() {
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
	resp := &gestalt.ResolveExternalCredentialResponse{
		MetadataJSON: credential.GetMetadataJson(),
		Credential:   cloneExternalCredential(credential),
	}
	switch {
	case credential.Grant != nil:
		resp.Token = credential.Grant.GetAccessToken()
		resp.ExpiresAt = cloneTime(credential.Grant.GetExpiresAt())
	case credential.Opaque != nil:
		fields, err := json.Marshal(credential.Opaque.GetFields())
		if err != nil {
			return nil, fmt.Errorf("encode opaque fields: %w", err)
		}
		resp.Token = string(fields)
		resp.Params = credential.Opaque.GetFields()
	}
	return resp, nil
}

func (p *Provider) ExchangeCredential(ctx context.Context, req *gestalt.ExchangeExternalCredentialRequest) (*gestalt.ExchangeExternalCredentialResponse, error) {
	if hostClient, ok, err := externalCredentialHostClient(ctx); err != nil {
		return nil, err
	} else if ok {
		resp, err := hostClient.ExchangeCredentialRaw(ctx, &sdkclient.ExchangeExternalCredentialRequest{
			Provider:            req.GetProvider(),
			Connection:          req.GetConnection(),
			ConnectionId:        req.GetConnectionId(),
			CredentialSubjectId: req.GetCredentialSubjectId(),
			ActorSubjectId:      req.GetActorSubjectId(),
			Instance:            req.GetInstance(),
			Auth:                externalCredentialAuthConfigToClient(req.GetAuth()),
			CredentialJson:      req.GetCredentialJson(),
			ConnectionParams:    req.GetConnectionParams(),
		})
		if err != nil {
			return nil, err
		}
		out := &gestalt.ExchangeExternalCredentialResponse{}
		if resp.TokenResponse != nil {
			out.TokenResponse = &gestalt.ExternalCredentialTokenResponse{
				AccessToken:   resp.TokenResponse.AccessToken,
				RefreshToken:  resp.TokenResponse.RefreshToken,
				ExpiresIn:     resp.TokenResponse.ExpiresIn,
				TokenType:     resp.TokenResponse.TokenType,
				ExtraJSON:     resp.TokenResponse.ExtraJson,
				RefreshSource: resp.TokenResponse.RefreshSource,
			}
		}
		return out, nil
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
	value.CreatedAt = cloneTime(src.CreatedAt)
	value.UpdatedAt = cloneTime(src.UpdatedAt)
	if src.Grant != nil {
		grant := *src.Grant
		grant.ExpiresAt = cloneTime(src.Grant.ExpiresAt)
		grant.LastRefreshedAt = cloneTime(src.Grant.LastRefreshedAt)
		value.Grant = &grant
	}
	if src.Client != nil {
		client := *src.Client
		client.ClientSecretExpiresAt = cloneTime(src.Client.ClientSecretExpiresAt)
		value.Client = &client
	}
	if src.Opaque != nil {
		value.Opaque = &gestalt.ExternalCredentialOpaque{Fields: cloneStringMap(src.Opaque.Fields)}
	}
	return &value
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

func externalCredentialKey(subject, audience, qualifier string) string {
	return subject + "\x00" + audience + "\x00" + qualifier
}

func externalCredentialHostClient(ctx context.Context) (*sdkclient.ExternalCredentials, bool, error) {
	if os.Getenv(gestalt.EnvHostServiceSocket) == "" {
		return nil, false, nil
	}
	hostClient, err := sdkclient.ConnectExternalCredentials(ctx, "")
	if err != nil {
		return nil, false, err
	}
	return hostClient, true, nil
}

func externalCredentialHostServiceMissing(err error) bool {
	var gestaltErr *sdkclient.GestaltError
	if !errors.As(err, &gestaltErr) || gestaltErr.Code != sdkclient.GestaltErrorCodeUnimplemented {
		return false
	}
	return strings.Contains(gestaltErr.Message, "unknown service gestalt.provider.v1.ExternalCredentials")
}

func externalCredentialToClient(value *gestalt.ExternalCredential) *sdkclient.ExternalCredential {
	if value == nil {
		return nil
	}
	out := &sdkclient.ExternalCredential{
		Id:           value.ID,
		Subject:      value.Subject,
		Audience:     value.Audience,
		Qualifier:    value.Qualifier,
		MetadataJson: value.MetadataJSON,
		CreatedAt:    cloneTime(value.CreatedAt),
		UpdatedAt:    cloneTime(value.UpdatedAt),
	}
	switch {
	case value.Grant != nil:
		out.Credential = &sdkclient.ExternalCredentialCredentialGrant{Value: &sdkclient.ExternalCredentialGrant{
			AccessToken:       value.Grant.AccessToken,
			RefreshToken:      value.Grant.RefreshToken,
			Scope:             value.Grant.Scope,
			ExpiresAt:         cloneTime(value.Grant.ExpiresAt),
			LastRefreshedAt:   cloneTime(value.Grant.LastRefreshedAt),
			RefreshErrorCount: value.Grant.RefreshErrorCount,
		}}
	case value.Client != nil:
		out.Credential = &sdkclient.ExternalCredentialCredentialClient{Value: &sdkclient.ExternalCredentialClientInfo{
			ClientId:              value.Client.ClientID,
			ClientSecret:          value.Client.ClientSecret,
			ClientSecretExpiresAt: cloneTime(value.Client.ClientSecretExpiresAt),
		}}
	case value.Opaque != nil:
		out.Credential = &sdkclient.ExternalCredentialCredentialOpaque{Value: &sdkclient.ExternalCredentialOpaque{
			Fields: cloneStringMap(value.Opaque.Fields),
		}}
	}
	return out
}

func externalCredentialFromClient(value *sdkclient.ExternalCredential) *gestalt.ExternalCredential {
	if value == nil {
		return nil
	}
	out := &gestalt.ExternalCredential{
		ID:           value.Id,
		Subject:      value.Subject,
		Audience:     value.Audience,
		Qualifier:    value.Qualifier,
		MetadataJSON: value.MetadataJson,
		CreatedAt:    cloneTime(value.CreatedAt),
		UpdatedAt:    cloneTime(value.UpdatedAt),
	}
	switch credential := value.Credential.(type) {
	case *sdkclient.ExternalCredentialCredentialGrant:
		if credential.Value != nil {
			out.Grant = &gestalt.ExternalCredentialGrant{
				AccessToken:       credential.Value.AccessToken,
				RefreshToken:      credential.Value.RefreshToken,
				Scope:             credential.Value.Scope,
				ExpiresAt:         cloneTime(credential.Value.ExpiresAt),
				LastRefreshedAt:   cloneTime(credential.Value.LastRefreshedAt),
				RefreshErrorCount: credential.Value.RefreshErrorCount,
			}
		}
	case *sdkclient.ExternalCredentialCredentialClient:
		if credential.Value != nil {
			out.Client = &gestalt.ExternalCredentialClientInfo{
				ClientID:              credential.Value.ClientId,
				ClientSecret:          credential.Value.ClientSecret,
				ClientSecretExpiresAt: cloneTime(credential.Value.ClientSecretExpiresAt),
			}
		}
	case *sdkclient.ExternalCredentialCredentialOpaque:
		if credential.Value != nil {
			out.Opaque = &gestalt.ExternalCredentialOpaque{Fields: cloneStringMap(credential.Value.Fields)}
		}
	}
	return out
}

func externalCredentialAuthConfigToClient(auth *gestalt.ExternalCredentialAuthConfig) *sdkclient.ExternalCredentialAuthConfig {
	if auth == nil {
		return nil
	}
	out := &sdkclient.ExternalCredentialAuthConfig{
		Type:            auth.Type,
		Token:           auth.Token,
		TokenPrefix:     auth.TokenPrefix,
		GrantType:       auth.GrantType,
		TokenUrl:        auth.TokenURL,
		ClientId:        auth.ClientID,
		ClientSecret:    auth.ClientSecret,
		ClientAuth:      auth.ClientAuth,
		TokenExchange:   auth.TokenExchange,
		Scopes:          auth.Scopes,
		ScopeParam:      auth.ScopeParam,
		ScopeSeparator:  auth.ScopeSeparator,
		TokenParams:     auth.TokenParams,
		RefreshParams:   auth.RefreshParams,
		AcceptHeader:    auth.AcceptHeader,
		AccessTokenPath: auth.AccessTokenPath,
		RefreshToken:    auth.RefreshToken,
	}
	for _, driver := range auth.TokenExchangeDrivers {
		if driver == nil {
			continue
		}
		out.TokenExchangeDrivers = append(out.TokenExchangeDrivers, &sdkclient.ExternalCredentialTokenExchangeDriver{
			Type:            driver.Type,
			TargetPrincipal: driver.TargetPrincipal,
			Scopes:          driver.Scopes,
			LifetimeSeconds: driver.LifetimeSeconds,
			Endpoint:        driver.Endpoint,
			Params:          driver.Params,
		})
	}
	return out
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
