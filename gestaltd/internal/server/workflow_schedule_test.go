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
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type stubWorkflowControl struct {
	defaultProviderName string
	provider            coreworkflow.Provider
	providers           map[string]coreworkflow.Provider
	selectionErr        error
	providerErr         error
}

func (s *stubWorkflowControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
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
	provider, err := s.ResolveProvider(name)
	if err != nil {
		return "", nil, err
	}
	return name, provider, nil
}

func (s *stubWorkflowControl) ResolveProvider(name string) (coreworkflow.Provider, error) {
	if s.providerErr != nil {
		return nil, s.providerErr
	}
	if s.providers != nil {
		provider, ok := s.providers[name]
		if !ok {
			return nil, errors.New("provider not found")
		}
		return provider, nil
	}
	return s.provider, nil
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
	schedules            map[string]*coreworkflow.Schedule
	triggers             map[string]*coreworkflow.EventTrigger
	runs                 map[string]*coreworkflow.Run
	executionRefs        map[string]*coreworkflow.ExecutionReference
	publishEventReqs     []coreworkflow.PublishEventRequest
	upsertReqs           []coreworkflow.UpsertScheduleRequest
	upsertTriggerReqs    []coreworkflow.UpsertEventTriggerRequest
	deleteReqs           []coreworkflow.DeleteScheduleRequest
	deleteTriggerReqs    []coreworkflow.DeleteEventTriggerRequest
	pauseReqs            []coreworkflow.PauseScheduleRequest
	pauseTriggerReqs     []coreworkflow.PauseEventTriggerRequest
	resumeReqs           []coreworkflow.ResumeScheduleRequest
	resumeTriggerReqs    []coreworkflow.ResumeEventTriggerRequest
	cancelReqs           []coreworkflow.CancelRunRequest
	nextUpsertErr        error
	nextUpsertTriggerErr error
	getErr               error
	getTriggerErr        error
	listErr              error
	listTriggersErr      error
	getRunErr            error
	listRunsErr          error
	cancelRunErr         error
	publishEventErr      error
}

func newMemoryWorkflowProvider() *memoryWorkflowProvider {
	return &memoryWorkflowProvider{
		schedules:     map[string]*coreworkflow.Schedule{},
		triggers:      map[string]*coreworkflow.EventTrigger{},
		runs:          map[string]*coreworkflow.Run{},
		executionRefs: map[string]*coreworkflow.ExecutionReference{},
	}
}

