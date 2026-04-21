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
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
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

type memoryWorkflowProvider struct {
	schedules     map[string]*coreworkflow.Schedule
	upsertReqs    []coreworkflow.UpsertScheduleRequest
	deleteReqs    []coreworkflow.DeleteScheduleRequest
	pauseReqs     []coreworkflow.PauseScheduleRequest
	resumeReqs    []coreworkflow.ResumeScheduleRequest
	nextUpsertErr error
	getErr        error
	listErr       error
}

func newMemoryWorkflowProvider() *memoryWorkflowProvider {
	return &memoryWorkflowProvider{schedules: map[string]*coreworkflow.Schedule{}}
}

func (p *memoryWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}

func (p *memoryWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return nil, errors.New("not implemented")
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

func (p *memoryWorkflowProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}

func (p *memoryWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return errors.New("not implemented")
}

func (p *memoryWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}

func (p *memoryWorkflowProvider) Ping(context.Context) error { return nil }
func (p *memoryWorkflowProvider) Close() error               { return nil }

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
	ref, err := services.WorkflowExecutionRefs.Get(context.Background(), createUpsert.ExecutionRef)
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
	oldRef, err := services.WorkflowExecutionRefs.Get(context.Background(), createUpsert.ExecutionRef)
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
	ref, err = services.WorkflowExecutionRefs.Get(context.Background(), updateUpsert.ExecutionRef)
	if err != nil {
		t.Fatalf("Get revoked ref: %v", err)
	}
	if ref.RevokedAt == nil || ref.RevokedAt.IsZero() {
		t.Fatalf("expected revoked execution ref, got %#v", ref)
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
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-ada:ref-ada",
		ProviderName: "basic",
		Target:       provider.schedules["sched-ada"].Target,
		SubjectID:    principal.UserSubjectID(ada.ID),
	}); err != nil {
		t.Fatalf("Put ada ref: %v", err)
	}
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "workflow_schedule:sched-grace:ref-grace",
		ProviderName: "basic",
		Target:       provider.schedules["sched-grace"].Target,
		SubjectID:    principal.UserSubjectID(grace.ID),
	}); err != nil {
		t.Fatalf("Put grace ref: %v", err)
	}
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
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
		if _, err := services.WorkflowExecutionRefs.Put(context.Background(), ref); err != nil {
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
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
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
	oldRef, err := services.WorkflowExecutionRefs.Get(context.Background(), "workflow_schedule:sched-ada:ref-old")
	if err != nil {
		t.Fatalf("Get old ref: %v", err)
	}
	if oldRef.RevokedAt != nil && !oldRef.RevokedAt.IsZero() {
		t.Fatalf("expected old ref to remain active, got %#v", oldRef)
	}
	newRef, err := services.WorkflowExecutionRefs.Get(context.Background(), provider.upsertReqs[0].ExecutionRef)
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
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
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
		if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
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
	oldRef, err := services.WorkflowExecutionRefs.Get(context.Background(), initialExecutionRef)
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
	finalRef, err := services.WorkflowExecutionRefs.Get(context.Background(), updatedExecutionRef)
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
		if _, err := services.WorkflowExecutionRefs.Put(context.Background(), ref); err != nil {
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
