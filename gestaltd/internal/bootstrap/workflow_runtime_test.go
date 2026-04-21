package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type funcInvoker struct {
	invoke func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

func (f funcInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	return f.invoke(ctx, p, providerName, instance, operation, params)
}

type erroringIndexedDB struct {
	err error
}

func (d *erroringIndexedDB) ObjectStore(string) indexeddb.ObjectStore {
	return erroringObjectStore{err: d.err}
}
func (d *erroringIndexedDB) CreateObjectStore(context.Context, string, indexeddb.ObjectStoreSchema) error {
	return d.err
}
func (d *erroringIndexedDB) DeleteObjectStore(context.Context, string) error { return d.err }
func (d *erroringIndexedDB) Ping(context.Context) error                      { return d.err }
func (d *erroringIndexedDB) Close() error                                    { return d.err }

type erroringObjectStore struct {
	err error
}

func (o erroringObjectStore) Get(context.Context, string) (indexeddb.Record, error) {
	return nil, o.err
}
func (o erroringObjectStore) GetKey(context.Context, string) (string, error) { return "", o.err }
func (o erroringObjectStore) Add(context.Context, indexeddb.Record) error    { return o.err }
func (o erroringObjectStore) Put(context.Context, indexeddb.Record) error    { return o.err }
func (o erroringObjectStore) Delete(context.Context, string) error           { return o.err }
func (o erroringObjectStore) Clear(context.Context) error                    { return o.err }
func (o erroringObjectStore) GetAll(context.Context, *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	return nil, o.err
}
func (o erroringObjectStore) GetAllKeys(context.Context, *indexeddb.KeyRange) ([]string, error) {
	return nil, o.err
}
func (o erroringObjectStore) Count(context.Context, *indexeddb.KeyRange) (int64, error) {
	return 0, o.err
}
func (o erroringObjectStore) DeleteRange(context.Context, indexeddb.KeyRange) (int64, error) {
	return 0, o.err
}
func (o erroringObjectStore) Index(string) indexeddb.Index { return erroringIndex(o) }
func (o erroringObjectStore) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, o.err
}
func (o erroringObjectStore) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, o.err
}

type erroringIndex struct {
	err error
}

func (i erroringIndex) Get(context.Context, ...any) (indexeddb.Record, error) { return nil, i.err }
func (i erroringIndex) GetKey(context.Context, ...any) (string, error)        { return "", i.err }
func (i erroringIndex) GetAll(context.Context, *indexeddb.KeyRange, ...any) ([]indexeddb.Record, error) {
	return nil, i.err
}
func (i erroringIndex) GetAllKeys(context.Context, *indexeddb.KeyRange, ...any) ([]string, error) {
	return nil, i.err
}
func (i erroringIndex) Count(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, i.err
}
func (i erroringIndex) Delete(context.Context, ...any) (int64, error) { return 0, i.err }
func (i erroringIndex) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, i.err
}
func (i erroringIndex) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, i.err
}

type workflowRoundTripProvider struct {
	workflowContext map[string]any
}

func (p *workflowRoundTripProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}
func (p *workflowRoundTripProvider) Name() string        { return "workflow-roundtrip" }
func (p *workflowRoundTripProvider) DisplayName() string { return "Workflow Round Trip" }
func (p *workflowRoundTripProvider) Description() string { return "workflow round trip test provider" }
func (p *workflowRoundTripProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeNone
}
func (p *workflowRoundTripProvider) AuthTypes() []string { return nil }
func (p *workflowRoundTripProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *workflowRoundTripProvider) CredentialFields() []core.CredentialFieldDef {
	return nil
}
func (p *workflowRoundTripProvider) DiscoveryConfig() *core.DiscoveryConfig { return nil }
func (p *workflowRoundTripProvider) ConnectionForOperation(string) string   { return "" }
func (p *workflowRoundTripProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        "workflow-roundtrip",
		DisplayName: "Workflow Round Trip",
		Description: "workflow round trip test provider",
		Operations: []catalog.CatalogOperation{
			{ID: "sync", Method: http.MethodPost},
		},
	}
}
func (p *workflowRoundTripProvider) Execute(ctx context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	p.workflowContext = invocation.WorkflowContextFromContext(ctx)
	return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
}

func newWorkflowRoundTripClient(t *testing.T, server proto.IntegrationProviderServer) proto.IntegrationProviderClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterIntegrationProviderServer(srv, server)
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
	return proto.NewIntegrationProviderClient(conn)
}

