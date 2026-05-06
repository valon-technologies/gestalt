package bootstrap_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	telemetrynoop "github.com/valon-technologies/gestalt/server/services/observability/drivers/noop"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	graphqlschema "github.com/valon-technologies/gestalt/server/services/plugins/graphql"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

func storeWorkflowExecutionRefForTarget(t *testing.T, deps bootstrap.Deps, providerName string, target coreworkflow.Target) string {
	t.Helper()
	pluginTarget := target.Plugin
	if pluginTarget == nil {
		t.Fatalf("workflow target plugin is nil: %#v", target)
		return ""
	}
	provider, err := deps.WorkflowRuntime.ResolveProvider(providerName)
	if err != nil {
		t.Fatalf("resolve workflow provider %q: %v", providerName, err)
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		t.Fatalf("workflow provider %q does not support execution refs", providerName)
	}
	ref, err := store.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           fmt.Sprintf("test:%s:%s:%s", strings.ReplaceAll(t.Name(), "/", "_"), providerName, pluginTarget.Operation),
		ProviderName: providerName,
		Target:       target,
		SubjectID:    "system:config",
		Permissions: []core.AccessPermission{{
			Plugin:     pluginTarget.PluginName,
			Operations: []string{pluginTarget.Operation},
		}},
	})
	if err != nil {
		t.Fatalf("store workflow execution ref: %v", err)
	}
	return ref.ID
}

func bootstrapGraphQLStringPtr(value string) *string {
	return &value
}

func bootstrapGraphQLSchema() graphqlschema.Schema {
	return graphqlschema.Schema{
		QueryType: &graphqlschema.TypeName{Name: "Query"},
		Types: []graphqlschema.FullType{
			{
				Kind: "OBJECT",
				Name: "Query",
				Fields: []graphqlschema.Field{
					{
						Name: "viewer",
						Args: []graphqlschema.InputValue{
							{Name: "team", Type: graphqlschema.TypeRef{Kind: "NON_NULL", OfType: &graphqlschema.TypeRef{Kind: "SCALAR", Name: bootstrapGraphQLStringPtr("String")}}},
						},
						Type: graphqlschema.TypeRef{Kind: "OBJECT", Name: bootstrapGraphQLStringPtr("Viewer")},
					},
				},
			},
			{
				Kind: "OBJECT",
				Name: "Viewer",
				Fields: []graphqlschema.Field{
					{Name: "id", Type: graphqlschema.TypeRef{Kind: "SCALAR", Name: bootstrapGraphQLStringPtr("ID")}},
					{Name: "name", Type: graphqlschema.TypeRef{Kind: "SCALAR", Name: bootstrapGraphQLStringPtr("String")}},
				},
			},
		},
	}
}

func startBootstrapGraphQLIntrospectionServer(t *testing.T) *httptest.Server {
	t.Helper()

	schema := bootstrapGraphQLSchema()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"__schema": schema,
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stubAuthFactory(name string) bootstrap.AuthFactory {
	return func(yaml.Node, bootstrap.Deps) (core.AuthenticationProvider, error) {
		return &coretesting.StubAuthProvider{N: name}, nil
	}
}

type stubAuthorizationProvider struct {
	name string
}

func (p *stubAuthorizationProvider) Name() string { return p.name }
func (p *stubAuthorizationProvider) Evaluate(context.Context, *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	return &core.AccessDecision{}, nil
}
func (p *stubAuthorizationProvider) EvaluateMany(context.Context, *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	return &core.AccessEvaluationsResponse{}, nil
}
func (p *stubAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return &core.ResourceSearchResponse{}, nil
}
func (p *stubAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}
func (p *stubAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}
func (p *stubAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}
func (p *stubAuthorizationProvider) ReadRelationships(context.Context, *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	return &core.ReadRelationshipsResponse{}, nil
}
func (p *stubAuthorizationProvider) WriteRelationships(context.Context, *core.WriteRelationshipsRequest) error {
	return nil
}
func (p *stubAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{}, nil
}
func (p *stubAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}
func (p *stubAuthorizationProvider) WriteModel(context.Context, *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	return &core.AuthorizationModelRef{}, nil
}

func stubAuthorizationFactory(name string) bootstrap.AuthorizationFactory {
	return func(yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
		return &stubAuthorizationProvider{name: name}, nil
	}
}

type memoryAuthorizationProvider struct {
	name string

	mu            sync.Mutex
	activeModelID string
	models        []*core.AuthorizationModelRef
	relsByModel   map[string]map[string]*core.Relationship
}

func newMemoryAuthorizationProvider(name string) *memoryAuthorizationProvider {
	return &memoryAuthorizationProvider{
		name:        name,
		relsByModel: map[string]map[string]*core.Relationship{},
	}
}

func (p *memoryAuthorizationProvider) Name() string { return p.name }

func (p *memoryAuthorizationProvider) Evaluate(ctx context.Context, req *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	resp, err := p.EvaluateMany(ctx, &core.AccessEvaluationsRequest{Requests: []*core.AccessEvaluationRequest{req}})
	if err != nil {
		return nil, err
	}
	if len(resp.Decisions) == 0 {
		return &core.AccessDecision{}, nil
	}
	return resp.Decisions[0], nil
}

func (p *memoryAuthorizationProvider) EvaluateMany(_ context.Context, req *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	resp := &core.AccessEvaluationsResponse{
		Decisions: make([]*core.AccessDecision, 0, len(req.GetRequests())),
	}
	rels := p.relsByModel[p.activeModelID]
	for _, item := range req.GetRequests() {
		allowed := false
		if item != nil && rels != nil {
			_, allowed = rels[bootstrapRelationshipKey(item.GetSubject(), item.GetAction().GetName(), item.GetResource())]
		}
		resp.Decisions = append(resp.Decisions, &core.AccessDecision{
			Allowed: allowed,
			ModelId: p.activeModelID,
		})
	}
	return resp, nil
}

func (p *memoryAuthorizationProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return &core.ResourceSearchResponse{}, nil
}

func (p *memoryAuthorizationProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return &core.SubjectSearchResponse{}, nil
}

func (p *memoryAuthorizationProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return &core.ActionSearchResponse{}, nil
}

func (p *memoryAuthorizationProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &core.AuthorizationMetadata{ActiveModelId: p.activeModelID}, nil
}

func (p *memoryAuthorizationProvider) ReadRelationships(_ context.Context, req *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	modelID := strings.TrimSpace(req.GetModelId())
	if modelID == "" {
		modelID = p.activeModelID
	}
	out := make([]*core.Relationship, 0, len(p.relsByModel[modelID]))
	for _, rel := range p.relsByModel[modelID] {
		out = append(out, cloneRelationship(rel))
	}
	return &core.ReadRelationshipsResponse{
		Relationships: out,
		ModelId:       modelID,
	}, nil
}

func (p *memoryAuthorizationProvider) WriteRelationships(_ context.Context, req *core.WriteRelationshipsRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	modelID := req.GetModelId()
	if modelID == "" {
		modelID = p.activeModelID
	}
	rels := p.relsByModel[modelID]
	if rels == nil {
		rels = map[string]*core.Relationship{}
		p.relsByModel[modelID] = rels
	}
	for _, key := range req.GetDeletes() {
		delete(rels, bootstrapRelationshipKey(key.GetSubject(), key.GetRelation(), key.GetResource()))
	}
	for _, rel := range req.GetWrites() {
		rels[bootstrapRelationshipKey(rel.GetSubject(), rel.GetRelation(), rel.GetResource())] = cloneRelationship(rel)
	}
	return nil
}

func (p *memoryAuthorizationProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, model := range p.models {
		if model.GetId() == p.activeModelID {
			return &core.GetActiveModelResponse{Model: model}, nil
		}
	}
	return &core.GetActiveModelResponse{}, nil
}

func (p *memoryAuthorizationProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &core.ListModelsResponse{Models: append([]*core.AuthorizationModelRef(nil), p.models...)}, nil
}

func (p *memoryAuthorizationProvider) WriteModel(_ context.Context, req *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	definition := req.GetModel()
	if definition == nil {
		return nil, fmt.Errorf("model is required")
	}
	modelVersion := definition.GetVersion()
	if modelVersion == 0 {
		modelVersion = 1
	}
	modelBytes, err := gproto.MarshalOptions{Deterministic: true}.Marshal(definition)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(modelBytes)
	modelID := "model-" + hex.EncodeToString(sum[:])
	for _, existing := range p.models {
		if existing.GetId() == modelID {
			p.activeModelID = modelID
			if p.relsByModel[modelID] == nil {
				p.relsByModel[modelID] = map[string]*core.Relationship{}
			}
			return existing, nil
		}
	}
	model := &core.AuthorizationModelRef{
		Id:      modelID,
		Version: fmt.Sprintf("%d", modelVersion),
	}
	p.models = append(p.models, model)
	p.activeModelID = model.GetId()
	if p.relsByModel[model.GetId()] == nil {
		p.relsByModel[model.GetId()] = map[string]*core.Relationship{}
	}
	return model, nil
}

func memoryAuthorizationFactory(provider *memoryAuthorizationProvider) bootstrap.AuthorizationFactory {
	return func(yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
		return provider, nil
	}
}

func writeMemoryAuthorizationModel(t *testing.T, provider *memoryAuthorizationProvider, model *core.AuthorizationModel) string {
	t.Helper()
	ref, err := provider.WriteModel(context.Background(), &core.WriteModelRequest{Model: model})
	if err != nil {
		t.Fatalf("WriteModel: %v", err)
	}
	return ref.GetId()
}

func bootstrapRelationshipKey(subject *core.SubjectRef, relation string, resource *core.ResourceRef) string {
	return strings.Join([]string{
		subject.GetType(),
		subject.GetId(),
		relation,
		resource.GetType(),
		resource.GetId(),
	}, "\x00")
}

func (p *memoryAuthorizationProvider) putRelationship(modelID string, rel *core.Relationship) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.relsByModel[modelID] == nil {
		p.relsByModel[modelID] = map[string]*core.Relationship{}
	}
	p.relsByModel[modelID][bootstrapRelationshipKey(rel.GetSubject(), rel.GetRelation(), rel.GetResource())] = cloneRelationship(rel)
}

func cloneRelationship(rel *core.Relationship) *core.Relationship {
	if rel == nil {
		return nil
	}
	return &core.Relationship{
		Subject: &core.SubjectRef{
			Type: rel.GetSubject().GetType(),
			Id:   rel.GetSubject().GetId(),
		},
		Relation: rel.GetRelation(),
		Resource: &core.ResourceRef{
			Type: rel.GetResource().GetType(),
			Id:   rel.GetResource().GetId(),
		},
	}
}

func stubSecretManagerFactory() bootstrap.SecretManagerFactory {
	return func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{}, nil
	}
}

func stubTelemetryFactory() bootstrap.TelemetryFactory {
	return func(yaml.Node) (core.TelemetryProvider, error) {
		return telemetrynoop.New(), nil
	}
}

type closableAuthProvider struct {
	*coretesting.StubAuthProvider
	closed *atomic.Bool
}

func (p *closableAuthProvider) Close() error {
	p.closed.Store(true)
	return nil
}

type closableAuthorizationProvider struct {
	*stubAuthorizationProvider
	closed *atomic.Bool
}

func (p *closableAuthorizationProvider) Close() error {
	p.closed.Store(true)
	return nil
}

type closableExternalCredentialProvider struct {
	closed *atomic.Int32
}

func (*closableExternalCredentialProvider) PutCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*closableExternalCredentialProvider) RestoreCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*closableExternalCredentialProvider) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	return nil, core.ErrNotFound
}

func (*closableExternalCredentialProvider) ListCredentials(context.Context, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*closableExternalCredentialProvider) ListCredentialsForConnection(context.Context, string, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*closableExternalCredentialProvider) DeleteCredential(context.Context, string) error {
	return nil
}

func (*closableExternalCredentialProvider) ValidateCredentialConfig(context.Context, *core.ValidateExternalCredentialConfigRequest) error {
	return nil
}

func (*closableExternalCredentialProvider) ResolveCredential(context.Context, *core.ResolveExternalCredentialRequest) (*core.ResolveExternalCredentialResponse, error) {
	return nil, core.ErrNotFound
}

func (*closableExternalCredentialProvider) ExchangeCredential(context.Context, *core.ExchangeExternalCredentialRequest) (*core.ExchangeExternalCredentialResponse, error) {
	return &core.ExchangeExternalCredentialResponse{}, nil
}

func (p *closableExternalCredentialProvider) Close() error {
	if p != nil && p.closed != nil {
		p.closed.Add(1)
	}
	return nil
}

func stubIndexedDBFactory() bootstrap.IndexedDBFactory {
	return func(yaml.Node) (indexeddb.IndexedDB, error) {
		return &coretesting.StubIndexedDB{}, nil
	}
}

type stubWorkflowProvider struct{}

func (s *stubWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (s *stubWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (s *stubWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}
func (s *stubWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (s *stubWorkflowProvider) SignalRun(context.Context, coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}
func (s *stubWorkflowProvider) SignalOrStartRun(context.Context, coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}
func (s *stubWorkflowProvider) UpsertSchedule(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (s *stubWorkflowProvider) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (s *stubWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, nil
}
func (s *stubWorkflowProvider) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return nil
}
func (s *stubWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (s *stubWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (s *stubWorkflowProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (s *stubWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (s *stubWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (s *stubWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return nil
}
func (s *stubWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (s *stubWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (s *stubWorkflowProvider) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}
func (s *stubWorkflowProvider) Ping(context.Context) error { return nil }
func (s *stubWorkflowProvider) Close() error               { return nil }

type recordingAgentProvider struct {
	coreagent.UnimplementedProvider
	mu                         sync.Mutex
	createSessionRequests      []coreagent.CreateSessionRequest
	updateSessionRequests      []coreagent.UpdateSessionRequest
	createTurnRequests         []coreagent.CreateTurnRequest
	cancelTurnRequests         []coreagent.CancelTurnRequest
	resolveInteractionRequests []coreagent.ResolveInteractionRequest
	sessions                   map[string]*coreagent.Session
	turns                      map[string]*coreagent.Turn
	turnEvents                 map[string][]*coreagent.TurnEvent
	interactions               map[string]*coreagent.Interaction
	sessionIdempotency         map[string]string
	turnIdempotency            map[string]string
	cancelTurnStatus           coreagent.ExecutionStatus
}

func newRecordingAgentProvider() *recordingAgentProvider {
	return &recordingAgentProvider{
		sessions:           map[string]*coreagent.Session{},
		turns:              map[string]*coreagent.Turn{},
		turnEvents:         map[string][]*coreagent.TurnEvent{},
		interactions:       map[string]*coreagent.Interaction{},
		sessionIdempotency: map[string]string{},
		turnIdempotency:    map[string]string{},
	}
}

func (p *recordingAgentProvider) ensureStateLocked() {
	if p.sessions == nil {
		p.sessions = map[string]*coreagent.Session{}
	}
	if p.turns == nil {
		p.turns = map[string]*coreagent.Turn{}
	}
	if p.turnEvents == nil {
		p.turnEvents = map[string][]*coreagent.TurnEvent{}
	}
	if p.interactions == nil {
		p.interactions = map[string]*coreagent.Interaction{}
	}
	if p.sessionIdempotency == nil {
		p.sessionIdempotency = map[string]string{}
	}
	if p.turnIdempotency == nil {
		p.turnIdempotency = map[string]string{}
	}
}

func agentProviderSessionIdempotencyScope(subject coreagent.SubjectContext, actor coreagent.Actor, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{"session", agentProviderSubjectScope(subject, actor), idempotencyKey}, "\x00")
}

func agentProviderTurnIdempotencyScope(subject coreagent.SubjectContext, actor coreagent.Actor, sessionID, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{"turn", agentProviderSubjectScope(subject, actor), strings.TrimSpace(sessionID), idempotencyKey}, "\x00")
}

func agentProviderSubjectScope(subject coreagent.SubjectContext, actor coreagent.Actor) string {
	if subjectID := strings.TrimSpace(subject.SubjectID); subjectID != "" {
		return subjectID
	}
	return strings.TrimSpace(actor.SubjectID)
}

func turnStatusIsTerminalForTest(status coreagent.ExecutionStatus) bool {
	switch status {
	case coreagent.ExecutionStatusSucceeded, coreagent.ExecutionStatusFailed, coreagent.ExecutionStatusCanceled:
		return true
	default:
		return false
	}
}

func (p *recordingAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	idempotencyScope := agentProviderSessionIdempotencyScope(req.Subject, req.CreatedBy, req.IdempotencyKey)
	if sessionID, ok := p.sessionIdempotency[idempotencyScope]; idempotencyScope != "" && ok {
		session, ok := p.sessions[sessionID]
		if !ok {
			return nil, core.ErrNotFound
		}
		return cloneBootstrapAgentSession(session), nil
	}
	p.createSessionRequests = append(p.createSessionRequests, req)
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", len(p.sessions)+1)
	}
	now := time.Now().UTC().Truncate(time.Second)
	session := &coreagent.Session{
		ID:         sessionID,
		Model:      strings.TrimSpace(req.Model),
		ClientRef:  strings.TrimSpace(req.ClientRef),
		State:      coreagent.SessionStateActive,
		Metadata:   maps.Clone(req.Metadata),
		CreatedBy:  req.CreatedBy,
		CreatedAt:  &now,
		UpdatedAt:  &now,
		LastTurnAt: nil,
	}
	p.sessions[sessionID] = cloneBootstrapAgentSession(session)
	if idempotencyScope != "" {
		p.sessionIdempotency[idempotencyScope] = sessionID
	}
	return cloneBootstrapAgentSession(session), nil
}

func (p *recordingAgentProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[strings.TrimSpace(req.SessionID)]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneBootstrapAgentSession(session), nil
}

func (p *recordingAgentProvider) ListSessions(context.Context, coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Session, 0, len(p.sessions))
	for _, session := range p.sessions {
		out = append(out, cloneBootstrapAgentSession(session))
	}
	return out, nil
}

func (p *recordingAgentProvider) UpdateSession(_ context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	p.updateSessionRequests = append(p.updateSessionRequests, req)
	session, ok := p.sessions[strings.TrimSpace(req.SessionID)]
	if !ok {
		return nil, core.ErrNotFound
	}
	if clientRef := strings.TrimSpace(req.ClientRef); clientRef != "" {
		session.ClientRef = clientRef
	}
	if req.State != "" {
		session.State = req.State
	}
	if req.Metadata != nil {
		session.Metadata = maps.Clone(req.Metadata)
	}
	now := time.Now().UTC().Truncate(time.Second)
	session.UpdatedAt = &now
	return cloneBootstrapAgentSession(session), nil
}

func (p *recordingAgentProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	idempotencyScope := agentProviderTurnIdempotencyScope(req.Subject, req.CreatedBy, req.SessionID, req.IdempotencyKey)
	if turnID, ok := p.turnIdempotency[idempotencyScope]; idempotencyScope != "" && ok {
		turn, ok := p.turns[turnID]
		if !ok {
			return nil, core.ErrNotFound
		}
		return cloneBootstrapAgentTurn(turn), nil
	}
	p.createTurnRequests = append(p.createTurnRequests, req)
	turnID := strings.TrimSpace(req.TurnID)
	if turnID == "" {
		turnID = fmt.Sprintf("turn-%d", len(p.turns)+1)
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn := &coreagent.Turn{
		ID:           turnID,
		SessionID:    strings.TrimSpace(req.SessionID),
		Model:        strings.TrimSpace(req.Model),
		Status:       coreagent.ExecutionStatusSucceeded,
		Messages:     cloneBootstrapAgentMessages(req.Messages),
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		CompletedAt:  &now,
		ExecutionRef: strings.TrimSpace(req.ExecutionRef),
	}
	p.turns[turnID] = cloneBootstrapAgentTurn(turn)
	if session := p.sessions[turn.SessionID]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	if idempotencyScope != "" {
		p.turnIdempotency[idempotencyScope] = turnID
	}
	return cloneBootstrapAgentTurn(turn), nil
}

func (p *recordingAgentProvider) GetTurn(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[strings.TrimSpace(req.TurnID)]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneBootstrapAgentTurn(turn), nil
}

func (p *recordingAgentProvider) ListTurns(_ context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := strings.TrimSpace(req.SessionID)
	out := make([]*coreagent.Turn, 0, len(p.turns))
	for _, turn := range p.turns {
		if sessionID != "" && strings.TrimSpace(turn.SessionID) != sessionID {
			continue
		}
		out = append(out, cloneBootstrapAgentTurn(turn))
	}
	return out, nil
}

func (p *recordingAgentProvider) CancelTurn(_ context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	p.cancelTurnRequests = append(p.cancelTurnRequests, req)
	turnID := strings.TrimSpace(req.TurnID)
	turn, ok := p.turns[turnID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	status := p.cancelTurnStatus
	if status == "" {
		status = coreagent.ExecutionStatusCanceled
	}
	turn.Status = status
	turn.StatusMessage = strings.TrimSpace(req.Reason)
	if turnStatusIsTerminalForTest(status) {
		turn.CompletedAt = &now
	} else {
		turn.CompletedAt = nil
	}
	return cloneBootstrapAgentTurn(turn), nil
}

func (p *recordingAgentProvider) ListTurnEvents(_ context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turnEvents[strings.TrimSpace(req.TurnID)]
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event.Seq <= req.AfterSeq {
			continue
		}
		out = append(out, cloneBootstrapAgentTurnEvent(event))
		if req.Limit > 0 && len(out) >= req.Limit {
			break
		}
	}
	return out, nil
}

func (p *recordingAgentProvider) GetInteraction(_ context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[strings.TrimSpace(req.InteractionID)]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneBootstrapAgentInteraction(interaction), nil
}

func (p *recordingAgentProvider) ListInteractions(_ context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnID := strings.TrimSpace(req.TurnID)
	out := make([]*coreagent.Interaction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if turnID != "" && strings.TrimSpace(interaction.TurnID) != turnID {
			continue
		}
		out = append(out, cloneBootstrapAgentInteraction(interaction))
	}
	return out, nil
}

func (p *recordingAgentProvider) ResolveInteraction(_ context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolveInteractionRequests = append(p.resolveInteractionRequests, req)
	interaction, ok := p.interactions[strings.TrimSpace(req.InteractionID)]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = maps.Clone(req.Resolution)
	interaction.ResolvedAt = &now
	return cloneBootstrapAgentInteraction(interaction), nil
}

func (p *recordingAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
		StructuredOutput:     true,
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}, nil
}

func (p *recordingAgentProvider) appendTurnEventLocked(turnID, eventType string, data map[string]any) {
	now := time.Now().UTC().Truncate(time.Second)
	events := p.turnEvents[turnID]
	p.turnEvents[turnID] = append(events, &coreagent.TurnEvent{
		ID:         fmt.Sprintf("%s-event-%d", turnID, len(events)+1),
		TurnID:     turnID,
		Seq:        int64(len(events) + 1),
		Type:       eventType,
		Source:     "managed",
		Visibility: "private",
		Data:       maps.Clone(data),
		CreatedAt:  &now,
	})
}

func (p *recordingAgentProvider) Ping(context.Context) error { return nil }
func (p *recordingAgentProvider) Close() error               { return nil }

func (p *recordingAgentProvider) CancelTurnRequests() []coreagent.CancelTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]coreagent.CancelTurnRequest(nil), p.cancelTurnRequests...)
}

type callbackAgentProvider struct {
	*recordingAgentProvider
	started                *runtimehost.StartedHostServices
	socketPath             string
	listRequests           []*proto.ListAgentToolsRequest
	listResponses          []*proto.ListAgentToolsResponse
	toolBodies             []string
	resolveInteractionHook func(context.Context, coreagent.ResolveInteractionRequest) error
}

type callbackSessionCatalogIntegration struct {
	coretesting.StubIntegration
	sessionCatalog *catalog.Catalog
}

func (p *callbackSessionCatalogIntegration) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return p.sessionCatalog, nil
}

type unavailableSessionCatalogIntegration struct {
	coretesting.StubIntegration
	err error
}

func (p *unavailableSessionCatalogIntegration) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return nil, p.err
}

func newCallbackAgentProvider(started *runtimehost.StartedHostServices) (*callbackAgentProvider, error) {
	if started == nil {
		return nil, fmt.Errorf("started host services are required")
	}
	var socketPath string
	for _, binding := range started.Bindings() {
		if binding.EnvVar == agentservice.DefaultHostSocketEnv {
			socketPath = binding.SocketPath
			break
		}
	}
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("agent host socket binding is missing")
	}
	return &callbackAgentProvider{
		recordingAgentProvider: newRecordingAgentProvider(),
		started:                started,
		socketPath:             socketPath,
	}, nil
}

func (p *callbackAgentProvider) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	p.ensureStateLocked()
	idempotencyScope := agentProviderTurnIdempotencyScope(req.Subject, req.CreatedBy, req.SessionID, req.IdempotencyKey)
	if turnID, ok := p.turnIdempotency[idempotencyScope]; idempotencyScope != "" && ok {
		turn, ok := p.turns[turnID]
		p.mu.Unlock()
		if !ok {
			return nil, core.ErrNotFound
		}
		return cloneBootstrapAgentTurn(turn), nil
	}
	p.createTurnRequests = append(p.createTurnRequests, req)
	turnID := strings.TrimSpace(req.TurnID)
	if turnID == "" {
		turnID = fmt.Sprintf("turn-%d", len(p.turns)+1)
	}
	needsInteraction, _ := req.Metadata["requireInteraction"].(bool)
	if !needsInteraction {
		for _, message := range req.Messages {
			if strings.TrimSpace(message.Text) == "request approval" {
				needsInteraction = true
				break
			}
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn := &coreagent.Turn{
		ID:           turnID,
		SessionID:    strings.TrimSpace(req.SessionID),
		Model:        strings.TrimSpace(req.Model),
		Status:       coreagent.ExecutionStatusRunning,
		Messages:     cloneBootstrapAgentMessages(req.Messages),
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		ExecutionRef: strings.TrimSpace(req.ExecutionRef),
	}
	p.appendTurnEventLocked(turn.ID, "turn.started", map[string]any{"session_id": turn.SessionID})
	p.turns[turn.ID] = cloneBootstrapAgentTurn(turn)
	if session := p.sessions[turn.SessionID]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	if idempotencyScope != "" {
		p.turnIdempotency[idempotencyScope] = turn.ID
	}
	p.mu.Unlock()
	cleanupPendingTurn := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.turns, turnID)
		if idempotencyScope != "" {
			delete(p.turnIdempotency, idempotencyScope)
		}
	}

	outputBody := ""
	if req.ToolSource == coreagent.ToolSourceModeMCPCatalog || len(req.Tools) > 0 {
		conn, err := grpc.NewClient(
			"passthrough:///localhost",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", p.socketPath)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			cleanupPendingTurn()
			return nil, fmt.Errorf("dial agent host: %w", err)
		}
		defer func() { _ = conn.Close() }()
		client := proto.NewAgentHostClient(conn)
		tools := append([]coreagent.Tool(nil), req.Tools...)
		if len(tools) == 0 && req.ToolSource == coreagent.ToolSourceModeMCPCatalog {
			listReq := &proto.ListAgentToolsRequest{
				SessionId: req.SessionID,
				TurnId:    turnID,
				PageSize:  5,
				RunGrant:  req.RunGrant,
			}
			listResp, err := client.ListTools(ctx, listReq)
			if err != nil {
				cleanupPendingTurn()
				return nil, err
			}
			p.listRequests = append(p.listRequests, gproto.Clone(listReq).(*proto.ListAgentToolsRequest))
			p.listResponses = append(p.listResponses, gproto.Clone(listResp).(*proto.ListAgentToolsResponse))
			for _, tool := range listResp.GetTools() {
				tools = append(tools, coreagent.Tool{
					ID:          tool.GetId(),
					Name:        tool.GetMcpName(),
					Description: tool.GetDescription(),
				})
			}
		}
		if len(tools) > 0 {
			resp, err := client.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
				SessionId:  req.SessionID,
				TurnId:     turnID,
				ToolCallId: "tool-call-1",
				ToolId:     tools[0].ID,
				RunGrant:   req.RunGrant,
				Arguments: func() *structpb.Struct {
					value, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
					if err != nil {
						panic(err)
					}
					return value
				}(),
			})
			if err != nil {
				cleanupPendingTurn()
				return nil, err
			}
			outputBody = resp.GetBody()
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	turn = p.turns[turnID]
	if turn == nil {
		return nil, core.ErrNotFound
	}
	turn.OutputText = outputBody
	turn.Status = coreagent.ExecutionStatusSucceeded
	turn.CompletedAt = &now
	if outputBody != "" {
		p.toolBodies = append(p.toolBodies, outputBody)
	}
	if needsInteraction {
		turn.Status = coreagent.ExecutionStatusWaitingForInput
		turn.StatusMessage = "waiting for input"
		turn.CompletedAt = nil
		interactionID := "interaction-" + turn.ID
		p.interactions[interactionID] = &coreagent.Interaction{
			ID:        interactionID,
			TurnID:    turn.ID,
			SessionID: turn.SessionID,
			Type:      coreagent.InteractionTypeApproval,
			State:     coreagent.InteractionStatePending,
			Title:     "Approve response",
			Prompt:    "Allow this turn to continue?",
			Request:   map[string]any{"approved": true},
			CreatedAt: &now,
		}
		p.appendTurnEventLocked(turn.ID, "interaction.requested", map[string]any{"interaction_id": interactionID})
	} else {
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}
	p.turns[turn.ID] = cloneBootstrapAgentTurn(turn)
	if session := p.sessions[turn.SessionID]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	return cloneBootstrapAgentTurn(turn), nil
}

