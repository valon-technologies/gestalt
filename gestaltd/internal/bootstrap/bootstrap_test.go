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

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	graphqlschema "github.com/valon-technologies/gestalt/server/services/apps/graphql"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	telemetrynoop "github.com/valon-technologies/gestalt/server/services/observability/drivers/noop"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

func bootstrapGraphQLStringPtr(value string) *string {
	return &value
}

func bootstrapTextAgentOutput() *proto.AgentOutput {
	return &proto.AgentOutput{Kind: &proto.AgentOutput_Text{Text: &proto.AgentTextOutput{}}}
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
		if !bootstrapRelationshipMatches(rel, req) {
			continue
		}
		out = append(out, cloneRelationship(rel))
		if req.GetPageSize() > 0 && int32(len(out)) >= req.GetPageSize() {
			break
		}
	}
	return &core.ReadRelationshipsResponse{
		Relationships: out,
		ModelId:       modelID,
	}, nil
}

func bootstrapRelationshipMatches(rel *core.Relationship, req *core.ReadRelationshipsRequest) bool {
	if rel == nil {
		return false
	}
	if subject := req.GetSubject(); subject != nil {
		if rel.GetSubject().GetType() != subject.GetType() || rel.GetSubject().GetId() != subject.GetId() {
			return false
		}
	}
	if relation := strings.TrimSpace(req.GetRelation()); relation != "" && rel.GetRelation() != relation {
		return false
	}
	if resource := req.GetResource(); resource != nil {
		if rel.GetResource().GetType() != resource.GetType() || rel.GetResource().GetId() != resource.GetId() {
			return false
		}
	}
	return true
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

func (s *stubWorkflowProvider) CreateDefinition(context.Context, coreworkflow.CreateDefinitionRequest) (*coreworkflow.Definition, error) {
	return &coreworkflow.Definition{}, nil
}
func (s *stubWorkflowProvider) GetDefinition(context.Context, coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	return &coreworkflow.Definition{}, nil
}
func (s *stubWorkflowProvider) UpdateDefinition(context.Context, coreworkflow.UpdateDefinitionRequest) (*coreworkflow.Definition, error) {
	return &coreworkflow.Definition{}, nil
}
func (s *stubWorkflowProvider) DeleteDefinition(context.Context, coreworkflow.DeleteDefinitionRequest) error {
	return nil
}
func (s *stubWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (s *stubWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (s *stubWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	return &coreworkflow.ListRunsResponse{}, nil
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
func (s *stubWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	return &req.Event, nil
}
func (s *stubWorkflowProvider) Ping(context.Context) error { return nil }
func (s *stubWorkflowProvider) Close() error               { return nil }

type recordingAgentProvider struct {
	coreagent.UnimplementedProvider
	mu                         sync.Mutex
	createSessionRequests      []*proto.CreateAgentProviderSessionRequest
	updateSessionRequests      []*proto.UpdateAgentProviderSessionRequest
	createTurnRequests         []*proto.CreateAgentProviderTurnRequest
	cancelTurnRequests         []*proto.CancelAgentProviderTurnRequest
	resolveInteractionRequests []*proto.ResolveAgentProviderInteractionRequest
	sessions                   map[string]*coreagent.Session
	turns                      map[string]*coreagent.Turn
	turnEvents                 map[string][]*coreagent.TurnEvent
	interactions               map[string]*coreagent.Interaction
	sessionIdempotency         map[string]string
	turnIdempotency            map[string]string
	cancelTurnStatus           coreagent.ExecutionStatus
}

func bootstrapAgentProtoStructToMap(src *structpb.Struct) map[string]any {
	if src == nil {
		return nil
	}
	return src.AsMap()
}

func bootstrapAgentMapToProtoStruct(src map[string]any) *structpb.Struct {
	if src == nil {
		return nil
	}
	out, _ := structpb.NewStruct(src)
	return out
}

func bootstrapAgentActorFromProto(src *proto.AgentActor) coreagent.Actor {
	if src == nil {
		return coreagent.Actor{}
	}
	return coreagent.Actor{
		SubjectID:   src.GetSubjectId(),
		SubjectKind: src.GetSubjectKind(),
		DisplayName: src.GetDisplayName(),
		AuthSource:  src.GetAuthSource(),
	}
}

func bootstrapAgentActorToProto(src coreagent.Actor) *proto.AgentActor {
	if src == (coreagent.Actor{}) {
		return nil
	}
	return &proto.AgentActor{
		SubjectId:   src.SubjectID,
		SubjectKind: src.SubjectKind,
		DisplayName: src.DisplayName,
		AuthSource:  src.AuthSource,
	}
}

func bootstrapAgentSubjectFromProto(src *proto.SubjectContext) core.RunAsSubject {
	if src == nil {
		return core.RunAsSubject{}
	}
	return core.RunAsSubject{
		SubjectID:           src.GetId(),
		SubjectKind:         src.GetKind(),
		CredentialSubjectID: src.GetCredentialSubjectId(),
		DisplayName:         src.GetDisplayName(),
		AuthSource:          src.GetAuthSource(),
	}
}

func bootstrapAgentMessagesFromProto(src []*proto.AgentMessage) []coreagent.Message {
	out := make([]coreagent.Message, 0, len(src))
	for _, message := range src {
		if message == nil {
			continue
		}
		out = append(out, coreagent.Message{
			Role:     message.GetRole(),
			Text:     message.GetText(),
			Metadata: bootstrapAgentProtoStructToMap(message.GetMetadata()),
		})
	}
	return out
}

func bootstrapAgentToolsFromProto(src []*proto.ResolvedAgentTool) []coreagent.Tool {
	out := make([]coreagent.Tool, 0, len(src))
	for _, tool := range src {
		if tool == nil {
			continue
		}
		out = append(out, coreagent.Tool{
			ID:          tool.GetId(),
			Name:        tool.GetName(),
			Description: tool.GetDescription(),
		})
	}
	return out
}

func bootstrapAgentToolsToProto(src []coreagent.Tool) []*proto.ResolvedAgentTool {
	out := make([]*proto.ResolvedAgentTool, 0, len(src))
	for _, tool := range src {
		out = append(out, &proto.ResolvedAgentTool{
			Id:          tool.ID,
			Name:        tool.Name,
			Description: tool.Description,
		})
	}
	return out
}

func bootstrapAgentSessionStateFromProto(src proto.AgentSessionState) coreagent.SessionState {
	switch src {
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return coreagent.SessionStateActive
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return coreagent.SessionStateArchived
	default:
		return ""
	}
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

func agentProviderSessionIdempotencyScope(subject core.RunAsSubject, actor coreagent.Actor, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{"session", agentProviderSubjectScope(subject, actor), idempotencyKey}, "\x00")
}

func agentProviderTurnIdempotencyScope(subject core.RunAsSubject, actor coreagent.Actor, sessionID, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{"turn", agentProviderSubjectScope(subject, actor), strings.TrimSpace(sessionID), idempotencyKey}, "\x00")
}

func agentProviderSubjectScope(subject core.RunAsSubject, actor coreagent.Actor) string {
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

func (p *recordingAgentProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	subject := bootstrapAgentSubjectFromProto(req.GetSubject())
	createdBy := bootstrapAgentActorFromProto(req.GetCreatedBy())
	idempotencyScope := agentProviderSessionIdempotencyScope(subject, createdBy, req.GetIdempotencyKey())
	if sessionID, ok := p.sessionIdempotency[idempotencyScope]; idempotencyScope != "" && ok {
		session, ok := p.sessions[sessionID]
		if !ok {
			return nil, core.ErrNotFound
		}
		return cloneBootstrapAgentSession(session), nil
	}
	p.createSessionRequests = append(p.createSessionRequests, gproto.Clone(req).(*proto.CreateAgentProviderSessionRequest))
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", len(p.sessions)+1)
	}
	now := time.Now().UTC().Truncate(time.Second)
	session := &coreagent.Session{
		ID:         sessionID,
		Model:      strings.TrimSpace(req.GetModel()),
		ClientRef:  strings.TrimSpace(req.GetClientRef()),
		State:      coreagent.SessionStateActive,
		Metadata:   bootstrapAgentProtoStructToMap(req.GetMetadata()),
		CreatedBy:  createdBy,
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

func (p *recordingAgentProvider) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneBootstrapAgentSession(session), nil
}

func (p *recordingAgentProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*coreagent.Session, 0, len(p.sessions))
	for _, session := range p.sessions {
		out = append(out, cloneBootstrapAgentSession(session))
	}
	return out, nil
}

func (p *recordingAgentProvider) UpdateSession(_ context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	p.updateSessionRequests = append(p.updateSessionRequests, gproto.Clone(req).(*proto.UpdateAgentProviderSessionRequest))
	session, ok := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if !ok {
		return nil, core.ErrNotFound
	}
	if clientRef := strings.TrimSpace(req.GetClientRef()); clientRef != "" {
		session.ClientRef = clientRef
	}
	if state := bootstrapAgentSessionStateFromProto(req.GetState()); state != "" {
		session.State = state
	}
	if req.GetMetadata() != nil {
		session.Metadata = bootstrapAgentProtoStructToMap(req.GetMetadata())
	}
	now := time.Now().UTC().Truncate(time.Second)
	session.UpdatedAt = &now
	return cloneBootstrapAgentSession(session), nil
}

func (p *recordingAgentProvider) CreateTurn(_ context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	subject := bootstrapAgentSubjectFromProto(req.GetSubject())
	createdBy := bootstrapAgentActorFromProto(req.GetCreatedBy())
	idempotencyScope := agentProviderTurnIdempotencyScope(subject, createdBy, req.GetSessionId(), req.GetIdempotencyKey())
	if turnID, ok := p.turnIdempotency[idempotencyScope]; idempotencyScope != "" && ok {
		turn, ok := p.turns[turnID]
		if !ok {
			return nil, core.ErrNotFound
		}
		return cloneBootstrapAgentTurn(turn), nil
	}
	p.createTurnRequests = append(p.createTurnRequests, gproto.Clone(req).(*proto.CreateAgentProviderTurnRequest))
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		turnID = fmt.Sprintf("turn-%d", len(p.turns)+1)
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn := &coreagent.Turn{
		ID:           turnID,
		SessionID:    strings.TrimSpace(req.GetSessionId()),
		Model:        strings.TrimSpace(req.GetModel()),
		Status:       coreagent.ExecutionStatusSucceeded,
		Messages:     cloneBootstrapAgentMessages(bootstrapAgentMessagesFromProto(req.GetMessages())),
		Output:       coreagent.TurnOutput{Text: &coreagent.TurnTextOutput{Text: "turn completed"}},
		CreatedBy:    createdBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		CompletedAt:  &now,
		ExecutionRef: strings.TrimSpace(req.GetExecutionRef()),
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

func (p *recordingAgentProvider) GetTurn(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[strings.TrimSpace(req.GetTurnId())]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneBootstrapAgentTurn(turn), nil
}

func (p *recordingAgentProvider) ListTurns(_ context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := strings.TrimSpace(req.GetSessionId())
	out := make([]*coreagent.Turn, 0, len(p.turns))
	for _, turn := range p.turns {
		if sessionID != "" && strings.TrimSpace(turn.SessionID) != sessionID {
			continue
		}
		out = append(out, cloneBootstrapAgentTurn(turn))
	}
	return out, nil
}

func (p *recordingAgentProvider) CancelTurn(_ context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureStateLocked()
	p.cancelTurnRequests = append(p.cancelTurnRequests, gproto.Clone(req).(*proto.CancelAgentProviderTurnRequest))
	turnID := strings.TrimSpace(req.GetTurnId())
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
	turn.StatusMessage = strings.TrimSpace(req.GetReason())
	if turnStatusIsTerminalForTest(status) {
		turn.CompletedAt = &now
	} else {
		turn.CompletedAt = nil
	}
	return cloneBootstrapAgentTurn(turn), nil
}

func (p *recordingAgentProvider) ListTurnEvents(_ context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turnEvents[strings.TrimSpace(req.GetTurnId())]
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event.Seq <= req.AfterSeq {
			continue
		}
		out = append(out, cloneBootstrapAgentTurnEvent(event))
		if req.GetLimit() > 0 && len(out) >= int(req.GetLimit()) {
			break
		}
	}
	return out, nil
}

func (p *recordingAgentProvider) GetInteraction(_ context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[strings.TrimSpace(req.GetInteractionId())]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneBootstrapAgentInteraction(interaction), nil
}

func (p *recordingAgentProvider) ListInteractions(_ context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnID := strings.TrimSpace(req.GetTurnId())
	out := make([]*coreagent.Interaction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if turnID != "" && strings.TrimSpace(interaction.TurnID) != turnID {
			continue
		}
		out = append(out, cloneBootstrapAgentInteraction(interaction))
	}
	return out, nil
}

func (p *recordingAgentProvider) ResolveInteraction(_ context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolveInteractionRequests = append(p.resolveInteractionRequests, gproto.Clone(req).(*proto.ResolveAgentProviderInteractionRequest))
	interaction, ok := p.interactions[strings.TrimSpace(req.GetInteractionId())]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = bootstrapAgentProtoStructToMap(req.GetResolution())
	interaction.ResolvedAt = &now
	return cloneBootstrapAgentInteraction(interaction), nil
}

func (p *recordingAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
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

func (p *recordingAgentProvider) CancelTurnRequests() []*proto.CancelAgentProviderTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*proto.CancelAgentProviderTurnRequest(nil), p.cancelTurnRequests...)
}

type callbackAgentProvider struct {
	*recordingAgentProvider
	started                *runtimehost.StartedHostServices
	socketPath             string
	listRequests           []*proto.ListAgentToolsRequest
	listResponses          []*proto.ListAgentToolsResponse
	toolBodies             []string
	resolveInteractionHook func(context.Context, *proto.ResolveAgentProviderInteractionRequest) error
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
	socketPath := strings.TrimSpace(started.SocketBinding().SocketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("agent host socket binding is missing")
	}
	return &callbackAgentProvider{
		recordingAgentProvider: newRecordingAgentProvider(),
		started:                started,
		socketPath:             socketPath,
	}, nil
}

func (p *callbackAgentProvider) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	p.ensureStateLocked()
	subject := bootstrapAgentSubjectFromProto(req.GetSubject())
	createdBy := bootstrapAgentActorFromProto(req.GetCreatedBy())
	idempotencyScope := agentProviderTurnIdempotencyScope(subject, createdBy, req.GetSessionId(), req.GetIdempotencyKey())
	if turnID, ok := p.turnIdempotency[idempotencyScope]; idempotencyScope != "" && ok {
		turn, ok := p.turns[turnID]
		p.mu.Unlock()
		if !ok {
			return nil, core.ErrNotFound
		}
		return cloneBootstrapAgentTurn(turn), nil
	}
	p.createTurnRequests = append(p.createTurnRequests, gproto.Clone(req).(*proto.CreateAgentProviderTurnRequest))
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		turnID = fmt.Sprintf("turn-%d", len(p.turns)+1)
	}
	needsInteraction, _ := bootstrapAgentProtoStructToMap(req.GetMetadata())["requireInteraction"].(bool)
	if !needsInteraction {
		for _, message := range bootstrapAgentMessagesFromProto(req.GetMessages()) {
			if strings.TrimSpace(message.Text) == "request approval" {
				needsInteraction = true
				break
			}
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn := &coreagent.Turn{
		ID:           turnID,
		SessionID:    strings.TrimSpace(req.GetSessionId()),
		Model:        strings.TrimSpace(req.GetModel()),
		Status:       coreagent.ExecutionStatusRunning,
		Messages:     cloneBootstrapAgentMessages(bootstrapAgentMessagesFromProto(req.GetMessages())),
		CreatedBy:    createdBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		ExecutionRef: strings.TrimSpace(req.GetExecutionRef()),
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
	if req.GetToolSource() == proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG || len(req.GetTools()) > 0 {
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
		tools := bootstrapAgentToolsFromProto(req.GetTools())
		if len(tools) == 0 && req.GetToolSource() == proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG {
			listReq := &proto.ListAgentToolsRequest{
				SessionId: req.GetSessionId(),
				TurnId:    turnID,
				PageSize:  5,
				RunGrant:  req.GetRunGrant(),
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
				SessionId:  req.GetSessionId(),
				TurnId:     turnID,
				ToolCallId: "tool-call-1",
				ToolId:     tools[0].ID,
				RunGrant:   req.GetRunGrant(),
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
	turn.Output = coreagent.TurnOutput{Text: &coreagent.TurnTextOutput{Text: outputBody}}
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

func (p *callbackAgentProvider) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	if p.resolveInteractionHook != nil {
		if err := p.resolveInteractionHook(ctx, req); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolveInteractionRequests = append(p.resolveInteractionRequests, gproto.Clone(req).(*proto.ResolveAgentProviderInteractionRequest))
	interaction, ok := p.interactions[strings.TrimSpace(req.GetInteractionId())]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = bootstrapAgentProtoStructToMap(req.GetResolution())
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

func (p *callbackAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
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
	cancelRequests []*proto.CancelAgentProviderTurnRequest
}

func (p *generatedIDAgentProvider) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return &coreagent.Session{ID: "generated-session-1", State: coreagent.SessionStateActive}, nil
}

func (p *generatedIDAgentProvider) GetSession(context.Context, *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) CreateTurn(_ context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return &coreagent.Turn{ID: "generated-turn-1", SessionID: req.GetSessionId(), Status: coreagent.ExecutionStatusRunning}, nil
}

func (p *generatedIDAgentProvider) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	return nil, nil
}

func (p *generatedIDAgentProvider) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	return nil, core.ErrNotFound
}

func (p *generatedIDAgentProvider) CancelTurn(_ context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelRequests = append(p.cancelRequests, gproto.Clone(req).(*proto.CancelAgentProviderTurnRequest))
	return &coreagent.Turn{ID: req.GetTurnId(), Status: coreagent.ExecutionStatusCanceled}, nil
}

func (p *generatedIDAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
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

func (p *generatedIDAgentProvider) CancelTurnRequests() []*proto.CancelAgentProviderTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*proto.CancelAgentProviderTurnRequest(nil), p.cancelRequests...)
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
	if src.Output.Text != nil {
		dst.Output.Text = &coreagent.TurnTextOutput{Text: src.Output.Text.Text}
	}
	if src.Output.Structured != nil {
		dst.Output.Structured = &coreagent.TurnStructuredOutput{
			Text:  src.Output.Structured.Text,
			Value: maps.Clone(src.Output.Structured.Value),
		}
	}
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
	definitions                map[string]*coreworkflow.Definition
	nextDefinitionID           int
	upsertedSchedules          []coreworkflow.UpsertScheduleRequest
	upsertedScheduleTokens     []string
	listedSchedules            []*coreworkflow.Schedule
	listSchedulesErr           error
	deletedSchedules           []coreworkflow.DeleteScheduleRequest
	deleteScheduleErr          error
	getSchedule                *coreworkflow.Schedule
	getScheduleErr             error
	schedules                  map[string]*coreworkflow.Schedule
	upsertedEventTriggers      []coreworkflow.UpsertEventTriggerRequest
	upsertedEventTriggerTokens []string
	listedEventTriggers        []*coreworkflow.EventTrigger
	listEventTriggersErr       error
	deletedEventTriggers       []coreworkflow.DeleteEventTriggerRequest
	deleteEventTriggerErr      error
	getEventTrigger            *coreworkflow.EventTrigger
	getEventTriggerErr         error
	eventTriggers              map[string]*coreworkflow.EventTrigger
	deleteMissingNotFound      bool
	deleteEventMissingNotFound bool
	closed                     *atomic.Bool
}

func (p *recordingWorkflowProvider) CreateDefinition(_ context.Context, req coreworkflow.CreateDefinitionRequest) (*coreworkflow.Definition, error) {
	if p.definitions == nil {
		p.definitions = map[string]*coreworkflow.Definition{}
	}
	p.nextDefinitionID++
	id := fmt.Sprintf("workflow-definition:%d", p.nextDefinitionID)
	if req.IdempotencyKey != "" {
		id = "workflow-definition:" + strings.TrimSpace(req.IdempotencyKey)
	}
	if existing := p.definitions[id]; existing != nil {
		value := *existing
		return &value, nil
	}
	definition := &coreworkflow.Definition{
		ID:        id,
		Target:    cloneBootstrapWorkflowTarget(req.Target),
		CreatedBy: req.CreatedBy,
	}
	p.definitions[id] = definition
	value := *definition
	return &value, nil
}

func (p *recordingWorkflowProvider) GetDefinition(_ context.Context, req coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	if definition := p.definitions[strings.TrimSpace(req.DefinitionID)]; definition != nil {
		value := *definition
		value.Target = cloneBootstrapWorkflowTarget(definition.Target)
		return &value, nil
	}
	return nil, core.ErrNotFound
}

func (p *recordingWorkflowProvider) UpdateDefinition(_ context.Context, req coreworkflow.UpdateDefinitionRequest) (*coreworkflow.Definition, error) {
	if p.definitions == nil {
		p.definitions = map[string]*coreworkflow.Definition{}
	}
	id := strings.TrimSpace(req.DefinitionID)
	if p.definitions[id] == nil {
		return nil, core.ErrNotFound
	}
	definition := &coreworkflow.Definition{
		ID:     id,
		Target: cloneBootstrapWorkflowTarget(req.Target),
	}
	p.definitions[id] = definition
	value := *definition
	return &value, nil
}

func (p *recordingWorkflowProvider) DeleteDefinition(_ context.Context, req coreworkflow.DeleteDefinitionRequest) error {
	if p.definitions == nil || p.definitions[strings.TrimSpace(req.DefinitionID)] == nil {
		return core.ErrNotFound
	}
	delete(p.definitions, strings.TrimSpace(req.DefinitionID))
	return nil
}

func (p *recordingWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *recordingWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}
func (p *recordingWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	return &coreworkflow.ListRunsResponse{}, nil
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
func (p *recordingWorkflowProvider) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, req)
	p.upsertedScheduleTokens = append(p.upsertedScheduleTokens, appaccessservice.InvocationTokenFromContext(ctx))
	schedule := &coreworkflow.Schedule{
		ID:           req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       req.Target,
		DefinitionID: req.DefinitionID,
		Paused:       req.Paused,
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
func (p *recordingWorkflowProvider) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	p.upsertedEventTriggers = append(p.upsertedEventTriggers, req)
	p.upsertedEventTriggerTokens = append(p.upsertedEventTriggerTokens, appaccessservice.InvocationTokenFromContext(ctx))
	trigger := &coreworkflow.EventTrigger{
		ID:           req.TriggerID,
		Match:        req.Match,
		Target:       req.Target,
		DefinitionID: req.DefinitionID,
		Paused:       req.Paused,
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
func (p *recordingWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	return &req.Event, nil
}
func (p *recordingWorkflowProvider) Ping(context.Context) error { return nil }
func (p *recordingWorkflowProvider) Close() error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
}

type recordingWorkflowConfigAppInvoker struct {
	providerName string
	operation    string
	subjectID    string
}

func (i *recordingWorkflowConfigAppInvoker) Invoke(_ context.Context, p *principal.Principal, providerName, _ string, operation string, _ map[string]any) (*core.OperationResult, error) {
	i.providerName = providerName
	i.operation = operation
	if p != nil {
		i.subjectID = p.SubjectID
	}
	return &core.OperationResult{Status: http.StatusOK}, nil
}

func requireWorkflowConfigTokenContext(t *testing.T, encryptionKey []byte, token string) appaccessservice.TokenContext {
	t.Helper()
	if strings.TrimSpace(token) == "" {
		t.Fatal("upsert invocation token is empty")
	}
	tokens, err := appaccessservice.NewInvocationTokenManager(encryptionKey)
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	tokenCtx, err := tokens.ResolveToken(token, "system:config")
	if err != nil {
		t.Fatalf("ResolveToken(config token): %v", err)
	}
	return tokenCtx
}

func assertWorkflowConfigTokenAllowsAppInvoke(t *testing.T, encryptionKey []byte, token, appName, operation string) {
	t.Helper()
	tokens, err := appaccessservice.NewInvocationTokenManager(encryptionKey)
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	invoker := &recordingWorkflowConfigAppInvoker{}
	appServer := appaccessservice.NewAppServer(invoker, tokens)
	if _, err := appServer.Invoke(context.Background(), &proto.AppInvokeRequest{
		InvocationToken: token,
		App:             appName,
		Operation:       operation,
	}); err != nil {
		t.Fatalf("Invoke(%s.%s) with config token: %v", appName, operation, err)
	}
	if invoker.providerName != appName || invoker.operation != operation {
		t.Fatalf("invoked %s.%s, want %s.%s", invoker.providerName, invoker.operation, appName, operation)
	}
}

func (p *recordingWorkflowProvider) scheduleGetResponse(schedule *coreworkflow.Schedule) *coreworkflow.Schedule {
	if schedule == nil {
		return nil
	}
	value := *schedule
	return &value
}

func cloneBootstrapWorkflowTarget(target coreworkflow.Target) coreworkflow.Target {
	clone := coreworkflow.Target{Steps: make([]coreworkflow.Step, len(target.Steps))}
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Inputs != nil {
			step.Inputs = make(map[string]coreworkflow.Value, len(step.Inputs))
			for key, value := range target.Steps[i].Inputs {
				step.Inputs[key] = coreworkflow.CloneValue(value)
			}
		}
		if step.App != nil {
			app := *step.App
			app.Input = coreworkflow.CloneValue(app.Input)
			step.App = &app
		}
		if step.Agent != nil {
			agent := *step.Agent
			agent.Messages = slices.Clone(agent.Messages)
			for j := range agent.Messages {
				agent.Messages[j].Metadata = maps.Clone(agent.Messages[j].Metadata)
			}
			agent.ToolRefs = slices.Clone(agent.ToolRefs)
			if agent.Output.Structured != nil {
				structured := *agent.Output.Structured
				structured.Schema = maps.Clone(structured.Schema)
				agent.Output.Structured = &structured
			}
			agent.ModelOptions = maps.Clone(agent.ModelOptions)
			step.Agent = &agent
		}
		if step.When != nil {
			when := *step.When
			when.Value = coreworkflow.CloneValue(when.Value)
			step.When = &when
		}
		step.Metadata = maps.Clone(step.Metadata)
		clone.Steps[i] = step
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

type trackedCache struct {
	*coretesting.StubCache
	closed *atomic.Int32
}

func (c *trackedCache) Close() error {
	if c.closed != nil {
		c.closed.Add(1)
	}
	return nil
}

func validConfig() *config.Config {
	return &config.Config{
		Apps: map[string]*config.ProviderEntry{},
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
				"test": {Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml")},
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

func requireHostService(t *testing.T, hostServices []runtimehost.HostService, name string) runtimehost.HostService {
	t.Helper()
	for _, hostService := range hostServices {
		if hostService.Name == name {
			return hostService
		}
	}
	t.Fatalf("host services = %v, want %q", hostServiceNames(hostServices), name)
	return runtimehost.HostService{}
}

func hostServiceNames(hostServices []runtimehost.HostService) []string {
	names := make([]string, 0, len(hostServices))
	for _, hostService := range hostServices {
		names = append(names, hostService.Name)
	}
	return names
}

func invokeAgentHostCallback(t *testing.T, hostServices []runtimehost.HostService, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	t.Helper()

	hostService := requireHostService(t, hostServices, "agent_host")
	if hostService.Register == nil {
		t.Fatal("agent host register func is nil")
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
	t.Cleanup(func() { _ = conn.Close() })

	return proto.NewAgentHostClient(conn).ExecuteTool(context.Background(), req)
}

func invokeAgentHostListTools(t *testing.T, hostServices []runtimehost.HostService, req *proto.ListAgentToolsRequest) *proto.ListAgentToolsResponse {
	t.Helper()

	hostService := requireHostService(t, hostServices, "agent_host")
	if hostService.Register == nil {
		t.Fatal("agent host register func is nil")
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
	cfg.Apps = map[string]*config.ProviderEntry{
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

func setWorkflowFixture(cfg *config.Config, app string, workflow *workflowFixture) {
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
		if workflowFixtureTargetApp(schedule.Target) == app {
			delete(cfg.Workflows.Schedules, key)
		}
	}
	for key, trigger := range cfg.Workflows.EventTriggers {
		if workflowFixtureTargetApp(trigger.Target) == app {
			delete(cfg.Workflows.EventTriggers, key)
		}
	}
	if workflow == nil {
		return
	}
	for key, schedule := range workflow.Schedules {
		cfg.Workflows.Schedules[key] = config.WorkflowScheduleConfig{
			Provider: workflow.Provider,
			Target:   workflowFixtureTarget(app, schedule.Operation, schedule.Input),
			Cron:     schedule.Cron,
			Timezone: schedule.Timezone,
			Paused:   schedule.Paused,
		}
	}
	for key, trigger := range workflow.EventTriggers {
		cfg.Workflows.EventTriggers[key] = config.WorkflowEventTriggerConfig{
			Provider: workflow.Provider,
			Target:   workflowFixtureTarget(app, trigger.Operation, trigger.Input),
			Match: config.WorkflowEventMatch{
				Type:    trigger.Match.Type,
				Source:  trigger.Match.Source,
				Subject: trigger.Match.Subject,
			},
			Paused: trigger.Paused,
		}
	}
}

func workflowFixtureTarget(app, operation string, input map[string]any) *config.WorkflowTargetConfig {
	return &config.WorkflowTargetConfig{
		Steps: []config.WorkflowStepConfig{{
			ID: operation,
			App: &config.WorkflowStepAppCallConfig{
				Name:      app,
				Operation: operation,
				Input:     workflowFixtureValue(input),
			},
		}},
	}
}

func workflowFixtureValue(input map[string]any) config.WorkflowValueConfig {
	if len(input) == 0 {
		return config.WorkflowValueConfig{}
	}
	fields := make(map[string]config.WorkflowValueConfig, len(input))
	for key, value := range input {
		fields[key] = config.WorkflowValueConfig{Literal: value, LiteralSet: true}
	}
	return config.WorkflowValueConfig{Object: fields}
}

func requireCoreWorkflowAppStep(t *testing.T, target coreworkflow.Target) *coreworkflow.AppCall {
	t.Helper()
	if len(target.Steps) == 0 || target.Steps[0].App == nil {
		t.Fatalf("target app step is nil: %#v", target)
	}
	return target.Steps[0].App
}

func coreWorkflowAppStepTarget(appName, operation string) coreworkflow.Target {
	return coreworkflow.Target{
		Steps: []coreworkflow.Step{{ID: operation, App: &coreworkflow.AppCall{Name: appName, Operation: operation}}},
	}
}

func workflowFixtureTargetApp(target *config.WorkflowTargetConfig) string {
	if target == nil || len(target.Steps) == 0 || target.Steps[0].App == nil {
		return ""
	}
	return target.Steps[0].App.Name
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
				cfg.Apps = map[string]*config.ProviderEntry{
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

func TestBootstrapResultClosesExtraCaches(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.Cache = map[string]*config.ProviderEntry{
		"primary": {
			Source: config.NewMetadataSource("https://example.invalid/cache/primary/v0.0.1/provider-release.yaml"),
		},
		"archive": {
			Source: config.NewMetadataSource("https://example.invalid/cache/archive/v0.0.1/provider-release.yaml"),
		},
	}

	factories := validFactories()
	var closeCount atomic.Int32
	factories.Cache = func(yaml.Node) (corecache.Cache, error) {
		return &trackedCache{
			StubCache: coretesting.NewStubCache(),
			closed:    &closeCount,
		}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if got := len(result.ExtraCaches); got != 2 {
		t.Fatalf("ExtraCaches = %d, want 2", got)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := closeCount.Load(); got != 2 {
		t.Fatalf("cache close count = %d, want 2", got)
	}
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
	factories.S3 = func(node yaml.Node) (s3sdk.S3, error) {
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
	var seenMu sync.Mutex
	factories.Workflow = func(_ context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		seenMu.Lock()
		seen[runtime.Name] = struct{}{}
		seenMu.Unlock()
		if requireHostService(t, hostServices, "agent_provider").Register == nil {
			return nil, fmt.Errorf("workflow provider missing agent_provider host service")
		}
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
	var seenMu sync.Mutex
	factories.Agent = func(_ context.Context, name string, node yaml.Node, hostServices []runtimehost.HostService, _ bootstrap.Deps) (coreagent.Provider, error) {
		var runtime struct {
			Name string `yaml:"name"`
		}
		if err := node.Decode(&runtime); err != nil {
			return nil, err
		}
		seenMu.Lock()
		seen[runtime.Name] = struct{}{}
		hostSockets[name] = requireHostService(t, hostServices, "agent_host").Name
		seenMu.Unlock()
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
		if got := hostSockets[name]; got != "agent_host" {
			t.Fatalf("agent host env for %q = %q, want %q", name, got, "agent_host")
		}
	}
	if got := result.AgentControl.ProviderNames(); !reflect.DeepEqual(got, []string{"cleanup", "reviewer"}) {
		t.Fatalf("agent provider names = %#v, want %#v", got, []string{"cleanup", "reviewer"})
	}
	selectedName, provider, err := result.AgentControl.ResolveProvider(context.Background(), "")
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	if selectedName != "reviewer" {
		t.Fatalf("selected agent provider = %q, want %q", selectedName, "reviewer")
	}
	session, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
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
			IndexedDB: &config.IndexedDBBindingConfig{
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
			App:        "roadmap",
			Operations: []string{"sync"},
		},
		{
			App:        "lever",
			Operations: []string{"sync"},
		},
		{
			App:        "ashby",
			Operations: []string{"sync"},
		},
		{
			App: "managed",
		},
	})
	p := &principal.Principal{
		SubjectID:           "user:user-123",
		UserID:              "user-123",
		CredentialSubjectID: "service_account:agent-credential",
		Kind:                principal.KindUser,
		Source:              principal.SourceSession,
		TokenPermissions:    perms,
		Scopes:              principal.PermissionApps(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-1",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	req := &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "demo-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "sync it"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs: []*proto.AgentToolRef{{
			App:       "roadmap",
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
	if createTurnReq.GetTurnId() != first.ID {
		t.Fatalf("CreateTurn turn_id = %q, want %q", createTurnReq.GetTurnId(), first.ID)
	}
	if createTurnReq.GetSessionId() != session.ID {
		t.Fatalf("CreateTurn session_id = %q, want %q", createTurnReq.GetSessionId(), session.ID)
	}
	if createTurnReq.GetExecutionRef() != first.ID {
		t.Fatalf("CreateTurn execution_ref = %q, want %q", createTurnReq.GetExecutionRef(), first.ID)
	}
	if createTurnReq.GetCreatedBy().GetSubjectId() != p.SubjectID {
		t.Fatalf("CreateTurn created_by.subject_id = %q, want %q", createTurnReq.GetCreatedBy().GetSubjectId(), p.SubjectID)
	}
	if len(createTurnReq.GetTools()) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", createTurnReq.GetTools())
	}
	if createTurnReq.GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG {
		t.Fatalf("CreateTurn tool source = %q, want mcp_catalog", createTurnReq.GetToolSource())
	}
	if len(createTurnReq.GetToolRefs()) != 1 || createTurnReq.GetToolRefs()[0].GetApp() != "roadmap" || createTurnReq.GetToolRefs()[0].GetOperation() != "sync" {
		t.Fatalf("CreateTurn tool refs = %#v", createTurnReq.GetToolRefs())
	}
	if strings.TrimSpace(createTurnReq.GetRunGrant()) == "" {
		t.Fatal("CreateTurn run_grant is empty")
	}
	if len(toolBodies) != 1 || !strings.Contains(toolBodies[0], `"subject":"user:user-123"`) || !strings.Contains(toolBodies[0], `"taskId":"task-123"`) {
		t.Fatalf("tool callback bodies = %#v", toolBodies)
	}

	_, err = result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "global-search-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "sync it without explicit tools"}},
		ToolRefsSet:    true,
		Output:         bootstrapTextAgentOutput(),
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

	_, err = result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "scoped-unavailable-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "sync ashby"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "ashby", Operation: "sync"}},
	})
	if err == nil || !strings.Contains(err.Error(), `no external credential stored for integration "ashby"`) {
		t.Fatalf("AgentManager.CreateTurn(scoped unavailable) error = %v, want ashby credential error", err)
	}
}

func TestBootstrapAgentHostToolCatalogExecutesExactAppIssueTool(t *testing.T) {
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
			App:        name,
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
			App: "managed",
		},
		{
			App:        "linear",
			Operations: []string{"list_issues", "list_comments", "list_customers", "list_documents"},
		},
		{
			App:        "customerRoadmapReview",
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
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-linear-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "linear-search-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "get my linear tickets"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "linear", Operation: "list_issues"}},
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

	second, err := result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "linear-search-after-unavailable-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "get my assigned tickets"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "linear", Operation: "list_issues"}},
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
				{ID: "epsilon_delete", Method: http.MethodDelete, Title: "Docs epsilon delete", Description: "Delete docs epsilon", Annotations: catalog.CapabilityAnnotations{DestructiveHint: &destructive}},
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
		App:        "docs",
		Operations: []string{"aardvark_admin", "alpha_search", "beta_list", "delta_export", "epsilon_delete", "gamma_get"},
	}, {
		App: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-candidate-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "candidate-search-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "search docs"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "docs"}},
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
	exact, err := result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "candidate-load-ref-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "load beta docs"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "docs", Operation: betaOperation}},
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

	mixed, err := result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "candidate-mixed-global-exact-hidden-idempotency-key",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "load hidden docs"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs: []*proto.AgentToolRef{
			{App: "*"},
			{App: "docs", Operation: "aardvark_admin"},
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
		if tool.GetRef().GetApp() == "docs" && tool.GetRef().GetOperation() == "aardvark_admin" {
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
		App:        "linear",
		Operations: []string{"issues"},
	}, {
		App:        "github",
		Operations: []string{"issues"},
	}, {
		App: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := principal.WithPrincipal(context.Background(), p)

	session, err := result.AgentManager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-configured-narrowing",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(ctx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "configured-narrowing-linear",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "show me my linear tickets"}},
		Output:         bootstrapTextAgentOutput(),
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
	if len(tools) != 1 || tools[0].GetRef().GetApp() != "linear" || tools[0].GetRef().GetOperation() != "issues" {
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
		App: "slack",
		Operations: []string{
			"events.reply",
			"events.setStatus",
		},
	}, {
		App: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: slackOnly,
		Scopes:           principal.PermissionApps(slackOnly),
	}
	ctx := invocation.WithInvocationSurface(principal.WithPrincipal(context.Background(), p), invocation.InvocationSurfaceHTTP)

	session, err := result.AgentManager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-http-slack-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(agentmanager.WithCallerAppName(ctx, "slack"), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "http-slack-linear-search",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "get my linear tickets"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "*"}},
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
		App:        "linear",
		Operations: []string{"issues"},
	}, {
		App:        "ashby",
		Operations: []string{"candidates"},
	}, {
		App: "managed",
	}})
	p := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	ctx := invocation.WithInvocationSurface(principal.WithPrincipal(context.Background(), p), invocation.InvocationSurfaceHTTP)

	session, err := result.AgentManager.CreateSession(ctx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-http-global-search",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(agentmanager.WithCallerAppName(ctx, "slack"), p, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "http-global-linear-search",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "get my linear tickets"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs:       []*proto.AgentToolRef{{App: "*"}},
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
	if tools[0].GetRef().GetApp() != "linear" || tools[0].GetRef().GetOperation() != "issues" {
		t.Fatalf("first listed tool = %#v, want connected linear issues before unavailable sentinels", tools[0])
	}
	if tools[1].GetRef().GetApp() != "ashby" || tools[1].GetRef().GetOperation() != "" || tools[1].GetMcpName() != "ashby__no_credential" {
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

	_, selected, err := result.AgentControl.ResolveProvider(context.Background(), "")
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}
	startCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "system:config"})
	if _, err := selected.CreateSession(startCtx, &proto.CreateAgentProviderSessionRequest{
		SessionId: "agent-session-plain",
		Model:     "gpt-test",
		CreatedBy: bootstrapAgentActorToProto(coreagent.Actor{SubjectID: "system:config"}),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := selected.CreateTurn(startCtx, &proto.CreateAgentProviderTurnRequest{
		TurnId:    "agent-turn-plain",
		SessionId: "agent-session-plain",
		Model:     "gpt-test",
		CreatedBy: bootstrapAgentActorToProto(coreagent.Actor{SubjectID: "system:config"}),
		Output:    bootstrapTextAgentOutput(),
		Messages: []*proto.AgentMessage{{
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

	interactions, err := selected.ListInteractions(context.Background(), &proto.ListAgentProviderInteractionsRequest{TurnId: "agent-turn-plain"})
	if err != nil {
		t.Fatalf("ListInteractions: %v", err)
	}
	if len(interactions) != 1 || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interactions = %#v, want one pending interaction", interactions)
	}

	provider.resolveInteractionHook = func(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) error {
		current, err := selected.GetInteraction(ctx, &proto.GetAgentProviderInteractionRequest{InteractionId: req.GetInteractionId()})
		if err != nil {
			return err
		}
		if current.State != coreagent.InteractionStatePending || current.TurnID != "agent-turn-plain" {
			return fmt.Errorf("interaction during direct provider resolve = %#v, want pending agent-turn-plain", current)
		}
		return nil
	}

	resolved, err := selected.ResolveInteraction(startCtx, &proto.ResolveAgentProviderInteractionRequest{
		InteractionId: interactions[0].ID,
		Resolution: func() *structpb.Struct {
			out, err := structpb.NewStruct(map[string]any{"approved": true})
			if err != nil {
				t.Fatalf("structpb.NewStruct: %v", err)
			}
			return out
		}(),
	})
	if err != nil {
		t.Fatalf("ResolveInteraction: %v", err)
	}
	if resolved == nil || resolved.State != coreagent.InteractionStateResolved || resolved.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolved)
	}

	resolvedInteractions, err := selected.ListInteractions(context.Background(), &proto.ListAgentProviderInteractionsRequest{TurnId: "agent-turn-plain"})
	if err != nil {
		t.Fatalf("ListInteractions(resolved): %v", err)
	}
	if len(resolvedInteractions) != 1 || resolvedInteractions[0].State != coreagent.InteractionStateResolved || resolvedInteractions[0].Resolution["approved"] != true {
		t.Fatalf("resolved interactions = %#v, want one resolved interaction", resolvedInteractions)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.createTurnRequests) != 1 || len(provider.createTurnRequests[0].GetTools()) != 0 || len(bootstrapAgentProtoStructToMap(provider.createTurnRequests[0].GetMetadata())) != 0 {
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
	session, err := result.AgentManager.CreateSession(startCtx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		Messages: []*proto.AgentMessage{{
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

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, &proto.ListAgentProviderInteractionsRequest{TurnId: turn.ID})
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interactions = %#v, want one pending interaction", interactions)
	}

	provider.resolveInteractionHook = func(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) error {
		current, err := provider.GetInteraction(ctx, &proto.GetAgentProviderInteractionRequest{InteractionId: req.GetInteractionId()})
		if err != nil {
			return err
		}
		if current.State != coreagent.InteractionStatePending || current.TurnID != turn.ID {
			return fmt.Errorf("interaction during manager resolve = %#v, want pending %q", current, turn.ID)
		}
		return nil
	}

	resolved, err := result.AgentManager.ResolveInteraction(startCtx, p, &proto.ResolveAgentProviderInteractionRequest{
		TurnId:        turn.ID,
		InteractionId: interactions[0].ID,
		Resolution:    bootstrapAgentMapToProtoStruct(map[string]any{"approved": true}),
	})
	if err != nil {
		t.Fatalf("AgentManager.ResolveInteraction: %v", err)
	}
	if resolved == nil || resolved.State != coreagent.InteractionStateResolved || resolved.Resolution["approved"] != true {
		t.Fatalf("resolved interaction = %#v, want resolved approved interaction", resolved)
	}

	resolvedInteractions, err := result.AgentManager.ListInteractions(startCtx, p, &proto.ListAgentProviderInteractionsRequest{TurnId: turn.ID})
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
	session, err := result.AgentManager.CreateSession(startCtx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		Messages: []*proto.AgentMessage{{
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

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, &proto.ListAgentProviderInteractionsRequest{TurnId: turn.ID})
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 || interactions[0].State != coreagent.InteractionStatePending {
		t.Fatalf("interactions = %#v, want one pending interaction", interactions)
	}

	provider.resolveInteractionHook = func(context.Context, *proto.ResolveAgentProviderInteractionRequest) error {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		delete(provider.interactions, interactions[0].ID)
		return nil
	}

	_, err = result.AgentManager.ResolveInteraction(startCtx, p, &proto.ResolveAgentProviderInteractionRequest{
		TurnId:        turn.ID,
		InteractionId: interactions[0].ID,
		Resolution:    bootstrapAgentMapToProtoStruct(map[string]any{"approved": true}),
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
	session, err := result.AgentManager.CreateSession(startCtx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		Messages: []*proto.AgentMessage{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, &proto.ListAgentProviderInteractionsRequest{TurnId: turn.ID})
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %#v, want one interaction", interactions)
	}

	provider.resolveInteractionHook = func(context.Context, *proto.ResolveAgentProviderInteractionRequest) error {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		current := provider.interactions[interactions[0].ID]
		if current == nil {
			return fmt.Errorf("interaction %q not found", interactions[0].ID)
		}
		current.ID = "interaction-mismatch"
		return nil
	}

	_, err = result.AgentManager.ResolveInteraction(startCtx, p, &proto.ResolveAgentProviderInteractionRequest{
		TurnId:        turn.ID,
		InteractionId: interactions[0].ID,
		Resolution:    bootstrapAgentMapToProtoStruct(map[string]any{"approved": true}),
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
	session, err := result.AgentManager.CreateSession(startCtx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		Messages: []*proto.AgentMessage{{
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

	if _, err := result.AgentManager.ListInteractions(startCtx, p, &proto.ListAgentProviderInteractionsRequest{TurnId: turn.ID}); err == nil {
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
	session, err := result.AgentManager.CreateSession(startCtx, p, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, p, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		Messages: []*proto.AgentMessage{{
			Role: "user",
			Text: "request approval",
		}},
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateTurn: %v", err)
	}

	interactions, err := result.AgentManager.ListInteractions(startCtx, p, &proto.ListAgentProviderInteractionsRequest{TurnId: turn.ID})
	if err != nil {
		t.Fatalf("AgentManager.ListInteractions: %v", err)
	}
	if len(interactions) != 1 {
		t.Fatalf("interactions = %#v, want one interaction", interactions)
	}

	provider.resolveInteractionHook = func(context.Context, *proto.ResolveAgentProviderInteractionRequest) error {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		current := provider.interactions[interactions[0].ID]
		if current == nil {
			return fmt.Errorf("interaction %q not found", interactions[0].ID)
		}
		current.SessionID = ""
		return nil
	}

	if _, err := result.AgentManager.ResolveInteraction(startCtx, p, &proto.ResolveAgentProviderInteractionRequest{
		TurnId:        turn.ID,
		InteractionId: interactions[0].ID,
		Resolution:    bootstrapAgentMapToProtoStruct(map[string]any{"approved": true}),
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
		App:        "roadmap",
		Operations: []string{"sync"},
	}, {
		App: "managed",
	}})
	full := &principal.Principal{
		SubjectID:        "user:user-123",
		UserID:           "user-123",
		Kind:             principal.KindUser,
		Source:           principal.SourceSession,
		TokenPermissions: perms,
		Scopes:           principal.PermissionApps(perms),
	}
	fullCtx := principal.WithPrincipal(context.Background(), full)

	session, err := result.AgentManager.CreateSession(fullCtx, full, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("AgentManager.CreateSession: %v", err)
	}

	first, err := result.AgentManager.CreateTurn(fullCtx, full, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "same-run",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "sync it"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs: []*proto.AgentToolRef{{
			App:       "roadmap",
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
		TokenPermissions: principal.CompilePermissions([]core.AccessPermission{{App: "managed"}}),
		Scopes:           []string{"managed"},
	}
	restrictedCtx := principal.WithPrincipal(context.Background(), restricted)

	_, err = result.AgentManager.CreateTurn(restrictedCtx, restricted, &proto.CreateAgentProviderTurnRequest{
		SessionId:      session.ID,
		IdempotencyKey: "same-run",
		Model:          "gpt-test",
		Messages:       []*proto.AgentMessage{{Role: "user", Text: "sync it"}},
		Output:         bootstrapTextAgentOutput(),
		ToolRefs: []*proto.AgentToolRef{{
			App:       "roadmap",
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
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.IndexedDBBindingConfig{
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
			envs = append(envs, hostService.Name)
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
	for _, want := range []string{"agent_provider", "indexeddb", "app", "workflow_provider"} {
		if !slices.Contains(got, want) {
			t.Fatalf("workflow provider host services = %v, want %q", got, want)
		}
	}
}

func TestBootstrapPassesIndexedDBHostSocketToAgentProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["agent_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
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
			IndexedDB: &config.IndexedDBBindingConfig{
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

	requireHostService(t, hostServices, "agent_host")
	indexedDBService := requireHostService(t, hostServices, "indexeddb")

	withIndexedDBHostClient(t, indexedDBService, func(client proto.IndexedDBClient) {
		agentStateCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(runtimehost.HostServiceBindingHeader, "agent_state"))
		if _, err := client.CreateObjectStore(agentStateCtx, &proto.CreateObjectStoreRequest{
			Name:   "runs",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(runs): %v", err)
		}
		record, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": "run-1", "status": "running"})
		if err != nil {
			t.Fatalf("RecordToProto: %v", err)
		}
		if _, err := client.Put(agentStateCtx, &proto.RecordRequest{
			Store:  "runs",
			Record: record,
		}); err != nil {
			t.Fatalf("Put(runs): %v", err)
		}
		resp, err := client.Get(agentStateCtx, &proto.ObjectStoreRequest{
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

		if _, err := client.CreateObjectStore(agentStateCtx, &proto.CreateObjectStoreRequest{
			Name:   "sessions",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(sessions): %v", err)
		}
	})

	if _, err := boundDB.ObjectStore("runs").Get(context.Background(), "run-1"); err != nil {
		t.Fatalf("logical backing store should contain run: %v", err)
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

	requireHostService(t, hostServices, "indexeddb")
	requireHostService(t, hostServices, "app")
	requireHostService(t, hostServices, "workflow_provider")
	requireHostService(t, hostServices, "agent_provider")
}

func TestBootstrapClosesWorkflowIndexedDBAndAppliesScopedConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
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
			IndexedDB: &config.IndexedDBBindingConfig{
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
		if decoded.Config["dsn"] == "sqlite://workflow.db" {
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

	if got := captured["table_prefix"]; got != "host_" {
		t.Fatalf("table_prefix = %#v, want %q", got, "host_")
	}
	if got := captured["prefix"]; got != "host_" {
		t.Fatalf("prefix = %#v, want %q", got, "host_")
	}
	if got := captured["schema"]; got != "should_be_removed" {
		t.Fatalf("schema = %#v, want %q", got, "should_be_removed")
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

	indexedDBService := requireHostService(t, hostServices, "indexeddb")

	withIndexedDBHostClient(t, indexedDBService, func(client proto.IndexedDBClient) {
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "external_credentials",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(external_credentials): %v", err)
		}
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "app_credentials",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(app_credentials): %v", err)
		}
		archiveCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(runtimehost.HostServiceBindingHeader, "archive"))
		if _, err := client.CreateObjectStore(archiveCtx, &proto.CreateObjectStoreRequest{
			Name:   "external_credentials",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(external_credentials archive binding): %v", err)
		}
	})
}

func TestBootstrapRoutesWorkflowIndexedDBHostServices(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
		Config: mustYAMLNode(t, map[string]any{"bucket": "workflow-state"}),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.IndexedDBBindingConfig{
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
	factories.IndexedDB = func(node yaml.Node) (indexeddb.IndexedDB, error) {
		var decoded struct {
			Config map[string]any `yaml:"config"`
		}
		if err := node.Decode(&decoded); err != nil {
			return nil, err
		}
		db := &trackedIndexedDB{
			StubIndexedDB: &coretesting.StubIndexedDB{},
			closed:        &closeCount,
		}
		if decoded.Config["bucket"] == "workflow-state" {
			boundDB = db
		}
		return db, nil
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

	indexedDBService := requireHostService(t, hostEnv, "indexeddb")

	withIndexedDBHostClient(t, indexedDBService, func(client proto.IndexedDBClient) {
		workflowStateCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(runtimehost.HostServiceBindingHeader, "workflow_state"))
		if _, err := client.CreateObjectStore(workflowStateCtx, &proto.CreateObjectStoreRequest{
			Name:   "workflow_runs",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(workflow_runs): %v", err)
		}
		record, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": "run-1", "status": "pending"})
		if err != nil {
			t.Fatalf("RecordToProto: %v", err)
		}
		if _, err := client.Put(workflowStateCtx, &proto.RecordRequest{
			Store:  "workflow_runs",
			Record: record,
		}); err != nil {
			t.Fatalf("Put(workflow_runs): %v", err)
		}
		resp, err := client.Get(workflowStateCtx, &proto.ObjectStoreRequest{
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

		if _, err := client.CreateObjectStore(workflowStateCtx, &proto.CreateObjectStoreRequest{
			Name:   "workflow_schedules",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(workflow_schedules): %v", err)
		}
	})

	if _, err := boundDB.ObjectStore("workflow_runs").Get(context.Background(), "run-1"); err != nil {
		t.Fatalf("logical backing store should contain run: %v", err)
	}
}

func TestBootstrapAppliesConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModeNone,
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
	nightly.Invokes = []config.WorkflowInvokeConfig{
		{App: "slack", Operation: "conversations.list"},
		{App: "slack", Operation: "conversations.history"},
	}
	cfg.Workflows.Schedules["nightly_sync"] = nightly
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	var workflowEncryptionKey []byte
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		workflowEncryptionKey = append([]byte(nil), deps.EncryptionKey...)
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
	gotApp := requireCoreWorkflowAppStep(t, got.Target)
	if gotApp.Name != "roadmap" || gotApp.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if gotApp.Input.Object["source"].Literal != "yaml" {
		t.Fatalf("target input = %#v", gotApp.Input)
	}
	if got.RequestedBy.SubjectID != "system:config" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
	if len(recorder.upsertedScheduleTokens) != 1 {
		t.Fatalf("upserted schedule tokens = %d, want 1", len(recorder.upsertedScheduleTokens))
	}
	tokenCtx := requireWorkflowConfigTokenContext(t, workflowEncryptionKey, recorder.upsertedScheduleTokens[0])
	if got := tokenCtx.CallerApp(); got != "system:config" {
		t.Fatalf("token caller app = %q, want system:config", got)
	}
	tokenPrincipal := tokenCtx.Principal()
	if tokenPrincipal == nil || tokenPrincipal.SubjectID != "system:config" || tokenPrincipal.Kind != "system" || tokenPrincipal.AuthSource() != "config" {
		t.Fatalf("token principal = %#v", tokenPrincipal)
	}
	if !principal.AllowsOperationPermission(tokenPrincipal, "roadmap", "sync") {
		t.Fatalf("token principal does not allow roadmap.sync: %#v", tokenPrincipal.TokenPermissions)
	}
	if !principal.AllowsOperationPermission(tokenPrincipal, "slack", "conversations.history") {
		t.Fatalf("token principal does not allow slack.conversations.history: %#v", tokenPrincipal.TokenPermissions)
	}
	assertWorkflowConfigTokenAllowsAppInvoke(t, workflowEncryptionKey, recorder.upsertedScheduleTokens[0], "roadmap", "sync")
	assertWorkflowConfigTokenAllowsAppInvoke(t, workflowEncryptionKey, recorder.upsertedScheduleTokens[0], "slack", "conversations.history")
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

func TestBootstrapRejectsConfiguredWorkflowSchedulesForUserCredentialedApps(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
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
	if !strings.Contains(err.Error(), `config-managed workflows do not support user-credentialed app "roadmap"`) {
		t.Fatalf("Bootstrap error = %v", err)
	}
}

func TestBootstrapAllowsConfiguredWorkflowScheduleCredentialModeNoneForUserCredentialedApps(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
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
	nightly.Target.Steps[0].App.CredentialMode = providermanifestv1.ConnectionModeNone
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
	gotApp := requireCoreWorkflowAppStep(t, recorder.upsertedSchedules[0].Target)
	if gotApp.CredentialMode != core.ConnectionModeNone {
		t.Fatalf("target credential mode = %q, want %q", gotApp.CredentialMode, core.ConnectionModeNone)
	}
}

func TestBootstrapConfiguredWorkflowScheduleRunAsAllowsUserCredentialedTarget(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
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
	nightly.RunAs = &config.WorkflowRunAsConfig{
		Subject: &config.WorkflowRunAsSubjectConfig{
			ID:          " service_account:roadmap-sync ",
			DisplayName: " Roadmap sync ",
		},
	}
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
	got := recorder.upsertedSchedules[0]
	if got.RequestedBy.SubjectID != "system:config" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
}

func TestBootstrapRejectsConfiguredWorkflowScheduleRunAsUserSubject(t *testing.T) {
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
	nightly := cfg.Workflows.Schedules["nightly_sync"]
	nightly.RunAs = &config.WorkflowRunAsConfig{
		Subject: &config.WorkflowRunAsSubjectConfig{ID: "user:ada"},
	}
	cfg.Workflows.Schedules["nightly_sync"] = nightly

	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return &recordingWorkflowProvider{}, nil
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected Bootstrap to reject user runAs subject")
	}
	if !strings.Contains(err.Error(), `workflows.schedules.nightly_sync.runAs.subject.id "user:ada" must identify a service_account subject`) {
		t.Fatalf("Bootstrap error = %v", err)
	}
}

func TestBootstrapAppliesConfiguredWorkflowSchedulesForRunAsConnectionOnUserDefaultApp(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
	cfg.Apps["roadmap"].Connections = map[string]*config.ConnectionDef{
		"bot": {
			Mode: providermanifestv1.ConnectionModeUser,
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
	nightly.Target.Steps[0].App.Connection = "bot"
	nightly.RunAs = &config.WorkflowRunAsConfig{
		Subject: &config.WorkflowRunAsSubjectConfig{ID: "service_account:roadmap-sync"},
	}
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
	gotApp := requireCoreWorkflowAppStep(t, recorder.upsertedSchedules[0].Target)
	if gotApp.Connection != "bot" {
		t.Fatalf("target connection = %q, want bot", gotApp.Connection)
	}
}

func TestBootstrapAllowsConfiguredWorkflowScheduleInvokesForUserCredentialedApps(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["slack"] = &config.ProviderEntry{
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
	nightly.Invokes = []config.WorkflowInvokeConfig{{App: "slack", Operation: "conversations.list"}}
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
}

func TestBootstrapDeletesRemovedConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := []*recordingWorkflowProvider{}
	sharedSchedules := map[string]*coreworkflow.Schedule{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			schedules: sharedSchedules,
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
	var recordersMu sync.Mutex
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recordersMu.Lock()
		defer recordersMu.Unlock()
		if sharedSchedules[name] == nil {
			sharedSchedules[name] = map[string]*coreworkflow.Schedule{}
		}
		recorder := &recordingWorkflowProvider{
			schedules: sharedSchedules[name],
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
	temporalStarts := 0
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name == "temporal" {
			temporalStarts++
			provider := &recordingWorkflowProvider{
				schedules: temporalSchedules,
				closed:    closed,
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
			Target:   coreWorkflowAppStepTarget("roadmap", "sync"),
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

func TestBootstrapSkipsRemovedWorkflowScheduleCleanupWhenProviderListFails(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	provider := &recordingWorkflowProvider{
		listSchedulesErr: status.Error(codes.Internal, "query temporal index: context canceled"),
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

	if len(provider.deletedSchedules) != 0 {
		t.Fatalf("deleted schedules = %d, want 0", len(provider.deletedSchedules))
	}
}

func TestBootstrapAppliesConfiguredWorkflowEventTriggers(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["slack"] = &config.ProviderEntry{
		ConnectionMode: providermanifestv1.ConnectionModeNone,
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
	taskUpdated.Invokes = []config.WorkflowInvokeConfig{{App: "slack", Operation: "conversations.history"}}
	cfg.Workflows.EventTriggers["task_updated"] = taskUpdated
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	var workflowEncryptionKey []byte
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		workflowEncryptionKey = append([]byte(nil), deps.EncryptionKey...)
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
	gotApp := requireCoreWorkflowAppStep(t, got.Target)
	if gotApp.Name != "roadmap" || gotApp.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if gotApp.Input.Object["source"].Literal != "yaml" {
		t.Fatalf("target input = %#v", gotApp.Input)
	}
	if got.RequestedBy.SubjectID != "system:config" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
	if len(recorder.upsertedEventTriggerTokens) != 1 {
		t.Fatalf("upserted event trigger tokens = %d, want 1", len(recorder.upsertedEventTriggerTokens))
	}
	tokenCtx := requireWorkflowConfigTokenContext(t, workflowEncryptionKey, recorder.upsertedEventTriggerTokens[0])
	tokenPrincipal := tokenCtx.Principal()
	if tokenPrincipal == nil || !principal.AllowsOperationPermission(tokenPrincipal, "roadmap", "sync") {
		t.Fatalf("token principal does not allow roadmap.sync: %#v", tokenPrincipal)
	}
	if tokenPrincipal == nil || !principal.AllowsOperationPermission(tokenPrincipal, "slack", "conversations.history") {
		t.Fatalf("token principal does not allow slack.conversations.history: %#v", tokenPrincipal)
	}
	assertWorkflowConfigTokenAllowsAppInvoke(t, workflowEncryptionKey, recorder.upsertedEventTriggerTokens[0], "roadmap", "sync")
	assertWorkflowConfigTokenAllowsAppInvoke(t, workflowEncryptionKey, recorder.upsertedEventTriggerTokens[0], "slack", "conversations.history")
}

func TestBootstrapConfiguredWorkflowEventTriggerRunAsAllowsUserCredentialedTarget(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Apps["roadmap"].ConnectionMode = providermanifestv1.ConnectionModeUser
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
		EventTriggers: map[string]workflowFixtureEventTrigger{
			"task_updated": {
				Match: workflowFixtureEventMatch{
					Type:   "task.updated",
					Source: "roadmap",
				},
				Operation: "sync",
			},
		},
	})
	trigger := cfg.Workflows.EventTriggers["task_updated"]
	trigger.RunAs = &config.WorkflowRunAsConfig{
		Subject: &config.WorkflowRunAsSubjectConfig{
			ID:   "service_account:roadmap-events",
			Kind: "service_account",
		},
	}
	cfg.Workflows.EventTriggers["task_updated"] = trigger

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	var workflowEncryptionKey []byte
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		workflowEncryptionKey = append([]byte(nil), deps.EncryptionKey...)
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
	if recorder == nil || len(recorder.upsertedEventTriggers) != 1 {
		t.Fatalf("recorded event triggers = %#v", recorders)
	}
	got := recorder.upsertedEventTriggers[0]
	if got.RequestedBy.SubjectID != "system:config" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
	if len(recorder.upsertedEventTriggerTokens) != 1 {
		t.Fatalf("upserted event trigger tokens = %d, want 1", len(recorder.upsertedEventTriggerTokens))
	}
	tokenPrincipal := requireWorkflowConfigTokenContext(t, workflowEncryptionKey, recorder.upsertedEventTriggerTokens[0]).Principal()
	if tokenPrincipal == nil || tokenPrincipal.SubjectID != "service_account:roadmap-events" || tokenPrincipal.Kind != "service_account" || tokenPrincipal.CredentialSubjectID != "service_account:roadmap-events" || tokenPrincipal.AuthSource() != "config" {
		t.Fatalf("token principal = %#v", tokenPrincipal)
	}
}

func TestBootstrapConfigManagedAgentStepsPreserveWorkflowSystemToolRefs(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {Source: config.ProviderSource{Path: "stub"}},
	}
	agentStepTarget := &config.WorkflowTargetConfig{Steps: []config.WorkflowStepConfig{{
		ID: "main",
		Agent: &config.WorkflowStepAgentConfig{
			Provider: "managed",
			Prompt:   config.WorkflowTextConfig{Template: "Inspect the workflow and sync the roadmap"},
			Tools: []config.WorkflowAgentToolRef{
				{System: coreagent.SystemToolWorkflow, Operation: "schedules.list"},
				{App: "roadmap", Operation: "sync"},
			},
		},
	}}}
	cfg.Workflows.Schedules = map[string]config.WorkflowScheduleConfig{
		"agent_schedule": {
			Provider: "temporal",
			Cron:     "*/10 * * * *",
			Timezone: "UTC",
			Target:   agentStepTarget,
		},
	}
	cfg.Workflows.EventTriggers = map[string]config.WorkflowEventTriggerConfig{
		"agent_event": {
			Provider: "temporal",
			Match: config.WorkflowEventMatch{
				Type: "roadmap.updated",
			},
			Target: agentStepTarget,
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
		if len(target.Steps) == 0 || target.Steps[0].Agent == nil || len(target.Steps[0].Agent.ToolRefs) != 2 {
			t.Fatalf("%s target = %#v", label, target)
		}
		if target.Steps[0].Agent.ToolRefs[0].System != coreagent.SystemToolWorkflow || target.Steps[0].Agent.ToolRefs[0].Operation != "schedules.list" {
			t.Fatalf("%s workflow tool ref = %#v", label, target.Steps[0].Agent.ToolRefs[0])
		}
		if target.Steps[0].Agent.ToolRefs[1].App != "roadmap" || target.Steps[0].Agent.ToolRefs[1].Operation != "sync" {
			t.Fatalf("%s app tool ref = %#v", label, target.Steps[0].Agent.ToolRefs[1])
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
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{
			eventTriggers: sharedEventTriggers,
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

func TestBootstrapMovesConfiguredWorkflowEventTriggersToNewProvider(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := map[string][]*recordingWorkflowProvider{}
	sharedEventTriggers := map[string]map[string]*coreworkflow.EventTrigger{}
	var recordersMu sync.Mutex
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recordersMu.Lock()
		defer recordersMu.Unlock()
		if sharedEventTriggers[name] == nil {
			sharedEventTriggers[name] = map[string]*coreworkflow.EventTrigger{}
		}
		recorder := &recordingWorkflowProvider{
			eventTriggers: sharedEventTriggers[name],
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
	var recordersMu sync.Mutex
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []runtimehost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recordersMu.Lock()
		defer recordersMu.Unlock()
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
			Target: coreWorkflowAppStepTarget("roadmap", "sync"),
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

func TestBootstrapSkipsRemovedWorkflowEventTriggerCleanupWhenProviderListFails(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	setWorkflowFixture(cfg, "roadmap", &workflowFixture{
		Provider: "temporal",
	})
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	provider := &recordingWorkflowProvider{
		listEventTriggersErr: status.Error(codes.Internal, "query temporal index shard 0: context canceled"),
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

	if len(provider.deletedEventTriggers) != 0 {
		t.Fatalf("deleted event triggers = %d, want 0", len(provider.deletedEventTriggers))
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
	cfg.Apps = map[string]*config.ProviderEntry{
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
	session, err := result.AgentManager.CreateSession(startCtx, systemPrincipal, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "reviewer",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, systemPrincipal, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		ToolRefs: []*proto.AgentToolRef{{
			App:       "roadmap",
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
	if strings.TrimSpace(createTurnReq.GetRunGrant()) == "" {
		t.Fatal("CreateTurn run_grant is empty")
	}
	if len(createTurnReq.GetTools()) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", createTurnReq.GetTools())
	}
	listResp := invokeAgentHostListTools(t, capturedHostServices, &proto.ListAgentToolsRequest{
		SessionId: session.ID,
		TurnId:    turn.ID,
		PageSize:  5,
		RunGrant:  createTurnReq.GetRunGrant(),
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
		RunGrant:   createTurnReq.GetRunGrant(),
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
		RunGrant:   createTurnReq.GetRunGrant(),
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
		RunGrant:   createTurnReq.GetRunGrant(),
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

	if _, err := result.AgentManager.CancelTurn(startCtx, systemPrincipal, &proto.CancelAgentProviderTurnRequest{TurnId: turn.ID, Reason: "done"}); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}
	cancelRequests := providerImpl.CancelTurnRequests()
	if len(cancelRequests) != 1 {
		t.Fatalf("CancelTurn requests = %d, want 1", len(cancelRequests))
	}
	if cancelRequests[0].GetTurnId() != turn.ID {
		t.Fatalf("CancelTurn turn_id = %q, want %q", cancelRequests[0].GetTurnId(), turn.ID)
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
	cfg.Apps = map[string]*config.ProviderEntry{
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
	session, err := result.AgentManager.CreateSession(startCtx, systemPrincipal, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "reviewer",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := result.AgentManager.CreateTurn(startCtx, systemPrincipal, &proto.CreateAgentProviderTurnRequest{
		SessionId: session.ID,
		Model:     "gpt-test",
		Output:    bootstrapTextAgentOutput(),
		ToolRefs: []*proto.AgentToolRef{{
			App:       "roadmap",
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
	if strings.TrimSpace(createTurnReq.GetRunGrant()) == "" {
		t.Fatal("CreateTurn run_grant is empty")
	}
	if len(createTurnReq.GetTools()) != 0 {
		t.Fatalf("CreateTurn tools = %#v, want no preloaded tools", createTurnReq.GetTools())
	}
	listResp := invokeAgentHostListTools(t, capturedHostServices, &proto.ListAgentToolsRequest{
		SessionId: session.ID,
		TurnId:    turn.ID,
		PageSize:  5,
		RunGrant:  createTurnReq.GetRunGrant(),
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
		RunGrant:   createTurnReq.GetRunGrant(),
	}); err != nil {
		t.Fatalf("invoke agent host callback before cancel: %v", err)
	}

	if _, err := result.AgentManager.CancelTurn(startCtx, systemPrincipal, &proto.CancelAgentProviderTurnRequest{TurnId: turn.ID, Reason: "done"}); err == nil {
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
	cfg.Apps = map[string]*config.ProviderEntry{
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

	_, provider, err := result.AgentControl.ResolveProvider(context.Background(), "")
	if err != nil {
		t.Fatalf("ResolveProvider: %v", err)
	}

	startCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "system:config"})
	tool := coreagent.Tool{
		ID: "roadmap.sync",
		Target: coreagent.ToolTarget{
			App:       "roadmap",
			Operation: "sync",
		},
	}
	if _, err := provider.CreateSession(startCtx, &proto.CreateAgentProviderSessionRequest{
		SessionId: "agent-session-1",
		Model:     "gpt-test",
	}); err == nil {
		t.Fatal("CreateSession error = nil, want mismatched session id failure")
	} else if !strings.Contains(err.Error(), `returned session id "generated-session-1" for requested session id "agent-session-1"`) {
		t.Fatalf("CreateSession error = %v, want mismatched session id failure", err)
	}

	replayedSession, err := provider.CreateSession(startCtx, &proto.CreateAgentProviderSessionRequest{
		SessionId:      "agent-session-1",
		IdempotencyKey: "workflow:github:run-1:session",
		Model:          "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession idempotent replay: %v", err)
	}
	if replayedSession.ID != "generated-session-1" {
		t.Fatalf("CreateSession idempotent replay ID = %q, want generated-session-1", replayedSession.ID)
	}

	if _, err := provider.CreateTurn(startCtx, &proto.CreateAgentProviderTurnRequest{
		TurnId:    "agent-turn-1",
		SessionId: "agent-session-1",
		Model:     "gpt-test",
		CreatedBy: bootstrapAgentActorToProto(coreagent.Actor{SubjectID: "system:config"}),
		Output:    bootstrapTextAgentOutput(),
		Tools:     bootstrapAgentToolsToProto([]coreagent.Tool{tool}),
	}); err == nil {
		t.Fatal("CreateTurn error = nil, want mismatched turn id failure")
	} else if !strings.Contains(err.Error(), `returned turn id "generated-turn-1" for requested turn id "agent-turn-1"`) {
		t.Fatalf("CreateTurn error = %v, want mismatched turn id failure", err)
	}

	cancelRequests := providerImpl.CancelTurnRequests()
	if len(cancelRequests) != 1 {
		t.Fatalf("CancelTurn requests = %d, want 1", len(cancelRequests))
	}
	if cancelRequests[0].GetTurnId() != "generated-turn-1" {
		t.Fatalf("CancelTurn turn_id = %q, want %q", cancelRequests[0].GetTurnId(), "generated-turn-1")
	}
	if cancelRequests[0].GetReason() != "agent provider returned mismatched turn id" {
		t.Fatalf("CancelTurn reason = %q, want %q", cancelRequests[0].GetReason(), "agent provider returned mismatched turn id")
	}

	replayedTurn, err := provider.CreateTurn(startCtx, &proto.CreateAgentProviderTurnRequest{
		TurnId:         "agent-turn-1",
		SessionId:      "agent-session-1",
		IdempotencyKey: "workflow:github:run-1:turn",
		Model:          "gpt-test",
		CreatedBy:      bootstrapAgentActorToProto(coreagent.Actor{SubjectID: "system:config"}),
		Output:         bootstrapTextAgentOutput(),
		Tools:          bootstrapAgentToolsToProto([]coreagent.Tool{tool}),
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
	factories.S3 = func(yaml.Node) (s3sdk.S3, error) {
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

	t.Run("rejects invalid app invokes dependency", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Apps = map[string]*config.ProviderEntry{
			"caller": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.AppInvocationDependency{
					{App: "missing", Operation: "ping"},
				},
			},
		}

		_, err := bootstrap.Validate(context.Background(), cfg, validFactories())
		if err == nil || !strings.Contains(err.Error(), `apps.caller.invokes[0] references unknown app "missing"`) {
			t.Fatalf("Validate error = %v, want unknown app invokes error", err)
		}
	})

	t.Run("accepts graphql surface app invokes dependency", func(t *testing.T) {
		t.Parallel()

		srv := startBootstrapGraphQLIntrospectionServer(t)
		root := t.TempDir()
		callerManifestPath := filepath.Join(root, "caller-manifest.yaml")
		if err := os.WriteFile(callerManifestPath, []byte("kind: app\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(caller-manifest.yaml): %v", err)
		}

		cfg := validConfig()
		cfg.Apps = map[string]*config.ProviderEntry{
			"caller": {
				Source:               config.NewMetadataSource("https://example.invalid/github-com-acme-caller/v1.0.0/provider-release.yaml"),
				ResolvedManifestPath: callerManifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.AppInvocationDependency{
					{App: "linear", Surface: "graphql"},
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

	t.Run("rejects graphql surface invoke when target app has no graphql surface", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		callerManifestPath := filepath.Join(root, "caller-manifest.yaml")
		if err := os.WriteFile(callerManifestPath, []byte("kind: app\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(caller-manifest.yaml): %v", err)
		}

		cfg := validConfig()
		cfg.Apps = map[string]*config.ProviderEntry{
			"caller": {
				Source:               config.NewMetadataSource("https://example.invalid/github-com-acme-caller/v1.0.0/provider-release.yaml"),
				ResolvedManifestPath: callerManifestPath,
				ResolvedManifest: &providermanifestv1.Manifest{
					Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "caller"},
					Spec:       &providermanifestv1.Spec{},
				},
				Invokes: []config.AppInvocationDependency{
					{App: "linear", Surface: "graphql"},
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
		if err == nil || !strings.Contains(err.Error(), `apps.caller.invokes[0] references app "linear" surface "graphql", but that surface is not configured`) {
			t.Fatalf("Validate error = %v, want missing graphql surface error", err)
		}
	})

	t.Run("accepts app configured with both openapi and graphql api surfaces", func(t *testing.T) {
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
		cfg.Apps = map[string]*config.ProviderEntry{
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

	t.Run("accepts app configured with graphql surface without eager introspection", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Apps = map[string]*config.ProviderEntry{
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
		cfg.Apps = map[string]*config.ProviderEntry{
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

	t.Run("workflow managed service account subjects stay unique across similar app names", func(t *testing.T) {
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
		cfg.Apps = map[string]*config.ProviderEntry{
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

func TestBootstrapAllowsAppConfiguredWithBothOpenAPIAndGraphQLAPISurfaces(t *testing.T) {
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
	cfg.Apps = map[string]*config.ProviderEntry{
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

	viewerResult, err := prov.Execute(context.Background(), "viewer", map[string]any{"team": "workspace"}, "")
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
	cfg.Apps = nil

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
		factories.S3 = func(node yaml.Node) (s3sdk.S3, error) {
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
				Runtime: &config.RuntimePlacementConfig{
					Image: "ghcr.io/example/simple-agent:latest",
					ImagePullAuth: &config.RuntimePlacementImagePullAuth{
						DockerConfigJSON: transportSecretRef("ghcr-docker-config"),
					},
				},
			},
		}

		if err := bootstrap.ResolveConfigSecrets(ctx, cfg, factories); err != nil {
			t.Fatalf("ResolveConfigSecrets: %v", err)
		}

		auth := cfg.Providers.Agent["simple"].Runtime.ImagePullAuth
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAppDynamic, Id: "calendar"},
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
			t.Fatal("expected static app access to be allowed")
		}
		if access.Role != "viewer" {
			t.Fatalf("static app role = %q, want %q", access.Role, "viewer")
		}

		dynamicPrincipal := &principal.Principal{
			UserID:    dynamicUser.ID,
			SubjectID: principal.UserSubjectID(dynamicUser.ID),
			Identity:  &core.UserIdentity{Email: dynamicUser.Email},
			Kind:      principal.KindUser,
		}
		access, allowed = result.Authorizer.ResolveAccess(ctx, dynamicPrincipal, "calendar")
		if !allowed {
			t.Fatal("expected dynamic app access to be allowed")
		}
		if access.Role != "editor" {
			t.Fatalf("dynamic app role = %q, want %q", access.Role, "editor")
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
				App:        "roadmap",
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
			t.Fatal("expected dynamic app access to be denied without authorization provider")
		}
		if access.Role != "" {
			t.Fatalf("dynamic app role without authorization provider = %q, want empty", access.Role)
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAppDynamic, Id: "calendar"},
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
			t.Fatal("expected provider-backed app access after restart")
		}
		if access.Role != "editor" {
			t.Fatalf("provider-backed app role after restart = %q, want %q", access.Role, "editor")
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAppDynamic, Id: "calendar"},
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
			t.Fatal("expected provider-backed app access to be allowed")
		}
		if access.Role != "viewer" {
			t.Fatalf("provider-backed app role = %q, want %q", access.Role, "viewer")
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
		cfg.Apps = map[string]*config.ProviderEntry{
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

	t.Run("authorization provider uses cached model while active model drifts", func(t *testing.T) {
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
		cfg.Apps = map[string]*config.ProviderEntry{
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
		if !allowed {
			t.Fatal("expected access to use the cached model while the provider active model drifts")
		}
		if access.Role != "viewer" {
			t.Fatalf("role during active model drift = %q, want %q", access.Role, "viewer")
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
	cfg.Apps = map[string]*config.ProviderEntry{
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
