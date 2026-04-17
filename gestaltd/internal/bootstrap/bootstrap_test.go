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
	"strings"
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
	"gopkg.in/yaml.v3"
)

func stubAuthFactory(name string) bootstrap.AuthFactory {
	return func(yaml.Node, bootstrap.Deps) (core.AuthProvider, error) {
		return &coretesting.StubAuthProvider{N: name}, nil
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
					Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"},
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

func TestBootstrapClosesWorkflowIndexedDBAndAppliesScopedConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Providers.IndexedDB["workflow_state"] = &config.ProviderEntry{
		Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb"},
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

	cfg := validConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Source:         config.ProviderSource{Ref: "github.com/example/roadmap", Version: "0.0.1"},
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
			Source:         config.ProviderSource{Ref: "github.com/example/roadmap", Version: "0.0.1"},
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

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("baseline", func(t *testing.T) {
		t.Parallel()

		if _, err := bootstrap.Validate(context.Background(), validConfig(), validFactories()); err != nil {
			t.Fatalf("Validate: %v", err)
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
				ConnectionMode: providermanifestv1.ConnectionModeEither,
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
		if decoded.Source == nil || decoded.Source.Ref != "github.com/valon-technologies/gestalt-providers/auth/oidc" {
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
			"secondary": {Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"}},
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
		if authCfg.Source == nil || authCfg.Source.Ref != "github.com/valon-technologies/gestalt-providers/auth/oidc" {
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
		&coretesting.StubIntegration{N: "svc", ConnMode: core.ConnectionModeEither},
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
			ConnectionMode: providermanifestv1.ConnectionModeEither,
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