func (p *callbackAgentProvider) ResolveInteraction(ctx context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	if p.resolveInteractionHook != nil {
		if err := p.resolveInteractionHook(ctx, req); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolveInteractionRequests = append(p.resolveInteractionRequests, req)
	interaction, ok := p.interactions[strings.TrimSpace(req.InteractionID)]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = maps.Clone(req.Resolution)
	interaction.ResolvedAt = &now
	if turn := p.turns[interaction.TurnID]; turn != nil {
		turn.Status = coreagent.ExecutionStatusSucceeded
		turn.StatusMessage = interaction.ID
		turn.CompletedAt = &now
		p.appendTurnEventLocked(turn.ID, "interaction.resolved", map[string]any{"interaction_id": interaction.ID})
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}
	return cloneBootstrapAgentInteraction(interaction), nil
}

func (p *callbackAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
		StructuredOutput:     true,
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}, nil
}

func (p *callbackAgentProvider) Ping(context.Context) error { return nil }

func (p *callbackAgentProvider) Close() error {
	if p == nil || p.started == nil {
		return nil
	}
	return p.started.Close()
}

type generatedIDAgentProvider struct {
	coreagent.UnimplementedProvider
	mu             sync.Mutex
	cancelRequests []coreagent.CancelTurnRequest
}

func (p *generatedIDAgentProvider) CreateSession(context.Context, coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	return &coreagent.Session{ID: "generated-session-1", State: coreagent.SessionStateActive}, nil
}

func (p *generatedIDAgentProvider) GetSession(context.Context, coreagent.GetSessionRequest) (*coreagent.Session, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) ListSessions(context.Context, coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) UpdateSession(context.Context, coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	return &coreagent.Turn{ID: "generated-turn-1", SessionID: req.SessionID, Status: coreagent.ExecutionStatusRunning}, nil
}

func (p *generatedIDAgentProvider) GetTurn(context.Context, coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) ListTurns(context.Context, coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) ListTurnEvents(context.Context, coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) GetInteraction(context.Context, coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) ListInteractions(context.Context, coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) ResolveInteraction(context.Context, coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) CancelTurn(_ context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelRequests = append(p.cancelRequests, req)
	return &coreagent.Turn{ID: req.TurnID, Status: coreagent.ExecutionStatusCanceled}, nil
}

func (p *generatedIDAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		Interactions:         true,
		ResumableTurns:       true,
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{coreagent.ToolSourceModeMCPCatalog},
	}, nil
}

func (p *generatedIDAgentProvider) Ping(context.Context) error { return nil }
func (p *generatedIDAgentProvider) Close() error               { return nil }

func (p *generatedIDAgentProvider) CancelTurnRequests() []coreagent.CancelTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]coreagent.CancelTurnRequest(nil), p.cancelRequests...)
}

func cloneBootstrapAgentSession(src *coreagent.Session) *coreagent.Session {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Metadata = maps.Clone(src.Metadata)
	return &dst
}

func cloneBootstrapAgentTurn(src *coreagent.Turn) *coreagent.Turn {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Messages = cloneBootstrapAgentMessages(src.Messages)
	dst.StructuredOutput = maps.Clone(src.StructuredOutput)
	return &dst
}

func cloneBootstrapAgentTurnEvent(src *coreagent.TurnEvent) *coreagent.TurnEvent {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Data = maps.Clone(src.Data)
	return &dst
}

func cloneBootstrapAgentInteraction(src *coreagent.Interaction) *coreagent.Interaction {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Request = maps.Clone(src.Request)
	dst.Resolution = maps.Clone(src.Resolution)
	return &dst
}

func cloneBootstrapAgentMessages(src []coreagent.Message) []coreagent.Message {
	if len(src) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(src))
	for _, message := range src {
		cloned := message
		cloned.Metadata = maps.Clone(message.Metadata)
		if len(message.Parts) > 0 {
			cloned.Parts = make([]coreagent.MessagePart, 0, len(message.Parts))
			for _, part := range message.Parts {
				partCopy := part
				partCopy.JSON = maps.Clone(part.JSON)
				if part.ToolCall != nil {
					value := *part.ToolCall
					value.Arguments = maps.Clone(part.ToolCall.Arguments)
					partCopy.ToolCall = &value
				}
				if part.ToolResult != nil {
					value := *part.ToolResult
					value.Output = maps.Clone(part.ToolResult.Output)
					partCopy.ToolResult = &value
				}
				cloned.Parts = append(cloned.Parts, partCopy)
			}
		}
		out = append(out, cloned)
	}
	return out
}

type recordingWorkflowProvider struct {
	upsertedSchedules          []coreworkflow.UpsertScheduleRequest
	listedSchedules            []*coreworkflow.Schedule
	listSchedulesErr           error
	deletedSchedules           []coreworkflow.DeleteScheduleRequest
	deleteScheduleErr          error
	getSchedule                *coreworkflow.Schedule
	getScheduleErr             error
	schedules                  map[string]*coreworkflow.Schedule
	omitScheduleExecutionRef   bool
	upsertedEventTriggers      []coreworkflow.UpsertEventTriggerRequest
	listedEventTriggers        []*coreworkflow.EventTrigger
	listEventTriggersErr       error
	deletedEventTriggers       []coreworkflow.DeleteEventTriggerRequest
	deleteEventTriggerErr      error
	getEventTrigger            *coreworkflow.EventTrigger
	getEventTriggerErr         error
	eventTriggers              map[string]*coreworkflow.EventTrigger
	executionRefs              map[string]*coreworkflow.ExecutionReference
	getExecutionReferenceErrs  map[string]error
	putExecutionReferenceErr   error
	deleteMissingNotFound      bool
	deleteEventMissingNotFound bool
	closed                     *atomic.Bool
}

func (p *recordingWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *recordingWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *recordingWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}
func (p *recordingWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *recordingWorkflowProvider) SignalRun(context.Context, coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}
func (p *recordingWorkflowProvider) SignalOrStartRun(context.Context, coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}
func (p *recordingWorkflowProvider) UpsertSchedule(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, req)
	schedule := &coreworkflow.Schedule{
		ID:           req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.RequestedBy,
	}
	if p.schedules == nil {
		p.schedules = map[string]*coreworkflow.Schedule{}
	}
	p.schedules[req.ScheduleID] = schedule
	return schedule, nil
}
func (p *recordingWorkflowProvider) GetSchedule(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.getSchedule != nil || p.getScheduleErr != nil {
		return p.scheduleGetResponse(p.getSchedule), p.getScheduleErr
	}
	if p.schedules != nil {
		if schedule, ok := p.schedules[req.ScheduleID]; ok {
			return p.scheduleGetResponse(schedule), nil
		}
	}
	return nil, core.ErrNotFound
}
func (p *recordingWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	if p.listSchedulesErr != nil {
		return nil, p.listSchedulesErr
	}
	if p.listedSchedules != nil {
		return append([]*coreworkflow.Schedule(nil), p.listedSchedules...), nil
	}
	out := make([]*coreworkflow.Schedule, 0, len(p.schedules))
	for _, schedule := range p.schedules {
		out = append(out, p.scheduleGetResponse(schedule))
	}
	return out, nil
}
func (p *recordingWorkflowProvider) DeleteSchedule(_ context.Context, req coreworkflow.DeleteScheduleRequest) error {
	p.deletedSchedules = append(p.deletedSchedules, req)
	if p.deleteScheduleErr != nil {
		return p.deleteScheduleErr
	}
	if p.schedules != nil {
		if _, ok := p.schedules[req.ScheduleID]; ok {
			delete(p.schedules, req.ScheduleID)
			return nil
		}
	}
	if p.deleteMissingNotFound {
		return core.ErrNotFound
	}
	if p.schedules != nil {
		delete(p.schedules, req.ScheduleID)
	}
	return nil
}
func (p *recordingWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (p *recordingWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}
func (p *recordingWorkflowProvider) UpsertEventTrigger(_ context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	p.upsertedEventTriggers = append(p.upsertedEventTriggers, req)
	trigger := &coreworkflow.EventTrigger{
		ID:           req.TriggerID,
		Match:        req.Match,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.RequestedBy,
	}
	if p.eventTriggers == nil {
		p.eventTriggers = map[string]*coreworkflow.EventTrigger{}
	}
	p.eventTriggers[req.TriggerID] = trigger
	return trigger, nil
}
func (p *recordingWorkflowProvider) GetEventTrigger(_ context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	if p.getEventTrigger != nil || p.getEventTriggerErr != nil {
		return p.getEventTrigger, p.getEventTriggerErr
	}
	if p.eventTriggers != nil {
		if trigger, ok := p.eventTriggers[req.TriggerID]; ok {
			return trigger, nil
		}
	}
	return nil, core.ErrNotFound
}
func (p *recordingWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	if p.listEventTriggersErr != nil {
		return nil, p.listEventTriggersErr
	}
	if p.listedEventTriggers != nil {
		return append([]*coreworkflow.EventTrigger(nil), p.listedEventTriggers...), nil
	}
	out := make([]*coreworkflow.EventTrigger, 0, len(p.eventTriggers))
	for _, trigger := range p.eventTriggers {
		out = append(out, trigger)
	}
	return out, nil
}
func (p *recordingWorkflowProvider) DeleteEventTrigger(_ context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	p.deletedEventTriggers = append(p.deletedEventTriggers, req)
	if p.deleteEventTriggerErr != nil {
		return p.deleteEventTriggerErr
	}
	if p.eventTriggers != nil {
		if _, ok := p.eventTriggers[req.TriggerID]; ok {
			delete(p.eventTriggers, req.TriggerID)
			return nil
		}
	}
	if p.deleteEventMissingNotFound {
		return core.ErrNotFound
	}
	if p.eventTriggers != nil {
		delete(p.eventTriggers, req.TriggerID)
	}
	return nil
}
func (p *recordingWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (p *recordingWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}
func (p *recordingWorkflowProvider) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}
func (p *recordingWorkflowProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if p.putExecutionReferenceErr != nil {
		return nil, p.putExecutionReferenceErr
	}
	if p.executionRefs == nil {
		p.executionRefs = map[string]*coreworkflow.ExecutionReference{}
	}
	stored := cloneBootstrapWorkflowExecutionRef(ref)
	p.executionRefs[stored.ID] = stored
	return cloneBootstrapWorkflowExecutionRef(stored), nil
}
func (p *recordingWorkflowProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	if err := p.getExecutionReferenceErrs[strings.TrimSpace(id)]; err != nil {
		return nil, err
	}
	if ref := p.executionRefs[strings.TrimSpace(id)]; ref != nil {
		return cloneBootstrapWorkflowExecutionRef(ref), nil
	}
	return nil, core.ErrNotFound
}
func (p *recordingWorkflowProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	subjectID = strings.TrimSpace(subjectID)
	out := make([]*coreworkflow.ExecutionReference, 0, len(p.executionRefs))
	for _, ref := range p.executionRefs {
		if ref == nil {
			continue
		}
		if subjectID != "" && strings.TrimSpace(ref.SubjectID) != subjectID {
			continue
		}
		out = append(out, cloneBootstrapWorkflowExecutionRef(ref))
	}
	return out, nil
}
func (p *recordingWorkflowProvider) Ping(context.Context) error { return nil }
func (p *recordingWorkflowProvider) Close() error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
}

func (p *recordingWorkflowProvider) scheduleGetResponse(schedule *coreworkflow.Schedule) *coreworkflow.Schedule {
	if schedule == nil {
		return nil
	}
	value := *schedule
	if p.omitScheduleExecutionRef {
		value.ExecutionRef = ""
	}
	return &value
}

func cloneBootstrapWorkflowExecutionRef(ref *coreworkflow.ExecutionReference) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	clone := *ref
	clone.Target = cloneBootstrapWorkflowTarget(ref.Target)
	clone.Permissions = append([]core.AccessPermission(nil), ref.Permissions...)
	for i := range clone.Permissions {
		clone.Permissions[i].Operations = append([]string(nil), clone.Permissions[i].Operations...)
		clone.Permissions[i].Actions = append([]string(nil), clone.Permissions[i].Actions...)
	}
	if ref.CreatedAt != nil {
		createdAt := ref.CreatedAt.UTC()
		clone.CreatedAt = &createdAt
	}
	if ref.RevokedAt != nil {
		revokedAt := ref.RevokedAt.UTC()
		clone.RevokedAt = &revokedAt
	}
	return &clone
}

func cloneBootstrapWorkflowTarget(target coreworkflow.Target) coreworkflow.Target {
	clone := coreworkflow.Target{}
	if target.Plugin != nil {
		plugin := *target.Plugin
		plugin.Input = maps.Clone(plugin.Input)
		clone.Plugin = &plugin
	}
	if target.Agent != nil {
		agent := *target.Agent
		agent.Messages = slices.Clone(agent.Messages)
		agent.ToolRefs = slices.Clone(agent.ToolRefs)
		agent.ResponseSchema = maps.Clone(agent.ResponseSchema)
		agent.ModelOptions = maps.Clone(agent.ModelOptions)
		agent.Metadata = maps.Clone(agent.Metadata)
		clone.Agent = &agent
	}
	return clone
}

type trackedIndexedDB struct {
	*coretesting.StubIndexedDB
	closed *atomic.Int32
}

func (t *trackedIndexedDB) Close() error {
	if t.closed != nil {
		t.closed.Add(1)
	}
	return nil
}

func validConfig() *config.Config {
	return &config.Config{
		Plugins: map[string]*config.ProviderEntry{},
		Providers: config.ProvidersConfig{
			Authentication: map[string]*config.ProviderEntry{
				"default": {
					Source: config.NewMetadataSource("https://example.invalid/github-com-valon-technologies-gestalt-providers-auth-oidc/v0.0.1-alpha.1/provider-release.yaml"),
					Config: yaml.Node{Kind: yaml.MappingNode},
				},
			},
			Secrets: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "test-secrets"}},
			},
			Telemetry: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "test-telemetry"}},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"test": {Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml")},
			},
		},
		Server: config.ServerConfig{
			Public:        config.ListenerConfig{Port: 8080},
			EncryptionKey: "test-key",
			Providers:     config.ServerProvidersConfig{IndexedDB: "test"},
		},
	}
}

func mustYAMLNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func selectedAuthenticationEntry(t *testing.T, cfg *config.Config) *config.ProviderEntry {
	t.Helper()
	_, entry, err := cfg.SelectedAuthenticationProvider()
	if err != nil {
		t.Fatalf("SelectedAuthenticationProvider: %v", err)
	}
	return entry
}

func validFactories() *bootstrap.FactoryRegistry {
	f := bootstrap.NewFactoryRegistry()
	f.Auth = stubAuthFactory("test-auth")
	f.ExternalCredentials = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.ExternalCredentialProvider, error) {
		return coretesting.NewStubExternalCredentialProvider(), nil
	}
	f.IndexedDB = stubIndexedDBFactory()
	f.Secrets["test-secrets"] = stubSecretManagerFactory()
	f.Telemetry["test-telemetry"] = stubTelemetryFactory()
	return f
}

