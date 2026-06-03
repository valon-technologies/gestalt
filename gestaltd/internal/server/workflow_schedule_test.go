package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	gproto "google.golang.org/protobuf/proto"
)

type stubWorkflowControl struct {
	defaultProviderName string
	provider            coreworkflow.Provider
	providers           map[string]coreworkflow.Provider
	selectionErr        error
	providerErr         error
}

func (s *stubWorkflowControl) ResolveProvider(_ context.Context, name string) (string, coreworkflow.Provider, error) {
	if s.selectionErr != nil {
		return "", nil, s.selectionErr
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = s.defaultProviderName
	}
	if name == "" {
		return "", nil, errors.New("provider not found")
	}
	if s.providerErr != nil {
		return "", nil, s.providerErr
	}
	if s.providers != nil {
		provider, ok := s.providers[name]
		if !ok {
			return "", nil, errors.New("provider not found")
		}
		return name, provider, nil
	}
	return name, s.provider, nil
}

func (s *stubWorkflowControl) ProviderNames() []string {
	if s.providers != nil {
		names := make([]string, 0, len(s.providers))
		for name := range s.providers {
			names = append(names, name)
		}
		slices.Sort(names)
		return names
	}
	if strings.TrimSpace(s.defaultProviderName) != "" {
		return []string{strings.TrimSpace(s.defaultProviderName)}
	}
	if s.provider != nil {
		return []string{"default"}
	}
	return nil
}

type memoryWorkflowProvider struct {
	definitions          map[string]*coreworkflow.Definition
	schedules            map[string]*coreworkflow.Schedule
	triggers             map[string]*coreworkflow.EventTrigger
	runs                 map[string]*coreworkflow.Run
	publishEventReqs     []*proto.PublishWorkflowProviderEventRequest
	upsertReqs           []*proto.UpsertWorkflowProviderScheduleRequest
	upsertTriggerReqs    []*proto.UpsertWorkflowProviderEventTriggerRequest
	deleteReqs           []*proto.DeleteWorkflowProviderScheduleRequest
	deleteTriggerReqs    []*proto.DeleteWorkflowProviderEventTriggerRequest
	pauseReqs            []*proto.PauseWorkflowProviderScheduleRequest
	pauseTriggerReqs     []*proto.PauseWorkflowProviderEventTriggerRequest
	resumeReqs           []*proto.ResumeWorkflowProviderScheduleRequest
	resumeTriggerReqs    []*proto.ResumeWorkflowProviderEventTriggerRequest
	listRunReqs          []coreworkflow.ListRunsRequest
	cancelReqs           []*proto.CancelWorkflowProviderRunRequest
	nextUpsertErr        error
	nextUpsertTriggerErr error
	getErr               error
	getTriggerErr        error
	listErr              error
	listTriggersErr      error
	getRunErr            error
	listRunsErr          error
	listRunsNextPage     string
	cancelRunErr         error
	publishEventErr      error
}

func newMemoryWorkflowProvider() *memoryWorkflowProvider {
	return &memoryWorkflowProvider{
		definitions: map[string]*coreworkflow.Definition{},
		schedules:   map[string]*coreworkflow.Schedule{},
		triggers:    map[string]*coreworkflow.EventTrigger{},
		runs:        map[string]*coreworkflow.Run{},
	}
}

func (p *memoryWorkflowProvider) CreateDefinition(_ context.Context, req *proto.CreateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	id := strings.TrimSpace(req.GetIdempotencyKey())
	if id == "" {
		id = "definition"
	}
	definition := &coreworkflow.Definition{ID: id, Target: cloneWorkflowTarget(workflowwire.TargetFromProto(req.GetTarget())), CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId())}
	p.definitions[id] = definition
	return workflowwire.DefinitionToProto(cloneWorkflowDefinition(definition))
}

func (p *memoryWorkflowProvider) GetDefinition(_ context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	definition := p.definitions[strings.TrimSpace(req.GetDefinitionId())]
	if definition == nil {
		return nil, core.ErrNotFound
	}
	return workflowwire.DefinitionToProto(cloneWorkflowDefinition(definition))
}

func (p *memoryWorkflowProvider) UpdateDefinition(_ context.Context, req *proto.UpdateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	id := strings.TrimSpace(req.GetDefinitionId())
	if p.definitions[id] == nil {
		return nil, core.ErrNotFound
	}
	definition := &coreworkflow.Definition{ID: id, Target: cloneWorkflowTarget(workflowwire.TargetFromProto(req.GetTarget()))}
	p.definitions[id] = definition
	return workflowwire.DefinitionToProto(cloneWorkflowDefinition(definition))
}