func (p *memoryWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetRun(_ context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	if p.getRunErr != nil {
		return nil, p.getRunErr
	}
	run, ok := p.runs[req.RunID]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	return cloneWorkflowRun(run), nil
}

func (p *memoryWorkflowProvider) ListRuns(_ context.Context, _ coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	if p.listRunsErr != nil {
		return nil, p.listRunsErr
	}
	out := make([]*coreworkflow.Run, 0, len(p.runs))
	for _, run := range p.runs {
		if run != nil {
			out = append(out, cloneWorkflowRun(run))
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) CancelRun(_ context.Context, req coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	if p.cancelRunErr != nil {
		return nil, p.cancelRunErr
	}
	run, ok := p.runs[req.RunID]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	run.Status = coreworkflow.RunStatusCanceled
	run.CompletedAt = &now
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		run.StatusMessage = reason
	}
	p.cancelReqs = append(p.cancelReqs, req)
	return cloneWorkflowRun(run), nil
}

func (p *memoryWorkflowProvider) UpsertSchedule(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertReqs = append(p.upsertReqs, req)
	if p.nextUpsertErr != nil {
		err := p.nextUpsertErr
		p.nextUpsertErr = nil
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	existing := p.schedules[req.ScheduleID]
	createdAt := &now
	if existing != nil && existing.CreatedAt != nil {
		createdAt = existing.CreatedAt
	}
	schedule := &coreworkflow.Schedule{
		ID:           req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.RequestedBy,
		CreatedAt:    createdAt,
		UpdatedAt:    &now,
	}
	p.schedules[req.ScheduleID] = cloneWorkflowSchedule(schedule)
	return cloneWorkflowSchedule(schedule), nil
}

func (p *memoryWorkflowProvider) GetSchedule(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.getErr != nil {
		return nil, p.getErr
	}
	schedule, ok := p.schedules[req.ScheduleID]
	if !ok || schedule == nil {
		return nil, core.ErrNotFound
	}
	return cloneWorkflowSchedule(schedule), nil
}

func (p *memoryWorkflowProvider) ListSchedules(_ context.Context, req coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	out := make([]*coreworkflow.Schedule, 0, len(p.schedules))
	for _, schedule := range p.schedules {
		if schedule != nil {
			out = append(out, cloneWorkflowSchedule(schedule))
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) DeleteSchedule(_ context.Context, req coreworkflow.DeleteScheduleRequest) error {
	schedule, ok := p.schedules[req.ScheduleID]
	if !ok || schedule == nil {
		return core.ErrNotFound
	}
	delete(p.schedules, req.ScheduleID)
	p.deleteReqs = append(p.deleteReqs, req)
	return nil
}

func (p *memoryWorkflowProvider) PauseSchedule(_ context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	schedule, ok := p.schedules[req.ScheduleID]
	if !ok || schedule == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	schedule.Paused = true
	schedule.UpdatedAt = &now
	p.pauseReqs = append(p.pauseReqs, req)
	return cloneWorkflowSchedule(schedule), nil
}

func (p *memoryWorkflowProvider) ResumeSchedule(_ context.Context, req coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	schedule, ok := p.schedules[req.ScheduleID]
	if !ok || schedule == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	schedule.Paused = false
	schedule.UpdatedAt = &now
	p.resumeReqs = append(p.resumeReqs, req)
	return cloneWorkflowSchedule(schedule), nil
}

func (p *memoryWorkflowProvider) UpsertEventTrigger(_ context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	p.upsertTriggerReqs = append(p.upsertTriggerReqs, req)
	if p.nextUpsertTriggerErr != nil {
		err := p.nextUpsertTriggerErr
		p.nextUpsertTriggerErr = nil
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	existing := p.triggers[req.TriggerID]
	createdAt := &now
	if existing != nil && existing.CreatedAt != nil {
		createdAt = existing.CreatedAt
	}
	trigger := &coreworkflow.EventTrigger{
		ID:           req.TriggerID,
		Match:        req.Match,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.RequestedBy,
		CreatedAt:    createdAt,
		UpdatedAt:    &now,
	}
	p.triggers[req.TriggerID] = cloneWorkflowEventTrigger(trigger)
	return cloneWorkflowEventTrigger(trigger), nil
}

func (p *memoryWorkflowProvider) GetEventTrigger(_ context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	if p.getTriggerErr != nil {
		return nil, p.getTriggerErr
	}
	trigger, ok := p.triggers[req.TriggerID]
	if !ok || trigger == nil {
		return nil, core.ErrNotFound
	}
	return cloneWorkflowEventTrigger(trigger), nil
}

func (p *memoryWorkflowProvider) ListEventTriggers(_ context.Context, _ coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	if p.listTriggersErr != nil {
		return nil, p.listTriggersErr
	}
	out := make([]*coreworkflow.EventTrigger, 0, len(p.triggers))
	for _, trigger := range p.triggers {
		if trigger != nil {
			out = append(out, cloneWorkflowEventTrigger(trigger))
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) DeleteEventTrigger(_ context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	trigger, ok := p.triggers[req.TriggerID]
	if !ok || trigger == nil {
		return core.ErrNotFound
	}
	delete(p.triggers, req.TriggerID)
	p.deleteTriggerReqs = append(p.deleteTriggerReqs, req)
	return nil
}

func (p *memoryWorkflowProvider) PauseEventTrigger(_ context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	trigger, ok := p.triggers[req.TriggerID]
	if !ok || trigger == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	trigger.Paused = true
	trigger.UpdatedAt = &now
	p.pauseTriggerReqs = append(p.pauseTriggerReqs, req)
	return cloneWorkflowEventTrigger(trigger), nil
}

func (p *memoryWorkflowProvider) ResumeEventTrigger(_ context.Context, req coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	trigger, ok := p.triggers[req.TriggerID]
	if !ok || trigger == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	trigger.Paused = false
	trigger.UpdatedAt = &now
	p.resumeTriggerReqs = append(p.resumeTriggerReqs, req)
	return cloneWorkflowEventTrigger(trigger), nil
}

func (p *memoryWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) error {
	if p.publishEventErr != nil {
		return p.publishEventErr
	}
	req.Event = cloneWorkflowEvent(req.Event)
	p.publishEventReqs = append(p.publishEventReqs, req)
	return nil
}

func (p *memoryWorkflowProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if p.executionRefs == nil {
		p.executionRefs = map[string]*coreworkflow.ExecutionReference{}
	}
	stored := cloneWorkflowExecutionReference(ref)
	p.executionRefs[stored.ID] = stored
	return cloneWorkflowExecutionReference(stored), nil
}

func (p *memoryWorkflowProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	ref := p.executionRefs[strings.TrimSpace(id)]
	if ref == nil {
		return nil, core.ErrNotFound
	}
	return cloneWorkflowExecutionReference(ref), nil
}

func (p *memoryWorkflowProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	subjectID = strings.TrimSpace(subjectID)
	out := make([]*coreworkflow.ExecutionReference, 0, len(p.executionRefs))
	for _, ref := range p.executionRefs {
		if ref == nil {
			continue
		}
		if subjectID != "" && strings.TrimSpace(ref.SubjectID) != subjectID {
			continue
		}
		out = append(out, cloneWorkflowExecutionReference(ref))
	}
	return out, nil
}

func (p *memoryWorkflowProvider) Ping(context.Context) error { return nil }
func (p *memoryWorkflowProvider) Close() error               { return nil }

func cloneWorkflowExecutionReference(ref *coreworkflow.ExecutionReference) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	cloned := *ref
	cloned.Target.Input = cloneMap(ref.Target.Input)
	cloned.Permissions = append([]core.AccessPermission(nil), ref.Permissions...)
	for i := range cloned.Permissions {
		cloned.Permissions[i].Operations = append([]string(nil), cloned.Permissions[i].Operations...)
	}
	if ref.CreatedAt != nil {
		value := *ref.CreatedAt
		cloned.CreatedAt = &value
	}
	if ref.RevokedAt != nil {
		value := *ref.RevokedAt
		cloned.RevokedAt = &value
	}
	return &cloned
}

func cloneWorkflowSchedule(schedule *coreworkflow.Schedule) *coreworkflow.Schedule {
	if schedule == nil {
		return nil
	}
	cloned := *schedule
	cloned.Target.Input = cloneMap(schedule.Target.Input)
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
	cloned.Target.Input = cloneMap(run.Target.Input)
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
	cloned.Target.Input = cloneMap(trigger.Target.Input)
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

func cloneWorkflowEvent(event coreworkflow.Event) coreworkflow.Event {
	cloned := event
	cloned.Data = cloneMap(event.Data)
	cloned.Extensions = cloneMap(event.Extensions)
	if event.Time != nil {
		value := *event.Time
		cloned.Time = &value
	}
	return cloned
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

type workflowScheduleResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
	Target   struct {
		Plugin     string         `json:"plugin"`
		Operation  string         `json:"operation"`
		Connection string         `json:"connection"`
		Instance   string         `json:"instance"`
		Input      map[string]any `json:"input"`
		Agent      *struct {
			ProviderName   string `json:"provider"`
			Model          string `json:"model"`
			Prompt         string `json:"prompt"`
			TimeoutSeconds int    `json:"timeoutSeconds"`
			ToolRefs       []struct {
				PluginName string `json:"pluginName"`
				Operation  string `json:"operation"`
			} `json:"toolRefs"`
		} `json:"agent"`
	} `json:"target"`
	Paused bool `json:"paused"`
}

type workflowEventTriggerResponse struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Match    struct {
		Type    string `json:"type"`
		Source  string `json:"source"`
		Subject string `json:"subject"`
	} `json:"match"`
	Target struct {
		Plugin     string         `json:"plugin"`
		Operation  string         `json:"operation"`
		Connection string         `json:"connection"`
		Instance   string         `json:"instance"`
		Input      map[string]any `json:"input"`
		Agent      *struct {
			ProviderName string `json:"provider"`
			Model        string `json:"model"`
			Prompt       string `json:"prompt"`
			ToolRefs     []struct {
				PluginName string `json:"pluginName"`
				Operation  string `json:"operation"`
			} `json:"toolRefs"`
		} `json:"agent"`
	} `json:"target"`
	Paused bool `json:"paused"`
}

func TestWorkflowScheduleCRUD(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
			ConnMode: core.ConnectionModeUser,
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

	createBody := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"incremental"}}}`)
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
	if created.Provider != "basic" || created.Target.Operation != "sync" || created.Target.Connection != "analytics" || created.Target.Instance != "tenant-a" {
		t.Fatalf("created schedule = %#v", created)
	}
	if created.Target.Plugin != "roadmap" {
		t.Fatalf("created target plugin = %q, want roadmap", created.Target.Plugin)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	createUpsert := provider.upsertReqs[len(provider.upsertReqs)-1]
	if createUpsert.Target.PluginName != "roadmap" || createUpsert.Target.Operation != "sync" {
		t.Fatalf("upsert target = %#v", createUpsert.Target)
	}
	if createUpsert.ExecutionRef == "" {
		t.Fatal("expected execution ref to be stored on create")
	}
	ref, err := provider.GetExecutionReference(context.Background(), createUpsert.ExecutionRef)
	if err != nil {
		t.Fatalf("Get execution ref: %v", err)
	}
	if ref.SubjectID != principal.UserSubjectID(user.ID) {
		t.Fatalf("execution ref = %#v", ref)
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
	if listed[0].Target.Plugin != "roadmap" {
		t.Fatalf("listed target plugin = %q, want roadmap", listed[0].Target.Plugin)
	}

	updateBody := bytes.NewBufferString(`{"cron":"0 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"full"}},"paused":true}`)
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
	updateUpsert := provider.upsertReqs[len(provider.upsertReqs)-1]
	if updateUpsert.ExecutionRef == "" || updateUpsert.ExecutionRef == createUpsert.ExecutionRef {
		t.Fatalf("update execution ref = %q, want rotated from %q", updateUpsert.ExecutionRef, createUpsert.ExecutionRef)
	}
	oldRef, err := provider.GetExecutionReference(context.Background(), createUpsert.ExecutionRef)
	if err != nil {
		t.Fatalf("Get rotated old ref: %v", err)
	}
	if oldRef.RevokedAt == nil || oldRef.RevokedAt.IsZero() {
		t.Fatalf("expected rotated execution ref to be revoked, got %#v", oldRef)
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
	ref, err = provider.GetExecutionReference(context.Background(), updateUpsert.ExecutionRef)
	if err != nil {
		t.Fatalf("Get revoked ref: %v", err)
	}
	if ref.RevokedAt == nil || ref.RevokedAt.IsZero() {
		t.Fatalf("expected revoked execution ref, got %#v", ref)
	}
}

func TestWorkflowScheduleAgentTargetCreateAndList(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
		})
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	testutil.CloseOnCleanup(t, ts)

	createBody := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"agent":{"provider":"managed","model":"deep","prompt":"Send the status summary","timeoutSeconds":90,"toolRefs":[{"pluginName":"roadmap","operation":"sync"}]}}}`)
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
	if created.Target.Agent == nil || created.Target.Agent.ProviderName != "managed" || created.Target.Agent.Model != "deep" {
		t.Fatalf("created agent target = %#v", created.Target.Agent)
	}
	if len(created.Target.Agent.ToolRefs) != 1 || created.Target.Agent.ToolRefs[0].PluginName != "roadmap" || created.Target.Agent.ToolRefs[0].Operation != "sync" {
		t.Fatalf("created agent tools = %#v", created.Target.Agent.ToolRefs)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	storedTarget := provider.upsertReqs[0].Target
	if storedTarget.Agent == nil || storedTarget.Agent.ToolSource != coreagent.ToolSourceModeExplicit {
		t.Fatalf("stored target = %#v", storedTarget)
	}
	if provider.upsertReqs[0].ExecutionRef == "" {
		t.Fatal("expected execution ref")
	}
	ref, err := provider.GetExecutionReference(context.Background(), provider.upsertReqs[0].ExecutionRef)
	if err != nil {
		t.Fatalf("Get execution ref: %v", err)
	}
	if ref.Target.Agent == nil || ref.TargetFingerprint == "" {
		t.Fatalf("execution ref = %#v", ref)
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
	if len(listed) != 1 || listed[0].Target.Agent == nil || listed[0].Target.Agent.Prompt != "Send the status summary" {
		t.Fatalf("listed schedules = %#v", listed)
	}
}

func TestWorkflowScheduleListAndMutationsAreOwnerScoped(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	grace := seedUser(t, services, "grace@example.test")
	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	provider.schedules["sched-ada"] = &coreworkflow.Schedule{
		ID:           "sched-ada",
		Cron:         "*/5 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-ada:ref-ada",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	provider.schedules["sched-grace"] = &coreworkflow.Schedule{
		ID:           "sched-grace",
		Cron:         "0 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-grace:ref-grace",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	provider.schedules["sched-analytics"] = &coreworkflow.Schedule{
		ID:           "sched-analytics",
		Cron:         "15 * * * *",
		Target:       coreworkflow.Target{PluginName: "analytics", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-analytics:ref-analytics",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	if _, err := provider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-ada:ref-ada",
		ProviderName: "basic",
		Target:       provider.schedules["sched-ada"].Target,
		SubjectID:    principal.UserSubjectID(ada.ID),
	}); err != nil {
		t.Fatalf("Put ada ref: %v", err)
	}
	if _, err := provider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-grace:ref-grace",
		ProviderName: "basic",
		Target:       provider.schedules["sched-grace"].Target,
		SubjectID:    principal.UserSubjectID(grace.ID),
	}); err != nil {
		t.Fatalf("Put grace ref: %v", err)
	}
	if _, err := provider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-analytics:ref-analytics",
		ProviderName: "basic",
		Target:       provider.schedules["sched-analytics"].Target,
		SubjectID:    principal.UserSubjectID(ada.ID),
	}); err != nil {
		t.Fatalf("Put analytics ref: %v", err)
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
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name: "roadmap",
				Operations: []catalog.CatalogOperation{
					{ID: "sync", Method: http.MethodPost},
				},
			},
		}, &coretesting.StubIntegration{
			N:        "analytics",
			ConnMode: core.ConnectionModeUser,
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

	services := coretesting.NewStubServices(t)
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

	body := bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"export"}}`)
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
	if len(provider.upsertReqs) != 1 || provider.upsertReqs[0].Target.Operation != "export" {
		t.Fatalf("upsert requests = %#v", provider.upsertReqs)
	}
}

func TestWorkflowScheduleAPITokenScopeFiltersOperations(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
		Permissions:         []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}},
	}); err != nil {
		t.Fatalf("StoreAPIToken: %v", err)
	}

	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	provider.schedules["sched-sync"] = &coreworkflow.Schedule{
		ID:           "sched-sync",
		Cron:         "*/5 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-sync:ref-sync",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	provider.schedules["sched-export"] = &coreworkflow.Schedule{
		ID:           "sched-export",
		Cron:         "0 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "export"},
		ExecutionRef: "workflow_schedule:sched-export:ref-export",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	for _, ref := range []*coreworkflow.ExecutionReference{
		{
			ID:           "workflow_schedule:sched-sync:ref-sync",
			ProviderName: "basic",
			Target:       provider.schedules["sched-sync"].Target,
			SubjectID:    principal.UserSubjectID(user.ID),
		},
		{
			ID:           "workflow_schedule:sched-export:ref-export",
			ProviderName: "basic",
			Target:       provider.schedules["sched-export"].Target,
			SubjectID:    principal.UserSubjectID(user.ID),
		},
	} {
		if _, err := provider.PutExecutionReference(context.Background(), ref); err != nil {
			t.Fatalf("Put execution ref %q: %v", ref.ID, err)
		}
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
			ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"export","instance":"tenant-a"}}`),
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

func TestWorkflowScheduleUpdateFailureKeepsExistingExecutionRef(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	oldTarget := coreworkflow.Target{
		PluginName: "roadmap",
		Operation:  "sync",
		Connection: "analytics",
		Instance:   "tenant-a",
	}
	now := time.Now().UTC().Truncate(time.Second)
	provider.schedules["sched-ada"] = &coreworkflow.Schedule{
		ID:           "sched-ada",
		Cron:         "*/5 * * * *",
		Timezone:     "UTC",
		Target:       oldTarget,
		ExecutionRef: "workflow_schedule:sched-ada:ref-old",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	if _, err := provider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-ada:ref-old",
		ProviderName: "basic",
		Target:       oldTarget,
		SubjectID:    principal.UserSubjectID(user.ID),
	}); err != nil {
		t.Fatalf("Put old ref: %v", err)
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
			ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"cron":"*/10 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-b"}}`),
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
	if provider.schedules["sched-ada"].ExecutionRef != "workflow_schedule:sched-ada:ref-old" {
		t.Fatalf("schedule execution ref = %q, want workflow_schedule:sched-ada:ref-old", provider.schedules["sched-ada"].ExecutionRef)
	}
	if provider.schedules["sched-ada"].Target.Instance != "tenant-a" {
		t.Fatalf("schedule target after failed update = %#v", provider.schedules["sched-ada"].Target)
	}
	oldRef, err := provider.GetExecutionReference(context.Background(), "workflow_schedule:sched-ada:ref-old")
	if err != nil {
		t.Fatalf("Get old ref: %v", err)
	}
	if oldRef.RevokedAt != nil && !oldRef.RevokedAt.IsZero() {
		t.Fatalf("expected old ref to remain active, got %#v", oldRef)
	}
	newRef, err := provider.GetExecutionReference(context.Background(), provider.upsertReqs[0].ExecutionRef)
	if err != nil {
		t.Fatalf("Get new ref: %v", err)
	}
	if newRef.RevokedAt == nil || newRef.RevokedAt.IsZero() {
		t.Fatalf("expected failed-update ref to be revoked, got %#v", newRef)
	}
}