func invokeWorkflowHostCallback(t *testing.T, hostServices []runtimehost.HostService, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	t.Helper()

	if len(hostServices) != 1 {
		t.Fatalf("workflow host services = %d, want 1", len(hostServices))
	}
	if hostServices[0].Register == nil {
		t.Fatal("workflow host register func is nil")
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	hostServices[0].Register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return proto.NewWorkflowHostClient(conn).InvokeOperation(context.Background(), req)
}

func invokeAgentHostCallback(t *testing.T, hostServices []runtimehost.HostService, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	t.Helper()

	if len(hostServices) != 1 {
		t.Fatalf("agent host services = %d, want 1", len(hostServices))
	}
	if hostServices[0].Register == nil {
		t.Fatal("agent host register func is nil")
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	hostServices[0].Register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return proto.NewAgentHostClient(conn).ExecuteTool(context.Background(), req)
}

func invokeAgentHostListTools(t *testing.T, hostServices []runtimehost.HostService, req *proto.ListAgentToolsRequest) *proto.ListAgentToolsResponse {
	t.Helper()

	if len(hostServices) != 1 {
		t.Fatalf("agent host services = %d, want 1", len(hostServices))
	}
	if hostServices[0].Register == nil {
		t.Fatal("agent host register func is nil")
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	hostServices[0].Register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := proto.NewAgentHostClient(conn).ListTools(context.Background(), req)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	return resp
}

func withIndexedDBHostClient(t *testing.T, hostService runtimehost.HostService, fn func(proto.IndexedDBClient)) {
	t.Helper()
	if hostService.Register == nil {
		t.Fatal("indexeddb host register func is nil")
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	hostService.Register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	fn(proto.NewIndexedDBClient(conn))
}

func workflowStartupCallbackConfig(baseURL string) *config.Config {
	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			ConnectionMode: providermanifestv1.ConnectionModeNone,
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: baseURL,
							Operations: []providermanifestv1.ProviderOperation{
								{Name: "sync", Method: http.MethodPost, Path: "/sync"},
							},
						},
					},
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	return cfg
}

type workflowFixture struct {
	Provider      string
	Schedules     map[string]workflowFixtureSchedule
	EventTriggers map[string]workflowFixtureEventTrigger
}

type workflowFixtureSchedule struct {
	Cron      string
	Timezone  string
	Operation string
	Input     map[string]any
	Paused    bool
}

type workflowFixtureEventTrigger struct {
	Match     workflowFixtureEventMatch
	Operation string
	Input     map[string]any
	Paused    bool
}

type workflowFixtureEventMatch struct {
	Type    string
	Source  string
	Subject string
}

func setWorkflowFixture(cfg *config.Config, plugin string, workflow *workflowFixture) {
	if cfg == nil {
		return
	}
	if cfg.Workflows.Schedules == nil {
		cfg.Workflows.Schedules = map[string]config.WorkflowScheduleConfig{}
	}
	if cfg.Workflows.EventTriggers == nil {
		cfg.Workflows.EventTriggers = map[string]config.WorkflowEventTriggerConfig{}
	}
	for key, schedule := range cfg.Workflows.Schedules {
		if workflowFixtureTargetPlugin(schedule.Target) == plugin {
			delete(cfg.Workflows.Schedules, key)
		}
	}
	for key, trigger := range cfg.Workflows.EventTriggers {
		if workflowFixtureTargetPlugin(trigger.Target) == plugin {
			delete(cfg.Workflows.EventTriggers, key)
		}
	}
	if workflow == nil {
		return
	}
	for key, schedule := range workflow.Schedules {
		cfg.Workflows.Schedules[key] = config.WorkflowScheduleConfig{
			Provider: workflow.Provider,
			Target:   workflowFixtureTarget(plugin, schedule.Operation, schedule.Input),
			Cron:     schedule.Cron,
			Timezone: schedule.Timezone,
			Paused:   schedule.Paused,
		}
	}
	for key, trigger := range workflow.EventTriggers {
		cfg.Workflows.EventTriggers[key] = config.WorkflowEventTriggerConfig{
			Provider: workflow.Provider,
			Target:   workflowFixtureTarget(plugin, trigger.Operation, trigger.Input),
			Match: config.WorkflowEventMatch{
				Type:    trigger.Match.Type,
				Source:  trigger.Match.Source,
				Subject: trigger.Match.Subject,
			},
			Paused: trigger.Paused,
		}
	}
}

func workflowFixtureTarget(plugin, operation string, input map[string]any) *config.WorkflowTargetConfig {
	return &config.WorkflowTargetConfig{
		Plugin: &config.WorkflowPluginTargetConfig{
			Name:      plugin,
			Operation: operation,
			Input:     maps.Clone(input),
		},
	}
}

func requireCoreWorkflowPluginTarget(t *testing.T, target coreworkflow.Target) *coreworkflow.PluginTarget {
	t.Helper()
	if target.Plugin == nil {
		t.Fatalf("target plugin is nil: %#v", target)
	}
	return target.Plugin
}

func coreWorkflowPluginTarget(pluginName, operation string) coreworkflow.Target {
	return coreworkflow.Target{
		Plugin: &coreworkflow.PluginTarget{
			PluginName: pluginName,
			Operation:  operation,
		},
	}
}

func protoWorkflowPluginTarget(pluginName, operation string) *proto.BoundWorkflowTarget {
	return &proto.BoundWorkflowTarget{
		Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{
				PluginName: pluginName,
				Operation:  operation,
			},
		},
	}
}

func workflowFixtureTargetPlugin(target *config.WorkflowTargetConfig) string {
	if target == nil || target.Plugin == nil {
		return ""
	}
	return target.Plugin.Name
}

func transportSecretRef(name string) string {
	return config.EncodeSecretRefTransport(config.SecretRef{
		Provider: "default",
		Name:     name,
	})
}

func TestBootstrapProviderBoundaryMetrics(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.ExternalCredentials = map[string]*config.ProviderEntry{
		"remote-creds": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.ExternalCredentials = "remote-creds"
	cfg.Providers.Authorization = map[string]*config.ProviderEntry{
		"remote-authz": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.Authorization = "remote-authz"

	factories := validFactories()
	factories.Authorization = stubAuthorizationFactory("authorization-provider")
	metrics := metrictest.NewManualMeterProvider(t)

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	<-result.ProvidersReady

	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	if err := result.Services.ExternalCredentials.PutCredential(ctx, &core.ExternalCredential{
		SubjectID:   principal.UserSubjectID("metrics-user"),
		Integration: "slack",
		Connection:  "default",
		Instance:    "default",
		AccessToken: "tok_metrics",
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
	if _, err := result.AuthorizationProvider.Evaluate(ctx, &core.AccessEvaluationRequest{
		Subject:  &core.SubjectRef{Type: "user", Id: "metrics-user"},
		Action:   &core.ActionRef{Name: "read"},
		Resource: &core.ResourceRef{Type: "integration", Id: "slack"},
	}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.credential.provider.operation.count", 1, map[string]string{
		"gestalt.credential.provider":  "remote-creds",
		"gestalt.credential.operation": "put_credential",
		"gestalt.provider":             "slack",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.authorization.provider.operation.count", 1, map[string]string{
		"gestalt.authorization.provider":  "remote-authz",
		"gestalt.authorization.operation": "evaluate",
	})
}

func TestBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.Auth == nil {
		t.Fatal("Auth is nil")
	}
	if result.Auth.Name() != "test-auth" {
		t.Errorf("Auth.Name: got %q, want %q", result.Auth.Name(), "test-auth")
	}
	if result.Services == nil {
		t.Fatal("Datastore is nil")
	}
	if result.Telemetry == nil {
		t.Fatal("Telemetry is nil")
	}
	if result.Invoker == nil {
		t.Fatal("Invoker is nil")
	}
	if result.CapabilityLister == nil {
		t.Fatal("CapabilityLister is nil")
	}
	invoker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		t.Fatalf("Invoker should be *invocation.Broker, got %T", result.Invoker)
	}
	lister, ok := result.CapabilityLister.(*invocation.Broker)
	if !ok {
		t.Fatalf("CapabilityLister should be *invocation.Broker, got %T", result.CapabilityLister)
	}
	if invoker != lister {
		t.Fatal("expected shared invoker and capability lister to be the same instance")
	}

	t.Run("invoker uses resolved REST connections", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name           string
			restConnection string
			specAuth       *providermanifestv1.ProviderAuth
			connections    map[string]*providermanifestv1.ManifestConnectionDef
			tokenConn      string
			tokenValue     string
			wantAuth       string
			wantAPIKey     string
		}{
			{
				name: "single named connection is inferred as default",
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {
						Auth: &providermanifestv1.ProviderAuth{
							Type:             providermanifestv1.AuthTypeOAuth2,
							ClientID:         "client-id",
							ClientSecret:     "client-secret",
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
				},
				tokenConn: "default",
			},
			{
				name:           "explicit REST connection is used for invoke",
				restConnection: "workspace",
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"workspace": {
						Auth: &providermanifestv1.ProviderAuth{
							Type:             providermanifestv1.AuthTypeOAuth2,
							ClientID:         "client-id",
							ClientSecret:     "client-secret",
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
					"backup": {
						Auth: &providermanifestv1.ProviderAuth{
							Type:             providermanifestv1.AuthTypeOAuth2,
							ClientID:         "client-id",
							ClientSecret:     "client-secret",
							AuthorizationURL: "https://example.com/authorize",
							TokenURL:         "https://example.com/token",
						},
					},
				},
				tokenConn: "workspace",
			},
			{
				name: "declarative auth mapping basic preserves derived authorization header",
				specAuth: &providermanifestv1.ProviderAuth{
					Type: providermanifestv1.AuthTypeManual,
					AuthMapping: &providermanifestv1.AuthMapping{
						Basic: &providermanifestv1.BasicAuthMapping{
							Username: providermanifestv1.AuthValue{
								ValueFrom: &providermanifestv1.AuthValueFrom{
									CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "username"},
								},
							},
							Password: providermanifestv1.AuthValue{
								ValueFrom: &providermanifestv1.AuthValueFrom{
									CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "password"},
								},
							},
						},
					},
				},
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {Mode: providermanifestv1.ConnectionModeUser},
				},
				tokenConn:  "default",
				tokenValue: `{"username":"alice","password":"secret"}`,
				wantAuth:   "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret")),
			},
			{
				name: "declarative auth mapping headers preserves derived upstream header",
				specAuth: &providermanifestv1.ProviderAuth{
					Type: providermanifestv1.AuthTypeManual,
					AuthMapping: &providermanifestv1.AuthMapping{
						Headers: map[string]providermanifestv1.AuthValue{
							"X-API-Key": {
								ValueFrom: &providermanifestv1.AuthValueFrom{
									CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "api_key"},
								},
							},
						},
					},
				},
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {Mode: providermanifestv1.ConnectionModeUser},
				},
				tokenConn:  "default",
				tokenValue: `{"api_key":"secret-key"}`,
				wantAPIKey: "secret-key",
			},
			{
				name:     "auth none still forwards bearer token when connection mode is user",
				specAuth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				connections: map[string]*providermanifestv1.ManifestConnectionDef{
					"workspace": {Mode: providermanifestv1.ConnectionModeUser},
				},
				restConnection: "workspace",
				tokenConn:      "workspace",
				wantAuth:       "Bearer workspace-access-token",
			},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var authHeader atomic.Value
				var apiKeyHeader atomic.Value
				var requestPath atomic.Value
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					authHeader.Store(r.Header.Get("Authorization"))
					apiKeyHeader.Store(r.Header.Get("X-API-Key"))
					requestPath.Store(r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"ok":true}`))
				}))
				defer srv.Close()

				connections := make(map[string]*providermanifestv1.ManifestConnectionDef, len(tc.connections))
				for name, def := range tc.connections {
					if def == nil {
						connections[name] = nil
						continue
					}
					copyDef := *def
					connections[name] = &copyDef
				}
				if tc.specAuth != nil {
					target := tc.restConnection
					if target == "" {
						if _, ok := connections["default"]; ok {
							target = "default"
						} else if len(connections) == 1 {
							for name := range connections {
								target = name
							}
						}
					}
					if target == "" {
						target = "default"
					}
					def := connections[target]
					if def == nil {
						def = &providermanifestv1.ManifestConnectionDef{}
					} else {
						copyDef := *def
						def = &copyDef
					}
					def.Auth = tc.specAuth
					connections[target] = def
				}

				cfg := validConfig()
				cfg.Plugins = map[string]*config.ProviderEntry{
					"slack": {
						ResolvedManifest: &providermanifestv1.Manifest{
							Spec: &providermanifestv1.Spec{
								Surfaces: &providermanifestv1.ProviderSurfaces{
									REST: &providermanifestv1.RESTSurface{
										BaseURL:    srv.URL,
										Connection: tc.restConnection,
										Operations: []providermanifestv1.ProviderOperation{
											{Name: "users.list", Method: http.MethodGet, Path: "/users"},
										},
									},
								},
								Connections: connections,
							},
						},
					},
				}

				result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
				if err != nil {
					t.Fatalf("Bootstrap: %v", err)
				}
				t.Cleanup(func() { _ = result.Close(context.Background()) })
				<-result.ProvidersReady

				user, err := result.Services.Users.FindOrCreateUser(ctx, "hugh@test.com")
				if err != nil {
					t.Fatalf("FindOrCreateUser: %v", err)
				}
				tokenValue := tc.tokenConn + "-access-token"
				if tc.tokenValue != "" {
					tokenValue = tc.tokenValue
				}
				if err := result.Services.ExternalCredentials.PutCredential(ctx, &core.ExternalCredential{
					SubjectID:    principal.UserSubjectID(user.ID),
					Integration:  "slack",
					Connection:   tc.tokenConn,
					Instance:     "default",
					AccessToken:  tokenValue,
					RefreshToken: "refresh-token",
				}); err != nil {
					t.Fatalf("PutCredential: %v", err)
				}

				principal := &principal.Principal{
					UserID: user.ID,
					Source: principal.SourceSession,
					Scopes: []string{"slack"},
				}
				got, err := result.Invoker.Invoke(ctx, principal, "slack", "", "users.list", nil)
				if err != nil {
					t.Fatalf("Invoke: %v", err)
				}
				if got.Status != http.StatusOK {
					t.Fatalf("status = %d, want %d", got.Status, http.StatusOK)
				}
				if gotPath, _ := requestPath.Load().(string); gotPath != "/users" {
					t.Fatalf("path = %q, want %q", gotPath, "/users")
				}
				wantAuth := "Bearer " + tokenValue
				if tc.wantAuth != "" || tc.specAuth != nil {
					wantAuth = tc.wantAuth
				}
				if gotAuth, _ := authHeader.Load().(string); gotAuth != wantAuth {
					t.Fatalf("Authorization = %q, want %q", gotAuth, wantAuth)
				}
				if gotAPIKey, _ := apiKeyHeader.Load().(string); gotAPIKey != tc.wantAPIKey {
					t.Fatalf("X-API-Key = %q, want %q", gotAPIKey, tc.wantAPIKey)
				}
			})
		}
	})
}

func TestBootstrapReturnsAuthorizationProvider(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Authorization = map[string]*config.ProviderEntry{
		"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.Authorization = "indexeddb"

	factories := validFactories()
	factories.Authorization = stubAuthorizationFactory("test-authorization")

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if result.AuthorizationProvider == nil {
		t.Fatal("AuthorizationProvider is nil")
	}
	if got := result.AuthorizationProvider.Name(); got != "test-authorization" {
		t.Fatalf("AuthorizationProvider.Name() = %q, want %q", got, "test-authorization")
	}
}

func TestBootstrapPassesConfiguredS3ResourceNamesToProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.S3 = map[string]*config.ProviderEntry{
		"archive": {Source: config.ProviderSource{Path: "stub"}},
		"main":    {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	seen := make(map[string]struct{}, len(cfg.Providers.S3))
	factories.S3 = func(node yaml.Node) (s3store.Client, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		seen[runtime.Name] = struct{}{}
		return &coretesting.StubS3{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(seen) != 2 {
		t.Fatalf("seen S3 runtime names = %v, want 2 entries", seen)
	}
	for _, name := range []string{"archive", "main"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing S3 runtime name %q in %v", name, seen)
		}
	}
}

func TestBootstrapPassesConfiguredWorkflowResourceNamesToProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"cleanup":  {Source: config.ProviderSource{Path: "stub"}},
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	seen := make(map[string]struct{}, len(cfg.Providers.Workflow))
	hostSockets := make(map[string]string, len(cfg.Providers.Workflow))
	factories.Workflow = func(_ context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		seen[runtime.Name] = struct{}{}
		if len(hostServices) != 1 {
			return nil, fmt.Errorf("workflow host services = %d, want 1", len(hostServices))
		}
		hostSockets[name] = hostServices[0].EnvVar
		return &stubWorkflowProvider{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(seen) != 2 {
		t.Fatalf("seen workflow runtime names = %v, want 2 entries", seen)
	}
	for _, name := range []string{"cleanup", "temporal"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing workflow runtime name %q in %v", name, seen)
		}
		if got := hostSockets[name]; got != workflowservice.DefaultHostSocketEnv {
			t.Fatalf("workflow host env for %q = %q, want %q", name, got, workflowservice.DefaultHostSocketEnv)
		}
	}
}

func TestBootstrapPassesConfiguredAgentResourceNamesToProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"cleanup": {Source: config.ProviderSource{Path: "stub"}},
		"reviewer": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	seen := make(map[string]struct{}, len(cfg.Providers.Agent))
	hostSockets := make(map[string]string, len(cfg.Providers.Agent))
	factories.Agent = func(_ context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		seen[runtime.Name] = struct{}{}
		if len(hostServices) != 1 {
			return nil, fmt.Errorf("agent host services = %d, want 1", len(hostServices))
		}
		hostSockets[name] = hostServices[0].EnvVar
		return newRecordingAgentProvider(), nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(seen) != 2 {
		t.Fatalf("seen agent runtime names = %v, want 2 entries", seen)
	}
	for _, name := range []string{"cleanup", "reviewer"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing agent runtime name %q in %v", name, seen)
		}
		if got := hostSockets[name]; got != agentservice.DefaultHostSocketEnv {
			t.Fatalf("agent host env for %q = %q, want %q", name, got, agentservice.DefaultHostSocketEnv)
		}
	}
	if got := result.AgentControl.ProviderNames(); !reflect.DeepEqual(got, []string{"cleanup", "reviewer"}) {
		t.Fatalf("agent provider names = %#v, want %#v", got, []string{"cleanup", "reviewer"})
	}
	selectedName, provider, err := result.AgentControl.ResolveProviderSelection("")
	if err != nil {
		t.Fatalf("ResolveProviderSelection: %v", err)
	}
	if selectedName != "reviewer" {
		t.Fatalf("selected agent provider = %q, want %q", selectedName, "reviewer")
	}
	session, err := provider.CreateSession(context.Background(), coreagent.CreateSessionRequest{
		Model: "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session == nil || session.ID != "session-1" {
		t.Fatalf("session = %#v, want ID session-1", session)
	}
}

func TestBootstrapAgentManagerCreateTurnPersistsMetadataForToolCallbacks(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
			IndexedDB: &config.HostIndexedDBBindingConfig{
				Provider:     "test",
				DB:           "agent_resume",
				ObjectStores: []string{"provider_state"},
			},
		},
	}

	factories := validFactories()
	factories.Builtins = append(
		factories.Builtins,
		&coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{{
					ID:          "sync",
					Method:      http.MethodPost,
					Title:       "Sync roadmap",
					Description: "Sync the roadmap state",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}}}`),
				}},
			},
			ExecuteFn: func(ctx context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
				body, err := json.Marshal(map[string]any{
					"operation": operation,
					"subject":   principal.FromContext(ctx).SubjectID,
					"taskId":    params["taskId"],
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusAccepted, Body: string(body)}, nil
			},
		},
		&coretesting.StubIntegration{
			N:        "lever",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name: "lever",
				Operations: []catalog.CatalogOperation{{
					ID:          "sync",
					Method:      http.MethodPost,
					Title:       "Roadmap sync",
					Description: "Unavailable static integration that should not fail global agent tool search",
				}},
			},
		},
		&callbackSessionCatalogIntegration{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
			sessionCatalog: &catalog.Catalog{
				Name: "ashby",
				Operations: []catalog.CatalogOperation{{
					ID:          "sync",
					Method:      http.MethodPost,
					Title:       "Roadmap sync",
					Description: "Unavailable session-catalog integration that should not fail global agent tool search",
				}},
			},
		},
	)

	var provider *callbackAgentProvider
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	perms := principal.CompilePermissions([]core.AccessPermission{
		{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		},
		{
			Plugin:     "lever",
			Operations: []string{"sync"},
		},
		{
			Plugin:     "ashby",
			Operations: []string{"sync"},
		},
		{
			Plugin: "managed",
		},
	})
	p := &principal.Principal{
		SubjectID:           "user:user-123",
		UserID:              "user-123",
		CredentialSubjectID: "service_account:agent-credential",
		Kind:                principal.KindUser,
		Source:              principal.SourceSession,
		TokenPermissions:    perms,
		Scopes:              principal.PermissionPlugins(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-1",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	req := coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "demo-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "sync it"}},
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "roadmap",
			Operation: "sync",
			Title:     "Roadmap sync",
		}},
	}

	first, err := result.AgentManager.CreateTurn(ctx, p, req)
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(first): %v", err)
	}
	second, err := result.AgentManager.CreateTurn(ctx, p, req)
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(second): %v", err)
	}
	if first == nil || second == nil {
		t.Fatalf("managed turns = %#v / %#v", first, second)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent turn ids = (%q, %q), want identical ids", first.ID, second.ID)
	}

	provider.mu.Lock()
	createTurnCount := len(provider.createTurnRequests)
	createTurnReq := provider.createTurnRequests[0]
	toolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()

	if createTurnCount != 1 {
		t.Fatalf("CreateTurn count = %d, want 1", createTurnCount)
	}
	if createTurnReq.TurnID != first.ID {
		t.Fatalf("CreateTurn turn_id = %q, want %q", createTurnReq.TurnID, first.ID)
	}
	if createTurnReq.SessionID != session.ID {
		t.Fatalf("CreateTurn session_id = %q, want %q", createTurnReq.SessionID, session.ID)
	}
	if createTurnReq.ExecutionRef != first.ID {
		t.Fatalf("CreateTurn execution_ref = %q, want %q", createTurnReq.ExecutionRef, first.ID)
	}
	if createTurnReq.CreatedBy.SubjectID != p.SubjectID {
		t.Fatalf("CreateTurn created_by.subject_id = %q, want %q", createTurnReq.CreatedBy.SubjectID, p.SubjectID)
	}
	if len(createTurnReq.Tools) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", createTurnReq.Tools)
	}
	if createTurnReq.ToolSource != coreagent.ToolSourceModeMCPCatalog {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", createTurnReq.ToolSource)
	}
	if len(createTurnReq.ToolRefs) != 1 || createTurnReq.ToolRefs[0].Plugin != "roadmap" || createTurnReq.ToolRefs[0].Operation != "sync" {
		t.Fatalf("CreateTurn tool refs = %#v", createTurnReq.ToolRefs)
	}
	if strings.TrimSpace(createTurnReq.RunGrant) == "" {
		t.Fatal("CreateTurn run_grant is empty")
	}
	if len(toolBodies) != 1 || !strings.Contains(toolBodies[0], `"subject":"user:user-123"`) || !strings.Contains(toolBodies[0], `"taskId":"task-123"`) {
		t.Fatalf("tool callback bodies = %#v", toolBodies)
	}

	_, err = result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "global-search-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "sync it without explicit tools"}},
		ToolRefsSet:    true,
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(global search): %v", err)
	}
	provider.mu.Lock()
	globalToolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(globalToolBodies) != 1 {
		t.Fatalf("global tool callback bodies = %#v, want no execution for empty catalog grant", globalToolBodies)
	}

	_, err = result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "scoped-unavailable-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "sync ashby"}},
		ToolRefs:       []coreagent.ToolRef{{Plugin: "ashby", Operation: "sync"}},
	})
	if err == nil || !strings.Contains(err.Error(), `no external credential stored for integration "ashby"`) {
		t.Fatalf("AgentManager.CreateTurn(scoped unavailable) error = %v, want ashby credential error", err)
	}
}

func TestBootstrapAgentHostToolCatalogExecutesExactPluginIssueTool(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	const unavailableIssueProviderCount = 120
	unavailableIssueProviders := make([]core.Provider, 0, unavailableIssueProviderCount)
	unavailableIssuePermissions := make([]core.AccessPermission, 0, unavailableIssueProviderCount)
	for i := range unavailableIssueProviderCount {
		name := fmt.Sprintf("aaa_ticket_issue_source_%03d", i)
		unavailableIssueProviders = append(unavailableIssueProviders, &coretesting.StubIntegration{
			N:        name,
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name:        name,
				DisplayName: "Assigned ticket issue source",
				Description: "Unavailable issue tracking integration.",
				Operations: []catalog.CatalogOperation{{
					ID:          "list_issues",
					Method:      http.MethodGet,
					Title:       "List assigned ticket issues",
					Description: "List tickets and issues assigned to the current user.",
					Parameters: []catalog.CatalogParameter{{
						Name:        "assignee",
						Type:        "string",
						Description: "Issue assignee filter.",
					}},
					ReadOnly: true,
				}},
			},
		})
		unavailableIssuePermissions = append(unavailableIssuePermissions, core.AccessPermission{
			Plugin:     name,
			Operations: []string{"list_issues"},
		})
	}
	factories.Builtins = append(factories.Builtins, unavailableIssueProviders...)
	factories.Builtins = append(
		factories.Builtins,
		&coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:        "linear",
				DisplayName: "Linear",
				Description: "Manage issues, projects, and teams.",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "list_issues",
						Method:      http.MethodGet,
						Title:       "All issues",
						Description: "All issues visible to the authenticated user. Can be filtered by assignee, team, state, labels, project, and cycle.",
						Parameters: []catalog.CatalogParameter{{
							Name:        "assignee",
							Type:        "string",
							Description: "Issue assignee filter.",
						}},
						ReadOnly: true,
					},
					{
						ID:          "list_comments",
						Method:      http.MethodGet,
						Title:       "All comments",
						Description: "All comments the user has access to in the workspace.",
						ReadOnly:    true,
					},
					{
						ID:          "list_customers",
						Method:      http.MethodGet,
						Title:       "All customers",
						Description: "All customers in the workspace, with optional filtering and sorting.",
						ReadOnly:    true,
					},
					{
						ID:          "list_documents",
						Method:      http.MethodGet,
						Title:       "All documents",
						Description: "All documents the user has access to in the workspace.",
						ReadOnly:    true,
					},
				},
			},
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				body, err := json.Marshal(map[string]any{
					"provider":  "linear",
					"operation": operation,
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		&coretesting.StubIntegration{
			N:        "customerRoadmapReview",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:        "customerRoadmapReview",
				DisplayName: "Customer Roadmap Review",
				Description: "Review customer roadmap views, customer needs, endpoints, and current user metadata.",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "publish_customer_view",
						Method:      http.MethodPost,
						Title:       "Publish customer view",
						Description: "Publish a customer-facing view.",
						ReadOnly:    true,
					},
					{
						ID:          "get_me",
						Method:      http.MethodGet,
						Title:       "Get me",
						Description: "Get current user metadata.",
						ReadOnly:    true,
					},
					{
						ID:          "get_endpoints",
						Method:      http.MethodGet,
						Title:       "Get endpoints",
						Description: "List available customer roadmap endpoints.",
						ReadOnly:    true,
					},
				},
			},
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				body, err := json.Marshal(map[string]any{
					"provider":  "customerRoadmapReview",
					"operation": operation,
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
	)

	var provider *callbackAgentProvider
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	permissions := []core.AccessPermission{
		{
			Plugin: "managed",
		},
		{
			Plugin:     "linear",
			Operations: []string{"list_issues", "list_comments", "list_customers", "list_documents"},
		},
		{
			Plugin:     "customerRoadmapReview",
			Operations: []string{"publish_customer_view", "get_me", "get_endpoints"},
		},
	}
	permissions = append(permissions, unavailableIssuePermissions...)
	perms := principal.CompilePermissions(permissions)
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-linear-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "linear-search-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "get my linear tickets"}},
		ToolRefs:       []coreagent.ToolRef{{Plugin: "linear", Operation: "list_issues"}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}
	if turn == nil {
		t.Fatal("AgentManager.CreateTurn returned nil turn")
	}

	provider.mu.Lock()
	toolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(toolBodies) != 1 || !strings.Contains(toolBodies[0], `"provider":"linear"`) || !strings.Contains(toolBodies[0], `"operation":"list_issues"`) {
		t.Fatalf("tool callback bodies = %#v, want exact catalog tool linear.list_issues", toolBodies)
	}

	second, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "linear-search-after-unavailable-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "get my assigned tickets"}},
		ToolRefs:       []coreagent.ToolRef{{Plugin: "linear", Operation: "list_issues"}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(after unavailable hits): %v", err)
	}
	if second == nil {
		t.Fatal("AgentManager.CreateTurn(after unavailable hits) returned nil turn")
	}

	provider.mu.Lock()
	toolBodies = append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(toolBodies) != 2 || !strings.Contains(toolBodies[1], `"provider":"linear"`) || !strings.Contains(toolBodies[1], `"operation":"list_issues"`) {
		t.Fatalf("tool callback bodies after unavailable hits = %#v, want exact catalog tool linear.list_issues", toolBodies)
	}

}

func TestBootstrapAgentHostToolCatalogListsAndExecutesVisibleTools(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	hidden := false
	destructive := true
	factories.Builtins = append(factories.Builtins, &coretesting.StubIntegration{
		N:        "docs",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name:        "docs",
			DisplayName: "Docs",
			Description: "Search and inspect docs",
			Operations: []catalog.CatalogOperation{
				{ID: "alpha_search", Method: http.MethodGet, Title: "Docs alpha search", Description: "Search docs alpha", ReadOnly: true},
				{ID: "beta_list", Method: http.MethodGet, Title: "Docs beta list", Description: "List docs beta", ReadOnly: true},
				{ID: "delta_export", Method: http.MethodGet, Title: "Docs delta export", Description: "Export docs delta", ReadOnly: true},
				{ID: "epsilon_delete", Method: http.MethodDelete, Title: "Docs epsilon delete", Description: "Delete docs epsilon", Annotations: catalog.OperationAnnotations{DestructiveHint: &destructive}},
				{ID: "gamma_get", Method: http.MethodGet, Title: "Docs gamma get", Description: "Get docs gamma", ReadOnly: true},
				{ID: "aardvark_admin", Method: http.MethodPost, Title: "Hidden docs admin", Description: "Hidden admin operation", Visible: &hidden},
			},
		},
		ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
			body, err := json.Marshal(map[string]any{
				"provider":  "docs",
				"operation": operation,
			})
			if err != nil {
				return nil, err
			}
			return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
		},
	})

	var provider *callbackAgentProvider
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "docs",
		Operations: []string{"aardvark_admin", "alpha_search", "beta_list", "delta_export", "epsilon_delete", "gamma_get"},
	}, {
		Plugin: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-candidate-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "candidate-search-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "search docs"}},
		ToolRefs:       []coreagent.ToolRef{{Plugin: "docs"}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(search): %v", err)
	}
	if turn == nil {
		t.Fatal("AgentManager.CreateTurn(search) returned nil turn")
	}

	provider.mu.Lock()
	listResponses := append([]*proto.ListAgentToolsResponse(nil), provider.listResponses...)
	toolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(listResponses) != 1 {
		t.Fatalf("list response count = %d, want 1", len(listResponses))
	}
	listResp := listResponses[0]
	if len(listResp.GetTools()) != 5 {
		t.Fatalf("listed tools = %#v, want five visible docs tools", listResp.GetTools())
	}
	if len(toolBodies) != 1 {
		t.Fatalf("tool callback bodies = %#v, want one listed tool execution", toolBodies)
	}
	var loadedBody map[string]string
	if err := json.Unmarshal([]byte(toolBodies[0]), &loadedBody); err != nil {
		t.Fatalf("tool callback body = %q: %v", toolBodies[0], err)
	}
	if loadedBody["provider"] != "docs" {
		t.Fatalf("tool callback body = %#v, want docs provider", loadedBody)
	}
	loadedOperation := loadedBody["operation"]
	if loadedOperation == "" || loadedOperation == "aardvark_admin" {
		t.Fatalf("loaded operation = %q, want visible docs operation", loadedOperation)
	}
	var betaOperation string
	var destructiveOperation string
	for _, tool := range listResp.GetTools() {
		ref := tool.GetRef()
		if ref.GetOperation() == "aardvark_admin" {
			t.Fatalf("listed hidden tool = %#v, want only visible tools for broad catalog", tool)
		}
		if ref.GetOperation() == "beta_list" {
			betaOperation = ref.GetOperation()
		}
		if ref.GetOperation() == "epsilon_delete" {
			destructiveOperation = ref.GetOperation()
		}
	}
	if betaOperation == "" {
		t.Fatalf("listed tools = %#v, want beta_list", listResp.GetTools())
	}
	if destructiveOperation == "" {
		t.Fatalf("listed tools = %#v, want visible destructive epsilon_delete", listResp.GetTools())
	}
	exact, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "candidate-load-ref-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "load beta docs"}},
		ToolRefs:       []coreagent.ToolRef{{Plugin: "docs", Operation: betaOperation}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(exact ref): %v", err)
	}
	if exact == nil {
		t.Fatal("AgentManager.CreateTurn(exact ref) returned nil turn")
	}

	provider.mu.Lock()
	toolBodies = append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(toolBodies) != 2 || !strings.Contains(toolBodies[1], fmt.Sprintf(`"operation":"%s"`, betaOperation)) {
		t.Fatalf("tool callback bodies after exact ref = %#v, want %s", toolBodies, betaOperation)
	}

	mixed, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "candidate-mixed-global-exact-hidden-idempotency-key",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "load hidden docs"}},
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "*"},
			{Plugin: "docs", Operation: "aardvark_admin"},
		},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(mixed global exact hidden ref): %v", err)
	}
	if mixed == nil {
		t.Fatal("AgentManager.CreateTurn(mixed global exact hidden ref) returned nil turn")
	}
	provider.mu.Lock()
	listResponses = append([]*proto.ListAgentToolsResponse(nil), provider.listResponses...)
	provider.mu.Unlock()
	if len(listResponses) != 3 {
		t.Fatalf("list response count after mixed global exact hidden ref = %d, want 3", len(listResponses))
	}
	hiddenListed := false
	for _, tool := range listResponses[2].GetTools() {
		if tool.GetRef().GetPlugin() == "docs" && tool.GetRef().GetOperation() == "aardvark_admin" {
			hiddenListed = true
			break
		}
	}
	if !hiddenListed {
		t.Fatalf("mixed global exact hidden listed tools = %#v, want aardvark_admin", listResponses[2].GetTools())
	}
}