func (p *memoryWorkflowProvider) DeleteDefinition(_ context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error {
	id := strings.TrimSpace(req.GetDefinitionId())
	if p.definitions[id] == nil {
		return core.ErrNotFound
	}
	delete(p.definitions, id)
	return nil
}

func (p *memoryWorkflowProvider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetRun(_ context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	if p.getRunErr != nil {
		return nil, p.getRunErr
	}
	run, ok := p.runs[req.GetRunId()]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	return workflowwire.RunToProto(cloneWorkflowRun(run))
}

func (p *memoryWorkflowProvider) ListRuns(_ context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	status, err := workflowwire.RunStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, err
	}
	coreReq := coreworkflow.ListRunsRequest{PageSize: int(req.GetPageSize()), PageToken: req.GetPageToken(), TargetApp: req.GetTargetApp(), Status: status}
	p.listRunReqs = append(p.listRunReqs, coreReq)
	if p.listRunsErr != nil {
		return nil, p.listRunsErr
	}
	out := &proto.ListWorkflowProviderRunsResponse{NextPageToken: p.listRunsNextPage}
	for _, run := range p.runs {
		if run != nil {
			pb, err := workflowwire.RunToProto(cloneWorkflowRun(run))
			if err != nil {
				return nil, err
			}
			out.Runs = append(out.Runs, pb)
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) CancelRun(_ context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	if p.cancelRunErr != nil {
		return nil, p.cancelRunErr
	}
	run, ok := p.runs[req.GetRunId()]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	run.Status = coreworkflow.RunStatusCanceled
	run.CompletedAt = &now
	if reason := strings.TrimSpace(req.GetReason()); reason != "" {
		run.StatusMessage = reason
	}
	p.cancelReqs = append(p.cancelReqs, gproto.Clone(req).(*proto.CancelWorkflowProviderRunRequest))
	return workflowwire.RunToProto(cloneWorkflowRun(run))
}

func (p *memoryWorkflowProvider) UpsertSchedule(_ context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	target := workflowwire.TargetFromProto(req.GetTarget())
	p.upsertReqs = append(p.upsertReqs, gproto.Clone(req).(*proto.UpsertWorkflowProviderScheduleRequest))
	if p.nextUpsertErr != nil {
		err := p.nextUpsertErr
		p.nextUpsertErr = nil
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	existing := p.schedules[req.GetScheduleId()]
	createdAt := &now
	if existing != nil && existing.CreatedAt != nil {
		createdAt = existing.CreatedAt
	}
	schedule := &coreworkflow.Schedule{
		ID:           req.GetScheduleId(),
		Cron:         req.GetCron(),
		Timezone:     req.GetTimezone(),
		Target:       target,
		DefinitionID: req.GetDefinitionId(),
		Paused:       req.GetPaused(),
		CreatedBySubjectID: strings.TrimSpace(req.GetRequestedBySubjectId()),
		CreatedAt:    createdAt,
		UpdatedAt:    &now,
	}
	p.schedules[req.GetScheduleId()] = cloneWorkflowSchedule(schedule)
	return workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
}

func (p *memoryWorkflowProvider) GetSchedule(_ context.Context, req *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	if p.getErr != nil {
		return nil, p.getErr
	}
	schedule, ok := p.schedules[req.GetScheduleId()]
	if !ok || schedule == nil {
		return nil, core.ErrNotFound
	}
	return workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
}

func (p *memoryWorkflowProvider) ListSchedules(_ context.Context, req *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	out := &proto.ListWorkflowProviderSchedulesResponse{}
	for _, schedule := range p.schedules {
		if schedule != nil {
			pb, err := workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
			if err != nil {
				return nil, err
			}
			out.Schedules = append(out.Schedules, pb)
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) DeleteSchedule(_ context.Context, req *proto.DeleteWorkflowProviderScheduleRequest) error {
	schedule, ok := p.schedules[req.GetScheduleId()]
	if !ok || schedule == nil {
		return core.ErrNotFound
	}
	delete(p.schedules, req.GetScheduleId())
	p.deleteReqs = append(p.deleteReqs, gproto.Clone(req).(*proto.DeleteWorkflowProviderScheduleRequest))
	return nil
}

func (p *memoryWorkflowProvider) PauseSchedule(_ context.Context, req *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, ok := p.schedules[req.GetScheduleId()]
	if !ok || schedule == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	schedule.Paused = true
	schedule.UpdatedAt = &now
	p.pauseReqs = append(p.pauseReqs, gproto.Clone(req).(*proto.PauseWorkflowProviderScheduleRequest))
	return workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
}

func (p *memoryWorkflowProvider) ResumeSchedule(_ context.Context, req *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, ok := p.schedules[req.GetScheduleId()]
	if !ok || schedule == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	schedule.Paused = false
	schedule.UpdatedAt = &now
	p.resumeReqs = append(p.resumeReqs, gproto.Clone(req).(*proto.ResumeWorkflowProviderScheduleRequest))
	return workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
}

func (p *memoryWorkflowProvider) UpsertEventTrigger(_ context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	match := workflowwire.EventMatchFromProto(req.GetMatch())
	target := workflowwire.TargetFromProto(req.GetTarget())
	p.upsertTriggerReqs = append(p.upsertTriggerReqs, gproto.Clone(req).(*proto.UpsertWorkflowProviderEventTriggerRequest))
	if p.nextUpsertTriggerErr != nil {
		err := p.nextUpsertTriggerErr
		p.nextUpsertTriggerErr = nil
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	existing := p.triggers[req.GetTriggerId()]
	createdAt := &now
	if existing != nil && existing.CreatedAt != nil {
		createdAt = existing.CreatedAt
	}
	trigger := &coreworkflow.EventTrigger{
		ID:           req.GetTriggerId(),
		Match:        match,
		Target:       target,
		DefinitionID: req.GetDefinitionId(),
		Paused:       req.GetPaused(),
		CreatedBySubjectID: strings.TrimSpace(req.GetRequestedBySubjectId()),
		CreatedAt:    createdAt,
		UpdatedAt:    &now,
	}
	p.triggers[req.GetTriggerId()] = cloneWorkflowEventTrigger(trigger)
	return workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
}

func (p *memoryWorkflowProvider) GetEventTrigger(_ context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	if p.getTriggerErr != nil {
		return nil, p.getTriggerErr
	}
	trigger, ok := p.triggers[req.GetTriggerId()]
	if !ok || trigger == nil {
		return nil, core.ErrNotFound
	}
	return workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
}

func (p *memoryWorkflowProvider) ListEventTriggers(_ context.Context, _ *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	if p.listTriggersErr != nil {
		return nil, p.listTriggersErr
	}
	out := &proto.ListWorkflowProviderEventTriggersResponse{}
	for _, trigger := range p.triggers {
		if trigger != nil {
			pb, err := workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
			if err != nil {
				return nil, err
			}
			out.Triggers = append(out.Triggers, pb)
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) DeleteEventTrigger(_ context.Context, req *proto.DeleteWorkflowProviderEventTriggerRequest) error {
	trigger, ok := p.triggers[req.GetTriggerId()]
	if !ok || trigger == nil {
		return core.ErrNotFound
	}
	delete(p.triggers, req.GetTriggerId())
	p.deleteTriggerReqs = append(p.deleteTriggerReqs, gproto.Clone(req).(*proto.DeleteWorkflowProviderEventTriggerRequest))
	return nil
}

func (p *memoryWorkflowProvider) PauseEventTrigger(_ context.Context, req *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, ok := p.triggers[req.GetTriggerId()]
	if !ok || trigger == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	trigger.Paused = true
	trigger.UpdatedAt = &now
	p.pauseTriggerReqs = append(p.pauseTriggerReqs, gproto.Clone(req).(*proto.PauseWorkflowProviderEventTriggerRequest))
	return workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
}

func (p *memoryWorkflowProvider) ResumeEventTrigger(_ context.Context, req *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, ok := p.triggers[req.GetTriggerId()]
	if !ok || trigger == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	trigger.Paused = false
	trigger.UpdatedAt = &now
	p.resumeTriggerReqs = append(p.resumeTriggerReqs, gproto.Clone(req).(*proto.ResumeWorkflowProviderEventTriggerRequest))
	return workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
}

func (p *memoryWorkflowProvider) PublishEvent(_ context.Context, req *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	if p.publishEventErr != nil {
		return nil, p.publishEventErr
	}
	p.publishEventReqs = append(p.publishEventReqs, gproto.Clone(req).(*proto.PublishWorkflowProviderEventRequest))
	return req.GetEvent(), nil
}

func (p *memoryWorkflowProvider) Ping(context.Context) error { return nil }
func (p *memoryWorkflowProvider) Close() error               { return nil }

func cloneWorkflowDefinition(definition *coreworkflow.Definition) *coreworkflow.Definition {
	if definition == nil {
		return nil
	}
	cloned := *definition
	cloned.Target = cloneWorkflowTarget(definition.Target)
	if definition.CreatedAt != nil {
		value := *definition.CreatedAt
		cloned.CreatedAt = &value
	}
	return &cloned
}

func cloneWorkflowSchedule(schedule *coreworkflow.Schedule) *coreworkflow.Schedule {
	if schedule == nil {
		return nil
	}
	cloned := *schedule
	cloned.Target = cloneWorkflowTarget(schedule.Target)
	if schedule.CreatedAt != nil {
		value := *schedule.CreatedAt
		cloned.CreatedAt = &value
	}
	if schedule.UpdatedAt != nil {
		value := *schedule.UpdatedAt
		cloned.UpdatedAt = &value
	}
	if schedule.NextRunAt != nil {
		value := *schedule.NextRunAt
		cloned.NextRunAt = &value
	}
	return &cloned
}

func cloneWorkflowRun(run *coreworkflow.Run) *coreworkflow.Run {
	if run == nil {
		return nil
	}
	cloned := *run
	cloned.Target = cloneWorkflowTarget(run.Target)
	if run.Trigger.Event != nil {
		event := *run.Trigger.Event
		event.Event.Data = cloneMap(run.Trigger.Event.Event.Data)
		event.Event.Extensions = cloneMap(run.Trigger.Event.Event.Extensions)
		if run.Trigger.Event.Event.Time != nil {
			value := *run.Trigger.Event.Event.Time
			event.Event.Time = &value
		}
		cloned.Trigger.Event = &event
	}
	if run.Trigger.Schedule != nil {
		schedule := *run.Trigger.Schedule
		if run.Trigger.Schedule.ScheduledFor != nil {
			value := *run.Trigger.Schedule.ScheduledFor
			schedule.ScheduledFor = &value
		}
		cloned.Trigger.Schedule = &schedule
	}
	if run.CreatedAt != nil {
		value := *run.CreatedAt
		cloned.CreatedAt = &value
	}
	if run.StartedAt != nil {
		value := *run.StartedAt
		cloned.StartedAt = &value
	}
	if run.CompletedAt != nil {
		value := *run.CompletedAt
		cloned.CompletedAt = &value
	}
	return &cloned
}

func cloneWorkflowEventTrigger(trigger *coreworkflow.EventTrigger) *coreworkflow.EventTrigger {
	if trigger == nil {
		return nil
	}
	cloned := *trigger
	cloned.Target = cloneWorkflowTarget(trigger.Target)
	if trigger.CreatedAt != nil {
		value := *trigger.CreatedAt
		cloned.CreatedAt = &value
	}
	if trigger.UpdatedAt != nil {
		value := *trigger.UpdatedAt
		cloned.UpdatedAt = &value
	}
	return &cloned
}

func cloneWorkflowTarget(target coreworkflow.Target) coreworkflow.Target {
	cloned := coreworkflow.Target{Steps: make([]coreworkflow.Step, len(target.Steps))}
	for i := range target.Steps {
		step := target.Steps[i]
		step.Inputs = cloneWorkflowValueMap(step.Inputs)
		if step.App != nil {
			app := *step.App
			app.Input = coreworkflow.CloneValue(step.App.Input)
			step.App = &app
		}
		if step.Agent != nil {
			agent := *step.Agent
			agent.Messages = slices.Clone(agent.Messages)
			agent.ToolRefs = slices.Clone(agent.ToolRefs)
			if agent.Output.Structured != nil {
				structured := *agent.Output.Structured
				structured.Schema = cloneMap(structured.Schema)
				agent.Output.Structured = &structured
			}
			agent.ModelOptions = cloneMap(agent.ModelOptions)
			step.Agent = &agent
		}
		step.Metadata = cloneMap(step.Metadata)
		cloned.Steps[i] = step
	}
	return cloned
}

func requireCoreWorkflowAppStep(t *testing.T, target coreworkflow.Target) *coreworkflow.AppCall {
	t.Helper()
	for i := range target.Steps {
		if target.Steps[i].App != nil {
			return target.Steps[i].App
		}
	}
	t.Fatalf("target app step is missing: %#v", target)
	return nil
}

func requireCoreWorkflowAgentStep(t *testing.T, target coreworkflow.Target) coreworkflow.Step {
	t.Helper()
	for i := range target.Steps {
		if target.Steps[i].Agent != nil {
			return target.Steps[i]
		}
	}
	t.Fatalf("target agent step is missing: %#v", target)
	return coreworkflow.Step{}
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneWorkflowValueMap(src map[string]coreworkflow.Value) map[string]coreworkflow.Value {
	if src == nil {
		return nil
	}
	dst := make(map[string]coreworkflow.Value, len(src))
	for key, value := range src {
		dst[key] = coreworkflow.CloneValue(value)
	}
	return dst
}

type workflowScheduleResponse struct {
	ID       string                 `json:"id"`
	Provider string                 `json:"provider"`
	Cron     string                 `json:"cron"`
	Timezone string                 `json:"timezone"`
	Target   workflowTargetResponse `json:"target"`
	Paused   bool                   `json:"paused"`
}

type workflowEventTriggerResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Match    struct {
		Type    string `json:"type"`
		Source  string `json:"source"`
		Subject string `json:"subject"`
	} `json:"match"`
	Target workflowTargetResponse `json:"target"`
	Paused bool                   `json:"paused"`
}

type workflowTargetResponse struct {
	Steps []workflowStepTargetResponse `json:"steps"`
}

type workflowStepTargetResponse struct {
	ID             string                     `json:"id"`
	App            *workflowAppStepResponse   `json:"app"`
	Agent          *workflowAgentStepResponse `json:"agent"`
	TimeoutSeconds int                        `json:"timeoutSeconds"`
}

type workflowAppStepResponse struct {
	Name           string         `json:"name"`
	Operation      string         `json:"operation"`
	Connection     string         `json:"connection"`
	Instance       string         `json:"instance"`
	CredentialMode string         `json:"credentialMode"`
	Input          map[string]any `json:"input"`
}

type workflowAgentStepResponse struct {
	ProviderName string `json:"provider"`
	Model        string `json:"model"`
	Prompt       *struct {
		Template string `json:"template"`
	} `json:"prompt"`
	TimeoutSeconds int `json:"timeoutSeconds"`
	ToolRefs       []struct {
		System    string `json:"system"`
		App       string `json:"app"`
		Operation string `json:"operation"`
	} `json:"tools"`
}

func requireWorkflowAppStep(t *testing.T, target workflowTargetResponse) *workflowAppStepResponse {
	t.Helper()
	for i := range target.Steps {
		if target.Steps[i].App != nil {
			return target.Steps[i].App
		}
	}
	t.Fatalf("target app step is missing: %#v", target)
	return nil
}

func requireWorkflowAgentStep(t *testing.T, target workflowTargetResponse) workflowStepTargetResponse {
	t.Helper()
	for i := range target.Steps {
		if target.Steps[i].Agent != nil {
			return target.Steps[i]
		}
	}
	t.Fatalf("target agent step is missing: %#v", target)
	return workflowStepTargetResponse{}
}

func requireCanonicalAppStepJSON(t *testing.T, body []byte) workflowAppStepResponse {
	t.Helper()

	var envelope struct {
		Target map[string]json.RawMessage `json:"target"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	if len(envelope.Target) == 0 {
		t.Fatalf("response target is empty: %s", body)
	}
	for _, field := range []string{"app", "agent", "operation", "connection", "instance", "input"} {
		if _, ok := envelope.Target[field]; ok {
			t.Fatalf("response target contains flat field %q: %s", field, body)
		}
	}
	rawSteps, ok := envelope.Target["steps"]
	if !ok {
		t.Fatalf("response target missing steps: %s", body)
	}
	var steps []workflowStepTargetResponse
	if err := json.Unmarshal(rawSteps, &steps); err != nil {
		t.Fatalf("decode response target steps: %v", err)
	}
	app := requireWorkflowAppStep(t, workflowTargetResponse{Steps: steps})
	if app.Name == "" || app.Operation == "" {
		t.Fatalf("response app step = %#v, want name and operation", app)
	}
	return *app
}

func TestWorkflowScheduleCRUD(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createBody := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"incremental"}}}]}}`)
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}

	createRespBody, err := io.ReadAll(createResp.Body)
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	requireCanonicalAppStepJSON(t, createRespBody)
	var created workflowScheduleResponse
	if err := json.Unmarshal(createRespBody, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdApp := requireWorkflowAppStep(t, created.Target)
	if created.Provider != "basic" || createdApp.Operation != "sync" || createdApp.Connection != "analytics" || createdApp.Instance != "tenant-a" {
		t.Fatalf("created schedule = %#v", created)
	}
	if createdApp.Name != "roadmap" {
		t.Fatalf("created app step = %q, want roadmap", createdApp.Name)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	createUpsertTarget := workflowwire.TargetFromProto(provider.upsertReqs[len(provider.upsertReqs)-1].GetTarget())
	createUpsertApp := requireCoreWorkflowAppStep(t, createUpsertTarget)
	if createUpsertApp.Name != "roadmap" || createUpsertApp.Operation != "sync" {
		t.Fatalf("upsert target = %#v", createUpsertTarget)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed schedules = %#v", listed)
	}
	listedApp := requireWorkflowAppStep(t, listed[0].Target)
	if listedApp.Name != "roadmap" {
		t.Fatalf("listed app step = %q, want roadmap", listedApp.Name)
	}

	updateBody := bytes.NewBufferString(`{"cron":"0 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"full"}}}]},"paused":true}`)
	updateReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/workflow/schedules/"+created.ID, updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer func() { _ = updateResp.Body.Close() }()
	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("expected 200, got %d: %s", updateResp.StatusCode, body)
	}
	if len(provider.upsertReqs) != 2 {
		t.Fatalf("upsert requests after update = %d, want 2", len(provider.upsertReqs))
	}
	updateUpsertTarget := workflowwire.TargetFromProto(provider.upsertReqs[len(provider.upsertReqs)-1].GetTarget())
	updateUpsertApp := requireCoreWorkflowAppStep(t, updateUpsertTarget)
	if updateUpsertApp.Input.Object["mode"].Literal != "full" {
		t.Fatalf("update target input = %#v", updateUpsertApp.Input)
	}

	pauseReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/"+created.ID+"/pause", nil)
	pauseReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	pauseResp, err := http.DefaultClient.Do(pauseReq)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	defer func() { _ = pauseResp.Body.Close() }()
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", pauseResp.StatusCode)
	}

	resumeReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/"+created.ID+"/resume", nil)
	resumeReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resumeResp, err := http.DefaultClient.Do(resumeReq)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	defer func() { _ = resumeResp.Body.Close() }()
	if resumeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resumeResp.StatusCode)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/schedules/"+created.ID, nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", deleteResp.StatusCode)
	}
	if _, ok := provider.schedules[created.ID]; ok {
		t.Fatal("expected schedule to be deleted from provider")
	}
}

func TestWorkflowScheduleAgentStepCreateAndList(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada-agent@example.test")
	provider := newMemoryWorkflowProvider()
	agentProvider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Agent = &stubAgentControl{defaultProviderName: "managed", provider: agentProvider}
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Providers: cfg.Providers,
			Agent:     cfg.Agent,
			Invoker:   cfg.Invoker,
			RunGrants: newServerTestAgentRunGrants(t),
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createBody := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"agent","agent":{"provider":"managed","model":"deep","prompt":"Send the status summary","output":{"text":{}},"tools":[{"app":"roadmap","operation":"sync"}]},"timeoutSeconds":90},{"id":"reply","app":{"name":"roadmap","operation":"sync","input":{"object":{"format":{"literal":"plain"},"text":{"stepOutput":{"stepId":"agent","path":"agent.output.text.text"}},"ref":{"signalPayload":"reply_ref"}}}}}]}}`)
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}
	var created workflowScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdStep := requireWorkflowAgentStep(t, created.Target)
	if createdStep.Agent.ProviderName != "managed" || createdStep.Agent.Model != "deep" {
		t.Fatalf("created agent step = %#v", createdStep.Agent)
	}
	if len(createdStep.Agent.ToolRefs) != 1 || createdStep.Agent.ToolRefs[0].App != "roadmap" || createdStep.Agent.ToolRefs[0].Operation != "sync" {
		t.Fatalf("created agent tools = %#v", createdStep.Agent.ToolRefs)
	}
	createdApp := requireWorkflowAppStep(t, created.Target)
	if createdApp.Name != "roadmap" || createdApp.Operation != "sync" {
		t.Fatalf("created app step = %#v", createdApp)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	storedTarget := workflowwire.TargetFromProto(provider.upsertReqs[0].GetTarget())
	requireCoreWorkflowAgentStep(t, storedTarget)
	storedApp := requireCoreWorkflowAppStep(t, storedTarget)
	if storedApp.Name != "roadmap" || storedApp.Operation != "sync" {
		t.Fatalf("stored app step = %#v", storedApp)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed schedules = %#v", listed)
	}
	listedStep := requireWorkflowAgentStep(t, listed[0].Target)
	if listedStep.Agent.Prompt == nil || listedStep.Agent.Prompt.Template != "Send the status summary" {
		t.Fatalf("listed agent prompt = %#v", listedStep.Agent.Prompt)
	}
	listedApp := requireWorkflowAppStep(t, listed[0].Target)
	if listedApp.Operation != "sync" {
		t.Fatalf("listed app step = %#v", listedApp)
	}
}

func TestWorkflowScheduleAgentStepPreservesWorkflowSystemToolRefs(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada-agent-system@example.test")
	provider := newMemoryWorkflowProvider()
	agentProvider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Agent = &stubAgentControl{defaultProviderName: "managed", provider: agentProvider}
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Providers: cfg.Providers,
			Agent:     cfg.Agent,
			Invoker:   cfg.Invoker,
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createBody := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"agent","agent":{"provider":"managed","model":"deep","prompt":"Manage schedules","output":{"text":{}},"tools":[{"system":"workflow","operation":"schedules.list"},{"app":"roadmap","operation":"sync"}]}}]}}`)
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}
	var created workflowScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdStep := requireWorkflowAgentStep(t, created.Target)
	if len(createdStep.Agent.ToolRefs) != 2 {
		t.Fatalf("created agent step = %#v", createdStep.Agent)
	}
	if createdStep.Agent.ToolRefs[0].System != coreagent.SystemToolWorkflow || createdStep.Agent.ToolRefs[0].Operation != "schedules.list" {
		t.Fatalf("created system tool ref = %#v", createdStep.Agent.ToolRefs[0])
	}
	if createdStep.Agent.ToolRefs[1].App != "roadmap" || createdStep.Agent.ToolRefs[1].Operation != "sync" {
		t.Fatalf("created app tool ref = %#v", createdStep.Agent.ToolRefs[1])
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	storedTarget := workflowwire.TargetFromProto(provider.upsertReqs[0].GetTarget())
	storedStep := requireCoreWorkflowAgentStep(t, storedTarget)
	if len(storedStep.Agent.ToolRefs) != 2 {
		t.Fatalf("stored agent step = %#v", storedTarget)
	}
	if storedStep.Agent.ToolRefs[0].System != coreagent.SystemToolWorkflow || storedStep.Agent.ToolRefs[0].Operation != "schedules.list" {
		t.Fatalf("stored system tool ref = %#v", storedStep.Agent.ToolRefs[0])
	}
}