func TestWorkflowRuntimeInvokeMergesConfiguredAndPerRunInput(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 30, 0, 0, time.UTC)
	roundTripProvider := &workflowRoundTripProvider{}
	roundTripClient := newWorkflowRoundTripClient(t, providerhost.NewProviderServer(roundTripProvider))
	roundTripRemote, err := providerhost.NewRemoteProvider(context.Background(), roundTripClient, providerhost.StaticProviderSpec{
		Name:           "workflow-roundtrip",
		DisplayName:    "Workflow Round Trip",
		Description:    "workflow round trip test provider",
		ConnectionMode: core.ConnectionModeNone,
		Catalog: &catalog.Catalog{
			Name:        "workflow-roundtrip",
			DisplayName: "Workflow Round Trip",
			Description: "workflow round trip test provider",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodPost},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := roundTripRemote.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})
	runtime := &workflowRuntime{}

	var gotPrincipal *principal.Principal
	var gotProvider string
	var gotInstance string
	var gotConnection string
	var gotOperation string
	var gotParams map[string]any
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance string, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotProvider = providerName
			gotInstance = instance
			gotConnection = invocation.ConnectionFromContext(ctx)
			gotOperation = operation
			gotParams = params
			ctx = principal.WithPrincipal(ctx, p)
			return roundTripRemote.Execute(ctx, operation, params, "")
		},
	})

	resp, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		RunID:        "run-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
			Input: map[string]any{
				"mode":   "full",
				"source": "scheduled",
			},
		},
		Input: map[string]any{
			"source": "event",
			"taskId": "task-456",
		},
		Metadata: map[string]any{
			"attempt": 2,
		},
		CreatedBy: coreworkflow.Actor{
			SubjectID:   principal.UserSubjectID("user-123"),
			SubjectKind: string(principal.KindUser),
			DisplayName: "Ada",
			AuthSource:  principal.SourceAPIToken.String(),
		},
		Trigger: coreworkflow.RunTrigger{
			Schedule: &coreworkflow.ScheduleTrigger{
				ScheduleID:   "sched-1",
				ScheduledFor: &scheduledFor,
			},
		},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusAccepted || resp.Body != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if gotPrincipal == nil || gotPrincipal.SubjectID != principal.WorkloadSubjectID("workflow.config") {
		t.Fatalf("principal = %#v", gotPrincipal)
	}
	if gotProvider != "roadmap" {
		t.Fatalf("provider = %q, want %q", gotProvider, "roadmap")
	}
	if gotInstance != "" {
		t.Fatalf("instance = %q, want empty instance for workload invocations", gotInstance)
	}
	if gotConnection != "" {
		t.Fatalf("connection = %q, want empty connection for workload invocations", gotConnection)
	}
	if gotOperation != "sync" {
		t.Fatalf("operation = %q, want %q", gotOperation, "sync")
	}
	if gotParams["mode"] != "full" {
		t.Fatalf("mode = %#v, want %q", gotParams["mode"], "full")
	}
	if gotParams["source"] != "event" {
		t.Fatalf("source = %#v, want %q", gotParams["source"], "event")
	}
	if gotParams["taskId"] != "task-456" {
		t.Fatalf("taskId = %#v, want %q", gotParams["taskId"], "task-456")
	}
	if roundTripProvider.workflowContext == nil {
		t.Fatal("workflow context = nil")
	}
	if roundTripProvider.workflowContext["runId"] != "run-123" || roundTripProvider.workflowContext["provider"] != "temporal" {
		t.Fatalf("workflow context ids = %#v", roundTripProvider.workflowContext)
	}
	metadata, ok := roundTripProvider.workflowContext["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("workflow metadata = %#v", roundTripProvider.workflowContext["metadata"])
	}
	if got := metadata["attempt"]; got != 2 && got != float64(2) {
		t.Fatalf("workflow metadata attempt = %#v", got)
	}
	createdBy, ok := roundTripProvider.workflowContext["createdBy"].(map[string]any)
	if !ok || createdBy["subjectId"] != principal.UserSubjectID("user-123") || createdBy["authSource"] != principal.SourceAPIToken.String() {
		t.Fatalf("workflow createdBy = %#v", roundTripProvider.workflowContext["createdBy"])
	}
	target, ok := roundTripProvider.workflowContext["target"].(map[string]any)
	if !ok || target["pluginName"] != "roadmap" || target["operation"] != "sync" {
		t.Fatalf("workflow target = %#v", roundTripProvider.workflowContext["target"])
	}
	if got := target["connection"]; got != "analytics" {
		t.Fatalf("workflow target connection = %#v, want %q", got, "analytics")
	}
	if got := target["instance"]; got != "tenant-a" {
		t.Fatalf("workflow target instance = %#v, want %q", got, "tenant-a")
	}
	trigger, ok := roundTripProvider.workflowContext["trigger"].(map[string]any)
	if !ok || trigger["kind"] != "schedule" || trigger["scheduleId"] != "sched-1" {
		t.Fatalf("workflow trigger = %#v", roundTripProvider.workflowContext["trigger"])
	}
	if got := trigger["scheduledFor"]; got != scheduledFor.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("scheduledFor = %#v, want %q", got, scheduledFor.UTC().Format(time.RFC3339Nano))
	}
}

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredHumanPrincipalAndSelectors(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-123",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		SubjectID: principal.UserSubjectID(user.ID),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{executionRefs: services.WorkflowExecutionRefs}

	var gotPrincipal *principal.Principal
	var gotProvider string
	var gotInstance string
	var gotConnection string
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			gotProvider = providerName
			gotInstance = instance
			gotConnection = invocation.ConnectionFromContext(ctx)
			if operation != "sync" {
				t.Fatalf("operation = %q, want %q", operation, "sync")
			}
			if params["taskId"] != "task-123" {
				t.Fatalf("params = %#v", params)
			}
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
		},
	})

	resp, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
			Input: map[string]any{
				"mode": "full",
			},
		},
		Input: map[string]any{
			"taskId": "task-123",
		},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusAccepted || resp.Body != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if gotPrincipal == nil || gotPrincipal.Kind != principal.KindUser || gotPrincipal.UserID != user.ID || gotPrincipal.SubjectID != principal.UserSubjectID(user.ID) {
		t.Fatalf("principal = %#v", gotPrincipal)
	}
	if gotProvider != "roadmap" {
		t.Fatalf("provider = %q, want %q", gotProvider, "roadmap")
	}
	if gotInstance != "tenant-a" {
		t.Fatalf("instance = %q, want %q", gotInstance, "tenant-a")
	}
	if gotConnection != "analytics" {
		t.Fatalf("connection = %q, want %q", gotConnection, "analytics")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefUsesStoredWorkloadPrincipal(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-workload",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
		SubjectID: principal.WorkloadSubjectID("scheduler"),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	runtime := &workflowRuntime{executionRefs: services.WorkflowExecutionRefs}

	var gotPrincipal *principal.Principal
	runtime.SetInvoker(funcInvoker{
		invoke: func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
			gotPrincipal = p
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
		},
	})

	if _, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-workload",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
	}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if gotPrincipal == nil {
		t.Fatal("principal = nil")
	}
	if gotPrincipal.Kind != principal.KindWorkload {
		t.Fatalf("principal kind = %q, want %q", gotPrincipal.Kind, principal.KindWorkload)
	}
	if gotPrincipal.SubjectID != principal.WorkloadSubjectID("scheduler") {
		t.Fatalf("subjectID = %q, want %q", gotPrincipal.SubjectID, principal.WorkloadSubjectID("scheduler"))
	}
}