func TestBootstrapAgentDefaultToolNarrowingThresholdConfigNarrowsImplicitCatalogGrant(t *testing.T) {
	t.Parallel()

	threshold := 0
	cfg := validConfig()
	cfg.Server.Agent.DefaultToolNarrowingThreshold = &threshold
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	factories.Builtins = append(factories.Builtins,
		&coretesting.StubIntegration{
			N:        "linear",
			DN:       "Linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:        "linear",
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:       "issues",
					Method:   http.MethodGet,
					Title:    "Issues",
					ReadOnly: true,
				}},
			},
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				body, err := json.Marshal(map[string]any{
					"provider":  "linear",
					"operation": operation,
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		&coretesting.StubIntegration{
			N:        "github",
			DN:       "GitHub",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:        "github",
				DisplayName: "GitHub",
				Operations: []catalog.CatalogOperation{{
					ID:       "issues",
					Method:   http.MethodGet,
					Title:    "Issues",
					ReadOnly: true,
				}},
			},
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				body, err := json.Marshal(map[string]any{
					"provider":  "github",
					"operation": operation,
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
	)

	var provider *callbackAgentProvider
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "linear",
		Operations: []string{"issues"},
	}, {
		Plugin:     "github",
		Operations: []string{"issues"},
	}, {
		Plugin: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-configured-narrowing",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "configured-narrowing-linear",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "show me my linear tickets"}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}
	if turn == nil {
		t.Fatal("AgentManager.CreateTurn returned nil turn")
	}

	provider.mu.Lock()
	listResponses := append([]*proto.ListAgentToolsResponse(nil), provider.listResponses...)
	toolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(listResponses) != 1 {
		t.Fatalf("list response count = %d, want 1", len(listResponses))
	}
	tools := listResponses[0].GetTools()
	if len(tools) != 1 || tools[0].GetRef().GetPlugin() != "linear" || tools[0].GetRef().GetOperation() != "issues" {
		t.Fatalf("listed tools = %#v, want only linear issues from configured narrowing", tools)
	}
	if len(toolBodies) != 1 || !strings.Contains(toolBodies[0], `"provider":"linear"`) {
		t.Fatalf("tool callback bodies = %#v, want linear execution", toolBodies)
	}
}

func TestBootstrapHTTPCallerWildcardCatalogToolRefsAreScopedByAuthorization(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	factories.Builtins = append(factories.Builtins, &coretesting.StubIntegration{
		N:        "linear",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name:        "linear",
			DisplayName: "Linear",
			Description: "Manage issues, projects, and teams.",
			Operations: []catalog.CatalogOperation{{
				ID:          "issues",
				Method:      http.MethodGet,
				Description: "All issues visible to the authenticated user. Can be filtered by assignee.",
				ReadOnly:    true,
			}},
		},
		ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
			body, err := json.Marshal(map[string]any{
				"provider":  "linear",
				"operation": operation,
			})
			if err != nil {
				return nil, err
			}
			return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
		},
	})

	var provider *callbackAgentProvider
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	slackOnly := principal.CompilePermissions([]core.AccessPermission{{
		Plugin: "slack",
		Operations: []string{
			"events.reply",
			"events.setStatus",
		},
	}, {
		Plugin: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: slackOnly,
		Scopes:           principal.PermissionPlugins(slackOnly),
	}
	ctx := invocation.WithInvocationSurface(principal.WithPrincipal(context.Background(), p), invocation.InvocationSurfaceHTTP)

	session, err := result.AgentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-http-slack-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		CallerPluginName: "slack",
		SessionID:        session.ID,
		IdempotencyKey:   "http-slack-linear-search",
		Model:            "gpt-test",
		Messages:         []coreagent.Message{{Role: "user", Text: "get my linear tickets"}},
		ToolRefs:         []coreagent.ToolRef{{Plugin: "*"}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn wildcard scoped turn: %v", err)
	}
	if turn == nil {
		t.Fatal("AgentManager.CreateTurn wildcard scoped turn returned nil")
	}
	provider.mu.Lock()
	listResponses := append([]*proto.ListAgentToolsResponse(nil), provider.listResponses...)
	toolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(listResponses) != 1 {
		t.Fatalf("list response count = %d, want 1", len(listResponses))
	}
	if len(listResponses[0].GetTools()) != 0 {
		t.Fatalf("listed tools = %#v, want none outside principal permissions", listResponses[0].GetTools())
	}
	if len(toolBodies) != 0 {
		t.Fatalf("tool callback bodies = %#v, want no execution outside principal permissions", toolBodies)
	}
}

func TestBootstrapGlobalCatalogToolRefsSurfaceUnavailableProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	factories.Builtins = append(factories.Builtins,
		&coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "linear",
				Operations: []catalog.CatalogOperation{{
					ID:          "issues",
					Method:      http.MethodGet,
					Description: "All issues visible to the authenticated user.",
					ReadOnly:    true,
				}},
			},
			ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				body, err := json.Marshal(map[string]any{
					"provider":  "linear",
					"operation": operation,
				})
				if err != nil {
					return nil, err
				}
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		&unavailableSessionCatalogIntegration{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
				CatalogVal: &catalog.Catalog{
					Name: "ashby",
					Operations: []catalog.CatalogOperation{{
						ID:          "candidates",
						Method:      http.MethodGet,
						Description: "All candidates visible to the authenticated user.",
						ReadOnly:    true,
					}},
				},
			},
			err: invocation.ErrNoCredential,
		},
	)

	var provider *callbackAgentProvider
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "linear",
		Operations: []string{"issues"},
	}, {
		Plugin:     "ashby",
		Operations: []string{"candidates"},
	}, {
		Plugin: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(principal.WithPrincipal(context.Background(), p), invocation.InvocationSurfaceHTTP)

	session, err := result.AgentManager.CreateSession(ctx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-http-global-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, coreagent.ManagerCreateTurnRequest{
		CallerPluginName: "slack",
		SessionID:        session.ID,
		IdempotencyKey:   "http-global-linear-search",
		Model:            "gpt-test",
		Messages:         []coreagent.Message{{Role: "user", Text: "get my linear tickets"}},
		ToolRefs:         []coreagent.ToolRef{{Plugin: "*"}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn global scoped turn: %v", err)
	}
	if turn == nil {
		t.Fatal("AgentManager.CreateTurn global scoped turn returned nil")
	}

	provider.mu.Lock()
	listResponses := append([]*proto.ListAgentToolsResponse(nil), provider.listResponses...)
	toolBodies := append([]string(nil), provider.toolBodies...)
	provider.mu.Unlock()
	if len(listResponses) != 1 {
		t.Fatalf("list response count = %d, want 1", len(listResponses))
	}
	tools := listResponses[0].GetTools()
	if len(tools) != 2 {
		t.Fatalf("listed tools = %#v, want connected linear issues plus ashby unavailable sentinel", tools)
	}
	if tools[0].GetRef().GetPlugin() != "linear" || tools[0].GetRef().GetOperation() != "issues" {
		t.Fatalf("first listed tool = %#v, want connected linear issues before unavailable sentinels", tools[0])
	}
	if tools[1].GetRef().GetPlugin() != "ashby" || tools[1].GetRef().GetOperation() != "" || tools[1].GetMcpName() != "ashby__no_credential" {
		t.Fatalf("second listed tool = %#v, want ashby unavailable sentinel", tools[1])
	}
	if len(toolBodies) != 1 || !strings.Contains(toolBodies[0], `"provider":"linear"`) || !strings.Contains(toolBodies[0], `"operation":"issues"`) {
		t.Fatalf("tool callback bodies = %#v, want executed linear issues", toolBodies)
	}
}

func TestBootstrapAgentProviderSupportsDirectTurnInteractionLifecycle(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var provider *callbackAgentProvider
	factories := validFactories()
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	_, selected, err := result.AgentControl.ResolveProviderSelection("")
	if err != nil {
		t.Fatalf("ResolveProviderSelection: %v", err)
	}
	startCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "system:config"})
	if _, err := selected.CreateSession(startCtx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-plain",
		Model:     "gpt-test",
		CreatedBy: coreagent.Actor{SubjectID: "system:config"},
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := selected.CreateTurn(startCtx, coreagent.CreateTurnRequest{
		TurnID:    "agent-turn-plain",
		SessionID: "agent-session-plain",
		Model:     "gpt-test",
		CreatedBy: coreagent.Actor{SubjectID: "system:config"},
		Messages: []coreagent.Message{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn == nil || turn.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("turn = %#v, want waiting_for_input", turn)
	}

	interactions, err := selected.ListInteractions(context.Background(), coreagent.ListInteractionsRequest{TurnID: "agent-turn-plain"})
	if err != nil {
		t.Fatalf("ListInteractions: %v", err)
	}
	if len(interactions) != 1 || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interactions = %#v, want one pending interaction", interactions)
	}

	provider.resolveInteractionHook = func(ctx context.Context, req coreagent.ResolveInteractionRequest) error {
		current, err := selected.GetInteraction(ctx, coreagent.GetInteractionRequest{InteractionID: req.InteractionID})
		if err != nil {
			return err
		}
		if current.State != coreagent.InteractionStatePending || current.TurnID != "agent-turn-plain" {
			return fmt.Errorf("interaction during direct provider resolve = %#v, want pending agent-turn-plain", current)
		}
		return nil
	}

	resolved, err := selected.ResolveInteraction(startCtx, coreagent.ResolveInteractionRequest{
		InteractionID: interactions[0].ID,
		Resolution: map[string]any{
			"approved": true,
		},
	})
	if err != nil {
		t.Fatalf("ResolveInteraction: %v", err)
	}
	if resolved == nil || resolved.State != coreagent.InteractionStateResolved || resolved.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolved)
	}

	resolvedInteractions, err := selected.ListInteractions(context.Background(), coreagent.ListInteractionsRequest{TurnID: "agent-turn-plain"})
	if err != nil {
		t.Fatalf("ListInteractions(resolved): %v", err)
	}
	if len(resolvedInteractions) != 1 || resolvedInteractions[0].State != coreagent.InteractionStateResolved || resolvedInteractions[0].Resolution["approved"] != true {
		t.Fatalf("resolved interactions = %#v, want one resolved interaction", resolvedInteractions)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.createTurnRequests) != 1 || len(provider.createTurnRequests[0].Tools) != 0 || len(provider.createTurnRequests[0].Metadata) != 0 {
		t.Fatalf("create turn requests = %#v, want plain turn without tools or metadata", provider.createTurnRequests)
	}
}

func TestBootstrapAgentManagerResolvesProviderOwnedInteractions(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var provider *callbackAgentProvider
	factories := validFactories()
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	p := &principal.Principal{
		SubjectID: "system:config",
	}
	startCtx := principal.WithPrincipal(context.Background(), p)
	session, err := result.AgentManager.CreateSession(startCtx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		Messages: []coreagent.Message{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}
	if turn == nil || turn.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("turn = %#v, want waiting_for_input", turn)
	}

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, turn.ID)
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interactions = %#v, want one pending interaction", interactions)
	}

	provider.resolveInteractionHook = func(ctx context.Context, req coreagent.ResolveInteractionRequest) error {
		current, err := provider.GetInteraction(ctx, coreagent.GetInteractionRequest{InteractionID: req.InteractionID})
		if err != nil {
			return err
		}
		if current.State != coreagent.InteractionStatePending || current.TurnID != turn.ID {
			return fmt.Errorf("interaction during manager resolve = %#v, want pending %q", current, turn.ID)
		}
		return nil
	}

	resolved, err := result.AgentManager.ResolveInteraction(startCtx, p, turn.ID, interactions[0].ID, map[string]any{
		"approved": true,
	})
	if err != nil {
		t.Fatalf("AgentManager.ResolveInteraction: %v", err)
	}
	if resolved == nil || resolved.State != coreagent.InteractionStateResolved || resolved.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolved)
	}

	resolvedInteractions, err := result.AgentManager.ListInteractions(startCtx, p, turn.ID)
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions(resolved): %v", err)
	}
	if len(resolvedInteractions) != 1 || resolvedInteractions[0].State != coreagent.InteractionStateResolved || resolvedInteractions[0].Resolution["approved"] != true {
		t.Fatalf("resolved interactions = %#v, want one resolved interaction", resolvedInteractions)
	}
}

func TestBootstrapAgentManagerResolveInteractionReturnsNotFoundWhenProviderInteractionDisappears(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var provider *callbackAgentProvider
	factories := validFactories()
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	p := &principal.Principal{
		SubjectID: "system:config",
	}
	startCtx := principal.WithPrincipal(context.Background(), p)
	session, err := result.AgentManager.CreateSession(startCtx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		Messages: []coreagent.Message{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}
	if turn == nil || turn.Status != coreagent.ExecutionStatusWaitingForInput {
		t.Fatalf("turn = %#v, want waiting_for_input", turn)
	}

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, turn.ID)
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interactions = %#v, want one pending interaction", interactions)
	}

	provider.resolveInteractionHook = func(context.Context, coreagent.ResolveInteractionRequest) error {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		delete(provider.interactions, interactions[0].ID)
		return nil
	}

	_, err = result.AgentManager.ResolveInteraction(startCtx, p, turn.ID, interactions[0].ID, map[string]any{
		"approved": true,
	})
	if !errors.Is(err, agentmanager.ErrAgentInteractionNotFound) {
		t.Fatalf("ResolveInteraction error = %v, want ErrAgentInteractionNotFound", err)
	}
}

func TestBootstrapAgentManagerResolveInteractionReturnsNotFoundOnProviderInteractionIDMismatch(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var provider *callbackAgentProvider
	factories := validFactories()
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	p := &principal.Principal{SubjectID: "system:config"}
	startCtx := principal.WithPrincipal(context.Background(), p)
	session, err := result.AgentManager.CreateSession(startCtx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		Messages: []coreagent.Message{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, turn.ID)
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %#v, want one interaction", interactions)
	}

	provider.resolveInteractionHook = func(context.Context, coreagent.ResolveInteractionRequest) error {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		current := provider.interactions[interactions[0].ID]
		if current == nil {
			return fmt.Errorf("interaction %q not found", interactions[0].ID)
		}
		current.ID = "interaction-mismatch"
		return nil
	}

	_, err = result.AgentManager.ResolveInteraction(startCtx, p, turn.ID, interactions[0].ID, map[string]any{
		"approved": true,
	})
	if !errors.Is(err, agentmanager.ErrAgentInteractionNotFound) {
		t.Fatalf("ResolveInteraction error = %v, want ErrAgentInteractionNotFound", err)
	}
}

func TestBootstrapAgentManagerListInteractionsRejectsMissingSessionID(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var provider *callbackAgentProvider
	factories := validFactories()
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	p := &principal.Principal{SubjectID: "system:config"}
	startCtx := principal.WithPrincipal(context.Background(), p)
	session, err := result.AgentManager.CreateSession(startCtx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		Messages: []coreagent.Message{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}

	provider.mu.Lock()
	for _, interaction := range provider.interactions {
		interaction.SessionID = ""
	}
	provider.mu.Unlock()

	if _, err := result.AgentManager.ListInteractions(startCtx, p, turn.ID); err == nil {
		t.Fatal("ListInteractions error = nil, want missing session id failure")
	} else if !strings.Contains(err.Error(), `for session "", want "`+session.ID+`"`) {
		t.Fatalf("ListInteractions error = %v, want missing session id failure", err)
	}
}

func TestBootstrapAgentManagerResolveInteractionRejectsMissingSessionID(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var provider *callbackAgentProvider
	factories := validFactories()
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		started, err := runtimehost.StartHostServices(hostServices)
		if err != nil {
			return nil, err
		}
		value, err := newCallbackAgentProvider(started)
		if err != nil {
			_ = started.Close()
			return nil, err
		}
		provider = value
		return value, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	p := &principal.Principal{SubjectID: "system:config"}
	startCtx := principal.WithPrincipal(context.Background(), p)
	session, err := result.AgentManager.CreateSession(startCtx, p, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		Messages: []coreagent.Message{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, turn.ID)
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %#v, want one interaction", interactions)
	}

	provider.resolveInteractionHook = func(context.Context, coreagent.ResolveInteractionRequest) error {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		current := provider.interactions[interactions[0].ID]
		if current == nil {
			return fmt.Errorf("interaction %q not found", interactions[0].ID)
		}
		current.SessionID = ""
		return nil
	}

	if _, err := result.AgentManager.ResolveInteraction(startCtx, p, turn.ID, interactions[0].ID, map[string]any{
		"approved": true,
	}); err == nil {
		t.Fatal("ResolveInteraction error = nil, want missing session id failure")
	} else if !strings.Contains(err.Error(), `without session id`) {
		t.Fatalf("ResolveInteraction error = %v, want missing session id failure", err)
	}
}

func TestBootstrapAgentManagerIdempotentTurnReplayRequiresCurrentToolAccess(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	factories := validFactories()
	factories.Builtins = append(factories.Builtins, &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{{
				ID:     "sync",
				Method: http.MethodPost,
			}},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: http.StatusAccepted}, nil
		},
	})

	provider := newRecordingAgentProvider()
	factories.Agent = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (coreagent.Provider, error) {
		return provider, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "roadmap",
		Operations: []string{"sync"},
	}, {
		Plugin: "managed",
	}})
	full := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	fullCtx := principal.WithPrincipal(context.Background(), full)

	session, err := result.AgentManager.CreateSession(fullCtx, full, coreagent.ManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}

	first, err := result.AgentManager.CreateTurn(fullCtx, full, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "same-run",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "sync it"}},
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "roadmap",
			Operation: "sync",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn(first): %v", err)
	}
	if first == nil {
		t.Fatalf("AgentManager.CreateTurn(first) returned nil turn: %#v", first)
	}

	restricted := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: principal.CompilePermissions([]core.AccessPermission{{Plugin: "managed"}}),
		Scopes:           []string{"managed"},
	}
	restrictedCtx := principal.WithPrincipal(context.Background(), restricted)

	_, err = result.AgentManager.CreateTurn(restrictedCtx, restricted, coreagent.ManagerCreateTurnRequest{
		SessionID:      session.ID,
		IdempotencyKey: "same-run",
		Model:          "gpt-test",
		Messages:       []coreagent.Message{{Role: "user", Text: "sync it"}},
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "roadmap",
			Operation: "sync",
		}},
	})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("AgentManager.CreateTurn(replay) error = %v, want %v", err, invocation.ErrAuthorizationDenied)
	}

	provider.mu.Lock()
	createTurnCount := len(provider.createTurnRequests)
	provider.mu.Unlock()
	if createTurnCount != 1 {
		t.Fatalf("CreateTurn count = %d, want 1", createTurnCount)
	}
}

func TestBootstrapPassesIndexedDBHostSocketToWorkflowProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.HostIndexedDBBindingConfig{
				Provider:     "workflow_state",
				DB:           "workflow",
				ObjectStores: []string{"workflow_schedules", "workflow_runs"},
			},
		},
	}

	factories := validFactories()
	hostEnvs := map[string][]string{}
	factories.Workflow = func(_ context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		if runtime.Name != name {
			return nil, fmt.Errorf("workflow runtime name = %q, want %q", runtime.Name, name)
		}
		envs := make([]string, 0, len(hostServices))
		for _, hostService := range hostServices {
			envs = append(envs, hostService.EnvVar)
		}
		hostEnvs[name] = envs
		return &stubWorkflowProvider{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	got := hostEnvs["basic"]
	if len(got) != 2 {
		t.Fatalf("workflow host services = %v, want 2 entries", got)
	}
	if got[0] != workflowservice.DefaultHostSocketEnv {
		t.Fatalf("workflow host env = %q, want %q", got[0], workflowservice.DefaultHostSocketEnv)
	}
	if got[1] != indexeddbservice.DefaultSocketEnv {
		t.Fatalf("workflow indexeddb env = %q, want %q", got[1], indexeddbservice.DefaultSocketEnv)
	}
}

func TestBootstrapPassesIndexedDBHostSocketToAgentProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["agent_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
		Config: mustYAMLNode(t, map[string]any{
			"dsn": map[string]any{
				"secret": map[string]any{
					"provider": "secrets",
					"name":     "agent-state-dsn",
				},
			},
		}),
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"simple": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.HostIndexedDBBindingConfig{
				Provider:     "agent_state",
				DB:           "agent_simple",
				ObjectStores: []string{"runs"},
			},
		},
	}

	factories := validFactories()
	var (
		boundDB      *trackedIndexedDB
		hostServices []runtimehost.HostService
	)
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		boundDB = &trackedIndexedDB{StubIndexedDB: &coretesting.StubIndexedDB{}}
		return boundDB, nil
	}
	factories.Agent = func(_ context.Context, _ string, _ yaml.Node, services []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		hostServices = append([]runtimehost.HostService(nil), services...)
		return newRecordingAgentProvider(), nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(hostServices) != 2 {
		t.Fatalf("agent host services = %d, want 2", len(hostServices))
	}
	if hostServices[0].EnvVar != agentservice.DefaultHostSocketEnv {
		t.Fatalf("agent host env = %q, want %q", hostServices[0].EnvVar, agentservice.DefaultHostSocketEnv)
	}
	if hostServices[1].EnvVar != indexeddbservice.DefaultSocketEnv {
		t.Fatalf("agent indexeddb env = %q, want %q", hostServices[1].EnvVar, indexeddbservice.DefaultSocketEnv)
	}

	withIndexedDBHostClient(t, hostServices[1], func(client proto.IndexedDBClient) {
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "runs",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(runs): %v", err)
		}
		record, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": "run-1", "status": "running"})
		if err != nil {
			t.Fatalf("RecordToProto: %v", err)
		}
		if _, err := client.Put(context.Background(), &proto.RecordRequest{
			Store:  "runs",
			Record: record,
		}); err != nil {
			t.Fatalf("Put(runs): %v", err)
		}
		resp, err := client.Get(context.Background(), &proto.ObjectStoreRequest{
			Store: "runs",
			Id:    "run-1",
		})
		if err != nil {
			t.Fatalf("Get(runs): %v", err)
		}
		got, err := indexeddbcodec.RecordFromProto(resp.GetRecord())
		if err != nil {
			t.Fatalf("RecordFromProto: %v", err)
		}
		if got["status"] != "running" {
			t.Fatalf("status = %#v, want %q", got["status"], "running")
		}

		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "sessions",
			Schema: &proto.ObjectStoreSchema{},
		}); err == nil {
			t.Fatal("CreateObjectStore(sessions) succeeded, want allowlist failure")
		}
	})

	if _, err := boundDB.ObjectStore("runs").Get(context.Background(), "run-1"); err != nil {
		t.Fatalf("logical backing store should contain run: %v", err)
	}
}

func TestBootstrapProvisionsAgentRouteStoresOnSelectedIndexedDB(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	factories := validFactories()
	selectedDB := &trackedIndexedDB{StubIndexedDB: &coretesting.StubIndexedDB{}}
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		return selectedDB, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()

	if !selectedDB.HasObjectStore("agent_session_routes") {
		t.Fatal("selected indexeddb missing agent_session_routes store")
	}
	if !selectedDB.HasObjectStore("agent_turn_routes") {
		t.Fatal("selected indexeddb missing agent_turn_routes store")
	}
}

func TestBootstrapPassesIndexedDBHostSocketsToAuthorizationProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["archive"] = &config.ProviderEntry{
		Source: config.ProviderSource{Path: "./providers/datastore/archive"},
	}
	cfg.Providers.Authorization = map[string]*config.ProviderEntry{
		"indexeddb": {
			Source: config.ProviderSource{Path: "stub"},
			Config: mustYAMLNode(t, map[string]any{"indexeddb": "test"}),
		},
	}
	cfg.Server.Providers.Authorization = "indexeddb"

	factories := validFactories()
	var hostServices []runtimehost.HostService
	factories.Authorization = func(_ yaml.Node, services []runtimehost.HostService, _ bootstrap.Deps) (core.AuthorizationProvider, error) {
		hostServices = append([]runtimehost.HostService(nil), services...)
		return &stubAuthorizationProvider{name: "test-authorization"}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(hostServices) != 3 {
		t.Fatalf("authorization host services = %d, want 3", len(hostServices))
	}
	if hostServices[0].EnvVar != indexeddbservice.DefaultSocketEnv {
		t.Fatalf("authorization default indexeddb env = %q, want %q", hostServices[0].EnvVar, indexeddbservice.DefaultSocketEnv)
	}
	wantNamed := []string{
		indexeddbservice.SocketEnv("archive"),
		indexeddbservice.SocketEnv("test"),
	}
	for i, want := range wantNamed {
		if got := hostServices[i+1].EnvVar; got != want {
			t.Fatalf("authorization indexeddb env[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestBootstrapClosesWorkflowIndexedDBAndAppliesScopedConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
		Config: mustYAMLNode(t, map[string]any{
			"dsn":          "sqlite://workflow.db",
			"table_prefix": "host_",
			"prefix":       "host_",
			"schema":       "should_be_removed",
		}),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.HostIndexedDBBindingConfig{
				Provider:     "workflow_state",
				DB:           "workflow",
				ObjectStores: []string{"workflow_runs"},
			},
		},
	}

	factories := validFactories()
	var (
		workflowCloseCount atomic.Int32
		captured           map[string]any
	)
	factories.IndexedDB = func(node yaml.Node) (indexeddb.IndexedDB, error) {
		var decoded struct {
			Config map[string]any `yaml:"config"`
		}
		if err := node.Decode(&decoded); err != nil {
			return nil, err
		}
		counter := (*atomic.Int32)(nil)
		if decoded.Config["table_prefix"] == "workflow_" && decoded.Config["prefix"] == "workflow_" {
			counter = &workflowCloseCount
			captured = decoded.Config
		}
		return &trackedIndexedDB{
			StubIndexedDB: &coretesting.StubIndexedDB{},
			closed:        counter,
		}, nil
	}
	factories.Workflow = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
		return &stubWorkflowProvider{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	if got := captured["table_prefix"]; got != "workflow_" {
		t.Fatalf("table_prefix = %#v, want %q", got, "workflow_")
	}
	if got := captured["prefix"]; got != "workflow_" {
		t.Fatalf("prefix = %#v, want %q", got, "workflow_")
	}
	if _, ok := captured["schema"]; ok {
		t.Fatalf("schema should be removed, got %#v", captured["schema"])
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("result.Close: %v", err)
	}
	if got := workflowCloseCount.Load(); got != 1 {
		t.Fatalf("workflowCloseCount after workflow shutdown = %d, want 1", got)
	}
}

func TestBootstrapRoutesExternalCredentialsIndexedDBHostServices(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Server.Providers.IndexedDB = "test"
	cfg.Providers.IndexedDB["archive"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/archive/provider-release.yaml"),
		Config: mustYAMLNode(t, map[string]any{"dsn": "sqlite://archive.db"}),
	}
	cfg.Providers.IndexedDB["test"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/test/provider-release.yaml"),
		Config: mustYAMLNode(t, map[string]any{"dsn": "sqlite://test.db"}),
	}
	cfg.Server.Providers.ExternalCredentials = "runner"
	cfg.Providers.ExternalCredentials = map[string]*config.ProviderEntry{
		"runner": {
			Source: config.NewMetadataSource("https://example.invalid/external-credentials/default/provider-release.yaml"),
			Config: mustYAMLNode(t, map[string]any{"indexeddb": "test"}),
		},
	}

	factories := validFactories()
	var hostServices []runtimehost.HostService
	factories.ExternalCredentials = func(_ context.Context, _ string, _ yaml.Node, services []runtimehost.HostService, _ bootstrap.Deps) (core.ExternalCredentialProvider, error) {
		hostServices = append([]runtimehost.HostService(nil), services...)
		return coretesting.NewStubExternalCredentialProvider(), nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(hostServices) != 3 {
		t.Fatalf("external credentials host services = %d, want 3", len(hostServices))
	}
	if hostServices[0].EnvVar != indexeddbservice.DefaultSocketEnv {
		t.Fatalf("external credentials default indexeddb env = %q, want %q", hostServices[0].EnvVar, indexeddbservice.DefaultSocketEnv)
	}
	wantNamed := []string{
		indexeddbservice.SocketEnv("archive"),
		indexeddbservice.SocketEnv("test"),
	}
	for i, want := range wantNamed {
		if got := hostServices[i+1].EnvVar; got != want {
			t.Fatalf("external credentials indexeddb env[%d] = %q, want %q", i, got, want)
		}
	}

	withIndexedDBHostClient(t, hostServices[0], func(client proto.IndexedDBClient) {
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "external_credentials",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(external_credentials): %v", err)
		}
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "plugin_credentials",
			Schema: &proto.ObjectStoreSchema{},
		}); err == nil {
			t.Fatal("CreateObjectStore(plugin_credentials) succeeded, want allowlist failure")
		}
	})
}