func TestWorkflowScheduleListAndMutationsAreOwnerScoped(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	grace := seedUser(t, services, "grace@example.test")
	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	provider.schedules["sched-ada"] = &coreworkflow.Schedule{
		ID:        "sched-ada",
		Cron:      "*/5 * * * *",
		Target:    workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(ada.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	provider.schedules["sched-grace"] = &coreworkflow.Schedule{
		ID:        "sched-grace",
		Cron:      "0 * * * *",
		Target:    workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(grace.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	provider.schedules["sched-analytics"] = &coreworkflow.Schedule{
		ID:   "sched-analytics",
		Cron: "15 * * * *",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "app",
			App: &coreworkflow.AppCall{
				Name:           "analytics",
				Operation:      "sync",
				CredentialMode: core.ConnectionModeNone,
			},
		}}},
		CreatedBySubjectID: principal.UserSubjectID(ada.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "ada-session":
					return &core.UserIdentity{Email: ada.Email, DisplayName: "Ada"}, nil
				case "grace-session":
					return &core.UserIdentity{Email: grace.Email, DisplayName: "Grace"}, nil
				default:
					return nil, core.ErrNotFound
				}
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		}, &coretesting.StubIntegration{
			N:        "analytics",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "analytics",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed schedules = %#v", listed)
	}
	listedIDs := []string{listed[0].ID, listed[1].ID}
	slices.Sort(listedIDs)
	if !slices.Equal(listedIDs, []string{"sched-ada", "sched-analytics"}) {
		t.Fatalf("listed schedules = %#v", listed)
	}
	var listedAnalytics *workflowScheduleResponse
	for i := range listed {
		if listed[i].ID == "sched-analytics" {
			listedAnalytics = &listed[i]
			break
		}
	}
	if listedAnalytics == nil {
		t.Fatalf("listed schedules missing analytics schedule: %#v", listed)
	}
	if got := requireWorkflowAppStep(t, listedAnalytics.Target).CredentialMode; got != string(core.ConnectionModeNone) {
		t.Fatalf("listed analytics credential mode = %q, want %q", got, core.ConnectionModeNone)
	}

	getAnalyticsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/sched-analytics", nil)
	getAnalyticsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getAnalyticsResp, err := http.DefaultClient.Do(getAnalyticsReq)
	if err != nil {
		t.Fatalf("get analytics request: %v", err)
	}
	defer func() { _ = getAnalyticsResp.Body.Close() }()
	if getAnalyticsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for analytics schedule, got %d", getAnalyticsResp.StatusCode)
	}
	var analytics workflowScheduleResponse
	if err := json.NewDecoder(getAnalyticsResp.Body).Decode(&analytics); err != nil {
		t.Fatalf("decode analytics schedule: %v", err)
	}
	if got := requireWorkflowAppStep(t, analytics.Target).CredentialMode; got != string(core.ConnectionModeNone) {
		t.Fatalf("analytics credential mode = %q, want %q", got, core.ConnectionModeNone)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/sched-grace", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", getResp.StatusCode)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/schedules/sched-grace", nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", deleteResp.StatusCode)
	}
	if _, ok := provider.schedules["sched-grace"]; !ok {
		t.Fatal("expected grace schedule to remain after unauthorized delete")
	}
	if _, ok := provider.schedules["sched-analytics"]; !ok {
		t.Fatal("expected analytics schedule to remain after deleting someone else's workflow")
	}
}