func TestWorkflowRuntimeInvokeExecutionRefRechecksAuthorizationThroughBroker(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	user, err := services.Users.FindOrCreateUser(context.Background(), "ada@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if _, err := services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
		ID:           "exec-ref-denied",
		ProviderName: "temporal",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		SubjectID: principal.UserSubjectID(user.ID),
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}
	if err := services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		SubjectID:   principal.UserSubjectID(user.ID),
		Integration: "roadmap",
		Connection:  "analytics",
		Instance:    "tenant-a",
		AccessToken: "user-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	providers := registry.New()
	executed := false
	if err := providers.Providers.Register("roadmap", &coretesting.StubIntegration{
		N:        "roadmap",
		ConnMode: core.ConnectionModeUser,
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "sync", Method: http.MethodPost},
			},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			executed = true
			return &core.OperationResult{Status: http.StatusAccepted, Body: `{"ok":true}`}, nil
		},
	}); err != nil {
		t.Fatalf("Register provider: %v", err)
	}

	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"roadmap-policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: "user:other-user", Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"roadmap": {AuthorizationPolicy: "roadmap-policy"},
	}, &providers.Providers, nil)
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	runtime := &workflowRuntime{executionRefs: services.WorkflowExecutionRefs}
	runtime.SetInvoker(invocation.NewBroker(&providers.Providers, services.Users, services.Tokens, invocation.WithAuthorizer(authz)))

	_, err = runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "temporal",
		ExecutionRef: "exec-ref-denied",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
	})
	if err == nil {
		t.Fatal("expected authorization error, got nil")
	}
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("error = %v, want ErrAuthorizationDenied", err)
	}
	if executed {
		t.Fatal("expected provider execution to be skipped")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefPreservesTokenPermissionCeiling(t *testing.T) {
	t.Parallel()

	services := coretesting.NewStubServices(t)
	ctx := context.Background()

	if _, err := services.WorkflowExecutionRefs.Put(ctx, &coreworkflow.ExecutionReference{
		ID:           "exec-ref-123",
		ProviderName: "basic",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "export",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		SubjectID: principal.UserSubjectID("user-123"),
		Permissions: []core.AccessPermission{{
			Plugin:     "roadmap",
			Operations: []string{"sync"},
		}},
	}); err != nil {
		t.Fatalf("Put execution ref: %v", err)
	}

	providers := registry.New()
	executed := false
	if err := providers.Providers.Register("roadmap", &coretesting.StubIntegration{
		N: "roadmap",
		CatalogVal: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{
				{ID: "export", Method: http.MethodPost},
			},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			executed = true
			return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
		},
	}); err != nil {
		t.Fatalf("Register provider: %v", err)
	}

	broker := invocation.NewBroker(&providers.Providers, services.Users, services.Tokens)
	runtime := &workflowRuntime{
		invoker:       broker,
		executionRefs: services.WorkflowExecutionRefs,
	}

	_, err := runtime.Invoke(ctx, coreworkflow.InvokeOperationRequest{
		ProviderName: "basic",
		RunID:        "run-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "export",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		ExecutionRef: "exec-ref-123",
	})
	if !errors.Is(err, invocation.ErrScopeDenied) {
		t.Fatalf("Invoke error = %v, want scope denied", err)
	}
	if executed {
		t.Fatal("expected provider Execute not to run when execution-ref permissions do not allow the operation")
	}
}

