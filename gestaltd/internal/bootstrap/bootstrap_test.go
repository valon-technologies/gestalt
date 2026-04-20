package bootstrap_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	telemetrynoop "github.com/valon-technologies/gestalt/server/internal/drivers/telemetry/noop"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	gproto "google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

func stubAuthFactory(name string) bootstrap.AuthFactory {
	return func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
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
	return func(yaml.Node, []providerhost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
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
	return func(yaml.Node, []providerhost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
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

func bootstrapManagedAuthorizationModel(policyRoles, pluginStaticRoles, pluginDynamicRoles, adminDynamicRoles []string) *core.AuthorizationModel {
	model := &core.AuthorizationModel{Version: 1}
	model.ResourceTypes = append(model.ResourceTypes, bootstrapAuthorizationResourceType(
		authorization.ProviderResourceTypePolicyStatic,
		nil,
		policyRoles,
		[]string{authorization.ProviderSubjectTypeSubject, authorization.ProviderSubjectTypeEmail},
	))
	model.ResourceTypes = appendIfAuthorizationResourceType(model.ResourceTypes, bootstrapAuthorizationResourceType(
		authorization.ProviderResourceTypePluginStatic,
		nil,
		pluginStaticRoles,
		[]string{authorization.ProviderSubjectTypeSubject, authorization.ProviderSubjectTypeEmail},
	))
	model.ResourceTypes = appendIfAuthorizationResourceType(model.ResourceTypes, bootstrapAuthorizationResourceType(
		authorization.ProviderResourceTypePluginDynamic,
		nil,
		pluginDynamicRoles,
		[]string{authorization.ProviderSubjectTypeUser, authorization.ProviderSubjectTypeEmail},
	))
	model.ResourceTypes = appendIfAuthorizationResourceType(model.ResourceTypes, bootstrapAuthorizationResourceType(
		authorization.ProviderResourceTypeAdminPolicyStatic,
		nil,
		policyRoles,
		[]string{authorization.ProviderSubjectTypeSubject, authorization.ProviderSubjectTypeEmail},
	))
	model.ResourceTypes = appendIfAuthorizationResourceType(model.ResourceTypes, bootstrapAuthorizationResourceType(
		authorization.ProviderResourceTypeAdminDynamic,
		nil,
		adminDynamicRoles,
		[]string{authorization.ProviderSubjectTypeUser, authorization.ProviderSubjectTypeEmail},
	))
	slices.SortFunc(model.ResourceTypes, func(left, right *core.AuthorizationModelResourceType) int {
		return strings.Compare(left.GetName(), right.GetName())
	})
	return model
}

func appendIfAuthorizationResourceType(target []*core.AuthorizationModelResourceType, resourceType *core.AuthorizationModelResourceType) []*core.AuthorizationModelResourceType {
	if resourceType == nil {
		return target
	}
	return append(target, resourceType)
}

func bootstrapAuthorizationResourceType(name string, extraRelations map[string][]string, actions []string, subjects []string) *core.AuthorizationModelResourceType {
	relations := map[string][]string{}
	for relation, relationSubjects := range extraRelations {
		relations[relation] = append([]string(nil), relationSubjects...)
	}
	for _, action := range actions {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		relations[action] = append([]string(nil), subjects...)
	}
	if len(relations) == 0 {
		return nil
	}
	resourceType := &core.AuthorizationModelResourceType{Name: name}
	relationNames := make([]string, 0, len(relations))
	for relation := range relations {
		relationNames = append(relationNames, relation)
	}
	slices.Sort(relationNames)
	for _, relation := range relationNames {
		resourceType.Relations = append(resourceType.Relations, &core.AuthorizationModelRelation{
			Name:         relation,
			SubjectTypes: append([]string(nil), relations[relation]...),
		})
	}
	actionNames := append([]string(nil), actions...)
	slices.Sort(actionNames)
	for _, action := range actionNames {
		action = strings.TrimSpace(action)
		if action == "" {
			continue
		}
		resourceType.Actions = append(resourceType.Actions, &core.AuthorizationModelAction{
			Name:      action,
			Relations: []string{action},
		})
	}
	return resourceType
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

type recordingWorkflowProvider struct {
	upsertedSchedules          []coreworkflow.UpsertScheduleRequest
	listedSchedules            []*coreworkflow.Schedule
	listSchedulesErr           error
	deletedSchedules           []coreworkflow.DeleteScheduleRequest
	deleteScheduleErr          error
	getSchedule                *coreworkflow.Schedule
	getScheduleErr             error
	schedules                  map[string]*coreworkflow.Schedule
	upsertedEventTriggers      []coreworkflow.UpsertEventTriggerRequest
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
func (p *recordingWorkflowProvider) UpsertSchedule(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, req)
	schedule := &coreworkflow.Schedule{
		ID:        req.ScheduleID,
		Cron:      req.Cron,
		Timezone:  req.Timezone,
		Target:    req.Target,
		Paused:    req.Paused,
		CreatedBy: req.RequestedBy,
	}
	if p.schedules == nil {
		p.schedules = map[string]*coreworkflow.Schedule{}
	}
	p.schedules[req.ScheduleID] = schedule
	return schedule, nil
}
func (p *recordingWorkflowProvider) GetSchedule(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.getSchedule != nil || p.getScheduleErr != nil {
		return p.getSchedule, p.getScheduleErr
	}
	if p.schedules != nil {
		if schedule, ok := p.schedules[req.ScheduleID]; ok {
			return schedule, nil
		}
	}
	return nil, core.ErrNotFound
}
func (p *recordingWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	if p.listSchedulesErr != nil {
		return nil, p.listSchedulesErr
	}
	return append([]*coreworkflow.Schedule(nil), p.listedSchedules...), nil
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
		ID:        req.TriggerID,
		Match:     req.Match,
		Target:    req.Target,
		Paused:    req.Paused,
		CreatedBy: req.RequestedBy,
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
	return append([]*coreworkflow.EventTrigger(nil), p.listedEventTriggers...), nil
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
func (p *recordingWorkflowProvider) Ping(context.Context) error { return nil }
func (p *recordingWorkflowProvider) Close() error {
	if p.closed != nil {
		p.closed.Store(true)
	}
	return nil
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
			Auth: map[string]*config.ProviderEntry{
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
				"test": {Source: config.ProviderSource{Path: "stub"}},
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

func selectedAuthEntry(t *testing.T, cfg *config.Config) *config.ProviderEntry {
	t.Helper()
	_, entry, err := cfg.SelectedAuthProvider()
	if err != nil {
		t.Fatalf("SelectedAuthProvider: %v", err)
	}
	return entry
}

func validFactories() *bootstrap.FactoryRegistry {
	f := bootstrap.NewFactoryRegistry()
	f.Auth = stubAuthFactory("test-auth")
	f.IndexedDB = stubIndexedDBFactory()
	f.Secrets["test-secrets"] = stubSecretManagerFactory()
	f.Telemetry["test-telemetry"] = stubTelemetryFactory()
	return f
}

func invokeWorkflowHostCallback(t *testing.T, hostServices []providerhost.HostService, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
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

func withIndexedDBHostClient(t *testing.T, hostService providerhost.HostService, fn func(proto.IndexedDBClient)) {
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
			ConnectionMode: providermanifestv1.ConnectionModeIdentity,
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"sync"},
			},
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
	return cfg
}

func transportSecretRef(name string) string {
	return config.EncodeSecretRefTransport(config.SecretRef{
		Provider: "default",
		Name:     name,
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

				cfg := validConfig()
				cfg.Plugins = map[string]*config.ProviderEntry{
					"slack": {
						ResolvedManifest: &providermanifestv1.Manifest{
							Spec: &providermanifestv1.Spec{
								Auth: tc.specAuth,
								Surfaces: &providermanifestv1.ProviderSurfaces{
									REST: &providermanifestv1.RESTSurface{
										BaseURL:    srv.URL,
										Connection: tc.restConnection,
										Operations: []providermanifestv1.ProviderOperation{
											{Name: "users.list", Method: http.MethodGet, Path: "/users"},
										},
									},
								},
								Connections: tc.connections,
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
				if err := result.Services.Tokens.StoreToken(ctx, &core.IntegrationToken{
					UserID:       user.ID,
					Integration:  "slack",
					Connection:   tc.tokenConn,
					Instance:     "default",
					AccessToken:  tokenValue,
					RefreshToken: "refresh-token",
				}); err != nil {
					t.Fatalf("StoreToken: %v", err)
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
	factories.Workflow = func(_ context.Context, name string, node yaml.Node, hostServices []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
		if got := hostSockets[name]; got != providerhost.DefaultWorkflowHostSocketEnv {
			t.Fatalf("workflow host env for %q = %q, want %q", name, got, providerhost.DefaultWorkflowHostSocketEnv)
		}
	}
}

func TestBootstrapPassesIndexedDBHostSocketToWorkflowProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{Source: config.ProviderSource{Path: "stub"}}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.PluginIndexedDBConfig{
				Provider:     "workflow_state",
				DB:           "workflow",
				ObjectStores: []string{"workflow_schedules", "workflow_runs"},
			},
		},
	}

	factories := validFactories()
	hostEnvs := map[string][]string{}
	factories.Workflow = func(_ context.Context, name string, node yaml.Node, hostServices []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	if got[0] != providerhost.DefaultWorkflowHostSocketEnv {
		t.Fatalf("workflow host env = %q, want %q", got[0], providerhost.DefaultWorkflowHostSocketEnv)
	}
	if got[1] != providerhost.DefaultIndexedDBSocketEnv {
		t.Fatalf("workflow indexeddb env = %q, want %q", got[1], providerhost.DefaultIndexedDBSocketEnv)
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
	var hostServices []providerhost.HostService
	factories.Authorization = func(_ yaml.Node, services []providerhost.HostService, _ bootstrap.Deps) (core.AuthorizationProvider, error) {
		hostServices = append([]providerhost.HostService(nil), services...)
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
	if hostServices[0].EnvVar != providerhost.DefaultIndexedDBSocketEnv {
		t.Fatalf("authorization default indexeddb env = %q, want %q", hostServices[0].EnvVar, providerhost.DefaultIndexedDBSocketEnv)
	}
	wantNamed := []string{
		providerhost.IndexedDBSocketEnv("archive"),
		providerhost.IndexedDBSocketEnv("test"),
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
			"namespace":    "should_be_removed",
		}),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.PluginIndexedDBConfig{
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
	factories.Workflow = func(context.Context, string, yaml.Node, []providerhost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
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
	if _, ok := captured["namespace"]; ok {
		t.Fatalf("namespace should be removed, got %#v", captured["namespace"])
	}

	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("result.Close: %v", err)
	}
	if got := workflowCloseCount.Load(); got != 1 {
		t.Fatalf("workflowCloseCount after workflow shutdown = %d, want 1", got)
	}
}

func TestBootstrapRoutesWorkflowIndexedDBHostServices(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.ProviderSource{Path: "./providers/datastore/memory"},
		Config: mustYAMLNode(t, map[string]any{"bucket": "workflow-state"}),
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"basic": {
			Source: config.ProviderSource{Path: "stub"},
			IndexedDB: &config.PluginIndexedDBConfig{
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
		hostEnv    []providerhost.HostService
	)
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		boundDB = &trackedIndexedDB{
			StubIndexedDB: &coretesting.StubIndexedDB{},
			closed:        &closeCount,
		}
		return boundDB, nil
	}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, hostServices []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		hostEnv = append([]providerhost.HostService(nil), hostServices...)
		return &stubWorkflowProvider{}, nil
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

	var indexedDBHost providerhost.HostService
	for _, hostService := range hostEnv {
		if hostService.EnvVar == providerhost.DefaultIndexedDBSocketEnv {
			indexedDBHost = hostService
			break
		}
	}
	if indexedDBHost.EnvVar == "" {
		t.Fatal("missing workflow indexeddb host service")
	}

	withIndexedDBHostClient(t, indexedDBHost, func(client proto.IndexedDBClient) {
		if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
			Name:   "workflow_runs",
			Schema: &proto.ObjectStoreSchema{},
		}); err != nil {
			t.Fatalf("CreateObjectStore(workflow_runs): %v", err)
		}
		record, err := gestalt.RecordToProto(gestalt.Record{"id": "run-1", "status": "pending"})
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
		got, err := gestalt.RecordFromProto(resp.GetRecord())
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

	if _, err := boundDB.ObjectStore("workflow_workflow_runs").Get(context.Background(), "run-1"); err != nil {
		t.Fatalf("prefixed backing store should contain run: %v", err)
	}
	if _, err := boundDB.ObjectStore("workflow_runs").Get(context.Background(), "run-1"); err == nil {
		t.Fatal("unprefixed backing store should remain empty")
	}
}

func TestBootstrapAppliesConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "America/New_York",
				Operation: "sync",
				Input: map[string]any{
					"source": "yaml",
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	}
	if len(recorder.upsertedSchedules) != 1 {
		t.Fatalf("upserted schedules = %d, want 1", len(recorder.upsertedSchedules))
	}
	got := recorder.upsertedSchedules[0]
	if got.ScheduleID != workflowConfigScheduleID("roadmap", "nightly_sync") {
		t.Fatalf("schedule id = %q", got.ScheduleID)
	}
	if got.Cron != "0 2 * * *" || got.Timezone != "America/New_York" {
		t.Fatalf("schedule timing = %#v", got)
	}
	if got.Target.PluginName != "roadmap" || got.Target.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if got.Target.Input["source"] != "yaml" {
		t.Fatalf("target input = %#v", got.Target.Input)
	}
	if got.RequestedBy.SubjectID != "config:workflow:roadmap" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
}

func TestValidateDoesNotApplyConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Paused:    true,
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	}
	if len(recorder.upsertedSchedules) != 0 {
		t.Fatalf("upserted schedules = %d, want 0", len(recorder.upsertedSchedules))
	}
	if len(recorder.deletedSchedules) != 0 {
		t.Fatalf("deleted schedules = %d, want 0", len(recorder.deletedSchedules))
	}
}

func TestBootstrapDeletesRemovedConfiguredWorkflowSchedules(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	recorders := []*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
	}
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
	staleID := workflowConfigScheduleID("roadmap", "nightly_sync")
	recorder := recorders[1]
	if len(recorder.deletedSchedules) != 1 {
		t.Fatalf("deleted schedules = %d, want 1", len(recorder.deletedSchedules))
	}
	if recorder.deletedSchedules[0].ScheduleID != staleID || recorder.deletedSchedules[0].PluginName != "roadmap" {
		t.Fatalf("delete request = %#v", recorder.deletedSchedules[0])
	}
	if len(recorder.upsertedSchedules) != 0 {
		t.Fatalf("upserted schedules = %d, want 0", len(recorder.upsertedSchedules))
	}
}

func TestBootstrapIgnoresUserSchedulesThatOnlyShareCfgPrefix(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = append(recorders[name], recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "backup",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	closed := &atomic.Bool{}
	db := &coretesting.StubIndexedDB{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return &recordingWorkflowProvider{closed: closed}, nil
	}

	cfg.Plugins["roadmap"].Workflow.Schedules = map[string]config.PluginWorkflowSchedule{
		"nightly_sync": {
			Cron:      "0 2 * * *",
			Timezone:  "UTC",
			Operation: "sync",
		},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())

	cfg = workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "backup",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"backup": {Source: config.ProviderSource{Path: "stub"}},
	}

	_, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil || !strings.Contains(err.Error(), `requires provider "temporal"`) {
		t.Fatalf("Bootstrap error = %v, want missing old provider cleanup failure", err)
	}
	if !closed.Load() {
		t.Fatal("workflow provider was not closed after reconcile failure")
	}
}

func TestBootstrapDoesNotApplyConfiguredWorkflowSchedulesWhenAuditBuildFails(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
				Input: map[string]any{
					"limit": 1,
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Providers.Audit = map[string]*config.ProviderEntry{
		"default": {Source: config.ProviderSource{Builtin: "test-audit"}},
	}

	factories := validFactories()
	recorder := &recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	recorder := &recordingWorkflowProvider{
		getSchedule: &coreworkflow.Schedule{ID: workflowConfigScheduleID("roadmap", "nightly_sync")},
	}
	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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

func TestBootstrapReAdoptsManagedSchedulesWhenOwnershipStateIsMissing(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	db1 := &coretesting.StubIndexedDB{}
	db2 := &coretesting.StubIndexedDB{}
	factories := validFactories()
	currentDB := db1
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return currentDB, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())
	if len(provider.upsertedSchedules) != 1 {
		t.Fatalf("initial upserted schedules = %d, want 1", len(provider.upsertedSchedules))
	}
	provider.schedules[workflowConfigScheduleID("roadmap", "nightly_sync")].Target.Input = map[string]any{
		"limit": float64(1),
	}

	currentDB = db2
	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap re-adopt: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.upsertedSchedules) != 2 {
		t.Fatalf("upserted schedules = %d, want 2", len(provider.upsertedSchedules))
	}
}

func TestBootstrapIgnoresMissingRemovedConfiguredWorkflowSchedule(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{deleteMissingNotFound: true}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing schedule: %v", err)
	}
	_ = result.Close(context.Background())

	if len(provider.deletedSchedules) != 1 {
		t.Fatalf("deleted schedules = %d, want 1", len(provider.deletedSchedules))
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing schedule replay: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.deletedSchedules) != 1 {
		t.Fatalf("deleted schedules after replay = %d, want 1", len(provider.deletedSchedules))
	}
}

func TestBootstrapIgnoresMissingOldScheduleDuringWorkflowProviderMove(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	temporal := &recordingWorkflowProvider{deleteMissingNotFound: true}
	backup := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name == "backup" {
			return backup, nil
		}
		return temporal, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "backup",
		Operations: []string{"sync"},
		Schedules: map[string]config.PluginWorkflowSchedule{
			"nightly_sync": {
				Cron:      "0 2 * * *",
				Timezone:  "UTC",
				Operation: "sync",
			},
		},
	}
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
	if len(temporal.deletedSchedules) == 0 {
		t.Fatal("expected temporal cleanup delete to be attempted")
	}
}

func TestBootstrapAppliesConfiguredWorkflowEventTriggers(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type:   "task.updated",
					Source: "roadmap",
				},
				Operation: "sync",
				Input: map[string]any{
					"source": "yaml",
				},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	}
	if len(recorder.upsertedEventTriggers) != 1 {
		t.Fatalf("upserted event triggers = %d, want 1", len(recorder.upsertedEventTriggers))
	}
	got := recorder.upsertedEventTriggers[0]
	if got.TriggerID != workflowConfigEventTriggerID("roadmap", "task_updated") {
		t.Fatalf("trigger id = %q", got.TriggerID)
	}
	if got.Match.Type != "task.updated" || got.Match.Source != "roadmap" || got.Match.Subject != "" {
		t.Fatalf("match = %#v", got.Match)
	}
	if got.Target.PluginName != "roadmap" || got.Target.Operation != "sync" {
		t.Fatalf("target = %#v", got.Target)
	}
	if got.Target.Input["source"] != "yaml" {
		t.Fatalf("target input = %#v", got.Target.Input)
	}
	if got.RequestedBy.SubjectID != "config:workflow:roadmap" || got.RequestedBy.SubjectKind != "system" || got.RequestedBy.AuthSource != "config" {
		t.Fatalf("requestedBy = %#v", got.RequestedBy)
	}
}

func TestValidateDoesNotApplyConfiguredWorkflowEventTriggers(t *testing.T) {
	t.Parallel()

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
				Paused:    true,
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := validFactories()
	recorders := map[string]*recordingWorkflowProvider{}
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders = append(recorders, recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
	}
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
	staleID := workflowConfigEventTriggerID("roadmap", "task_updated")
	recorder := recorders[1]
	if len(recorder.deletedEventTriggers) != 1 {
		t.Fatalf("deleted event triggers = %d, want 1", len(recorder.deletedEventTriggers))
	}
	if recorder.deletedEventTriggers[0].TriggerID != staleID || recorder.deletedEventTriggers[0].PluginName != "roadmap" {
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
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		recorders[name] = append(recorders[name], recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "backup",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		recorder := &recordingWorkflowProvider{}
		if name == "backup" && len(recorders[name]) == 1 {
			recorder.getEventTrigger = &coreworkflow.EventTrigger{ID: workflowConfigEventTriggerID("roadmap", "task_updated")}
		}
		recorders[name] = append(recorders[name], recorder)
		return recorder, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "backup",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	recorder := &recordingWorkflowProvider{
		getEventTrigger: &coreworkflow.EventTrigger{ID: workflowConfigEventTriggerID("roadmap", "task_updated")},
	}
	factories := validFactories()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
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

func TestBootstrapReAdoptsManagedEventTriggersWhenOwnershipStateIsMissing(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	db1 := &coretesting.StubIndexedDB{}
	db2 := &coretesting.StubIndexedDB{}
	factories := validFactories()
	currentDB := db1
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return currentDB, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap initial: %v", err)
	}
	_ = result.Close(context.Background())
	if len(provider.upsertedEventTriggers) != 1 {
		t.Fatalf("initial upserted event triggers = %d, want 1", len(provider.upsertedEventTriggers))
	}
	provider.eventTriggers[workflowConfigEventTriggerID("roadmap", "task_updated")].Target.Input = map[string]any{
		"limit": float64(1),
	}

	currentDB = db2
	cfg.Plugins["roadmap"].Workflow.EventTriggers["task_updated"] = config.PluginWorkflowEventTrigger{
		Match: config.PluginWorkflowEventMatch{
			Type: "task.updated",
		},
		Operation: "sync",
		Input: map[string]any{
			"limit": 1,
		},
	}
	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap re-adopt: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.upsertedEventTriggers) != 2 {
		t.Fatalf("upserted event triggers = %d, want 2", len(provider.upsertedEventTriggers))
	}
}

func TestBootstrapIgnoresMissingRemovedConfiguredWorkflowEventTrigger(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	provider := &recordingWorkflowProvider{deleteEventMissingNotFound: true}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		return provider, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing event trigger: %v", err)
	}
	_ = result.Close(context.Background())

	if len(provider.deletedEventTriggers) != 1 {
		t.Fatalf("deleted event triggers = %d, want 1", len(provider.deletedEventTriggers))
	}

	result, err = bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap remove missing event trigger replay: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	if len(provider.deletedEventTriggers) != 1 {
		t.Fatalf("deleted event triggers after replay = %d, want 1", len(provider.deletedEventTriggers))
	}
}

func TestBootstrapIgnoresMissingOldEventTriggerDuringWorkflowProviderMove(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	temporal := &recordingWorkflowProvider{deleteEventMissingNotFound: true}
	backup := &recordingWorkflowProvider{}
	factories := validFactories()
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) { return db, nil }
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, _ bootstrap.Deps) (coreworkflow.Provider, error) {
		if name == "backup" {
			return backup, nil
		}
		return temporal, nil
	}

	cfg := workflowStartupCallbackConfig("https://example.invalid")
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "temporal",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	cfg.Plugins["roadmap"].Workflow = &config.PluginWorkflowConfig{
		Provider:   "backup",
		Operations: []string{"sync"},
		EventTriggers: map[string]config.PluginWorkflowEventTrigger{
			"task_updated": {
				Match: config.PluginWorkflowEventMatch{
					Type: "task.updated",
				},
				Operation: "sync",
			},
		},
	}
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
	if len(temporal.deletedEventTriggers) == 0 {
		t.Fatal("expected temporal cleanup delete to be attempted")
	}
}

func workflowConfigScheduleID(pluginName, scheduleKey string) string {
	sum := sha256.Sum256([]byte(pluginName + "\x00" + scheduleKey))
	return coreworkflow.ConfigManagedSchedulePrefix + hex.EncodeToString(sum[:])
}

func workflowConfigEventTriggerID(pluginName, triggerKey string) string {
	sum := sha256.Sum256([]byte(pluginName + "\x00event_trigger\x00" + triggerKey))
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
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []providerhost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
			UserID:      principal.IdentityPrincipal,
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-bootstrap-token",
		}); err != nil {
			return nil, fmt.Errorf("store identity token: %w", err)
		}
		resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			PluginName: "roadmap",
			Target: &proto.BoundWorkflowTarget{
				PluginName: "roadmap",
				Operation:  "sync",
			},
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
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []providerhost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
			UserID:      principal.IdentityPrincipal,
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-validate-token",
		}); err != nil {
			return nil, fmt.Errorf("store identity token: %w", err)
		}
		resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			PluginName: "roadmap",
			Target: &proto.BoundWorkflowTarget{
				PluginName: "roadmap",
				Operation:  "sync",
			},
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
					ConnectionMode: providermanifestv1.ConnectionModeIdentity,
					Workflow: &config.PluginWorkflowConfig{
						Provider:   "temporal",
						Operations: []string{"sync"},
					},
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

			factories := validFactories()
			factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []providerhost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
				if name != "temporal" {
					return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
				}
				if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
					UserID:      principal.IdentityPrincipal,
					Integration: "roadmap",
					Connection:  config.PluginConnectionName,
					Instance:    "default",
					AccessToken: "workflow-validate-token",
				}); err != nil {
					return nil, fmt.Errorf("store identity token: %w", err)
				}
				resp, err := invokeWorkflowHostCallback(t, hostServices, &proto.InvokeWorkflowOperationRequest{
					PluginName: "roadmap",
					Target: &proto.BoundWorkflowTarget{
						PluginName: "roadmap",
						Operation:  "sync",
					},
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
			Source:         config.NewMetadataSource("https://example.invalid/github-com-example-roadmap/v0.0.1/provider-release.yaml"),
			ConnectionMode: providermanifestv1.ConnectionModeIdentity,
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"sync"},
			},
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

	factories := validFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, _ []providerhost.HostService, deps bootstrap.Deps) (coreworkflow.Provider, error) {
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
		if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
			UserID:      principal.IdentityPrincipal,
			Integration: "roadmap",
			Connection:  connection,
			Instance:    "default",
			AccessToken: "workflow-validate-token",
		}); err != nil {
			return nil, fmt.Errorf("store identity token for connection %q: %w", connection, err)
		}
		resp, err := deps.WorkflowRuntime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
			ProviderName: name,
			PluginName:   "roadmap",
			Target: coreworkflow.Target{
				PluginName: "roadmap",
				Operation:  "sync",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("startup invoke: %w", err)
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
	factories.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
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
		t.Fatal("auth provider was not closed")
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
	factories.Authorization = func(yaml.Node, []providerhost.HostService, bootstrap.Deps) (core.AuthorizationProvider, error) {
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

	t.Run("workflow managed workloads reject either providers", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		cfg := validConfig()
		cfg.Plugins = map[string]*config.ProviderEntry{
			"svc": {
				ConnectionMode: providermanifestv1.ConnectionMode("either"),
				Workflow: &config.PluginWorkflowConfig{
					Provider:   "temporal",
					Operations: []string{"run"},
				},
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
		factories.Workflow = func(context.Context, string, yaml.Node, []providerhost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
			return &stubWorkflowProvider{}, nil
		}

		_, err := bootstrap.Validate(context.Background(), cfg, factories)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `unsupported connection mode "either"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("workflow managed workload tokens stay unique across similar plugin names", func(t *testing.T) {
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
				Workflow: &config.PluginWorkflowConfig{
					Provider:   "temporal",
					Operations: []string{"run"},
				},
				ResolvedManifest: manifest,
			},
			"foo_bar": {
				Workflow: &config.PluginWorkflowConfig{
					Provider:   "temporal",
					Operations: []string{"run"},
				},
				ResolvedManifest: manifest,
			},
		}
		cfg.Providers.Workflow = map[string]*config.ProviderEntry{
			"temporal": {Source: config.ProviderSource{Path: "stub"}},
		}

		factories := validFactories()
		factories.Workflow = func(context.Context, string, yaml.Node, []providerhost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
			return &stubWorkflowProvider{}, nil
		}

		if _, err := bootstrap.Validate(context.Background(), cfg, factories); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
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

	authRuntime, err := config.BuildComponentRuntimeConfigNode("auth", "auth", selectedAuthEntry(t, cfg), yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "clientId"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "prepared-auth"},
		},
	})
	if err != nil {
		t.Fatalf("BuildComponentRuntimeConfigNode(auth): %v", err)
	}
	selectedAuthEntry(t, cfg).Config = authRuntime

	var gotAuthNode yaml.Node
	factories := validFactories()
	factories.Auth = func(node yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
				f.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
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

func TestBootstrapEncryptionKeyDerivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("passphrase produces 32-byte key", func(t *testing.T) {
		t.Parallel()

		var receivedKey []byte
		factories := validFactories()
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
			factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
		factories.Auth = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
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
		factories.Auth = func(node yaml.Node, _ bootstrap.Deps) (core.AuthProvider, error) {
			receivedNode = node
			return &coretesting.StubAuthProvider{N: "test-auth"}, nil
		}

		cfg := validConfig()
		selectedAuthEntry(t, cfg).Config = yaml.Node{
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

	t.Run("resolves config secret ref in workload tokens", func(t *testing.T) {
		t.Parallel()

		factories := validFactories()
		factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{
				Secrets: map[string]string{"workload-token": "gst_wld_resolved-workload-token"},
			}, nil
		}
		factories.Builtins = []core.Provider{
			&coretesting.StubIntegration{N: "weather", ConnMode: core.ConnectionModeNone},
		}

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Workloads: map[string]config.WorkloadDef{
				"triage-bot": {
					Token: transportSecretRef("workload-token"),
					Providers: map[string]config.WorkloadProviderDef{
						"weather": {Allow: []string{"forecast"}},
					},
				},
			},
		}

		result, err := bootstrap.Bootstrap(ctx, cfg, factories)
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		<-result.ProvidersReady

		if result.Authorizer == nil {
			t.Fatal("Authorizer is nil")
		}
		if _, ok := result.Authorizer.ResolveWorkloadToken("gst_wld_resolved-workload-token"); !ok {
			t.Fatal("expected resolved workload token to authenticate")
		}
	})

	t.Run("authorization provider backs human access decisions", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
				"calendar-policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						{Email: "static@example.test", Role: "viewer"},
					},
				},
				"admin-policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						{Email: "seed-admin@example.test", Role: "admin"},
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
		existingModelID := writeMemoryAuthorizationModel(t, provider, bootstrapManagedAuthorizationModel(
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
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: dynamicUser.Email},
			Relation: "editor",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "calendar"},
		})
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: dynamicUser.Email},
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
		if err := result.Services.ManagedIdentities.CreateIdentity(ctx, &core.ManagedIdentity{
			ID:          "svc-bot",
			DisplayName: "Service Bot",
		}); err != nil {
			t.Fatalf("CreateIdentity: %v", err)
		}
		if _, err := result.Services.IdentityGrants.UpsertGrant(ctx, &core.ManagedIdentityGrant{
			IdentityID: "svc-bot",
			Plugin:     "calendar",
		}); err != nil {
			t.Fatalf("UpsertGrant: %v", err)
		}
		if _, err := result.Services.IdentityPluginAccess.GetAccess(ctx, "svc-bot", "calendar"); err != nil {
			t.Fatalf("GetAccess managed identity after start: %v", err)
		}

		staticPrincipal := &principal.Principal{
			Identity: &core.UserIdentity{Email: "static@example.test"},
			Kind:     principal.KindUser,
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

	t.Run("dynamic human authorizations require an authorization provider", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
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
			Policies: map[string]config.HumanPolicyDef{
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
		writeMemoryAuthorizationModel(t, provider, bootstrapManagedAuthorizationModel(
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
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: dynamicUser.Email},
			Relation: "editor",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "calendar"},
		})
		provider.putRelationship(provider.activeModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: dynamicUser.Email},
			Relation: "admin",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypeAdminDynamic, Id: authorization.ProviderResourceIDAdminDynamicGlobal},
		})
		if err := result.Start(ctx); err != nil {
			t.Fatalf("Start(first): %v", err)
		}

		canonicalIdentityID, err := result.Services.Users.CanonicalIdentityIDForUser(ctx, dynamicUser.ID)
		if err != nil {
			t.Fatalf("CanonicalIdentityIDForUser(first): %v", err)
		}
		if err := result.Services.IdentityPluginAccess.DeleteAccess(ctx, canonicalIdentityID, "calendar"); err != nil {
			t.Fatalf("DeleteAccess(calendar): %v", err)
		}
		if err := result.Services.WorkspaceRoles.DeleteRole(ctx, canonicalIdentityID, "admin"); err != nil {
			t.Fatalf("DeleteRole(admin): %v", err)
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

		canonicalIdentityID, err = result.Services.Users.CanonicalIdentityIDForUser(ctx, dynamicUser.ID)
		if err != nil {
			t.Fatalf("CanonicalIdentityIDForUser: %v", err)
		}
		pluginAccess, err := result.Services.IdentityPluginAccess.GetAccess(ctx, canonicalIdentityID, "calendar")
		if err != nil {
			t.Fatalf("GetAccess(calendar): %v", err)
		}
		if !pluginAccess.InvokeAllOperations {
			t.Fatal("expected restart rehydrate to restore invoke-all plugin access")
		}
		roles, err := result.Services.WorkspaceRoles.ListByPrincipal(ctx, canonicalIdentityID)
		if err != nil {
			t.Fatalf("ListByPrincipal: %v", err)
		}
		if len(roles) != 1 || roles[0].Role != "admin" {
			t.Fatalf("workspace roles after restart = %+v, want [admin]", roles)
		}
	})

	t.Run("authorization provider preserves existing provider dynamic roles", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Authorization = config.AuthorizationConfig{
			Policies: map[string]config.HumanPolicyDef{
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
		existingModelID := writeMemoryAuthorizationModel(t, provider, bootstrapManagedAuthorizationModel(
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
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: dynamicUser.Email},
			Relation: "viewer",
			Resource: &core.ResourceRef{Type: authorization.ProviderResourceTypePluginDynamic, Id: "calendar"},
		})
		provider.putRelationship(existingModelID, &core.Relationship{
			Subject:  &core.SubjectRef{Type: authorization.ProviderSubjectTypeEmail, Id: dynamicUser.Email},
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
			Policies: map[string]config.HumanPolicyDef{
				"calendar-policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						{Email: "static@example.test", Role: "viewer"},
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
			Policies: map[string]config.HumanPolicyDef{
				"calendar-policy": {
					Default: "deny",
					Members: []config.HumanPolicyMemberDef{
						{Email: "static@example.test", Role: "viewer"},
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
			Identity: &core.UserIdentity{Email: "static@example.test"},
			Kind:     principal.KindUser,
		}
		access, allowed := result.Authorizer.ResolveAccess(ctx, staticPrincipal, "calendar")
		if allowed {
			t.Fatal("expected access to be denied while the provider active model drifts")
		}
		if access.Role != "" {
			t.Fatalf("role during active model drift = %q, want empty", access.Role)
		}
		if err := result.Authorizer.ReloadDynamic(ctx); err != nil {
			t.Fatalf("expected reload to heal active model drift: %v", err)
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
		cfg.Providers.Auth = map[string]*config.ProviderEntry{
			"secondary": {Source: config.NewMetadataSource("https://example.invalid/github-com-valon-technologies-gestalt-providers-auth-oidc/v0.0.1-alpha.1/provider-release.yaml")},
		}
		cfg.Server.Providers.Auth = "secondary"
		cfg.Providers.Auth["secondary"].Config = yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "issuerUrl", Tag: "!!str"},
				{Kind: yaml.ScalarNode, Value: "https://issuer.example.test", Tag: "!!str"},
			},
		}

		var authNode yaml.Node
		factories := validFactories()
		factories.Auth = func(node yaml.Node, _ bootstrap.Deps) (core.AuthProvider, error) {
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

	t.Run("omits auth when auth provider is unset", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Providers.Auth = nil
		cfg.Server.Providers.Auth = ""

		var authFactoryCalled atomic.Bool
		factories := validFactories()
		factories.Auth = func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
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

func TestBootstrapWorkloadAuthorizationRejectsEitherProvider(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Authorization = config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"svc": {Allow: []string{"run"}},
				},
			},
		},
	}

	factories := validFactories()
	factories.Builtins = []core.Provider{
		&coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionMode("either")},
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `unsupported connection mode "either"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapRejectsBuiltinEitherProviderWithoutAuthorizationConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	factories := validFactories()
	factories.Builtins = []core.Provider{
		&coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionMode("either")},
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `unsupported connection mode "either"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapWorkflowAuthorizationRejectsEitherProvider(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"svc": {
			ConnectionMode: providermanifestv1.ConnectionMode("either"),
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"run"},
			},
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
	factories.Workflow = func(context.Context, string, yaml.Node, []providerhost.HostService, bootstrap.Deps) (coreworkflow.Provider, error) {
		return &stubWorkflowProvider{}, nil
	}

	_, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `unsupported connection mode "either"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