func TestCreateWorkflowScheduleAllowsAuthorizedCatalogOperation(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
					{ID: "export", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"export"}}]}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}
	if len(provider.upsertReqs) != 1 || requireCoreWorkflowAppStep(t, workflowwire.TargetFromProto(provider.upsertReqs[0].GetTarget())).Operation != "export" {
		t.Fatalf("upsert requests = %#v", provider.upsertReqs)
	}
}

func TestCreateWorkflowScheduleRejectsPublicTargetCredentialMode(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	cases := []struct {
		name string
		body string
		want string
	}{{
		name: "app step",
		body: `{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","credentialMode":"none"}}]}}`,
		want: "workflow target.steps[0].app.credentialMode is not supported",
	}}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/", bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request: %v", tc.name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatalf("%s close response: %v", tc.name, closeErr)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", tc.name, resp.StatusCode, body)
		}
		if !strings.Contains(string(body), tc.want) {
			t.Fatalf("%s: response body = %s, want %q", tc.name, body, tc.want)
		}
	}
	if len(provider.upsertReqs) != 0 {
		t.Fatalf("upsert requests = %#v, want none", provider.upsertReqs)
	}
}

func TestWorkflowScheduleAPITokenScopeFiltersOperations(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := services.APITokens.StoreAPIToken(context.Background(), &core.APIToken{
		ID:                  "workflow-scope-token",
		OwnerKind:           core.APITokenOwnerKindUser,
		OwnerID:             user.ID,
		CredentialSubjectID: principal.UserSubjectID(user.ID),
		Name:                "workflow-scope-token",
		HashedToken:         hashed,
		ExpiresAt:           &expiresAt,
		Permissions:         []core.AccessPermission{{App: "roadmap", Operations: []string{"sync"}}},
	}); err != nil {
		t.Fatalf("StoreAPIToken: %v", err)
	}

	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	provider.schedules["sched-sync"] = &coreworkflow.Schedule{
		ID:        "sched-sync",
		Cron:      "*/5 * * * *",
		Target:    workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	provider.schedules["sched-export"] = &coreworkflow.Schedule{
		ID:        "sched-export",
		Cron:      "0 * * * *",
		Target:    workflowAppStepTarget("roadmap", "export"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
					{ID: "export", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.Header.Set("Authorization", "Bearer "+plaintext)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "sched-sync" {
		t.Fatalf("listed schedules = %#v", listed)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/sched-export", nil)
	getReq.Header.Set("Authorization", "Bearer "+plaintext)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", getResp.StatusCode)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/schedules/sched-export", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+plaintext)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", deleteResp.StatusCode)
	}

	createReq, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/schedules/",
		bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"export","instance":"tenant-a"}}]}}`),
	)
	createReq.Header.Set("Authorization", "Bearer "+plaintext)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", createResp.StatusCode)
	}
}

func TestWorkflowScheduleUpdateFailureKeepsExistingTarget(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	oldTarget := coreworkflow.Target{
		Steps: []coreworkflow.Step{{
			ID: "app",
			App: &coreworkflow.AppCall{
				Name:       "roadmap",
				Operation:  "sync",
				Connection: "analytics",
				Instance:   "tenant-a",
			},
		}},
	}
	now := time.Now().UTC().Truncate(time.Second)
	provider.schedules["sched-ada"] = &coreworkflow.Schedule{
		ID:        "sched-ada",
		Cron:      "*/5 * * * *",
		Timezone:  "UTC",
		Target:    oldTarget,
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	provider.nextUpsertErr = errors.New("boom")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	updateReq, _ := http.NewRequest(
		http.MethodPut,
		ts.URL+"/api/v1/workflow/schedules/sched-ada",
		bytes.NewBufferString(`{"cron":"*/10 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-b"}}]}}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer func() { _ = updateResp.Body.Close() }()
	if updateResp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("expected 500, got %d: %s", updateResp.StatusCode, body)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	if requireCoreWorkflowAppStep(t, provider.schedules["sched-ada"].Target).Instance != "tenant-a" {
		t.Fatalf("schedule target after failed update = %#v", provider.schedules["sched-ada"].Target)
	}
}