func TestBootstrapRoutesWorkflowIndexedDBHostServices(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.1/provider-release.yaml"),
		Config: mustYAMLNode(t, map[string]any{"bucket": "workflow-state"}),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.HostIndexedDBBindingConfig{
				Provider:     "workflow_state",
				DB:           "workflow",
				ObjectStores: []string{"workflow_runs"},
			},
		},
	}

	factories := validFactories()
	var (
		closeCount atomic.Int32
		boundDB    *trackedIndexedDB
		hostEnv    []runtimehost.HostService
	)
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		boundDB = &trackedIndexedDB{
			StubIndexedDB: &coretesting.StubIndexedDB{},
			closed:        &closeCount,
		}
		return boundDB, nil
	}
	workflowProvider := &recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		hostEnv = append([]runtimehost.HostService(nil), hostServices...)
		return workflowProvider, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(hostEnv) != 2 {
		t.Fatalf("workflow host services = %d, want 2", len(hostEnv))
	}

	var indexedDBHost runtimehost.HostService
	for _, hostService := range hostEnv {
		if hostService.EnvVar == indexeddbservice.DefaultSocketEnv {
			indexedDBHost = hostService
			break
		}
	}
	if indexedDBHost.EnvVar == "" {
		t.Fatal("missing workflow indexeddb host service")
	}

	provider, err := result.WorkflowControl.ResolveProvider("basic")
	if err != nil {
		t.Fatalf("ResolveProvider(basic): %v", err)
	}
	executionRefs, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		t.Fatal("workflow provider with indexeddb cleanup does not preserve execution reference store")
	}
	target := coreWorkflowPluginTarget("roadmap", "sync")
	if _, err := executionRefs.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-test:ref-test",
		ProviderName: "basic",
		Target:       target,
		SubjectID:    "user:ada",
	}); err != nil {
		t.Fatalf("PutExecutionReference: %v", err)
	}
	if _, err := workflowProvider.GetExecutionReference(context.Background(), "workflow_schedule:sched-test:ref-test"); err != nil {
		t.Fatalf("underlying workflow provider missing execution ref: %v", err)
	}

	withIndexedDBHostClient(t, indexedDBHost, func(client proto.IndexedDBClient) {
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "workflow_runs",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(workflow_runs): %v", err)
		}
		record, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": "run-1", "status": "pending"})
		if err != nil {
			t.Fatalf("RecordToProto: %v", err)
		}
		if _, err := client.Put(context.Background(), &proto.RecordRequest{
			Store:  "workflow_runs",
			Record: record,
		}); err != nil {
			t.Fatalf("Put(workflow_runs): %v", err)
		}
		resp, err := client.Get(context.Background(), &proto.ObjectStoreRequest{
			Store: "workflow_runs",
			Id:    "run-1",
		})
		if err != nil {
			t.Fatalf("Get(workflow_runs): %v", err)
		}
		got, err := indexeddbcodec.RecordFromProto(resp.GetRecord())
		if err != nil {
			t.Fatalf("RecordFromProto: %v", err)
		}
		if got["status"] != "pending" {
			t.Fatalf("status = %#v, want %q", got["status"], "pending")
		}

		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "workflow_schedules",
			Schema: &proto.ObjectStoreSchema{},
		}); err == nil {
			t.Fatal("CreateObjectStore(workflow_schedules) succeeded, want allowlist failure")
		}
	})

	if _, err := boundDB.ObjectStore("workflow_runs").Get(context.Background(), "run-1"); err != nil {
		t.Fatalf("logical backing store should contain run: %v", err)
	}
}

func TestBootstrapAppliesConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModePlatform,
		Auth: &config.ConnectionAuthDef{
			Type:  providermanifestv1.AuthTypeBearer,
			Token: "platform-token",
		},
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: "https://slack.example.invalid",
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "conversations.list", Method: http.MethodPost, Path: "/conversations.list"},
							{Name: "conversations.history", Method: http.MethodPost, Path: "/conversations.history"},
						},
					},
				},
			},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "America/New_York",
				Operation: "sync",
				Input: map[string]any{
					"source": "yaml",
				},
			},
		},
	})
	nightly := cfg.Workflows.Schedules["nightly_sync"]
	nightly.Permissions = []core.AccessPermission{{
		Plugin: "slack",
		Operations: []string{
			"conversations.list",
			"conversations.history",
		},
	}}
	cfg.Workflows.Schedules["nightly_sync"] = nightly
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	recorder := recorders["temporal"]
	if recorder == nil {
		t.Fatal("missing workflow recorder for temporal")
		return
	}
	if len(recorder.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(recorder.upsertedSchedules))
	}
	got := recorder.upsertedSchedules[0]
	if got.ScheduleID != workflowConfigScheduleID("nightly_sync") {
		t.Fatalf("schedule id = %q", got.ScheduleID)
	}
	if got.Cron != "0 2 * * *" || got.Timezone != "America/New_York" {
		t.Fatalf("schedule timing = %#v", got)
	}
	gotPlugin := requireCoreWorkflowPluginTarget(t, got.Target)
	if gotPlugin.PluginName != "roadmap" || gotPlugin.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if gotPlugin.Input["source"] != "yaml" {
		t.Fatalf("target input = %#v", gotPlugin.Input)
	}
	if got.RequestedBy.SubjectID != "system:config" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
	if strings.TrimSpace(got.ExecutionRef) == "" {
		t.Fatal("execution ref = empty")
	}
	ref, err := recorder.GetExecutionReference(context.Background(), got.ExecutionRef)
	if err != nil {
		t.Fatalf("Get execution ref: %v", err)
	}
	if ref.SubjectID != "system:config" {
		t.Fatalf("subjectID = %q, want %q", ref.SubjectID, "system:config")
	}
	wantPermissions := []core.AccessPermission{
		{Plugin: "roadmap", Operations: []string{"sync"}},
		{Plugin: "slack", Operations: []string{"conversations.list", "conversations.history"}},
	}
	if !reflect.DeepEqual(ref.Permissions, wantPermissions) {
		t.Fatalf("permissions = %#v, want %#v", ref.Permissions, wantPermissions)
	}
}

func TestBootstrapRecreatesConfiguredWorkflowScheduleExecutionRefWhenPermissionsChange(t *testing.T) {
	t.Parallel()

	factories := validFactories()
	recorders := []*recordingWorkflowProvider{}
	sharedSchedules := map[string]*coreworkflow.Schedule{}
	sharedExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			schedules:     sharedSchedules,
			executionRefs: sharedExecutionRefs,
		}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	<-result.ProvidersReady
	if len(recorders) != 1 || len(recorders[0].upsertedSchedules) != 1 {
		t.Fatalf("initial upserts = %#v", recorders)
	}
	initialExecutionRef := recorders[0].upsertedSchedules[0].ExecutionRef
	if strings.TrimSpace(initialExecutionRef) == "" {
		t.Fatal("initial execution ref = empty")
	}
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModePlatform,
		Auth: &config.ConnectionAuthDef{
			Type:  providermanifestv1.AuthTypeBearer,
			Token: "platform-token",
		},
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: "https://slack.example.invalid",
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "conversations.history", Method: http.MethodPost, Path: "/conversations.history"},
						},
					},
				},
			},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	nightly := cfg.Workflows.Schedules["nightly_sync"]
	nightly.Permissions = []core.AccessPermission{{
		Plugin:     "slack",
		Operations: []string{"conversations.history"},
	}}
	cfg.Workflows.Schedules["nightly_sync"] = nightly

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap with permissions: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders) != 2 || len(recorders[1].upsertedSchedules) != 1 {
		t.Fatalf("second upserts = %#v", recorders)
	}
	nextExecutionRef := recorders[1].upsertedSchedules[0].ExecutionRef
	if nextExecutionRef == initialExecutionRef {
		t.Fatalf("execution ref = %q, want recreated after permissions change", nextExecutionRef)
	}
	oldRef, err := recorders[1].GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get old execution ref: %v", err)
	}
	if oldRef.RevokedAt == nil || oldRef.RevokedAt.IsZero() {
		t.Fatalf("old execution ref revokedAt = %#v, want set", oldRef.RevokedAt)
	}
	nextRef, err := recorders[1].GetExecutionReference(context.Background(), nextExecutionRef)
	if err != nil {
		t.Fatalf("Get new execution ref: %v", err)
	}
	wantPermissions := []core.AccessPermission{
		{Plugin: "roadmap", Operations: []string{"sync"}},
		{Plugin: "slack", Operations: []string{"conversations.history"}},
	}
	if !reflect.DeepEqual(nextRef.Permissions, wantPermissions) {
		t.Fatalf("permissions = %#v, want %#v", nextRef.Permissions, wantPermissions)
	}
}

func TestBootstrapKeepsExistingConfiguredWorkflowScheduleWhenExecutionRefRefreshFails(t *testing.T) {
	t.Parallel()

	factories := validFactories()
	recorders := []*recordingWorkflowProvider{}
	sharedSchedules := map[string]*coreworkflow.Schedule{}
	sharedExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	failExecutionRefWrites := false
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			schedules:     sharedSchedules,
			executionRefs: sharedExecutionRefs,
		}
		if failExecutionRefWrites {
			recorder.putExecutionReferenceErr = errors.New("execution ref index unavailable")
		}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	<-result.ProvidersReady
	if len(recorders) != 1 || len(recorders[0].upsertedSchedules) != 1 {
		t.Fatalf("initial upserts = %#v", recorders)
	}
	initialExecutionRef := recorders[0].upsertedSchedules[0].ExecutionRef
	if strings.TrimSpace(initialExecutionRef) == "" {
		t.Fatal("initial execution ref = empty")
	}
	_ = result.Close(context.Background())
	existingSchedule := sharedSchedules[workflowConfigScheduleID("nightly_sync")]
	if existingSchedule == nil || existingSchedule.Target.Plugin == nil {
		t.Fatalf("existing schedule target = %#v", existingSchedule)
	}
	existingSchedule.Target.Plugin.Input = map[string]any{}

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModePlatform,
		Auth: &config.ConnectionAuthDef{
			Type:  providermanifestv1.AuthTypeBearer,
			Token: "platform-token",
		},
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: "https://slack.example.invalid",
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "conversations.history", Method: http.MethodPost, Path: "/conversations.history"},
						},
					},
				},
			},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	nightly := cfg.Workflows.Schedules["nightly_sync"]
	nightly.Permissions = []core.AccessPermission{{
		Plugin:     "slack",
		Operations: []string{"conversations.history"},
	}}
	cfg.Workflows.Schedules["nightly_sync"] = nightly
	failExecutionRefWrites = true

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap with stale execution ref index: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders) != 2 {
		t.Fatalf("recorders = %d, want 2", len(recorders))
	}
	if len(recorders[1].upsertedSchedules) != 0 {
		t.Fatalf("second upserts = %d, want 0", len(recorders[1].upsertedSchedules))
	}
	if _, err := recorders[1].GetExecutionReference(context.Background(), initialExecutionRef); err != nil {
		t.Fatalf("existing execution ref should remain available: %v", err)
	}
}

func TestValidateDoesNotApplyConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Paused:    true,
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	recorder := recorders["temporal"]
	if recorder == nil {
		t.Fatal("missing workflow recorder for temporal")
		return
	}
	if len(recorder.upsertedSchedules) != 0 {
		t.Fatalf("upserted schedules = %d, want 0", len(recorder.upsertedSchedules))
	}
	if len(recorder.deletedSchedules) != 0 {
		t.Fatalf("deleted schedules = %d, want 0", len(recorder.deletedSchedules))
	}
}

func TestBootstrapRejectsConfiguredWorkflowSchedulesForUserCredentialedPlugins(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})

	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return &recordingWorkflowProvider{}, nil
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected Bootstrap to reject user-credentialed config-managed schedules")
	}
	if !strings.Contains(err.Error(), `config-managed workflows do not support user-credentialed plugin "roadmap"`) {
		t.Fatalf("Bootstrap error = %v", err)
	}
}

func TestBootstrapAppliesConfiguredWorkflowSchedulesForPlatformConnectionOnUserDefaultPlugin(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
	cfg.Plugins["roadmap"].Connections = map[string]*config.ConnectionDef{
		"bot": {
			Mode: providermanifestv1.ConnectionModePlatform,
			Auth: config.ConnectionAuthDef{
				Type:  providermanifestv1.AuthTypeBearer,
				Token: "platform-token",
			},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	nightly := cfg.Workflows.Schedules["nightly_sync"]
	nightly.Target.Plugin.Connection = "bot"
	cfg.Workflows.Schedules["nightly_sync"] = nightly

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	recorder := recorders["temporal"]
	if recorder == nil || len(recorder.upsertedSchedules) != 1 {
		t.Fatalf("recorded schedules = %#v", recorders)
	}
	gotPlugin := requireCoreWorkflowPluginTarget(t, recorder.upsertedSchedules[0].Target)
	if gotPlugin.Connection != "bot" {
		t.Fatalf("target connection = %q, want bot", gotPlugin.Connection)
	}
}

func TestBootstrapAllowsConfiguredWorkflowSchedulePermissionScopesForUserCredentialedPlugins(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModeUser,
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: "https://slack.example.invalid",
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "conversations.list", Method: http.MethodPost, Path: "/conversations.list"},
						},
					},
				},
			},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	nightly := cfg.Workflows.Schedules["nightly_sync"]
	nightly.Permissions = []core.AccessPermission{{
		Plugin:     "slack",
		Operations: []string{"conversations.list"},
	}}
	cfg.Workflows.Schedules["nightly_sync"] = nightly

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	recorder := recorders["temporal"]
	if recorder == nil || len(recorder.upsertedSchedules) != 1 {
		t.Fatalf("recorded schedules = %#v", recorders)
	}
	ref, err := recorder.GetExecutionReference(context.Background(), recorder.upsertedSchedules[0].ExecutionRef)
	if err != nil {
		t.Fatalf("Get execution ref: %v", err)
	}
	wantPermissions := []core.AccessPermission{
		{Plugin: "roadmap", Operations: []string{"sync"}},
		{Plugin: "slack", Operations: []string{"conversations.list"}},
	}
	if !reflect.DeepEqual(ref.Permissions, wantPermissions) {
		t.Fatalf("permissions = %#v, want %#v", ref.Permissions, wantPermissions)
	}
}

func TestBootstrapDeletesRemovedConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := []*recordingWorkflowProvider{}
	sharedSchedules := map[string]*coreworkflow.Schedule{}
	sharedExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			schedules:     sharedSchedules,
			executionRefs: sharedExecutionRefs,
		}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(recorders) != 1 || len(recorders[0].upsertedSchedules) != 1 {
		t.Fatalf("initial upserts = %#v", recorders)
	}
	initialExecutionRef := recorders[0].upsertedSchedules[0].ExecutionRef
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove schedule: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders) != 2 {
		t.Fatalf("recorders = %d, want 2", len(recorders))
	}
	staleID := workflowConfigScheduleID("nightly_sync")
	recorder := recorders[1]
	if len(recorder.deletedSchedules) != 1 {
		t.Fatalf("deleted schedules = %d, want 1", len(recorder.deletedSchedules))
	}
	if recorder.deletedSchedules[0].ScheduleID != staleID {
		t.Fatalf("delete request = %#v", recorder.deletedSchedules[0])
	}
	if len(recorder.upsertedSchedules) != 0 {
		t.Fatalf("upserted schedules = %d, want 0", len(recorder.upsertedSchedules))
	}
	ref, err := recorder.GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get revoked execution ref: %v", err)
	}
	if ref.RevokedAt == nil || ref.RevokedAt.IsZero() {
		t.Fatalf("revokedAt = %#v, want set", ref.RevokedAt)
	}
}

func TestBootstrapSkipsRemovedConfiguredWorkflowScheduleCleanupWhenListFails(t *testing.T) {
	t.Parallel()

	factories := validFactories()
	recorders := []*recordingWorkflowProvider{}
	sharedSchedules := map[string]*coreworkflow.Schedule{}
	sharedExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	failListSchedules := false
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			schedules:     sharedSchedules,
			executionRefs: sharedExecutionRefs,
		}
		if failListSchedules {
			recorder.listSchedulesErr = errors.New("schedule index unavailable")
		}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	<-result.ProvidersReady
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{Provider: "temporal"})
	failListSchedules = true

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap with schedule cleanup list failure: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders) != 2 {
		t.Fatalf("recorders = %d, want 2", len(recorders))
	}
	if len(recorders[1].deletedSchedules) != 0 {
		t.Fatalf("deleted schedules = %d, want 0", len(recorders[1].deletedSchedules))
	}
}

func TestBootstrapIgnoresUserSchedulesThatOnlyShareCfgPrefix(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			listedSchedules: []*coreworkflow.Schedule{{ID: "cfg_backup"}},
		}
		recorders[name] = recorder
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	recorder := recorders["temporal"]
	if recorder == nil {
		t.Fatal("missing workflow recorder for temporal")
		return
	}
	if len(recorder.deletedSchedules) != 0 {
		t.Fatalf("deleted schedules = %d, want 0", len(recorder.deletedSchedules))
	}
}

func TestBootstrapMovesConfiguredWorkflowSchedulesToNewProvider(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := map[string][]*recordingWorkflowProvider{}
	sharedSchedules := map[string]map[string]*coreworkflow.Schedule{}
	sharedExecutionRefs := map[string]map[string]*coreworkflow.ExecutionReference{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if sharedSchedules[name] == nil {
			sharedSchedules[name] = map[string]*coreworkflow.Schedule{}
		}
		if sharedExecutionRefs[name] == nil {
			sharedExecutionRefs[name] = map[string]*coreworkflow.ExecutionReference{}
		}
		recorder := &recordingWorkflowProvider{
			schedules:     sharedSchedules[name],
			executionRefs: sharedExecutionRefs[name],
		}
		recorders[name] = append(recorders[name], recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(recorders["temporal"]) != 1 || len(recorders["temporal"][0].upsertedSchedules) != 1 {
		t.Fatalf("initial temporal recorders = %#v", recorders["temporal"])
	}
	initialExecutionRef := recorders["temporal"][0].upsertedSchedules[0].ExecutionRef
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "backup",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap move provider: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders["temporal"]) != 2 || len(recorders["backup"]) != 2 {
		t.Fatalf("recorders = %#v", recorders)
	}
	if len(recorders["temporal"][1].deletedSchedules) != 1 {
		t.Fatalf("temporal deleted schedules = %d, want 1", len(recorders["temporal"][1].deletedSchedules))
	}
	if len(recorders["backup"][1].upsertedSchedules) != 1 {
		t.Fatalf("backup upserted schedules = %d, want 1", len(recorders["backup"][1].upsertedSchedules))
	}
	backupExecutionRef := recorders["backup"][1].upsertedSchedules[0].ExecutionRef
	oldRef, err := recorders["temporal"][1].GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get initial execution ref: %v", err)
	}
	if oldRef.RevokedAt == nil || oldRef.RevokedAt.IsZero() {
		t.Fatalf("initial revokedAt = %#v, want set", oldRef.RevokedAt)
	}
	newRef, err := recorders["backup"][1].GetExecutionReference(context.Background(), backupExecutionRef)
	if err != nil {
		t.Fatalf("Get backup execution ref: %v", err)
	}
	if newRef.ProviderName != "backup" {
		t.Fatalf("providerName = %q, want %q", newRef.ProviderName, "backup")
	}
	if newRef.RevokedAt != nil {
		t.Fatalf("backup revokedAt = %#v, want nil", newRef.RevokedAt)
	}
}

func TestBootstrapClosesWorkflowProvidersWhenConfigScheduleReconcileFails(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	closed := &atomic.Bool{}
	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	temporalSchedules := map[string]*coreworkflow.Schedule{}
	temporalExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	temporalStarts := 0
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name == "temporal" {
			temporalStarts++
			provider := &recordingWorkflowProvider{
				schedules:     temporalSchedules,
				executionRefs: temporalExecutionRefs,
				closed:        closed,
			}
			if temporalStarts > 1 {
				provider.deleteScheduleErr = fmt.Errorf("delete boom")
			}
			return provider, nil
		}
		return &recordingWorkflowProvider{closed: closed}, nil
	}

	cfg.Workflows.Schedules = map[string]config.WorkflowScheduleConfig{
		"nightly_sync": {
			Target:   workflowFixtureTarget("roadmap", "sync", nil),
			Cron:     "0 2 * * *",
			Timezone: "UTC",
		},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "backup",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	_, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), "delete boom") {
		t.Fatalf("Bootstrap error = %v, want delete failure", err)
	}
	if !closed.Load() {
		t.Fatal("workflow provider was not closed after reconcile failure")
	}
}

func TestBootstrapDoesNotApplyConfiguredWorkflowSchedulesWhenAuditBuildFails(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Providers.Audit = map[string]*config.ProviderEntry{
		"default": {Source: config.ProviderSource{Builtin: "test-audit"}},
	}

	factories := validFactories()
	recorder := &recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return recorder, nil
	}
	factories.Audit = func(context.Context, config.ProviderEntry, core.TelemetryProvider) (core.AuditSink, func(context.Context) error, error) {
		return nil, nil, fmt.Errorf("audit boom")
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), "audit boom") {
		t.Fatalf("Bootstrap error = %v, want audit failure", err)
	}
	if len(recorder.upsertedSchedules) != 0 {
		t.Fatalf("upserted schedules = %d, want 0", len(recorder.upsertedSchedules))
	}
}

func TestBootstrapRejectsExistingUnmanagedWorkflowScheduleID(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	recorder := &recordingWorkflowProvider{
		getSchedule: &coreworkflow.Schedule{
			ID:       workflowConfigScheduleID("nightly_sync"),
			Cron:     "0 2 * * *",
			Timezone: "UTC",
			Target:   coreWorkflowPluginTarget("roadmap", "sync"),
		},
	}
	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return recorder, nil
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing unmanaged schedule id") {
		t.Fatalf("Bootstrap error = %v, want ownership conflict", err)
	}
	if len(recorder.upsertedSchedules) != 0 {
		t.Fatalf("upserted schedules = %d, want 0", len(recorder.upsertedSchedules))
	}
}

func TestBootstrapReusesConfiguredWorkflowExecutionRefAcrossUnchangedBootstrap(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	initialExecutionRef := provider.upsertedSchedules[0].ExecutionRef
	provider.executionRefs[initialExecutionRef].Target.Plugin.Input["limit"] = float64(1)
	_ = result.Close(context.Background())

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap replay: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.upsertedSchedules) != 2 {
		t.Fatalf("upserted schedules = %d, want 2", len(provider.upsertedSchedules))
	}
	reusedExecutionRef := provider.upsertedSchedules[1].ExecutionRef
	if reusedExecutionRef != initialExecutionRef {
		t.Fatalf("execution ref = %q, want reuse of %q", reusedExecutionRef, initialExecutionRef)
	}
	ref, err := provider.GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get execution ref: %v", err)
	}
	if ref.RevokedAt != nil {
		t.Fatalf("revokedAt = %#v, want nil", ref.RevokedAt)
	}
}

func TestBootstrapReplacesUnreadableConfiguredWorkflowExecutionRef(t *testing.T) {
	t.Parallel()

	const staleExecutionRef = "workflow_schedule:stale-ref"

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	provider := &recordingWorkflowProvider{
		schedules: map[string]*coreworkflow.Schedule{
			workflowConfigScheduleID("nightly_sync"): {
				ID:           workflowConfigScheduleID("nightly_sync"),
				Cron:         "0 2 * * *",
				Timezone:     "UTC",
				Target:       coreWorkflowPluginTarget("roadmap", "sync"),
				ExecutionRef: staleExecutionRef,
				CreatedBy: coreworkflow.Actor{
					SubjectID:   "system:config",
					SubjectKind: "system",
					AuthSource:  "config",
				},
			},
		},
		getExecutionReferenceErrs: map[string]error{
			staleExecutionRef: status.Error(codes.Internal, "query temporal index: workflow task failed"),
		},
	}
	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(provider.upsertedSchedules))
	}
	replacementExecutionRef := provider.upsertedSchedules[0].ExecutionRef
	if replacementExecutionRef == "" || replacementExecutionRef == staleExecutionRef {
		t.Fatalf("replacement execution ref = %q, want fresh ref", replacementExecutionRef)
	}
	if _, err := provider.GetExecutionReference(context.Background(), replacementExecutionRef); err != nil {
		t.Fatalf("Get replacement execution ref: %v", err)
	}
}

func TestBootstrapRefreshesConfiguredWorkflowExecutionRefWhenMetadataChanges(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	initialExecutionRef := provider.upsertedSchedules[0].ExecutionRef
	_ = result.Close(context.Background())
	provider.executionRefs[initialExecutionRef].DisplayName = "Stale config name"

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap replay: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.upsertedSchedules) != 2 {
		t.Fatalf("upserted schedules = %d, want 2", len(provider.upsertedSchedules))
	}
	refreshedExecutionRef := provider.upsertedSchedules[1].ExecutionRef
	if refreshedExecutionRef == initialExecutionRef {
		t.Fatalf("execution ref = %q, want refreshed ref after display name drift", refreshedExecutionRef)
	}
	ref, err := provider.GetExecutionReference(context.Background(), refreshedExecutionRef)
	if err != nil {
		t.Fatalf("Get refreshed execution ref: %v", err)
	}
	if ref.DisplayName != "Gestalt config" {
		t.Fatalf("displayName = %q, want refreshed config display name", ref.DisplayName)
	}
	oldRef, err := provider.GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get old execution ref: %v", err)
	}
	if oldRef.RevokedAt == nil || oldRef.RevokedAt.IsZero() {
		t.Fatalf("old revokedAt = %#v, want set", oldRef.RevokedAt)
	}
}

func TestBootstrapIgnoresMissingRemovedConfiguredWorkflowSchedule(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{deleteMissingNotFound: true}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())
	provider.schedules = map[string]*coreworkflow.Schedule{}

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing schedule: %v", err)
	}
	_ = result.Close(context.Background())

	if len(provider.deletedSchedules) != 0 {
		t.Fatalf("deleted schedules = %d, want 0", len(provider.deletedSchedules))
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing schedule replay: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.deletedSchedules) != 0 {
		t.Fatalf("deleted schedules after replay = %d, want 0", len(provider.deletedSchedules))
	}
}

func TestBootstrapIgnoresMissingPreviousScheduleDuringWorkflowProviderMove(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	temporal := &recordingWorkflowProvider{deleteMissingNotFound: true}
	backup := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name == "backup" {
			return backup, nil
		}
		return temporal, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())
	temporal.schedules = map[string]*coreworkflow.Schedule{}

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "backup",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap move provider: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(backup.upsertedSchedules) != 1 {
		t.Fatalf("backup upserted schedules = %d, want 1", len(backup.upsertedSchedules))
	}
	if len(temporal.deletedSchedules) != 0 {
		t.Fatalf("temporal deleted schedules = %d, want 0", len(temporal.deletedSchedules))
	}
}

func TestBootstrapDeletesRemovedConfiguredWorkflowSchedulesWhenProviderDropsExecutionRef(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	initialExecutionRef := provider.upsertedSchedules[0].ExecutionRef
	_ = result.Close(context.Background())

	provider.omitScheduleExecutionRef = true
	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove schedule: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	ref, err := provider.GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get revoked execution ref: %v", err)
	}
	if ref.RevokedAt != nil {
		t.Fatalf("revokedAt = %#v, want nil when provider omits execution_ref", ref.RevokedAt)
	}
}

func TestBootstrapAppliesConfiguredWorkflowEventTriggers(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModePlatform,
		Auth: &config.ConnectionAuthDef{
			Type:  providermanifestv1.AuthTypeBearer,
			Token: "platform-token",
		},
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: "https://slack.example.invalid",
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "conversations.history", Method: http.MethodPost, Path: "/conversations.history"},
						},
					},
				},
			},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type:   "task.updated",
					Source: "roadmap",
				},
				Operation: "sync",
				Input: map[string]any{
					"source": "yaml",
				},
			},
		},
	})
	taskUpdated := cfg.Workflows.EventTriggers["task_updated"]
	taskUpdated.Permissions = []core.AccessPermission{{
		Plugin:     "slack",
		Operations: []string{"conversations.history"},
	}}
	cfg.Workflows.EventTriggers["task_updated"] = taskUpdated
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	recorder := recorders["temporal"]
	if recorder == nil {
		t.Fatal("missing workflow recorder for temporal")
		return
	}
	if len(recorder.upsertedEventTriggers) != 1 {
		t.Fatalf("upserted event triggers = %d, want 1", len(recorder.upsertedEventTriggers))
	}
	got := recorder.upsertedEventTriggers[0]
	if got.TriggerID != workflowConfigEventTriggerID("task_updated") {
		t.Fatalf("trigger id = %q", got.TriggerID)
	}
	if got.Match.Type != "task.updated" || got.Match.Source != "roadmap" || got.Match.Subject != "" {
		t.Fatalf("match = %#v", got.Match)
	}
	gotPlugin := requireCoreWorkflowPluginTarget(t, got.Target)
	if gotPlugin.PluginName != "roadmap" || gotPlugin.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if gotPlugin.Input["source"] != "yaml" {
		t.Fatalf("target input = %#v", gotPlugin.Input)
	}
	if got.RequestedBy.SubjectID != "system:config" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
	if strings.TrimSpace(got.ExecutionRef) == "" {
		t.Fatal("execution ref = empty")
	}
	ref, err := recorder.GetExecutionReference(context.Background(), got.ExecutionRef)
	if err != nil {
		t.Fatalf("Get execution ref: %v", err)
	}
	if ref.SubjectID != "system:config" {
		t.Fatalf("subjectID = %q, want %q", ref.SubjectID, "system:config")
	}
	wantPermissions := []core.AccessPermission{
		{Plugin: "roadmap", Operations: []string{"sync"}},
		{Plugin: "slack", Operations: []string{"conversations.history"}},
	}
	if !reflect.DeepEqual(ref.Permissions, wantPermissions) {
		t.Fatalf("permissions = %#v, want %#v", ref.Permissions, wantPermissions)
	}
}