func TestWorkflowRuntimeInvokeExecutionRefLookupInfrastructureErrorIsInternal(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("boom")
	runtime := &workflowRuntime{
		executionRefs: coredata.NewWorkflowExecutionRefService(&erroringIndexedDB{err: lookupErr}),
	}
	runtime.SetInvoker(funcInvoker{
		invoke: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
			t.Fatal("invoke should not be called when execution-ref lookup fails")
			return nil, nil
		},
	})

	_, err := runtime.Invoke(context.Background(), coreworkflow.InvokeOperationRequest{
		ProviderName: "basic",
		ExecutionRef: "exec-ref-123",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "sync",
		},
	})
	if err == nil {
		t.Fatal("expected internal error, got nil")
	}
	if !errors.Is(err, invocation.ErrInternal) {
		t.Fatalf("error = %v, want ErrInternal", err)
	}
	if errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("error = %v, should not be ErrAuthorizationDenied", err)
	}
}

func TestWorkflowTriggerContextPrefersScheduleOverManual(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 30, 0, 0, time.UTC)
	trigger := workflowTriggerContext(coreworkflow.RunTrigger{
		Manual: true,
		Schedule: &coreworkflow.ScheduleTrigger{
			ScheduleID:   "sched-1",
			ScheduledFor: &scheduledFor,
		},
	})
	if trigger == nil {
		t.Fatal("trigger context = nil")
	}
	if got := trigger["kind"]; got != "schedule" {
		t.Fatalf("trigger kind = %#v, want %q", got, "schedule")
	}
	if got := trigger["scheduleId"]; got != "sched-1" {
		t.Fatalf("scheduleId = %#v, want %q", got, "sched-1")
	}
}

func TestWorkflowTriggerContextIncludesEventMetadataInLowerCamelCase(t *testing.T) {
	t.Parallel()

	eventTime := time.Date(2026, time.April, 15, 13, 45, 0, 0, time.UTC)
	trigger := workflowTriggerContext(coreworkflow.RunTrigger{
		Event: &coreworkflow.EventTriggerInvocation{
			TriggerID: "trigger-1",
			Event: coreworkflow.Event{
				ID:              "evt-1",
				Source:          "urn:test",
				SpecVersion:     "1.0",
				Type:            "demo.refresh",
				Subject:         "customer/cust_123",
				Time:            &eventTime,
				DataContentType: "application/json",
				Data: map[string]any{
					"customerId": "cust_123",
				},
				Extensions: map[string]any{
					"attempt": 2,
				},
			},
		},
	})
	if trigger == nil {
		t.Fatal("trigger context = nil")
	}
	if got := trigger["kind"]; got != "event" {
		t.Fatalf("trigger kind = %#v, want %q", got, "event")
	}
	if got := trigger["triggerId"]; got != "trigger-1" {
		t.Fatalf("triggerId = %#v, want %q", got, "trigger-1")
	}
	event, ok := trigger["event"].(map[string]any)
	if !ok {
		t.Fatalf("trigger event = %#v", trigger["event"])
	}
	if got := event["specVersion"]; got != "1.0" {
		t.Fatalf("specVersion = %#v, want %q", got, "1.0")
	}
	if got := event["dataContentType"]; got != "application/json" {
		t.Fatalf("dataContentType = %#v, want %q", got, "application/json")
	}
	if got := event["type"]; got != "demo.refresh" {
		t.Fatalf("event type = %#v, want %q", got, "demo.refresh")
	}
}