func TestWorkflowScheduleCreateFailureHidesInternalError(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	provider.nextUpsertErr = errors.New("boom")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/schedules/",
		bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a"}}]}}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 500, got %d: %s", createResp.StatusCode, body)
	}

	body, err := io.ReadAll(createResp.Body)
	if err != nil {
		t.Fatalf("read create response body: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "boom") {
		t.Fatalf("expected generic provider error, got body %q", text)
	}
	if !strings.Contains(text, "workflow schedule request failed for integration") || !strings.Contains(text, "roadmap") {
		t.Fatalf("expected generic workflow provider message, got body %q", text)
	}
}

func TestWorkflowScheduleCreatePinsResolvedInstance(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	seedToken(t, services, &core.ExternalCredential{
		ID:          "roadmap-default-tenant-a",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "roadmap",
		Connection:  "default",
		Instance:    "tenant-a",
		AccessToken: "token-a",
	})

	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/schedules/",
		bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"default"}}]}}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}

	var created workflowScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdApp := requireWorkflowAppStep(t, created.Target)
	if createdApp.Instance != "tenant-a" {
		t.Fatalf("created schedule target instance = %q, want tenant-a", createdApp.Instance)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	storedTarget := workflowwire.TargetFromProto(provider.upsertReqs[0].GetTarget())
	if requireCoreWorkflowAppStep(t, storedTarget).Instance != "tenant-a" {
		t.Fatalf("stored target = %#v, want resolved instance tenant-a", storedTarget)
	}
}