func TestBootstrapConfigManagedAgentTargetsPreserveWorkflowSystemToolRefs(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {Source: config.ProviderSource{Path: "stub"}},
	}
	agentTarget := &config.WorkflowTargetConfig{Agent: &config.WorkflowAgentConfig{
		Provider: "managed",
		Prompt:   "Inspect the workflow and sync the roadmap",
		Tools: []config.WorkflowAgentToolRef{
			{System: coreagent.SystemToolWorkflow, Operation: "schedules.list"},
			{Plugin: "roadmap", Operation: "sync"},
		},
	}}
	cfg.Workflows.Schedules = map[string]config.WorkflowScheduleConfig{
		"agent_schedule": {
			Provider: "temporal",
			Cron:     "*/10 * * * *",
			Timezone: "UTC",
			Target:   agentTarget,
		},
	}
	cfg.Workflows.EventTriggers = map[string]config.WorkflowEventTriggerConfig{
		"agent_event": {
			Provider: "temporal",
			Match: config.WorkflowEventMatch{
				Type: "roadmap.updated",
			},
			Target: agentTarget,
		},
	}

	factories := validFactories()
	factories.Agent = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (coreagent.Provider, error) {
		return newRecordingAgentProvider(), nil
	}
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	recorder := recorders["temporal"]
	if recorder == nil {
		t.Fatal("missing workflow recorder for temporal")
		return
	}
	if len(recorder.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(recorder.upsertedSchedules))
	}
	if len(recorder.upsertedEventTriggers) != 1 {
		t.Fatalf("upserted event triggers = %d, want 1", len(recorder.upsertedEventTriggers))
	}
	for label, target := range map[string]coreworkflow.Target{
		"schedule":      recorder.upsertedSchedules[0].Target,
		"event trigger": recorder.upsertedEventTriggers[0].Target,
	} {
		if target.Agent == nil || len(target.Agent.ToolRefs) != 2 {
			t.Fatalf("%s target = %#v", label, target)
		}
		if target.Agent.ToolRefs[0].System != coreagent.SystemToolWorkflow || target.Agent.ToolRefs[0].Operation != "schedules.list" {
			t.Fatalf("%s workflow tool ref = %#v", label, target.Agent.ToolRefs[0])
		}
		if target.Agent.ToolRefs[1].Plugin != "roadmap" || target.Agent.ToolRefs[1].Operation != "sync" {
			t.Fatalf("%s plugin tool ref = %#v", label, target.Agent.ToolRefs[1])
		}
	}
	for _, executionRef := range []string{
		recorder.upsertedSchedules[0].ExecutionRef,
		recorder.upsertedEventTriggers[0].ExecutionRef,
	} {
		ref, err := recorder.GetExecutionReference(context.Background(), executionRef)
		if err != nil {
			t.Fatalf("Get execution ref %q: %v", executionRef, err)
		}
		if len(ref.Permissions) != 1 || ref.Permissions[0].Plugin != "roadmap" || len(ref.Permissions[0].Operations) != 1 || ref.Permissions[0].Operations[0] != "sync" {
			t.Fatalf("permissions = %#v", ref.Permissions)
		}
	}
}

func TestValidateDoesNotApplyConfiguredWorkflowEventTriggers(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
				Paused:    true,
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = recorder
		return recorder, nil
	}

	if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	recorder := recorders["temporal"]
	if recorder == nil {
		t.Fatal("missing workflow recorder for temporal")
		return
	}
	if len(recorder.upsertedEventTriggers) != 0 {
		t.Fatalf("upserted event triggers = %d, want 0", len(recorder.upsertedEventTriggers))
	}
	if len(recorder.deletedEventTriggers) != 0 {
		t.Fatalf("deleted event triggers = %d, want 0", len(recorder.deletedEventTriggers))
	}
}

func TestBootstrapDeletesRemovedConfiguredWorkflowEventTriggers(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := []*recordingWorkflowProvider{}
	sharedEventTriggers := map[string]*coreworkflow.EventTrigger{}
	sharedExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			eventTriggers: sharedEventTriggers,
			executionRefs: sharedExecutionRefs,
		}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(recorders) != 1 || len(recorders[0].upsertedEventTriggers) != 1 {
		t.Fatalf("initial upserts = %#v", recorders)
	}
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove event trigger: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders) != 2 {
		t.Fatalf("recorders = %d, want 2", len(recorders))
	}
	staleID := workflowConfigEventTriggerID("task_updated")
	recorder := recorders[1]
	if len(recorder.deletedEventTriggers) != 1 {
		t.Fatalf("deleted event triggers = %d, want 1", len(recorder.deletedEventTriggers))
	}
	if recorder.deletedEventTriggers[0].TriggerID != staleID {
		t.Fatalf("delete request = %#v", recorder.deletedEventTriggers[0])
	}
	if len(recorder.upsertedEventTriggers) != 0 {
		t.Fatalf("upserted event triggers = %d, want 0", len(recorder.upsertedEventTriggers))
	}
}

func TestBootstrapSkipsRemovedConfiguredWorkflowEventTriggerCleanupWhenListFails(t *testing.T) {
	t.Parallel()

	factories := validFactories()
	recorders := []*recordingWorkflowProvider{}
	sharedEventTriggers := map[string]*coreworkflow.EventTrigger{}
	sharedExecutionRefs := map[string]*coreworkflow.ExecutionReference{}
	failListEventTriggers := false
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			eventTriggers: sharedEventTriggers,
			executionRefs: sharedExecutionRefs,
		}
		if failListEventTriggers {
			recorder.listEventTriggersErr = errors.New("event trigger index unavailable")
		}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match:     workflowFixtureEventMatch{Type: "task.updated"},
				Operation: "sync",
			},
		},
	})

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	<-result.ProvidersReady
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{Provider: "temporal"})
	failListEventTriggers = true

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap with event trigger cleanup list failure: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders) != 2 {
		t.Fatalf("recorders = %d, want 2", len(recorders))
	}
	if len(recorders[1].deletedEventTriggers) != 0 {
		t.Fatalf("deleted event triggers = %d, want 0", len(recorders[1].deletedEventTriggers))
	}
}

func TestBootstrapMovesConfiguredWorkflowEventTriggersToNewProvider(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := map[string][]*recordingWorkflowProvider{}
	sharedEventTriggers := map[string]map[string]*coreworkflow.EventTrigger{}
	sharedExecutionRefs := map[string]map[string]*coreworkflow.ExecutionReference{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if sharedEventTriggers[name] == nil {
			sharedEventTriggers[name] = map[string]*coreworkflow.EventTrigger{}
		}
		if sharedExecutionRefs[name] == nil {
			sharedExecutionRefs[name] = map[string]*coreworkflow.ExecutionReference{}
		}
		recorder := &recordingWorkflowProvider{
			eventTriggers: sharedEventTriggers[name],
			executionRefs: sharedExecutionRefs[name],
		}
		recorders[name] = append(recorders[name], recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(recorders["temporal"]) != 1 || len(recorders["temporal"][0].upsertedEventTriggers) != 1 {
		t.Fatalf("initial temporal recorders = %#v", recorders["temporal"])
	}
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "backup",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap move provider: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorders["temporal"]) != 2 || len(recorders["backup"]) != 2 {
		t.Fatalf("recorders = %#v", recorders)
	}
	if len(recorders["temporal"][1].deletedEventTriggers) != 1 {
		t.Fatalf("temporal deleted event triggers = %d, want 1", len(recorders["temporal"][1].deletedEventTriggers))
	}
	if len(recorders["backup"][1].upsertedEventTriggers) != 1 {
		t.Fatalf("backup upserted event triggers = %d, want 1", len(recorders["backup"][1].upsertedEventTriggers))
	}
}

func TestBootstrapRejectsExistingUnmanagedWorkflowEventTriggerIDDuringProviderMove(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := map[string][]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		if name == "backup" && len(recorders[name]) == 1 {
			recorder.getEventTrigger = &coreworkflow.EventTrigger{ID: workflowConfigEventTriggerID("task_updated")}
		}
		recorders[name] = append(recorders[name], recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "backup",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	_, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing unmanaged trigger id") {
		t.Fatalf("Bootstrap error = %v, want ownership conflict", err)
	}
	if len(recorders["backup"]) != 2 {
		t.Fatalf("backup recorders = %d, want 2", len(recorders["backup"]))
	}
	if len(recorders["backup"][1].upsertedEventTriggers) != 0 {
		t.Fatalf("backup upserted event triggers = %d, want 0", len(recorders["backup"][1].upsertedEventTriggers))
	}
}

func TestBootstrapRejectsExistingUnmanagedWorkflowEventTriggerID(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	recorder := &recordingWorkflowProvider{
		getEventTrigger: &coreworkflow.EventTrigger{
			ID: workflowConfigEventTriggerID("task_updated"),
			Match: coreworkflow.EventMatch{
				Type: "task.updated",
			},
			Target: coreWorkflowPluginTarget("roadmap", "sync"),
		},
	}
	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return recorder, nil
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing unmanaged trigger id") {
		t.Fatalf("Bootstrap error = %v, want ownership conflict", err)
	}
	if len(recorder.upsertedEventTriggers) != 0 {
		t.Fatalf("upserted event triggers = %d, want 0", len(recorder.upsertedEventTriggers))
	}
}

func TestBootstrapIgnoresMissingRemovedConfiguredWorkflowEventTrigger(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{deleteEventMissingNotFound: true}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())
	provider.eventTriggers = map[string]*coreworkflow.EventTrigger{}

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing event trigger: %v", err)
	}
	_ = result.Close(context.Background())

	if len(provider.deletedEventTriggers) != 0 {
		t.Fatalf("deleted event triggers = %d, want 0", len(provider.deletedEventTriggers))
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing event trigger replay: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.deletedEventTriggers) != 0 {
		t.Fatalf("deleted event triggers after replay = %d, want 0", len(provider.deletedEventTriggers))
	}
}

func TestBootstrapIgnoresMissingPreviousEventTriggerDuringWorkflowProviderMove(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	temporal := &recordingWorkflowProvider{deleteEventMissingNotFound: true}
	backup := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name == "backup" {
			return backup, nil
		}
		return temporal, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())
	temporal.eventTriggers = map[string]*coreworkflow.EventTrigger{}

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "backup",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
		"backup":   {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap move provider: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(backup.upsertedEventTriggers) != 1 {
		t.Fatalf("backup upserted event triggers = %d, want 1", len(backup.upsertedEventTriggers))
	}
	if len(temporal.deletedEventTriggers) != 0 {
		t.Fatalf("temporal deleted event triggers = %d, want 0", len(temporal.deletedEventTriggers))
	}
}

func workflowConfigScheduleID(scheduleKey string) string {
	sum := sha256.Sum256([]byte(scheduleKey))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigEventTriggerID(triggerKey string) string {
	sum := sha256.Sum256([]byte("event_trigger\x00" + triggerKey))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func TestBootstrapStartsWorkflowProvidersAfterInvokerIsReady(t *testing.T) {
	t.Parallel()

	var requestPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := workflowStartupCallbackConfig(srv.URL)
	factories := validFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
			SubjectID:   "system:config",
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-bootstrap-token",
		}); err != nil {
			return nil, fmt.Errorf("store startup token: %w", err)
		}
		executionRef := storeWorkflowExecutionRefForTarget(t, deps, name, coreWorkflowPluginTarget("roadmap", "sync"))
		resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			Target:       protoWorkflowPluginTarget("roadmap", "sync"),
			ExecutionRef: executionRef,
		})
		if err != nil {
			return nil, fmt.Errorf("startup callback: %w", err)
		}
		if resp.GetStatus() != http.StatusAccepted || resp.GetBody() != `{"ok":true}` {
			return nil, fmt.Errorf("startup callback response = %#v", resp)
		}
		return &stubWorkflowProvider{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if got, _ := requestPath.Load().(string); got != "/sync" {
		t.Fatalf("request path = %q, want %q", got, "/sync")
	}
}

func TestValidateStartsWorkflowProvidersAfterInvokerIsReady(t *testing.T) {
	t.Parallel()

	var requestPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := workflowStartupCallbackConfig(srv.URL)
	factories := validFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
			SubjectID:   "system:config",
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-validate-token",
		}); err != nil {
			return nil, fmt.Errorf("store startup token: %w", err)
		}
		executionRef := storeWorkflowExecutionRefForTarget(t, deps, name, coreWorkflowPluginTarget("roadmap", "sync"))
		resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			Target:       protoWorkflowPluginTarget("roadmap", "sync"),
			ExecutionRef: executionRef,
		})
		if err != nil {
			return nil, fmt.Errorf("startup callback: %w", err)
		}
		if resp.GetStatus() != http.StatusAccepted || resp.GetBody() != `{"ok":true}` {
			return nil, fmt.Errorf("startup callback response = %#v", resp)
		}
		return &stubWorkflowProvider{}, nil
	}

	if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got, _ := requestPath.Load().(string); got != "/sync" {
		t.Fatalf("request path = %q, want %q", got, "/sync")
	}
}

func TestBootstrapStartupWorkflowCallbackRequiresExecutionRef(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := workflowStartupCallbackConfig(srv.URL)
	cfg.Plugins["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
	cfg.Plugins["roadmap"].AuthorizationPolicy = "roadmap-policy"
	cfg.Plugins["roadmap"].ResolvedManifest.Spec.Surfaces.REST.Operations[0].AllowedRoles = []string{"viewer"}
	cfg.Authorization.Policies = map[string]config.SubjectPolicyDef{
		"roadmap-policy": {
			Members: []config.SubjectPolicyMemberDef{{
				SubjectID: principal.UserSubjectID("viewer-user"),
				Role:      "viewer",
			}},
		},
	}

	factories := validFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
			SubjectID:   "system:config",
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-startup-token",
		}); err != nil {
			return nil, fmt.Errorf("store startup token: %w", err)
		}
		_, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			Target: protoWorkflowPluginTarget("roadmap", "sync"),
		})
		if err == nil {
			return nil, fmt.Errorf("expected startup callback execution_ref failure")
		}
		if status.Code(err) != codes.InvalidArgument {
			return nil, fmt.Errorf("startup callback status = %s, want %s", status.Code(err), codes.InvalidArgument)
		}
		return &stubWorkflowProvider{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
}

func TestBootstrapStartsAgentProvidersAfterInvokerIsReady(t *testing.T) {
	t.Parallel()

	var requestPath atomic.Value
	var requestBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestPath.Store(r.URL.Path)
		requestBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			ConnectionMode: providermanifestv1.ConnectionModeNone,
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: srv.URL,
							Operations: []providermanifestv1.ProviderOperation{
								{Name: "sync", Method: http.MethodPost, Path: "/sync"},
							},
						},
					},
				},
			},
		},
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"reviewer": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var capturedHostServices []runtimehost.HostService
	providerImpl := newRecordingAgentProvider()
	factories := validFactories()
	factories.Agent = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreagent.Provider, error) {
		if name != "reviewer" {
			return nil, fmt.Errorf("agent name = %q, want %q", name, "reviewer")
		}
		capturedHostServices = append([]runtimehost.HostService(nil), hostServices...)
		return providerImpl, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	systemPrincipal := &principal.Principal{SubjectID: "system:config", Kind: principal.Kind("system"), Source: principal.SourceEnv}
	startCtx := principal.WithPrincipal(context.Background(), systemPrincipal)
	session, err := result.AgentManager.CreateSession(startCtx, systemPrincipal, coreagent.ManagerCreateSessionRequest{
		ProviderName: "reviewer",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, systemPrincipal, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "roadmap",
			Operation: "sync",
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	providerImpl.mu.Lock()
	if len(providerImpl.createTurnRequests) != 1 {
		t.Fatalf("CreateTurn requests = %d, want 1", len(providerImpl.createTurnRequests))
	}
	createTurnReq := providerImpl.createTurnRequests[0]
	if stored := providerImpl.turns[turn.ID]; stored != nil {
		stored.Status = coreagent.ExecutionStatusRunning
		stored.CompletedAt = nil
	}
	providerImpl.mu.Unlock()
	if strings.TrimSpace(createTurnReq.RunGrant) == "" {
		t.Fatal("CreateTurn run_grant is empty")
	}
	if len(createTurnReq.Tools) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", createTurnReq.Tools)
	}
	listResp := invokeAgentHostListTools(t, capturedHostServices, &proto.ListAgentToolsRequest{
		SessionId: session.ID,
		TurnId:    turn.ID,
		PageSize:  5,
		RunGrant:  createTurnReq.RunGrant,
	})
	if len(listResp.GetTools()) != 1 {
		t.Fatalf("ListTools tools = %#v, want one tool", listResp.GetTools())
	}
	tool := listResp.GetTools()[0]
	args, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	resp, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  session.ID,
		TurnId:     turn.ID,
		ToolCallId: "tool-call-1",
		ToolId:     tool.GetId(),
		Arguments:  args,
		RunGrant:   createTurnReq.RunGrant,
	})
	if err != nil {
		t.Fatalf("invoke agent host callback: %v", err)
	}
	if resp.GetStatus() != http.StatusAccepted || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("agent host callback response = %#v", resp)
	}

	if got, _ := requestPath.Load().(string); got != "/sync" {
		t.Fatalf("request path = %q, want %q", got, "/sync")
	}
	if got, _ := requestBody.Load().(string); !strings.Contains(got, `"taskId":"task-123"`) {
		t.Fatalf("request body = %q, want taskId payload", got)
	}
	if _, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  "wrong-session",
		TurnId:     turn.ID,
		ToolCallId: "tool-call-mismatch",
		ToolId:     tool.GetId(),
		Arguments:  args,
		RunGrant:   createTurnReq.RunGrant,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("invoke agent host callback with mismatched session status = %s, want %s", status.Code(err), codes.PermissionDenied)
	}
	providerImpl.mu.Lock()
	if stored := providerImpl.turns[turn.ID]; stored != nil {
		stored.ID = "different-live-turn"
		stored.SessionID = session.ID
		stored.Status = coreagent.ExecutionStatusRunning
		stored.CompletedAt = nil
	}
	providerImpl.mu.Unlock()
	if _, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  session.ID,
		TurnId:     turn.ID,
		ToolCallId: "tool-call-wrong-turn",
		ToolId:     tool.GetId(),
		Arguments:  args,
		RunGrant:   createTurnReq.RunGrant,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("invoke agent host callback with mismatched provider turn status = %s, want %s", status.Code(err), codes.PermissionDenied)
	}
	providerImpl.mu.Lock()
	if stored := providerImpl.turns[turn.ID]; stored != nil {
		stored.ID = turn.ID
		stored.Status = coreagent.ExecutionStatusRunning
		stored.CompletedAt = nil
	}
	providerImpl.mu.Unlock()

	if _, err := result.AgentManager.CancelTurn(startCtx, systemPrincipal, turn.ID, "done"); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}
	cancelRequests := providerImpl.CancelTurnRequests()
	if len(cancelRequests) != 1 {
		t.Fatalf("CancelTurn requests = %d, want 1", len(cancelRequests))
	}
	if cancelRequests[0].TurnID != turn.ID {
		t.Fatalf("CancelTurn turn_id = %q, want %q", cancelRequests[0].TurnID, turn.ID)
	}
	providerImpl.mu.Lock()
	if stored := providerImpl.turns[turn.ID]; stored != nil {
		stored.Status = coreagent.ExecutionStatusRunning
		stored.CompletedAt = nil
	}
	providerImpl.mu.Unlock()
	if _, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  session.ID,
		TurnId:     turn.ID,
		ToolCallId: "tool-call-2",
		ToolId:     tool.GetId(),
		Arguments:  args,
		RunGrant:   createTurnReq.RunGrant,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("invoke agent host callback after cancel status = %s, want %s", status.Code(err), codes.PermissionDenied)
	}
}

func TestBootstrapDoesNotRevokeAgentGrantWhenCancelReturnsLiveTurn(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			ConnectionMode: providermanifestv1.ConnectionModeNone,
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: srv.URL,
							Operations: []providermanifestv1.ProviderOperation{
								{Name: "sync", Method: http.MethodPost, Path: "/sync"},
							},
						},
					},
				},
			},
		},
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"reviewer": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	var capturedHostServices []runtimehost.HostService
	providerImpl := newRecordingAgentProvider()
	providerImpl.cancelTurnStatus = coreagent.ExecutionStatusRunning
	factories := validFactories()
	factories.Agent = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreagent.Provider, error) {
		if name != "reviewer" {
			return nil, fmt.Errorf("agent name = %q, want %q", name, "reviewer")
		}
		capturedHostServices = append([]runtimehost.HostService(nil), hostServices...)
		return providerImpl, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	systemPrincipal := &principal.Principal{SubjectID: "system:config", Kind: principal.Kind("system"), Source: principal.SourceEnv}
	startCtx := principal.WithPrincipal(context.Background(), systemPrincipal)
	session, err := result.AgentManager.CreateSession(startCtx, systemPrincipal, coreagent.ManagerCreateSessionRequest{
		ProviderName: "reviewer",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, systemPrincipal, coreagent.ManagerCreateTurnRequest{
		SessionID: session.ID,
		Model:     "gpt-test",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "roadmap",
			Operation: "sync",
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	providerImpl.mu.Lock()
	createTurnReq := providerImpl.createTurnRequests[0]
	if stored := providerImpl.turns[turn.ID]; stored != nil {
		stored.Status = coreagent.ExecutionStatusRunning
		stored.CompletedAt = nil
	}
	providerImpl.mu.Unlock()
	if strings.TrimSpace(createTurnReq.RunGrant) == "" {
		t.Fatal("CreateTurn run_grant is empty")
	}
	if len(createTurnReq.Tools) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", createTurnReq.Tools)
	}
	listResp := invokeAgentHostListTools(t, capturedHostServices, &proto.ListAgentToolsRequest{
		SessionId: session.ID,
		TurnId:    turn.ID,
		PageSize:  5,
		RunGrant:  createTurnReq.RunGrant,
	})
	if len(listResp.GetTools()) != 1 {
		t.Fatalf("ListTools tools = %#v, want one tool", listResp.GetTools())
	}
	tool := listResp.GetTools()[0]
	args, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	if _, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  session.ID,
		TurnId:     turn.ID,
		ToolCallId: "tool-call-before-cancel",
		ToolId:     tool.GetId(),
		Arguments:  args,
		RunGrant:   createTurnReq.RunGrant,
	}); err != nil {
		t.Fatalf("invoke agent host callback before cancel: %v", err)
	}

	if _, err := result.AgentManager.CancelTurn(startCtx, systemPrincipal, turn.ID, "done"); err == nil {
		t.Fatal("CancelTurn error = nil, want live turn rejection")
	} else if !strings.Contains(err.Error(), "returned live turn") {
		t.Fatalf("CancelTurn error = %v, want live turn rejection", err)
	}
	if _, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  session.ID,
		TurnId:     turn.ID,
		ToolCallId: "tool-call-after-live-cancel",
		ToolId:     tool.GetId(),
		Arguments:  args,
		RunGrant:   createTurnReq.RunGrant,
	}); err != nil {
		t.Fatalf("invoke agent host callback after live cancel: %v", err)
	}
}

func TestBootstrapAgentProviderRejectsMismatchedRequestedSessionOrTurnID(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			ConnectionMode: providermanifestv1.ConnectionModeNone,
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: "http://example.invalid",
							Operations: []providermanifestv1.ProviderOperation{
								{Name: "sync", Method: http.MethodPost, Path: "/sync"},
							},
						},
					},
				},
			},
		},
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"reviewer": {
			Source:  config.ProviderSource{Path: "stub"},
			Default: true,
		},
	}

	providerImpl := &generatedIDAgentProvider{}
	var capturedHostServices []runtimehost.HostService
	factories := validFactories()
	factories.Agent = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreagent.Provider, error) {
		if name != "reviewer" {
			return nil, fmt.Errorf("agent name = %q, want %q", name, "reviewer")
		}
		capturedHostServices = append([]runtimehost.HostService(nil), hostServices...)
		return providerImpl, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	_, provider, err := result.AgentControl.ResolveProviderSelection("")
	if err != nil {
		t.Fatalf("ResolveProviderSelection: %v", err)
	}

	startCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "system:config"})
	tool := coreagent.Tool{
		ID: "roadmap.sync",
		Target: coreagent.ToolTarget{
			Plugin:    "roadmap",
			Operation: "sync",
		},
	}
	if _, err := provider.CreateSession(startCtx, coreagent.CreateSessionRequest{
		SessionID: "agent-session-1",
		Model:     "gpt-test",
	}); err == nil {
		t.Fatal("CreateSession error = nil, want mismatched session id failure")
	} else if !strings.Contains(err.Error(), `returned session id "generated-session-1" for requested session id "agent-session-1"`) {
		t.Fatalf("CreateSession error = %v, want mismatched session id failure", err)
	}

	replayedSession, err := provider.CreateSession(startCtx, coreagent.CreateSessionRequest{
		SessionID:      "agent-session-1",
		IdempotencyKey: "workflow:github:run-1:session",
		Model:          "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession idempotent replay: %v", err)
	}
	if replayedSession.ID != "generated-session-1" {
		t.Fatalf("CreateSession idempotent replay ID = %q, want generated-session-1", replayedSession.ID)
	}

	if _, err := provider.CreateTurn(startCtx, coreagent.CreateTurnRequest{
		TurnID:    "agent-turn-1",
		SessionID: "agent-session-1",
		Model:     "gpt-test",
		CreatedBy: coreagent.Actor{SubjectID: "system:config"},
		Tools:     []coreagent.Tool{tool},
	}); err == nil {
		t.Fatal("CreateTurn error = nil, want mismatched turn id failure")
	} else if !strings.Contains(err.Error(), `returned turn id "generated-turn-1" for requested turn id "agent-turn-1"`) {
		t.Fatalf("CreateTurn error = %v, want mismatched turn id failure", err)
	}

	cancelRequests := providerImpl.CancelTurnRequests()
	if len(cancelRequests) != 1 {
		t.Fatalf("CancelTurn requests = %d, want 1", len(cancelRequests))
	}
	if cancelRequests[0].TurnID != "generated-turn-1" {
		t.Fatalf("CancelTurn turn_id = %q, want %q", cancelRequests[0].TurnID, "generated-turn-1")
	}
	if cancelRequests[0].Reason != "agent provider returned mismatched turn id" {
		t.Fatalf("CancelTurn reason = %q, want %q", cancelRequests[0].Reason, "agent provider returned mismatched turn id")
	}

	replayedTurn, err := provider.CreateTurn(startCtx, coreagent.CreateTurnRequest{
		TurnID:         "agent-turn-1",
		SessionID:      "agent-session-1",
		IdempotencyKey: "workflow:github:run-1:turn",
		Model:          "gpt-test",
		CreatedBy:      coreagent.Actor{SubjectID: "system:config"},
		Tools:          []coreagent.Tool{tool},
	})
	if err != nil {
		t.Fatalf("CreateTurn idempotent replay: %v", err)
	}
	if replayedTurn.ID != "generated-turn-1" {
		t.Fatalf("CreateTurn idempotent replay ID = %q, want generated-turn-1", replayedTurn.ID)
	}
	cancelRequests = providerImpl.CancelTurnRequests()
	if len(cancelRequests) != 1 {
		t.Fatalf("CancelTurn requests after idempotent replay = %d, want 1", len(cancelRequests))
	}

	args, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	if _, err := invokeAgentHostCallback(t, capturedHostServices, &proto.ExecuteAgentToolRequest{
		SessionId:  "agent-session-1",
		TurnId:     "agent-turn-1",
		ToolCallId: "tool-call-1",
		ToolId:     tool.ID,
		Arguments:  args,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("invoke agent host callback after mismatch status = %s, want %s", status.Code(err), codes.PermissionDenied)
	}
}