func TestWorkflowScheduleCreateFailureHidesInternalError(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
			ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a"}}`),
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

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	seedToken(t, services, &core.IntegrationToken{
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
			ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"cron":"*/5 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"sync","connection":"default"}}`),
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
	if created.Target.Instance != "tenant-a" {
		t.Fatalf("created schedule target instance = %q, want tenant-a", created.Target.Instance)
	}
	if len(provider.upsertReqs) != 1 {
		t.Fatalf("upsert requests = %d, want 1", len(provider.upsertReqs))
	}
	if provider.upsertReqs[0].Target.Instance != "tenant-a" {
		t.Fatalf("stored target = %#v, want resolved instance tenant-a", provider.upsertReqs[0].Target)
	}
}

func TestGlobalWorkflowScheduleLookupIgnoresUnrelatedProviderFailures(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	advancedProvider.getErr = errors.New("advanced down")
	advancedProvider.listErr = errors.New("advanced down")

	now := time.Now().UTC().Truncate(time.Second)
	basicProvider.schedules["sched-ada-basic"] = &coreworkflow.Schedule{
		ID:           "sched-ada-basic",
		Cron:         "*/5 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-ada-basic:ref-basic",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	if _, err := basicProvider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-ada-basic:ref-basic",
		ProviderName: "basic",
		Target:       basicProvider.schedules["sched-ada-basic"].Target,
		SubjectID:    principal.UserSubjectID(user.ID),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
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
				ConnMode: core.ConnectionModeUser,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeUser,
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

func TestGlobalWorkflowScheduleRejectsDuplicateActiveExecutionRefs(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	provider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)
	schedule := &coreworkflow.Schedule{
		ID:           "sched-ada",
		Cron:         "*/5 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-ada:active-ref-1",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	provider.schedules[schedule.ID] = schedule
	for _, refID := range []string{"workflow_schedule:sched-ada:active-ref-1", "workflow_schedule:sched-ada:active-ref-2"} {
		if _, err := provider.PutExecutionReference(context.Background(), &coreworkflow.ExecutionReference{
			ID:           refID,
			ProviderName: "basic",
			Target:       schedule.Target,
			SubjectID:    principal.UserSubjectID(user.ID),
		}); err != nil {
			t.Fatalf("Put execution ref %q: %v", refID, err)
		}
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
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeUser,
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

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workflow/schedules/sched-ada", nil)
	getReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("expected 500, got %d: %s", getResp.StatusCode, body)
	}
}

func TestGlobalWorkflowScheduleCRUDAcrossProviders(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user := seedUser(t, services, "ada@example.test")
	seedToken(t, services, &core.IntegrationToken{
		ID:          "roadmap-default-token",
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "roadmap",
		Connection:  "default",
		AccessToken: "roadmap-token",
	})
	seedToken(t, services, &core.IntegrationToken{
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
				ConnMode: core.ConnectionModeUser,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"provider":"basic","cron":"*/5 * * * *","timezone":"UTC","target":{"plugin":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"incremental"}}}`),
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
	if created.Provider != "basic" || created.Target.Plugin != "roadmap" || created.Target.Operation != "sync" {
		t.Fatalf("created schedule = %#v", created)
	}
	if len(basicProvider.upsertReqs) != 1 {
		t.Fatalf("basic upsert requests = %d, want 1", len(basicProvider.upsertReqs))
	}
	if basicProvider.upsertReqs[0].Target.PluginName != "roadmap" {
		t.Fatalf("basic create target = %#v", basicProvider.upsertReqs[0].Target)
	}
	initialExecutionRef := basicProvider.upsertReqs[0].ExecutionRef

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
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Provider != "basic" || listed[0].Target.Plugin != "roadmap" {
		t.Fatalf("listed schedules = %#v", listed)
	}

	updateReq, _ := http.NewRequest(
		http.MethodPut,
		ts.URL+"/api/v1/workflow/schedules/"+created.ID,
		bytes.NewBufferString(`{"provider":"advanced","cron":"0 * * * *","timezone":"UTC","target":{"plugin":"analytics","operation":"sync","connection":"warehouse","instance":"tenant-b","input":{"mode":"full"}},"paused":true}`),
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
	if updated.Provider != "advanced" || updated.Target.Plugin != "analytics" || !updated.Paused {
		t.Fatalf("updated schedule = %#v", updated)
	}
	if len(advancedProvider.upsertReqs) != 1 {
		t.Fatalf("advanced upsert requests = %d, want 1", len(advancedProvider.upsertReqs))
	}
	if len(basicProvider.deleteReqs) != 1 || basicProvider.deleteReqs[0].ScheduleID != created.ID {
		t.Fatalf("basic delete requests = %#v", basicProvider.deleteReqs)
	}
	if _, ok := basicProvider.schedules[created.ID]; ok {
		t.Fatal("expected global update to remove schedule from old provider")
	}
	if _, ok := advancedProvider.schedules[created.ID]; !ok {
		t.Fatal("expected global update to store schedule in new provider")
	}
	updatedExecutionRef := advancedProvider.upsertReqs[0].ExecutionRef
	if updatedExecutionRef == "" || updatedExecutionRef == initialExecutionRef {
		t.Fatalf("updated execution ref = %q, want rotated from %q", updatedExecutionRef, initialExecutionRef)
	}
	oldRef, err := basicProvider.GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get initial execution ref: %v", err)
	}
	if oldRef.RevokedAt == nil || oldRef.RevokedAt.IsZero() {
		t.Fatalf("expected initial execution ref to be revoked, got %#v", oldRef)
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
	finalRef, err := advancedProvider.GetExecutionReference(context.Background(), updatedExecutionRef)
	if err != nil {
		t.Fatalf("Get final execution ref: %v", err)
	}
	if finalRef.RevokedAt == nil || finalRef.RevokedAt.IsZero() {
		t.Fatalf("expected final execution ref to be revoked, got %#v", finalRef)
	}
}

func TestGlobalWorkflowScheduleListAndMutationsAreOwnerScopedAcrossProviders(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	grace := seedUser(t, services, "grace@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)

	basicProvider.schedules["sched-ada-basic"] = &coreworkflow.Schedule{
		ID:           "sched-ada-basic",
		Cron:         "*/5 * * * *",
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-ada-basic:ref-basic",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	advancedProvider.schedules["sched-ada-advanced"] = &coreworkflow.Schedule{
		ID:           "sched-ada-advanced",
		Cron:         "0 * * * *",
		Target:       coreworkflow.Target{PluginName: "analytics", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-ada-advanced:ref-advanced",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	advancedProvider.schedules["sched-grace-advanced"] = &coreworkflow.Schedule{
		ID:           "sched-grace-advanced",
		Cron:         "15 * * * *",
		Target:       coreworkflow.Target{PluginName: "analytics", Operation: "sync"},
		ExecutionRef: "workflow_schedule:sched-grace-advanced:ref-grace-advanced",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	for _, ref := range []*coreworkflow.ExecutionReference{
		{
			ID:           "workflow_schedule:sched-ada-basic:ref-basic",
			ProviderName: "basic",
			Target:       basicProvider.schedules["sched-ada-basic"].Target,
			SubjectID:    principal.UserSubjectID(ada.ID),
		},
		{
			ID:           "workflow_schedule:sched-ada-advanced:ref-advanced",
			ProviderName: "advanced",
			Target:       advancedProvider.schedules["sched-ada-advanced"].Target,
			SubjectID:    principal.UserSubjectID(ada.ID),
		},
		{
			ID:           "workflow_schedule:sched-grace-advanced:ref-grace-advanced",
			ProviderName: "advanced",
			Target:       advancedProvider.schedules["sched-grace-advanced"].Target,
			SubjectID:    principal.UserSubjectID(grace.ID),
		},
	} {
		targetProvider := basicProvider
		if ref.ProviderName == "advanced" {
			targetProvider = advancedProvider
		}
		if _, err := targetProvider.PutExecutionReference(context.Background(), ref); err != nil {
			t.Fatalf("Put execution ref %q: %v", ref.ID, err)
		}
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
				ConnMode: core.ConnectionModeUser,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeUser,
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

	services := coretesting.NewStubServices(t)
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
				ConnMode: core.ConnectionModeUser,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"provider":"basic","match":{"type":"roadmap.item.updated","source":"roadmap","subject":"item"},"target":{"plugin":"roadmap","operation":"sync","connection":"analytics","instance":"tenant-a","input":{"mode":"incremental"}}}`),
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
	if created.Provider != "basic" || created.Match.Type != "roadmap.item.updated" || created.Target.Plugin != "roadmap" || created.Target.Operation != "sync" {
		t.Fatalf("created trigger = %#v", created)
	}
	if len(basicProvider.upsertTriggerReqs) != 1 {
		t.Fatalf("basic trigger upsert requests = %d, want 1", len(basicProvider.upsertTriggerReqs))
	}
	if basicProvider.upsertTriggerReqs[0].Target.PluginName != "roadmap" {
		t.Fatalf("basic create target = %#v", basicProvider.upsertTriggerReqs[0].Target)
	}
	initialExecutionRef := basicProvider.upsertTriggerReqs[0].ExecutionRef

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
		bytes.NewBufferString(`{"provider":"advanced","match":{"type":"analytics.item.synced","source":"analytics","subject":"sync"},"target":{"plugin":"analytics","operation":"sync","connection":"warehouse","instance":"tenant-b","input":{"mode":"full"}},"paused":true}`),
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
	if updated.Provider != "advanced" || updated.Match.Type != "analytics.item.synced" || updated.Target.Plugin != "analytics" || !updated.Paused {
		t.Fatalf("updated trigger = %#v", updated)
	}
	if len(advancedProvider.upsertTriggerReqs) != 1 {
		t.Fatalf("advanced trigger upsert requests = %d, want 1", len(advancedProvider.upsertTriggerReqs))
	}
	if len(basicProvider.deleteTriggerReqs) != 1 || basicProvider.deleteTriggerReqs[0].TriggerID != created.ID {
		t.Fatalf("basic delete trigger requests = %#v", basicProvider.deleteTriggerReqs)
	}
	if _, ok := basicProvider.triggers[created.ID]; ok {
		t.Fatal("expected global update to remove event trigger from old provider")
	}
	if _, ok := advancedProvider.triggers[created.ID]; !ok {
		t.Fatal("expected global update to store event trigger in new provider")
	}
	updatedExecutionRef := advancedProvider.upsertTriggerReqs[0].ExecutionRef
	if updatedExecutionRef == "" || updatedExecutionRef == initialExecutionRef {
		t.Fatalf("updated execution ref = %q, want rotated from %q", updatedExecutionRef, initialExecutionRef)
	}
	oldRef, err := basicProvider.GetExecutionReference(context.Background(), initialExecutionRef)
	if err != nil {
		t.Fatalf("Get initial execution ref: %v", err)
	}
	if oldRef.RevokedAt == nil || oldRef.RevokedAt.IsZero() {
		t.Fatalf("expected initial execution ref to be revoked, got %#v", oldRef)
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
	finalRef, err := advancedProvider.GetExecutionReference(context.Background(), updatedExecutionRef)
	if err != nil {
		t.Fatalf("Get final execution ref: %v", err)
	}
	if finalRef.RevokedAt == nil || finalRef.RevokedAt.IsZero() {
		t.Fatalf("expected final execution ref to be revoked, got %#v", finalRef)
	}
}

func TestGlobalWorkflowEventTriggerListAndMutationsAreOwnerScopedAcrossProviders(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	ada := seedUser(t, services, "ada@example.test")
	grace := seedUser(t, services, "grace@example.test")
	basicProvider := newMemoryWorkflowProvider()
	advancedProvider := newMemoryWorkflowProvider()
	now := time.Now().UTC().Truncate(time.Second)

	basicProvider.triggers["trg-ada-basic"] = &coreworkflow.EventTrigger{
		ID:           "trg-ada-basic",
		Match:        coreworkflow.EventMatch{Type: "roadmap.item.updated"},
		Target:       coreworkflow.Target{PluginName: "roadmap", Operation: "sync"},
		ExecutionRef: "workflow_event_trigger:trg-ada-basic:ref-basic",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	advancedProvider.triggers["trg-ada-advanced"] = &coreworkflow.EventTrigger{
		ID:           "trg-ada-advanced",
		Match:        coreworkflow.EventMatch{Type: "analytics.item.synced"},
		Target:       coreworkflow.Target{PluginName: "analytics", Operation: "sync"},
		ExecutionRef: "workflow_event_trigger:trg-ada-advanced:ref-advanced",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	advancedProvider.triggers["trg-grace-advanced"] = &coreworkflow.EventTrigger{
		ID:           "trg-grace-advanced",
		Match:        coreworkflow.EventMatch{Type: "analytics.item.failed"},
		Target:       coreworkflow.Target{PluginName: "analytics", Operation: "sync"},
		ExecutionRef: "workflow_event_trigger:trg-grace-advanced:ref-grace-advanced",
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	for _, ref := range []*coreworkflow.ExecutionReference{
		{
			ID:           "workflow_event_trigger:trg-ada-basic:ref-basic",
			ProviderName: "basic",
			Target:       basicProvider.triggers["trg-ada-basic"].Target,
			SubjectID:    principal.UserSubjectID(ada.ID),
		},
		{
			ID:           "workflow_event_trigger:trg-ada-advanced:ref-advanced",
			ProviderName: "advanced",
			Target:       advancedProvider.triggers["trg-ada-advanced"].Target,
			SubjectID:    principal.UserSubjectID(ada.ID),
		},
		{
			ID:           "workflow_event_trigger:trg-grace-advanced:ref-grace-advanced",
			ProviderName: "advanced",
			Target:       advancedProvider.triggers["trg-grace-advanced"].Target,
			SubjectID:    principal.UserSubjectID(grace.ID),
		},
	} {
		targetProvider := basicProvider
		if ref.ProviderName == "advanced" {
			targetProvider = advancedProvider
		}
		if _, err := targetProvider.PutExecutionReference(context.Background(), ref); err != nil {
			t.Fatalf("Put execution ref %q: %v", ref.ID, err)
		}
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
				ConnMode: core.ConnectionModeUser,
				CatalogVal: &catalog.Catalog{
					Name: "roadmap",
					Operations: []catalog.CatalogOperation{
						{ID: "sync", Method: http.MethodPost},
					},
				},
			},
			&coretesting.StubIntegration{
				N:        "analytics",
				ConnMode: core.ConnectionModeUser,
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

func TestWorkflowEventTriggerCreateRequiresMatchType(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
			ConnMode: core.ConnectionModeUser,
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
		bytes.NewBufferString(`{"match":{"source":"roadmap"},"target":{"plugin":"roadmap","operation":"sync"}}`),
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

func TestWorkflowEventPublishFansOutAcrossWorkflowProviders(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: http.MethodPost}},
			},
		}, &coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
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
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var body struct {
		Status string `json:"status"`
		Event  struct {
			ID          string         `json:"id"`
			Type        string         `json:"type"`
			Source      string         `json:"source"`
			Subject     string         `json:"subject"`
			SpecVersion string         `json:"specVersion"`
			Time        *time.Time     `json:"time"`
			Data        map[string]any `json:"data"`
			Extensions  map[string]any `json:"extensions"`
		} `json:"event"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if body.Status != "published" {
		t.Fatalf("publish response status = %q, want published", body.Status)
	}
	if body.Event.ID == "" || body.Event.SpecVersion != "1.0" || body.Event.Time == nil {
		t.Fatalf("published event = %#v", body.Event)
	}
	if body.Event.Type != "roadmap.item.updated" || body.Event.Source != "roadmap" || body.Event.Subject != "item" {
		t.Fatalf("published event = %#v", body.Event)
	}
	if got := body.Event.Data["id"]; got != "item-1" {
		t.Fatalf("published event data = %#v", body.Event.Data)
	}
	if got := body.Event.Extensions["traceId"]; got != "trace-1" {
		t.Fatalf("published event extensions = %#v", body.Event.Extensions)
	}

	for name, provider := range map[string]*memoryWorkflowProvider{
		"basic":    basicProvider,
		"advanced": advancedProvider,
	} {
		if len(provider.publishEventReqs) != 1 {
			t.Fatalf("%s publish requests = %d, want 1", name, len(provider.publishEventReqs))
		}
		if got := provider.publishEventReqs[0].PluginName; got != "" {
			t.Fatalf("%s publish plugin = %q, want empty global publish", name, got)
		}
		for _, publishReq := range provider.publishEventReqs {
			if publishReq.Event.ID != body.Event.ID || publishReq.Event.SpecVersion != "1.0" || publishReq.Event.Time == nil || !publishReq.Event.Time.Equal(*body.Event.Time) {
				t.Fatalf("%s publish event = %#v, response = %#v", name, publishReq.Event, body.Event)
			}
		}
	}
}

func TestWorkflowEventPublishRequiresType(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
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
			ConnMode: core.ConnectionModeUser,
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