func TestGlobalWorkflowScheduleLookupIgnoresUnrelatedProviderFailures(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	advancedProvider.getErr = errors.New("advanced down")
	advancedProvider.listErr = errors.New("advanced down")

	now := time.Now().UTC().Truncate(time.Second)
	basicProvider.schedules["sched-ada-basic"] = &coreworkflow.Schedule{
		ID:        "sched-ada-basic",
		Cron:      "*/5 * * * *",
		Target:    workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "roadmap",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "analytics",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
		)
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("expected 200, got %d: %s", listResp.StatusCode, body)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "sched-ada-basic" {
		t.Fatalf("listed schedules = %#v", listed)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/sched-ada-basic", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("expected 200, got %d: %s", getResp.StatusCode, body)
	}
}

func TestGlobalWorkflowScheduleCRUDAcrossProviders(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	seedToken(t, services, &core.ExternalCredential{
		ID:          "roadmap-default-token",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "roadmap",
		Connection:  "default",
		AccessToken: "roadmap-token",
	})
	seedToken(t, services, &core.ExternalCredential{
		ID:          "analytics-default-token",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "analytics",
		Connection:  "default",
		AccessToken: "analytics-token",
	})
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "roadmap",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "analytics",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
		)
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/schedules/",
		bytes.NewBufferString(`{"provider":"basic","cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"incremental"}}}]}}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}

	var created workflowScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdApp := requireWorkflowAppStep(t, created.Target)
	if created.Provider != "basic" || createdApp.Name != "roadmap" || createdApp.Operation != "sync" {
		t.Fatalf("created schedule = %#v", created)
	}
	if len(basicProvider.upsertReqs) != 1 {
		t.Fatalf("basic upsert requests = %d, want 1", len(basicProvider.upsertReqs))
	}
	basicCreateTarget := workflowwire.TargetFromProto(basicProvider.upsertReqs[0].GetTarget())
	if requireCoreWorkflowAppStep(t, basicCreateTarget).Name != "roadmap" {
		t.Fatalf("basic create target = %#v", basicCreateTarget)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("expected 200, got %d: %s", listResp.StatusCode, body)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Provider != "basic" {
		t.Fatalf("listed schedules = %#v", listed)
	}
	listedApp := requireWorkflowAppStep(t, listed[0].Target)
	if listedApp.Name != "roadmap" {
		t.Fatalf("listed schedules = %#v", listed)
	}

	updateReq, _ := http.NewRequest(
		http.MethodPut,
		ts.URL+"/api/v1/workflow/schedules/"+created.ID,
		bytes.NewBufferString(`{"provider":"advanced","cron":"0 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"analytics","operation":"sync","connection":"warehouse","instance":"tenant-b","input":{"mode":"full"}}}]},"paused":true}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer func() { _ = updateResp.Body.Close() }()
	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("expected 200, got %d: %s", updateResp.StatusCode, body)
	}

	var updated workflowScheduleResponse
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	updatedApp := requireWorkflowAppStep(t, updated.Target)
	if updated.Provider != "advanced" || updatedApp.Name != "analytics" || !updated.Paused {
		t.Fatalf("updated schedule = %#v", updated)
	}
	if len(advancedProvider.upsertReqs) != 1 {
		t.Fatalf("advanced upsert requests = %d, want 1", len(advancedProvider.upsertReqs))
	}
	if len(basicProvider.deleteReqs) != 1 || basicProvider.deleteReqs[0].GetScheduleId() != created.ID {
		t.Fatalf("basic delete requests = %#v", basicProvider.deleteReqs)
	}
	if _, ok := basicProvider.schedules[created.ID]; ok {
		t.Fatal("expected global update to remove schedule from old provider")
	}
	if _, ok := advancedProvider.schedules[created.ID]; !ok {
		t.Fatal("expected global update to store schedule in new provider")
	}

	pauseReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/"+created.ID+"/pause", nil)
	pauseReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	pauseResp, err := http.DefaultClient.Do(pauseReq)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	defer func() { _ = pauseResp.Body.Close() }()
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", pauseResp.StatusCode)
	}
	if len(advancedProvider.pauseReqs) != 1 {
		t.Fatalf("advanced pause requests = %d, want 1", len(advancedProvider.pauseReqs))
	}

	resumeReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/schedules/"+created.ID+"/resume", nil)
	resumeReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resumeResp, err := http.DefaultClient.Do(resumeReq)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	defer func() { _ = resumeResp.Body.Close() }()
	if resumeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resumeResp.StatusCode)
	}
	if len(advancedProvider.resumeReqs) != 1 {
		t.Fatalf("advanced resume requests = %d, want 1", len(advancedProvider.resumeReqs))
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/schedules/"+created.ID, nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", deleteResp.StatusCode)
	}
	if _, ok := advancedProvider.schedules[created.ID]; ok {
		t.Fatal("expected schedule to be deleted from current global provider")
	}
}