func TestBootstrapConfiguredWorkflowScheduleExecutionRefInvokesPolicyProtectedPlugin(t *testing.T) {
	t.Parallel()

	var requestPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := workflowStartupCallbackConfig(srv.URL)
	cfg.Plugins["roadmap"].AuthorizationPolicy = "roadmap-policy"
	cfg.Plugins["roadmap"].ResolvedManifest.Spec.Surfaces.REST.Operations[0].AllowedRoles = []string{"viewer"}
	cfg.Authorization.Policies = map[string]config.SubjectPolicyDef{
		"roadmap-policy": {
			Members: []config.SubjectPolicyMemberDef{{
				SubjectID: principal.UserSubjectID("viewer-user"),
				Role:      "viewer",
			}},
		},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		Schedules: map[string]workflowFixtureSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Operation: "sync",
			},
		},
	})

	recorder := &recordingWorkflowProvider{}
	var hostServices []runtimehost.HostService
	factories := validFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, services []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		hostServices = append([]runtimehost.HostService(nil), services...)
		return recorder, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(recorder.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(recorder.upsertedSchedules))
	}
	executionRef := recorder.upsertedSchedules[0].ExecutionRef
	if executionRef == "" {
		t.Fatal("execution ref is empty")
	}
	resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
		Target:       protoWorkflowPluginTarget("roadmap", "sync"),
		ExecutionRef: executionRef,
	})
	if err != nil {
		t.Fatalf("invoke workflow host callback: %v", err)
	}
	if resp.GetStatus() != http.StatusAccepted || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("workflow host callback response = %#v", resp)
	}
	if got, _ := requestPath.Load().(string); got != "/sync" {
		t.Fatalf("request path = %q, want %q", got, "/sync")
	}
}

func TestValidateManagedWorkflowStartupCallbackUsesPreparedProviderStub(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		source config.ProviderSource
	}{
		{
			name:   "remote release metadata",
			source: config.NewMetadataSource("https://example.invalid/github-com-example-roadmap/v0.0.1/provider-release.yaml"),
		},
		{
			name:   "local release metadata",
			source: config.NewLocalReleaseMetadataSource(filepath.Join(t.TempDir(), "roadmap", "dist", "provider-release.yaml")),
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.Plugins = map[string]*config.ProviderEntry{
				"roadmap": {
					Source:         tc.source,
					ConnectionMode: providermanifestv1.ConnectionModeUser,
					ResolvedManifest: &providermanifestv1.Manifest{
						DisplayName: "Roadmap",
						Description: "Managed roadmap plugin",
						Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: "roadmap"},
						Spec: &providermanifestv1.Spec{
							Surfaces: &providermanifestv1.ProviderSurfaces{
								REST: &providermanifestv1.RESTSurface{
									BaseURL: "https://example.invalid",
									Operations: []providermanifestv1.ProviderOperation{
										{Name: "sync", Method: http.MethodPost, Path: "/sync"},
									},
								},
							},
						},
					},
				},
			}
			cfg.Providers.Workflow = map[string]*config.ProviderEntry{
				"temporal": {Source: config.ProviderSource{Path: "stub"}},
			}
			setWorkflowFixture(cfg, "roadmap", &workflowFixture{
				Provider: "temporal",
			})

			factories := validFactories()
			factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
				if name != "temporal" {
					return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
				}
				if err := deps.Services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
					SubjectID:   "system:config",
					Integration: "roadmap",
					Connection:  config.PluginConnectionName,
					Instance:    "default",
					AccessToken: "workflow-validate-token",
				}); err != nil {
					return nil, fmt.Errorf("store startup token: %w", err)
				}
				executionRef := storeWorkflowExecutionRefForTarget(t, deps, name, coreWorkflowPluginTarget("roadmap", "sync"))
				resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
					Target:       protoWorkflowPluginTarget("roadmap", "sync"),
					ExecutionRef: executionRef,
				})
				if err != nil {
					return nil, fmt.Errorf("startup callback: %w", err)
				}
				if resp.GetStatus() != http.StatusAccepted || resp.GetBody() != `{}` {
					return nil, fmt.Errorf("startup callback response = %#v", resp)
				}
				return &stubWorkflowProvider{}, nil
			}

			if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestValidateManagedWorkflowStartupInvokesMCPPassthroughPreparedProviders(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	catalogData, err := yaml.Marshal(&catalog.Catalog{
		Name: "roadmap",
		Operations: []catalog.CatalogOperation{{
			ID:        "sync",
			Method:    http.MethodPost,
			Transport: catalog.TransportMCPPassthrough,
		}},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.yaml"), catalogData, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.yaml"), []byte("kind: plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Source:               config.NewMetadataSource("https://example.invalid/github-com-example-roadmap/v0.0.1/provider-release.yaml"),
			ConnectionMode:       providermanifestv1.ConnectionModeUser,
			ResolvedManifestPath: filepath.Join(root, "manifest.yaml"),
			ResolvedManifest: &providermanifestv1.Manifest{
				DisplayName: "Roadmap",
				Description: "Managed roadmap plugin",
				Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: "roadmap"},
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						MCP: &providermanifestv1.MCPSurface{
							Connection: config.PluginConnectionName,
							URL:        "https://example.invalid/mcp",
						},
					},
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})

	factories := validFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		connMaps, err := bootstrap.BuildConnectionMaps(cfg)
		if err != nil {
			return nil, fmt.Errorf("build connection maps: %w", err)
		}
		connection := connMaps.DefaultConnection["roadmap"]
		if connection == "" {
			connection = config.PluginConnectionName
		}
		if err := deps.Services.ExternalCredentials.PutCredential(context.Background(), &core.ExternalCredential{
			SubjectID:   "system:config",
			Integration: "roadmap",
			Connection:  connection,
			Instance:    "default",
			AccessToken: "workflow-validate-token",
		}); err != nil {
			return nil, fmt.Errorf("store startup token for connection %q: %w", connection, err)
		}
		req := coreworkflow.InvokeOperationRequest{
			ProviderName: name,
			Target:       coreWorkflowPluginTarget("roadmap", "sync"),
		}
		configPrincipal := &principal.Principal{
			SubjectID:           "system:config",
			CredentialSubjectID: "system:config",
			TokenPermissions: principal.CompilePermissions([]core.AccessPermission{{
				Plugin:     "roadmap",
				Operations: []string{"sync"},
			}}),
		}
		resp, err := deps.WorkflowRuntime.Invoke(principal.WithPrincipal(context.Background(), configPrincipal), req)
		if err != nil {
			return nil, fmt.Errorf("workflow runtime invoke: %w", err)
		}
		if resp.Status != http.StatusOK || resp.Body != `{}` {
			return nil, fmt.Errorf("startup invoke response = %#v", resp)
		}
		return &stubWorkflowProvider{}, nil
	}

	if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestBootstrapS3BuildFailureClosesIndexedDBsOnce(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["archive"] = &config.ProviderEntry{
		Source: config.ProviderSource{Path: "stub"},
	}
	cfg.Providers.S3 = map[string]*config.ProviderEntry{
		"assets": {Source: config.ProviderSource{Path: "stub"}},
	}

	var selectedClosed atomic.Int32
	var extraClosed atomic.Int32
	var indexeddbBuilds atomic.Int32

	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		switch indexeddbBuilds.Add(1) {
		case 1:
			return &trackedIndexedDB{
				StubIndexedDB: &coretesting.StubIndexedDB{},
				closed:        &selectedClosed,
			}, nil
		case 2:
			return &trackedIndexedDB{
				StubIndexedDB: &coretesting.StubIndexedDB{},
				closed:        &extraClosed,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected indexeddb build #%d", indexeddbBuilds.Load())
		}
	}
	factories.S3 = func(yaml.Node) (s3store.Client, error) {
		return nil, fmt.Errorf("boom")
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("Bootstrap: expected error, got nil")
	}
	if !strings.Contains(err.Error(), `bootstrap: s3 from resource "assets": s3 provider: boom`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := selectedClosed.Load(); got != 1 {
		t.Fatalf("selected indexeddb close count = %d, want 1", got)
	}
	if got := extraClosed.Load(); got != 1 {
		t.Fatalf("extra indexeddb close count = %d, want 1", got)
	}
}

func TestResultCloseClosesAuthProvider(t *testing.T) {
	t.Parallel()

	closed := &atomic.Bool{}
	factories := validFactories()
	factories.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthenticationProvider, error) {
		return &closableAuthProvider{
			StubAuthProvider: &coretesting.StubAuthProvider{N: "test-auth"},
			closed:           closed,
		}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), validConfig(), factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Result.Close: %v", err)
	}
	if !closed.Load() {
		t.Fatal("authentication provider was not closed")
	}
}

func TestResultCloseClosesAuthorizationProvider(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Authorization = map[string]*config.ProviderEntry{
		"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.Authorization = "indexeddb"

	closed := &atomic.Bool{}
	factories := validFactories()
	factories.Authorization = func(yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
		return &closableAuthorizationProvider{
			stubAuthorizationProvider: &stubAuthorizationProvider{name: "test-authorization"},
			closed:                    closed,
		}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Result.Close: %v", err)
	}
	if !closed.Load() {
		t.Fatal("authorization provider was not closed")
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("baseline", func(t *testing.T) {
		t.Parallel()

		if _, err := bootstrap.Validate(context.Background(), validConfig(), validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("with authorization provider configured", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		factories := validFactories()
		factories.Authorization = stubAuthorizationFactory("test-authorization")

		if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
			t.Fatalf("Validate with authorization provider: %v", err)
		}
	})

	t.Run("rejects invalid plugin invokes dependency", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"caller": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "missing", Operation: "ping"},
				},
			},
		}

		_, err := bootstrap.Validate(context.Background(), cfg, validFactories())
		if err == nil || !strings.Contains(err.Error(), `plugins.caller.invokes[0] references unknown plugin "missing"`) {
			t.Fatalf("Validate error = %v, want unknown plugin invokes error", err)
		}
	})

	t.Run("accepts graphql surface plugin invokes dependency", func(t *testing.T) {
		t.Parallel()

		srv := startBootstrapGraphQLIntrospectionServer(t)
		root := t.TempDir()
		callerManifestPath := filepath.Join(root, "caller-manifest.yaml")
		if err := os.WriteFile(callerManifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(caller-manifest.yaml): %v", err)
		}

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"caller": {
				Source:               config.NewMetadataSource("https://example.invalid/github-com-acme-caller/v1.0.0/provider-release.yaml"),
				ResolvedManifestPath: callerManifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "linear", Surface: "graphql"},
				},
			},
			"linear": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							GraphQL: &providermanifestv1.GraphQLSurface{
								URL: srv.URL,
							},
						},
					},
				},
			},
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("rejects graphql surface invoke when target plugin has no graphql surface", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		callerManifestPath := filepath.Join(root, "caller-manifest.yaml")
		if err := os.WriteFile(callerManifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(caller-manifest.yaml): %v", err)
		}

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"caller": {
				Source:               config.NewMetadataSource("https://example.invalid/github-com-acme-caller/v1.0.0/provider-release.yaml"),
				ResolvedManifestPath: callerManifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.PluginInvocationDependency{
					{Plugin: "linear", Surface: "graphql"},
				},
			},
			"linear": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							REST: &providermanifestv1.RESTSurface{
								BaseURL: "https://linear.example/api",
								Operations: []providermanifestv1.ProviderOperation{
									{Name: "status", Method: http.MethodGet, Path: "/status"},
								},
							},
						},
					},
				},
			},
		}

		_, err := bootstrap.Validate(context.Background(), cfg, validFactories())
		if err == nil || !strings.Contains(err.Error(), `plugins.caller.invokes[0] references plugin "linear" surface "graphql", but that surface is not configured`) {
			t.Fatalf("Validate error = %v, want missing graphql surface error", err)
		}
	})

	t.Run("accepts plugin configured with both openapi and graphql api surfaces", func(t *testing.T) {
		t.Parallel()

		schema := map[string]any{
			"queryType": map[string]any{"name": "Query"},
			"types": []any{
				map[string]any{
					"kind": "OBJECT",
					"name": "Query",
					"fields": []any{
						map[string]any{
							"name": "viewer",
							"args": []any{
								map[string]any{
									"name": "team",
									"type": map[string]any{"kind": "SCALAR", "name": "String"},
								},
							},
							"type": map[string]any{"kind": "OBJECT", "name": "Viewer"},
						},
					},
				},
				map[string]any{
					"kind": "OBJECT",
					"name": "Viewer",
					"fields": []any{
						map[string]any{"name": "id", "type": map[string]any{"kind": "SCALAR", "name": "ID"}},
						map[string]any{"name": "name", "type": map[string]any{"kind": "SCALAR", "name": "String"}},
					},
				},
				map[string]any{"kind": "SCALAR", "name": "String"},
				map[string]any{"kind": "SCALAR", "name": "ID"},
			},
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/openapi.json":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"openapi": "3.1.0",
					"info": map[string]any{
						"title":   "Linear API",
						"version": "1.0.0",
					},
					"paths": map[string]any{
						"/status": map[string]any{
							"get": map[string]any{
								"operationId": "status",
								"responses": map[string]any{
									"200": map[string]any{"description": "ok"},
								},
							},
						},
					},
				})
			case "/status":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "/graphql":
				var payload struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(payload.Query, "__schema") {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"__schema": schema}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"viewer": map[string]any{
							"id":   "user-123",
							"name": "Platform",
						},
					},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"linear": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						DefaultConnection: "rest",
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"rest":    {Mode: providermanifestv1.ConnectionModeNone},
							"graphql": {Mode: providermanifestv1.ConnectionModeNone},
						},
						Surfaces: &providermanifestv1.ProviderSurfaces{
							OpenAPI: &providermanifestv1.OpenAPISurface{
								Document:   srv.URL + "/openapi.json",
								BaseURL:    srv.URL,
								Connection: "rest",
							},
							GraphQL: &providermanifestv1.GraphQLSurface{
								URL:        srv.URL + "/graphql",
								Connection: "graphql",
							},
						},
					},
				},
			},
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("accepts plugin configured with graphql surface without eager introspection", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"linear": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							GraphQL: &providermanifestv1.GraphQLSurface{
								URL: "http://127.0.0.1:1/graphql",
							},
						},
					},
				},
			},
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("workflow managed subjects allow normalized credentialed providers", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"svc": {
				ConnectionMode: providermanifestv1.ConnectionModeUser,
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							REST: &providermanifestv1.RESTSurface{
								BaseURL: srv.URL,
								Operations: []providermanifestv1.ProviderOperation{
									{Name: "run", Method: http.MethodPost, Path: "/run"},
								},
							},
						},
					},
				},
			},
		}
		cfg.Providers.Workflow = map[string]*config.ProviderEntry{
			"temporal": {Source: config.ProviderSource{Path: "stub"}},
		}

		factories := validFactories()
		factories.Workflow = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
			return &stubWorkflowProvider{}, nil
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("workflow managed service account subjects stay unique across similar plugin names", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		manifest := &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				Surfaces: &providermanifestv1.ProviderSurfaces{
					REST: &providermanifestv1.RESTSurface{
						BaseURL: srv.URL,
						Operations: []providermanifestv1.ProviderOperation{
							{Name: "run", Method: http.MethodPost, Path: "/run"},
						},
					},
				},
			},
		}

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"foo-bar": {
				ResolvedManifest: manifest,
			},
			"foo_bar": {
				ResolvedManifest: manifest,
			},
		}
		cfg.Providers.Workflow = map[string]*config.ProviderEntry{
			"temporal": {Source: config.ProviderSource{Path: "stub"}},
		}

		factories := validFactories()
		factories.Workflow = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
			return &stubWorkflowProvider{}, nil
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