func TestGlobalWorkflowScheduleListAndMutationsAreOwnerScopedAcrossProviders(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	grace := seedUser(t, services, "grace@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)

	basicProvider.schedules["sched-ada-basic"] = &coreworkflow.Schedule{
		ID:        "sched-ada-basic",
		Cron:      "*/5 * * * *",
		Target:    workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(ada.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	advancedProvider.schedules["sched-ada-advanced"] = &coreworkflow.Schedule{
		ID:        "sched-ada-advanced",
		Cron:      "0 * * * *",
		Target:    workflowAppStepTarget("analytics", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(ada.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	advancedProvider.schedules["sched-grace-advanced"] = &coreworkflow.Schedule{
		ID:        "sched-grace-advanced",
		Cron:      "15 * * * *",
		Target:    workflowAppStepTarget("analytics", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(grace.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "ada-session":
					return &core.UserIdentity{Email: ada.Email, DisplayName: "Ada"}, nil
				case "grace-session":
					return &core.UserIdentity{Email: grace.Email, DisplayName: "Grace"}, nil
				default:
					return nil, core.ErrNotFound
				}
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "roadmap",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "analytics",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
		)
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowScheduleResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed schedules = %#v", listed)
	}
	listedIDs := []string{listed[0].ID, listed[1].ID}
	slices.Sort(listedIDs)
	if !slices.Equal(listedIDs, []string{"sched-ada-advanced", "sched-ada-basic"}) {
		t.Fatalf("listed schedules = %#v", listed)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/sched-grace-advanced", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", getResp.StatusCode)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/schedules/sched-grace-advanced", nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", deleteResp.StatusCode)
	}
	if _, ok := advancedProvider.schedules["sched-grace-advanced"]; !ok {
		t.Fatal("expected grace schedule to remain after unauthorized global delete")
	}
}

func TestGlobalWorkflowEventTriggerCRUDAcrossProviders(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: ada.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "roadmap",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "analytics",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
		)
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/event-triggers/",
		bytes.NewBufferString(`{"provider":"basic","match":{"type":"roadmap.item.updated","source":"roadmap","subject":"item"},"target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"incremental"}}}]}}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}

	createRespBody, err := io.ReadAll(createResp.Body)
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	requireCanonicalAppStepJSON(t, createRespBody)
	var created workflowEventTriggerResponse
	if err := json.Unmarshal(createRespBody, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdApp := requireWorkflowAppStep(t, created.Target)
	if created.Provider != "basic" || created.Match.Type != "roadmap.item.updated" || createdApp.Name != "roadmap" || createdApp.Operation != "sync" {
		t.Fatalf("created trigger = %#v", created)
	}
	if len(basicProvider.upsertTriggerReqs) != 1 {
		t.Fatalf("basic trigger upsert requests = %d, want 1", len(basicProvider.upsertTriggerReqs))
	}
	basicCreateTarget := workflowwire.TargetFromProto(basicProvider.upsertTriggerReqs[0].GetTarget())
	if requireCoreWorkflowAppStep(t, basicCreateTarget).Name != "roadmap" {
		t.Fatalf("basic create target = %#v", basicCreateTarget)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/event-triggers/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("expected 200, got %d: %s", listResp.StatusCode, body)
	}
	var listed []workflowEventTriggerResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Provider != "basic" || listed[0].Match.Type != "roadmap.item.updated" {
		t.Fatalf("listed triggers = %#v", listed)
	}

	updateReq, _ := http.NewRequest(
		http.MethodPut,
		ts.URL+"/api/v1/workflow/event-triggers/"+created.ID,
		bytes.NewBufferString(`{"provider":"advanced","match":{"type":"analytics.item.synced","source":"analytics","subject":"sync"},"target":{"steps":[{"id":"app","app":{"name":"analytics","operation":"sync","connection":"warehouse","instance":"tenant-b","input":{"mode":"full"}}}]},"paused":true}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer func() { _ = updateResp.Body.Close() }()
	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("expected 200, got %d: %s", updateResp.StatusCode, body)
	}

	var updated workflowEventTriggerResponse
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	updatedApp := requireWorkflowAppStep(t, updated.Target)
	if updated.Provider != "advanced" || updated.Match.Type != "analytics.item.synced" || updatedApp.Name != "analytics" || !updated.Paused {
		t.Fatalf("updated trigger = %#v", updated)
	}
	if len(advancedProvider.upsertTriggerReqs) != 1 {
		t.Fatalf("advanced trigger upsert requests = %d, want 1", len(advancedProvider.upsertTriggerReqs))
	}
	if len(basicProvider.deleteTriggerReqs) != 1 || basicProvider.deleteTriggerReqs[0].GetTriggerId() != created.ID {
		t.Fatalf("basic delete trigger requests = %#v", basicProvider.deleteTriggerReqs)
	}
	if _, ok := basicProvider.triggers[created.ID]; ok {
		t.Fatal("expected global update to remove event trigger from old provider")
	}
	if _, ok := advancedProvider.triggers[created.ID]; !ok {
		t.Fatal("expected global update to store event trigger in new provider")
	}

	pauseReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/event-triggers/"+created.ID+"/pause", nil)
	pauseReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	pauseResp, err := http.DefaultClient.Do(pauseReq)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	defer func() { _ = pauseResp.Body.Close() }()
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", pauseResp.StatusCode)
	}
	if len(advancedProvider.pauseTriggerReqs) != 1 {
		t.Fatalf("advanced pause trigger requests = %d, want 1", len(advancedProvider.pauseTriggerReqs))
	}

	resumeReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workflow/event-triggers/"+created.ID+"/resume", nil)
	resumeReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resumeResp, err := http.DefaultClient.Do(resumeReq)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	defer func() { _ = resumeResp.Body.Close() }()
	if resumeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resumeResp.StatusCode)
	}
	if len(advancedProvider.resumeTriggerReqs) != 1 {
		t.Fatalf("advanced resume trigger requests = %d, want 1", len(advancedProvider.resumeTriggerReqs))
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/event-triggers/"+created.ID, nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", deleteResp.StatusCode)
	}
	if _, ok := advancedProvider.triggers[created.ID]; ok {
		t.Fatal("expected event trigger to be deleted from current global provider")
	}
}

func TestWorkflowEventTriggerAgentThenAppStepsCreateAndList(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada-event-agent@example.test")
	provider := newMemoryWorkflowProvider()
	agentProvider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Agent = &stubAgentControl{defaultProviderName: "managed", provider: agentProvider}
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Providers: cfg.Providers,
			Agent:     cfg.Agent,
			Invoker:   cfg.Invoker,
			RunGrants: newServerTestAgentRunGrants(t),
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createReq, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/event-triggers/",
		bytes.NewBufferString(`{"provider":"basic","match":{"type":"slack.message.created","source":"slack","subject":"thread"},"target":{"steps":[{"id":"agent","agent":{"provider":"managed","model":"deep","prompt":"Reply to the Slack thread","output":{"text":{}}}},{"id":"reply","app":{"name":"roadmap","operation":"sync","input":{"object":{"format":{"literal":"final"},"text":{"stepOutput":{"stepId":"agent","path":"agent.output.text.text"}},"thread_ts":{"signalPayload":"extensions.slack_thread_ts"}}}}}]}}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected 201, got %d: %s", createResp.StatusCode, body)
	}
	var created workflowEventTriggerResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdStep := requireWorkflowAgentStep(t, created.Target)
	if createdStep.Agent.ProviderName != "managed" || createdStep.Agent.Model != "deep" {
		t.Fatalf("created agent step = %#v", createdStep.Agent)
	}
	createdApp := requireWorkflowAppStep(t, created.Target)
	if createdApp.Name != "roadmap" || createdApp.Operation != "sync" {
		t.Fatalf("created app step = %#v", createdApp)
	}
	if len(provider.upsertTriggerReqs) != 1 {
		t.Fatalf("upsert trigger requests = %d, want 1", len(provider.upsertTriggerReqs))
	}
	storedTarget := workflowwire.TargetFromProto(provider.upsertTriggerReqs[0].GetTarget())
	requireCoreWorkflowAgentStep(t, storedTarget)
	storedApp := requireCoreWorkflowAppStep(t, storedTarget)
	if storedApp.Name != "roadmap" || storedApp.Operation != "sync" {
		t.Fatalf("stored app step = %#v", storedApp)
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/event-triggers/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowEventTriggerResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed triggers = %#v", listed)
	}
	listedApp := requireWorkflowAppStep(t, listed[0].Target)
	if listedApp.Operation != "sync" {
		t.Fatalf("listed app step = %#v", listedApp)
	}
}

func TestGlobalWorkflowRejectsInvalidJSONBodies(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: ada.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
					{ID: "export", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	cases := []struct {
		name string
		path string
		body string
		want string
	}{{
		name: "trailing JSON",
		path: "/api/v1/workflow/schedules/",
		body: `{"cron":"*/5 * * * *","timezone":"UTC","target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync"}}]} {"timeZone":"UTC"}`,
		want: `invalid JSON body`,
	}}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request: %v", tc.name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatalf("%s close response: %v", tc.name, closeErr)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", tc.name, resp.StatusCode, body)
		}
		if !strings.Contains(string(body), tc.want) {
			t.Fatalf("%s: response body = %s, want %q", tc.name, body, tc.want)
		}
	}
	if len(provider.upsertReqs) != 0 || len(provider.upsertTriggerReqs) != 0 {
		t.Fatalf("invalid JSON should not upsert schedules=%d triggers=%d", len(provider.upsertReqs), len(provider.upsertTriggerReqs))
	}
}

func TestGlobalWorkflowEventTriggerListAndMutationsAreOwnerScopedAcrossProviders(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	grace := seedUser(t, services, "grace@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)

	basicProvider.triggers["trg-ada-basic"] = &coreworkflow.EventTrigger{
		ID:        "trg-ada-basic",
		Match:     coreworkflow.EventMatch{Type: "roadmap.item.updated"},
		Target:    workflowAppStepTarget("roadmap", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(ada.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	advancedProvider.triggers["trg-ada-advanced"] = &coreworkflow.EventTrigger{
		ID:    "trg-ada-advanced",
		Match: coreworkflow.EventMatch{Type: "analytics.item.synced"},
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID: "app",
			App: &coreworkflow.AppCall{
				Name:           "analytics",
				Operation:      "sync",
				CredentialMode: core.ConnectionModeNone,
			},
		}}},
		CreatedBySubjectID: principal.UserSubjectID(ada.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	advancedProvider.triggers["trg-grace-advanced"] = &coreworkflow.EventTrigger{
		ID:        "trg-grace-advanced",
		Match:     coreworkflow.EventMatch{Type: "analytics.item.failed"},
		Target:    workflowAppStepTarget("analytics", "sync"),
		CreatedBySubjectID: principal.UserSubjectID(grace.ID),
		CreatedAt: &now,
		UpdatedAt: &now,
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				switch token {
				case "ada-session":
					return &core.UserIdentity{Email: ada.Email, DisplayName: "Ada"}, nil
				case "grace-session":
					return &core.UserIdentity{Email: grace.Email, DisplayName: "Grace"}, nil
				default:
					return nil, core.ErrNotFound
				}
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{
				N:        "roadmap",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeSubject,
				CatalogVal: &catalog.Catalog{
					Name: "analytics",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
		)
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/event-triggers/", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}
	var listed []workflowEventTriggerResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed triggers = %#v", listed)
	}
	listedIDs := []string{listed[0].ID, listed[1].ID}
	slices.Sort(listedIDs)
	if !slices.Equal(listedIDs, []string{"trg-ada-advanced", "trg-ada-basic"}) {
		t.Fatalf("listed triggers = %#v", listed)
	}
	var listedAdvanced *workflowEventTriggerResponse
	for i := range listed {
		if listed[i].ID == "trg-ada-advanced" {
			listedAdvanced = &listed[i]
			break
		}
	}
	if listedAdvanced == nil {
		t.Fatalf("listed triggers missing advanced trigger: %#v", listed)
	}
	if got := requireWorkflowAppStep(t, listedAdvanced.Target).CredentialMode; got != string(core.ConnectionModeNone) {
		t.Fatalf("listed advanced trigger credential mode = %q, want %q", got, core.ConnectionModeNone)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/event-triggers/trg-grace-advanced", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", getResp.StatusCode)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workflow/event-triggers/trg-grace-advanced", nil)
	deleteReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer func() { _ = deleteResp.Body.Close() }()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", deleteResp.StatusCode)
	}
	if _, ok := advancedProvider.triggers["trg-grace-advanced"]; !ok {
		t.Fatal("expected grace trigger to remain after unauthorized global delete")
	}
}

func TestWorkflowEventTriggerCreateRejectsPublicTargetCredentialMode(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/event-triggers/",
		bytes.NewBufferString(`{"match":{"type":"roadmap.item.updated"},"target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync","credentialMode":"none"}}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close response: %v", closeErr)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "workflow target.steps[0].app.credentialMode is not supported") {
		t.Fatalf("response body = %s, want credential mode error", body)
	}
	if len(provider.upsertTriggerReqs) != 0 {
		t.Fatalf("upsert trigger requests = %#v, want none", provider.upsertTriggerReqs)
	}
}

func TestWorkflowEventTriggerCreateRequiresMatchType(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/event-triggers/",
		bytes.NewBufferString(`{"match":{"source":"roadmap"},"target":{"steps":[{"id":"app","app":{"name":"roadmap","operation":"sync"}}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
	if len(provider.upsertTriggerReqs) != 0 {
		t.Fatalf("upsert trigger requests = %d, want 0", len(provider.upsertTriggerReqs))
	}
}

func TestWorkflowEventPublishRequiresCallerAppSource(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		}, &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name:       "slack",
				Operations: []catalog.CatalogOperation{{ID: "chat.postMessage", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			providers: map[string]coreworkflow.Provider{
				"basic":    basicProvider,
				"advanced": advancedProvider,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/events",
		bytes.NewBufferString(`{"type":"roadmap.item.updated","source":"roadmap","subject":"item","data":{"id":"item-1"},"extensions":{"traceId":"trace-1"}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("publish request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "workflow event source is required") {
		t.Fatalf("response body = %s, want workflow event source error", body)
	}
	if len(basicProvider.publishEventReqs) != 0 || len(advancedProvider.publishEventReqs) != 0 {
		t.Fatalf("publish requests basic=%d advanced=%d, want none", len(basicProvider.publishEventReqs), len(advancedProvider.publishEventReqs))
	}
}

func TestWorkflowEventPublishRequiresType(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: user.Email, DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeSubject,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/workflow/events",
		bytes.NewBufferString(`{"source":"roadmap","data":{"id":"item-1"}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("publish request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
	if len(provider.publishEventReqs) != 0 {
		t.Fatalf("publish event requests = %d, want 0", len(provider.publishEventReqs))
	}
}