func TestBootstrapAllowsPluginConfiguredWithBothOpenAPIAndGraphQLAPISurfaces(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"queryType": map[string]any{"name": "Query"},
		"types": []any{
			map[string]any{
				"kind": "OBJECT",
				"name": "Query",
				"fields": []any{
					map[string]any{
						"name": "viewer",
						"args": []any{
							map[string]any{
								"name": "team",
								"type": map[string]any{"kind": "SCALAR", "name": "String"},
							},
						},
						"type": map[string]any{"kind": "OBJECT", "name": "Viewer"},
					},
				},
			},
			map[string]any{
				"kind": "OBJECT",
				"name": "Viewer",
				"fields": []any{
					map[string]any{"name": "id", "type": map[string]any{"kind": "SCALAR", "name": "ID"}},
					map[string]any{"name": "name", "type": map[string]any{"kind": "SCALAR", "name": "String"}},
				},
			},
			map[string]any{"kind": "SCALAR", "name": "String"},
			map[string]any{"kind": "SCALAR", "name": "ID"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"openapi": "3.1.0",
				"info": map[string]any{
					"title":   "Linear API",
					"version": "1.0.0",
				},
				"paths": map[string]any{
					"/status": map[string]any{
						"get": map[string]any{
							"operationId": "status",
							"responses": map[string]any{
								"200": map[string]any{"description": "ok"},
							},
						},
					},
				},
			})
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/graphql":
			var payload struct {
				Query string `json:"query"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(payload.Query, "__schema") {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"__schema": schema}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{
						"id":   "user-123",
						"name": "Platform",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"linear": {
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					DefaultConnection: "rest",
					Connections: map[string]*providermanifestv1.ManifestConnectionDef{
						"rest":    {Mode: providermanifestv1.ConnectionModeNone},
						"graphql": {Mode: providermanifestv1.ConnectionModeNone},
					},
					Surfaces: &providermanifestv1.ProviderSurfaces{
						OpenAPI: &providermanifestv1.OpenAPISurface{
							Document:   srv.URL + "/openapi.json",
							BaseURL:    srv.URL,
							Connection: "rest",
						},
						GraphQL: &providermanifestv1.GraphQLSurface{
							URL:        srv.URL + "/graphql",
							Connection: "graphql",
						},
					},
				},
			},
		},
	}

	if _, err := bootstrap.Validate(context.Background(), cfg, validFactories()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
	<-result.ProvidersReady

	prov, err := result.Providers.Get("linear")
	if err != nil {
		t.Fatalf("Providers.Get(linear): %v", err)
	}

	if got := prov.ConnectionForOperation("status"); got != "rest" {
		t.Fatalf("ConnectionForOperation(status) = %q, want %q", got, "rest")
	}
	if got := prov.ConnectionForOperation("viewer"); got != "" {
		t.Fatalf("ConnectionForOperation(viewer) = %q, want empty static connection for lazy graphql op", got)
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("Catalog() = nil, want static API catalog")
	}
	if got, ok := invocation.CatalogOperationTransport(cat, "status"); !ok || got != catalog.TransportREST {
		t.Fatalf("status transport = %q, ok=%v, want %q", got, ok, catalog.TransportREST)
	}
	if got, ok := invocation.CatalogOperationTransport(cat, "viewer"); ok {
		t.Fatalf("viewer should not be in the static catalog; got transport = %q", got)
	}

	sessionCat, _, err := core.CatalogForRequest(context.Background(), prov, "")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if got, ok := invocation.CatalogOperationTransport(sessionCat, "viewer"); !ok || got != "graphql" {
		t.Fatalf("session viewer transport = %q, ok=%v, want %q", got, ok, "graphql")
	}

	statusResult, err := prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	if statusResult.Status != http.StatusOK || !strings.Contains(statusResult.Body, `"ok":true`) {
		t.Fatalf("status result = %+v, want 200 with ok body", statusResult)
	}

	viewerResult, err := prov.Execute(context.Background(), "viewer", map[string]any{"team": "platform"}, "")
	if err != nil {
		t.Fatalf("Execute(viewer): %v", err)
	}
	if viewerResult.Status != http.StatusOK || !strings.Contains(viewerResult.Body, `"Platform"`) {
		t.Fatalf("viewer result = %+v, want 200 with graphql body", viewerResult)
	}
}

func TestBootstrapNoIntegrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Plugins = nil

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if got := result.Providers.List(); len(got) != 0 {
		t.Errorf("expected empty providers, got %v", got)
	}
}

func TestBootstrap_ReusesPreparedComponentRuntimeConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()

	authRuntime, err := config.BuildComponentRuntimeConfigNode("authentication", "authentication", selectedAuthenticationEntry(t, cfg), yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "clientId"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "prepared-auth"},
		},
	})
	if err != nil {
		t.Fatalf("BuildComponentRuntimeConfigNode(authentication): %v", err)
	}
	selectedAuthenticationEntry(t, cfg).Config = authRuntime

	var gotAuthNode yaml.Node
	factories := validFactories()
	factories.Auth = func(node yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
		gotAuthNode = node
		return &coretesting.StubAuthProvider{N: "test-auth"}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	authMap, err := config.NodeToMap(gotAuthNode)
	if err != nil {
		t.Fatalf("NodeToMap(auth): %v", err)
	}
	authConfig, ok := authMap["config"].(map[string]any)
	if !ok {
		t.Fatalf("auth runtime config = %#v", authMap["config"])
	}
	if _, nested := authConfig["config"]; nested {
		t.Fatalf("auth config was rewrapped: %#v", authConfig)
	}
	if authConfig["clientId"] != "prepared-auth" {
		t.Fatalf("auth config = %#v", authConfig)
	}

}

func TestBootstrapFactoryError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*bootstrap.FactoryRegistry)
	}{
		{
			name: "auth factory error",
			mutate: func(f *bootstrap.FactoryRegistry) {
				f.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthenticationProvider, error) {
					return nil, fmt.Errorf("auth broke")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			factories := validFactories()
			tc.mutate(factories)
			_, err := bootstrap.Bootstrap(ctx, validConfig(), factories)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestBootstrapClosesExternalCredentialsProviderWhenAuthorizationBuildFails(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.ExternalCredentials = map[string]*config.ProviderEntry{
		"remote": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.ExternalCredentials = "remote"
	cfg.Providers.Authorization = map[string]*config.ProviderEntry{
		"remote": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.Authorization = "remote"

	closed := &atomic.Int32{}
	factories := validFactories()
	factories.ExternalCredentials = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.ExternalCredentialProvider, error) {
		return &closableExternalCredentialProvider{closed: closed}, nil
	}
	factories.Authorization = func(yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
		return nil, fmt.Errorf("authorization broke")
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected authorization build error, got nil")
	}
	if !strings.Contains(err.Error(), "authorization broke") {
		t.Fatalf("Bootstrap error = %v, want authorization failure", err)
	}
	if got := closed.Load(); got != 1 {
		t.Fatalf("external credential provider close count = %d, want 1", got)
	}
}

func TestBootstrapRejectsNilExternalCredentialsProvider(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.ExternalCredentials = map[string]*config.ProviderEntry{
		"remote": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Server.Providers.ExternalCredentials = "remote"

	factories := validFactories()
	factories.ExternalCredentials = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (core.ExternalCredentialProvider, error) {
		var provider *closableExternalCredentialProvider
		return provider, nil
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected nil external credentials provider error, got nil")
	}
	if !strings.Contains(err.Error(), "external credentials provider") || !strings.Contains(err.Error(), "returned nil") {
		t.Fatalf("Bootstrap error = %v, want nil external credentials provider failure", err)
	}
}

func TestBootstrapEncryptionKeyDerivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("passphrase produces 32-byte key", func(t *testing.T) {
		t.Parallel()

		var receivedKey []byte
		factories := validFactories()
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
			receivedKey = deps.EncryptionKey
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = "my-passphrase"

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if len(receivedKey) != 32 {
			t.Errorf("key length: got %d, want 32", len(receivedKey))
		}
	})

	t.Run("hex key is decoded directly", func(t *testing.T) {
		t.Parallel()

		want := make([]byte, 32)
		for i := range want {
			want[i] = byte(i)
		}
		hexKey := hex.EncodeToString(want)

		var receivedKey []byte
		factories := validFactories()
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
			receivedKey = deps.EncryptionKey
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = hexKey

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if hex.EncodeToString(receivedKey) != hexKey {
			t.Errorf("hex key not decoded: got %x, want %x", receivedKey, want)
		}
	})

	t.Run("same passphrase produces same key", func(t *testing.T) {
		t.Parallel()

		var keys [][]byte
		for i := 0; i < 2; i++ {
			factories := validFactories()
			factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
				keys = append(keys, deps.EncryptionKey)
				return &coretesting.StubAuthProvider{N: "test-auth"}, nil
			}
			cfg := validConfig()
			cfg.Server.EncryptionKey = "deterministic"
			result, err := bootstrap.Bootstrap(ctx, cfg, factories)
			if err != nil {
				t.Fatalf("Bootstrap: %v", err)
			}
			<-result.ProvidersReady
		}
		if hex.EncodeToString(keys[0]) != hex.EncodeToString(keys[1]) {
			t.Error("key derivation is not deterministic")
		}
	})
}

func TestBootstrapSecretResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("resolves config secret ref in encryption key", func(t *testing.T) {
		t.Parallel()

		var receivedKey []byte
		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"enc-key": "resolved-passphrase"},
			}, nil
		}
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthenticationProvider, error) {
			receivedKey = deps.EncryptionKey
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = transportSecretRef("enc-key")

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if len(receivedKey) != 32 {
			t.Errorf("key length: got %d, want 32", len(receivedKey))
		}
	})

	t.Run("leaves non-secret values unchanged", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Server.EncryptionKey = "plain-passphrase"

		result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.Auth == nil {
			t.Fatal("Auth is nil")
		}
	})

	t.Run("error on unresolvable secret", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Server.EncryptionKey = transportSecretRef("missing-key")

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "missing-key") {
			t.Errorf("error should mention secret name: %v", err)
		}
	})

	t.Run("error on empty resolved value", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"empty-secret": ""},
			}, nil
		}

		cfg := validConfig()
		cfg.Server.EncryptionKey = transportSecretRef("empty-secret")

		_, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "empty value") {
			t.Errorf("error should mention empty value: %v", err)
		}
	})

	t.Run("resolves config secret ref in yaml.Node auth config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"auth-secret": "resolved-auth-secret"},
			}, nil
		}

		var receivedNode yaml.Node
		factories.Auth = func(node yaml.Node, _ bootstrap.Deps) (core.AuthenticationProvider, error) {
			receivedNode = node
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		selectedAuthenticationEntry(t, cfg).Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "clientSecret", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: transportSecretRef("auth-secret"), Tag: "!!str"},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Source *config.ProviderSource `yaml:"source"`
			Config map[string]string      `yaml:"config"`
		}
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Source == nil || decoded.Source.MetadataURL() != "https://example.invalid/github-com-valon-technologies-gestalt-providers-auth-oidc/v0.0.1-alpha.1/provider-release.yaml" {
			t.Fatalf("source = %+v", decoded.Source)
		}
		if decoded.Config["clientSecret"] != "resolved-auth-secret" {
			t.Errorf("clientSecret: got %q, want %q", decoded.Config["clientSecret"], "resolved-auth-secret")
		}
	})

	t.Run("resolves config secret ref in yaml.Node indexeddb config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"indexeddb-dsn": "mysql://resolved-dsn"},
			}, nil
		}

		var receivedNode yaml.Node
		factories.IndexedDB = func(node yaml.Node) (indexeddb.IndexedDB, error) {
			receivedNode = node
			return &coretesting.StubIndexedDB{}, nil
		}

		cfg := validConfig()
		ds := cfg.Providers.IndexedDB["test"]
		ds.Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "dsn", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: transportSecretRef("indexeddb-dsn"), Tag: "!!str"},
			},
		}
		cfg.Providers.IndexedDB["test"] = ds

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Source *config.ProviderEntry `yaml:"provider"`
			Config map[string]string     `yaml:"config"`
		}
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Config["dsn"] != "mysql://resolved-dsn" {
			t.Errorf("dsn: got %q, want %q", decoded.Config["dsn"], "mysql://resolved-dsn")
		}
	})

	t.Run("resolves config secret ref in yaml.Node s3 config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"s3-token": "resolved-s3-token"},
			}, nil
		}

		var receivedNode yaml.Node
		factories.S3 = func(node yaml.Node) (s3store.Client, error) {
			receivedNode = node
			return &coretesting.StubS3{}, nil
		}

		cfg := validConfig()
		cfg.Providers.S3 = map[string]*config.ProviderEntry{
			"assets": {
				Source: config.ProviderSource{Path: "stub"},
				Config: yaml.Node{
					Kind: yaml.MappingNode,
					Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Value: "token", Tag: "!!str"},
						{Kind: yaml.ScalarNode, Value: transportSecretRef("s3-token"), Tag: "!!str"},
					},
				},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var decoded struct {
			Config map[string]string `yaml:"config"`
		}
		if err := receivedNode.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Config["token"] != "resolved-s3-token" {
			t.Errorf("token: got %q, want %q", decoded.Config["token"], "resolved-s3-token")
		}
	})

	t.Run("resolves config secret ref in runtime provider config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"modal-token-id": "ak-test", "modal-token-secret": "as-test"},
			}, nil
		}

		cfg := validConfig()
		cfg.Runtime.Providers = map[string]*config.RuntimeProviderEntry{
			"modal": {
				ProviderEntry: config.ProviderEntry{
					Config: yaml.Node{
						Kind: yaml.MappingNode,
						Content: []*yaml.Node{
							{Kind: yaml.ScalarNode, Value: "app", Tag: "!!str"},
							{Kind: yaml.ScalarNode, Value: "gestalt-runtime", Tag: "!!str"},
							{Kind: yaml.ScalarNode, Value: "tokenId", Tag: "!!str"},
							{Kind: yaml.ScalarNode, Value: transportSecretRef("modal-token-id"), Tag: "!!str"},
							{Kind: yaml.ScalarNode, Value: "tokenSecret", Tag: "!!str"},
							{Kind: yaml.ScalarNode, Value: transportSecretRef("modal-token-secret"), Tag: "!!str"},
						},
					},
				},
			},
		}

		if err := bootstrap.ResolveConfigSecrets(ctx, cfg, factories); err != nil {
			t.Fatalf("ResolveConfigSecrets: %v", err)
		}

		var decoded map[string]string
		if err := cfg.Runtime.Providers["modal"].Config.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded["tokenId"] != "ak-test" {
			t.Errorf("tokenId: got %q, want %q", decoded["tokenId"], "ak-test")
		}
		if decoded["tokenSecret"] != "as-test" {
			t.Errorf("tokenSecret: got %q, want %q", decoded["tokenSecret"], "as-test")
		}
	})

	t.Run("resolves config secret ref in agent runtime image pull auth", func(t *testing.T) {
		t.Parallel()

		dockerConfigJSON := `{"auths":{"ghcr.io":{"username":"ghcr-user","password":"resolved-ghcr-token"}}}`
		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"ghcr-docker-config": dockerConfigJSON},
			}, nil
		}

		cfg := validConfig()
		cfg.Providers.Agent = map[string]*config.ProviderEntry{
			"simple": {
				Execution: &config.ExecutionConfig{
					Mode: config.ExecutionModeHosted,
					Runtime: &config.HostedRuntimeConfig{
						Image: "ghcr.io/example/simple-agent:latest",
						ImagePullAuth: &config.HostedRuntimeImagePullAuth{
							DockerConfigJSON: transportSecretRef("ghcr-docker-config"),
						},
					},
				},
			},
		}

		if err := bootstrap.ResolveConfigSecrets(ctx, cfg, factories); err != nil {
			t.Fatalf("ResolveConfigSecrets: %v", err)
		}

		auth := cfg.Providers.Agent["simple"].Execution.Runtime.ImagePullAuth
		if auth == nil {
			t.Fatal("imagePullAuth = nil")
			return
		}
		if auth.DockerConfigJSON != dockerConfigJSON {
			t.Fatalf("imagePullAuth.dockerConfigJson = %q, want resolved Docker config JSON", auth.DockerConfigJSON)
		}
	})

	t.Run("authorization provider backs subject access decisions", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"calendar-policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						{SubjectID: "user:static-viewer", Role: "viewer"},
					},
				},
				"admin-policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						{SubjectID: "user:seed-admin", Role: "admin"},
					},
				},
			},
		}
		cfg.Server.Admin.AuthorizationPolicy = "admin-policy"
		cfg.Plugins = map[string]*config.ProviderEntry{
			"calendar": {
				AuthorizationPolicy: "calendar-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		provider := newMemoryAuthorizationProvider("memory-authorization")
		existingModelID := writeMemoryAuthorizationModel(t, provider, authorization.ProviderAuthorizationModelForRoles(
			[]string{"admin", "viewer"},
			[]string{"viewer"},
			[]string{"editor"},
			[]string{"admin"},
		))
		unmanagedKey := bootstrapRelationshipKey(
			&core.SubjectRef{Type: "team", Id: "ops"},
			"owner",
			&core.ResourceRef{Type: "foreign_resource", Id: "roadmap"},
		)
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: "team", Id: "ops"},
			Relation: "owner",
			Resource: &core.ResourceRef{Type: "foreign_resource", Id: "roadmap"},
		})
		factories := validFactories()
		factories.Authorization = memoryAuthorizationFactory(provider)

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady
		dynamicUser, err := result.Services.Users.FindOrCreateUser(ctx, "dynamic@example.test")
		if err != nil {
			t.Fatalf("FindOrCreateUser(dynamic): %v", err)
		}
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(dynamicUser.ID)},
			Relation: "editor",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "calendar"},
		})
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(dynamicUser.ID)},
			Relation: "admin",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAdminDynamic, Id: authorization.ProviderResourceIDAdminDynamicGlobal},
		})

		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if got := provider.activeModelID; got != existingModelID {
			t.Fatalf("active model id = %q, want %q", got, existingModelID)
		}
		if len(provider.models) != 1 {
			t.Fatalf("expected existing model to be reused, got %d models", len(provider.models))
		}
		if _, ok := provider.relsByModel[existingModelID][unmanagedKey]; !ok {
			t.Fatal("expected unrelated provider relationship to be preserved")
		}
		staticPrincipal := &principal.Principal{
			SubjectID: "user:static-viewer",
			UserID:    "static-viewer",
			Identity:  &core.UserIdentity{Email: "static@example.test"},
			Kind:      principal.KindUser,
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, staticPrincipal, "calendar")
		if !allowed {
			t.Fatal("expected static plugin access to be allowed")
		}
		if access.Role != "viewer" {
			t.Fatalf("static plugin role = %q, want %q", access.Role, "viewer")
		}

		dynamicPrincipal := &principal.Principal{
			UserID:    dynamicUser.ID,
			SubjectID: principal.UserSubjectID(dynamicUser.ID),
			Identity:  &core.UserIdentity{Email: dynamicUser.Email},
			Kind:      principal.KindUser,
		}
		access, allowed = result.Authorizer.ResolveAccess(ctx, dynamicPrincipal, "calendar")
		if !allowed {
			t.Fatal("expected dynamic plugin access to be allowed")
		}
		if access.Role != "editor" {
			t.Fatalf("dynamic plugin role = %q, want %q", access.Role, "editor")
		}

		adminAccess, allowed := result.Authorizer.ResolveAdminAccess(ctx, dynamicPrincipal, "admin-policy")
		if !allowed {
			t.Fatal("expected dynamic admin access to be allowed")
		}
		if adminAccess.Role != "admin" {
			t.Fatalf("dynamic admin role = %q, want %q", adminAccess.Role, "admin")
		}
	})

	t.Run("authorization provider honors system workflow principals", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"roadmap-policy": {Default: "deny"},
			},
		}
		cfg.Plugins = map[string]*config.ProviderEntry{
			"roadmap": {
				AuthorizationPolicy: "roadmap-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		provider := newMemoryAuthorizationProvider("memory-authorization")
		factories := validFactories()
		factories.Authorization = memoryAuthorizationFactory(provider)

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}

		configPrincipal := &principal.Principal{
			SubjectID:           "system:config",
			CredentialSubjectID: "system:config",
			TokenPermissions: principal.CompilePermissions([]core.AccessPermission{{
				Plugin:     "roadmap",
				Operations: []string{"sync"},
			}}),
		}
		if !result.Authorizer.AllowProvider(ctx, configPrincipal, "roadmap") {
			t.Fatal("expected config workflow principal to be allowed for roadmap provider")
		}
		if !result.Authorizer.AllowOperation(ctx, configPrincipal, "roadmap", "sync") {
			t.Fatal("expected config workflow principal to be allowed for roadmap.sync")
		}
		if result.Authorizer.AllowOperation(ctx, configPrincipal, "roadmap", "status") {
			t.Fatal("expected config workflow principal to be denied for roadmap.status")
		}
		if !result.Authorizer.AllowCatalogOperation(ctx, configPrincipal, "roadmap", catalog.CatalogOperation{ID: "sync"}) {
			t.Fatal("expected config workflow principal to be allowed for sync catalog operation")
		}
		if result.Authorizer.AllowCatalogOperation(ctx, configPrincipal, "roadmap", catalog.CatalogOperation{ID: "status"}) {
			t.Fatal("expected config workflow principal to be denied for status catalog operation")
		}

	})

	t.Run("non-user resolve access uses subject policy membership", func(t *testing.T) {
		t.Parallel()
		subjectID := "service_account:triage-bot"

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"roadmap-policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{{
						SubjectID: subjectID,
						Role:      "viewer",
					}},
				},
			},
		}
		cfg.Plugins = map[string]*config.ProviderEntry{
			"roadmap": {
				AuthorizationPolicy: "roadmap-policy",
				ConnectionMode:      providermanifestv1.ConnectionModeUser,
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady

		subjectPrincipal := &principal.Principal{
			SubjectID: subjectID,
			Kind:      principal.Kind("service_account"),
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, subjectPrincipal, "roadmap")
		if !allowed {
			t.Fatal("expected non-user ResolveAccess to use subject policy membership")
		}
		if access.Policy != "roadmap-policy" {
			t.Fatalf("subject access policy = %q, want %q", access.Policy, "roadmap-policy")
		}
		if access.Role != "viewer" {
			t.Fatalf("subject access role = %q, want viewer", access.Role)
		}
		if !result.Authorizer.AllowProvider(ctx, subjectPrincipal, "roadmap") {
			t.Fatal("expected non-user subject to be allowed for roadmap provider")
		}
	})

	t.Run("dynamic subject authorizations require an authorization provider", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"calendar-policy": {Default: "deny"},
				"admin-policy":    {Default: "deny"},
			},
		}
		cfg.Server.Admin.AuthorizationPolicy = "admin-policy"
		cfg.Plugins = map[string]*config.ProviderEntry{
			"calendar": {
				AuthorizationPolicy: "calendar-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady

		dynamicUser, err := result.Services.Users.FindOrCreateUser(ctx, "dynamic@example.test")
		if err != nil {
			t.Fatalf("FindOrCreateUser(dynamic): %v", err)
		}
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}

		dynamicPrincipal := &principal.Principal{
			UserID:    dynamicUser.ID,
			SubjectID: principal.UserSubjectID(dynamicUser.ID),
			Identity:  &core.UserIdentity{Email: dynamicUser.Email},
			Kind:      principal.KindUser,
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, dynamicPrincipal, "calendar")
		if allowed {
			t.Fatal("expected dynamic plugin access to be denied without authorization provider")
		}
		if access.Role != "" {
			t.Fatalf("dynamic plugin role without authorization provider = %q, want empty", access.Role)
		}

		adminAccess, allowed := result.Authorizer.ResolveAdminAccess(ctx, dynamicPrincipal, "admin-policy")
		if allowed {
			t.Fatal("expected dynamic admin access to be denied without authorization provider")
		}
		if adminAccess.Role != "" {
			t.Fatalf("dynamic admin role without authorization provider = %q, want empty", adminAccess.Role)
		}
	})

	t.Run("authorization provider rehydrates human canonical state on restart", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"calendar-policy": {Default: "deny"},
				"admin-policy":    {Default: "deny"},
			},
		}
		cfg.Server.Admin.AuthorizationPolicy = "admin-policy"
		cfg.Plugins = map[string]*config.ProviderEntry{
			"calendar": {
				AuthorizationPolicy: "calendar-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		db := &coretesting.StubIndexedDB{}
		provider := newMemoryAuthorizationProvider("memory-authorization")
		writeMemoryAuthorizationModel(t, provider, authorization.ProviderAuthorizationModelForRoles(
			nil,
			nil,
			[]string{"editor"},
			[]string{"admin"},
		))

		factories := validFactories()
		factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
			return db, nil
		}
		factories.Authorization = memoryAuthorizationFactory(provider)

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap(first): %v", err)
		}
		<-result.ProvidersReady

		dynamicUser, err := result.Services.Users.FindOrCreateUser(ctx, "dynamic@example.test")
		if err != nil {
			t.Fatalf("FindOrCreateUser(dynamic): %v", err)
		}
		provider.putRelationship(provider.activeModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(dynamicUser.ID)},
			Relation: "editor",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "calendar"},
		})
		provider.putRelationship(provider.activeModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(dynamicUser.ID)},
			Relation: "admin",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAdminDynamic, Id: authorization.ProviderResourceIDAdminDynamicGlobal},
		})
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start(first): %v", err)
		}

		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close(first): %v", err)
		}

		result, err = bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap(second): %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start(second): %v", err)
		}

		dynamicPrincipal := &principal.Principal{
			UserID:    dynamicUser.ID,
			SubjectID: principal.UserSubjectID(dynamicUser.ID),
			Identity:  &core.UserIdentity{Email: dynamicUser.Email},
			Kind:      principal.KindUser,
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, dynamicPrincipal, "calendar")
		if !allowed {
			t.Fatal("expected provider-backed plugin access after restart")
		}
		if access.Role != "editor" {
			t.Fatalf("provider-backed plugin role after restart = %q, want %q", access.Role, "editor")
		}

		adminAccess, allowed := result.Authorizer.ResolveAdminAccess(ctx, dynamicPrincipal, "admin-policy")
		if !allowed {
			t.Fatal("expected provider-backed admin access after restart")
		}
		if adminAccess.Role != "admin" {
			t.Fatalf("provider-backed admin role after restart = %q, want %q", adminAccess.Role, "admin")
		}

	})

	t.Run("authorization provider preserves existing provider dynamic roles", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"calendar-policy": {Default: "deny"},
				"admin-policy":    {Default: "deny"},
			},
		}
		cfg.Server.Admin.AuthorizationPolicy = "admin-policy"
		cfg.Plugins = map[string]*config.ProviderEntry{
			"calendar": {
				AuthorizationPolicy: "calendar-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		provider := newMemoryAuthorizationProvider("memory-authorization")
		existingModelID := writeMemoryAuthorizationModel(t, provider, authorization.ProviderAuthorizationModelForRoles(
			nil,
			nil,
			[]string{"editor", "viewer"},
			[]string{"admin", "operator"},
		))

		factories := validFactories()
		factories.Authorization = memoryAuthorizationFactory(provider)

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady

		dynamicUser, err := result.Services.Users.FindOrCreateUser(ctx, "dynamic@example.test")
		if err != nil {
			t.Fatalf("FindOrCreateUser(dynamic): %v", err)
		}
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(dynamicUser.ID)},
			Relation: "viewer",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "calendar"},
		})
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeSubject, Id: principal.UserSubjectID(dynamicUser.ID)},
			Relation: "operator",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAdminDynamic, Id: authorization.ProviderResourceIDAdminDynamicGlobal},
		})

		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}

		dynamicPrincipal := &principal.Principal{
			UserID:    dynamicUser.ID,
			SubjectID: principal.UserSubjectID(dynamicUser.ID),
			Identity:  &core.UserIdentity{Email: dynamicUser.Email},
			Kind:      principal.KindUser,
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, dynamicPrincipal, "calendar")
		if !allowed {
			t.Fatal("expected provider-backed plugin access to be allowed")
		}
		if access.Role != "viewer" {
			t.Fatalf("provider-backed plugin role = %q, want %q", access.Role, "viewer")
		}

		adminAccess, allowed := result.Authorizer.ResolveAdminAccess(ctx, dynamicPrincipal, "admin-policy")
		if !allowed {
			t.Fatal("expected provider-backed admin access to be allowed")
		}
		if adminAccess.Role != "operator" {
			t.Fatalf("provider-backed admin role = %q, want %q", adminAccess.Role, "operator")
		}
	})

	t.Run("authorization provider provisions a new model when the active model is unmanaged", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"calendar-policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						{SubjectID: "user:static-viewer", Role: "viewer"},
					},
				},
			},
		}
		cfg.Plugins = map[string]*config.ProviderEntry{
			"calendar": {
				AuthorizationPolicy: "calendar-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		provider := newMemoryAuthorizationProvider("memory-authorization")
		provider.models = []*core.AuthorizationModelRef{{
			Id:      "model-existing",
			Version: "v1",
		}}
		provider.activeModelID = "model-existing"
		provider.relsByModel["model-existing"] = map[string]*core.Relationship{}
		unmanagedKey := bootstrapRelationshipKey(
			&core.SubjectRef{Type: "team", Id: "ops"},
			"owner",
			&core.ResourceRef{Type: "foreign_resource", Id: "roadmap"},
		)
		provider.putRelationship("model-existing", &core.Relationship{
			Subject:  &core.SubjectRef{Type: "team", Id: "ops"},
			Relation: "owner",
			Resource: &core.ResourceRef{Type: "foreign_resource", Id: "roadmap"},
		})

		factories := validFactories()
		factories.Authorization = memoryAuthorizationFactory(provider)

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if got := provider.activeModelID; got == "model-existing" {
			t.Fatalf("expected a newly provisioned model, active model remained %q", got)
		}
		if len(provider.models) != 2 {
			t.Fatalf("expected a new model to be written, got %d models", len(provider.models))
		}
		if _, ok := provider.relsByModel["model-existing"][unmanagedKey]; !ok {
			t.Fatal("expected unmanaged relationships on the old model to be preserved")
		}
	})

	t.Run("authorization provider denies until reload heals active model drift", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.SubjectPolicyDef{
				"calendar-policy": {
					Default: "deny",
					Members: []config.SubjectPolicyMemberDef{
						{SubjectID: "user:static-viewer", Role: "viewer"},
					},
				},
			},
		}
		cfg.Plugins = map[string]*config.ProviderEntry{
			"calendar": {
				AuthorizationPolicy: "calendar-policy",
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{},
				},
			},
		}
		cfg.Providers.Authorization = map[string]*config.ProviderEntry{
			"indexeddb": {Source: config.ProviderSource{Path: "stub"}},
		}
		cfg.Server.Providers.Authorization = "indexeddb"

		provider := newMemoryAuthorizationProvider("memory-authorization")
		factories := validFactories()
		factories.Authorization = memoryAuthorizationFactory(provider)

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		t.Cleanup(func() { _ = result.Close(context.Background()) })
		<-result.ProvidersReady
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		managedModelID := provider.activeModelID

		provider.mu.Lock()
		provider.models = append(provider.models, &core.AuthorizationModelRef{Id: "model-foreign", Version: "v1"})
		provider.relsByModel["model-foreign"] = map[string]*core.Relationship{}
		provider.activeModelID = "model-foreign"
		provider.mu.Unlock()

		staticPrincipal := &principal.Principal{
			SubjectID: "user:static-viewer",
			UserID:    "static-viewer",
			Identity:  &core.UserIdentity{Email: "static@example.test"},
			Kind:      principal.KindUser,
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, staticPrincipal, "calendar")
		if allowed {
			t.Fatal("expected access to be denied while the provider active model drifts")
		}
		if access.Role != "" {
			t.Fatalf("role during active model drift = %q, want empty", access.Role)
		}
		if err := result.Authorizer.ReloadAuthorizationState(ctx); err != nil {
			t.Fatalf("expected authorization state reload to heal active model drift: %v", err)
		}
		if got := provider.activeModelID; got != managedModelID {
			t.Fatalf("active model id after reload = %q, want %q", got, managedModelID)
		}
		access, allowed = result.Authorizer.ResolveAccess(ctx, staticPrincipal, "calendar")
		if !allowed {
			t.Fatal("expected access to recover after provider reload heals active model drift")
		}
		if access.Role != "viewer" {
			t.Fatalf("role after healing active model drift = %q, want %q", access.Role, "viewer")
		}
	})

	t.Run("ignores secret refs inside secrets provider config", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"enc-key": "resolved-passphrase"},
			}, nil
		}

		cfg := validConfig()
		cfg.Providers.Secrets["default"] = &config.ProviderEntry{
			Source: config.ProviderSource{Builtin: "test-secrets"},
			Config: yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "prefix", Tag: "!!str"},
					{Kind: yaml.ScalarNode, Value: transportSecretRef("ignored-provider-secret"), Tag: "!!str"},
				},
			},
		}
		cfg.Server.EncryptionKey = config.EncodeSecretRefTransport(config.SecretRef{
			Provider: "default",
			Name:     "enc-key",
		})

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
	})

	t.Run("requires configured provider for programmatic config refs", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		delete(cfg.Providers.Secrets, "default")
		cfg.Server.EncryptionKey = config.EncodeSecretRefTransport(config.SecretRef{
			Provider: "env",
			Name:     "GESTALT_ENCRYPTION_KEY",
		})

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `unknown secrets provider "env"`) {
			t.Fatalf("expected unknown provider error, got %v", err)
		}
	})

	t.Run("configured secrets provider without source errors with config key", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Secrets["default"] = &config.ProviderEntry{}

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `secrets provider "default" has no source`) {
			t.Fatalf("expected missing source error, got %v", err)
		}
	})

	t.Run("configured builtin secrets provider errors keep config key", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Secrets["default"] = &config.ProviderEntry{
			Source: config.ProviderSource{Builtin: "missing-builtin"},
		}

		_, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `secrets provider "default" references unknown builtin "missing-builtin"`) {
			t.Fatalf("expected config-key builtin error, got %v", err)
		}
	})

	t.Run("passes top-level provider selection to auth factory", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Authentication = map[string]*config.ProviderEntry{
			"secondary": {Source: config.NewMetadataSource("https://example.invalid/github-com-valon-technologies-gestalt-providers-auth-oidc/v0.0.1-alpha.1/provider-release.yaml")},
		}
		cfg.Server.Providers.Authentication = "secondary"
		cfg.Providers.Authentication["secondary"].Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "issuerUrl", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: "https://issuer.example.test", Tag: "!!str"},
			},
		}

		var authNode yaml.Node
		factories := validFactories()
		factories.Auth = func(node yaml.Node, _ bootstrap.Deps) (core.AuthenticationProvider, error) {
			authNode = node
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		var authCfg struct {
			Source *config.ProviderSource `yaml:"source"`
			Config map[string]string      `yaml:"config"`
		}
		if err := authNode.Decode(&authCfg); err != nil {
			t.Fatalf("decode auth node: %v", err)
		}
		if authCfg.Source == nil || authCfg.Source.MetadataURL() != "https://example.invalid/github-com-valon-technologies-gestalt-providers-auth-oidc/v0.0.1-alpha.1/provider-release.yaml" {
			t.Fatalf("auth source = %+v", authCfg.Source)
		}
		if authCfg.Config["issuerUrl"] != "https://issuer.example.test" {
			t.Fatalf("auth config = %+v", authCfg.Config)
		}
	})

	t.Run("omits authentication when the authentication provider is unset", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Authentication = nil
		cfg.Server.Providers.Authentication = ""

		var authFactoryCalled atomic.Bool
		factories := validFactories()
		factories.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthenticationProvider, error) {
			authFactoryCalled.Store(true)
			return &coretesting.StubAuthProvider{N: "unexpected"}, nil
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.Auth != nil {
			t.Fatalf("Auth = %T, want nil", result.Auth)
		}
		if authFactoryCalled.Load() {
			t.Fatal("auth factory was called")
		}
	})

	t.Run("result includes SecretManager", func(t *testing.T) {
		t.Parallel()

		result, err := bootstrap.Bootstrap(ctx, validConfig(), validFactories())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady
		if result.SecretManager == nil {
			t.Fatal("SecretManager is nil")
		}
	})

	t.Run("secrets factory error", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return nil, fmt.Errorf("secrets broke")
		}

		_, err := bootstrap.Bootstrap(ctx, validConfig(), factories)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "secrets broke") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBootstrapRejectsBuiltinEitherProviderWithoutAuthorizationConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	factories := validFactories()
	factories.Builtins = []core.Provider{
		&coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionMode("either")},
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), `unsupported connection mode "either"`) {
		t.Fatalf("Bootstrap error = %v, want unsupported connection mode either", err)
	}
}
func TestBootstrapWorkflowAuthorizationAllowsNormalizedCredentialedProvider(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"svc": {
			ConnectionMode: providermanifestv1.ConnectionModeUser,
			ResolvedManifest: &providermanifestv1.Manifest{
				Spec: &providermanifestv1.Spec{
					Surfaces: &providermanifestv1.ProviderSurfaces{
						REST: &providermanifestv1.RESTSurface{
							BaseURL: srv.URL,
							Operations: []providermanifestv1.ProviderOperation{
								{Name: "run", Method: http.MethodPost, Path: "/run"},
							},
						},
					},
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Authorization = config.AuthorizationConfig{}

	factories := validFactories()
	factories.Workflow = func(context.Context, string, yaml.Node, []runtimehost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
		return &stubWorkflowProvider{}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
}
