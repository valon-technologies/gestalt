package providerdev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestHTTPTransportDispatchesProviderRPCs(t *testing.T) {
	t.Parallel()

	local := &recordingIntegrationClient{
		supportsSessionCatalog: true,
		sessionCatalog: &proto.Catalog{
			Name: "roadmap",
			Operations: []*proto.CatalogOperation{{
				Id:             "echo",
				Transport:      catalog.TransportPlugin,
				AllowedRoles:   []string{"viewer"},
				RequiredScopes: []string{"local.session.scope"},
			}},
		},
	}
	spec := providerhost.StaticProviderSpec{
		Name:           "roadmap",
		DisplayName:    "Roadmap",
		ConnectionMode: core.ConnectionModeNone,
		Catalog: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{{
				ID:             "echo",
				Transport:      catalog.TransportPlugin,
				AllowedRoles:   []string{"viewer"},
				RequiredScopes: []string{"local.scope"},
			}},
		},
	}
	remoteSpec := providerhost.StaticProviderSpec{
		Name:           "roadmap",
		ConnectionMode: core.ConnectionModeUser,
		AuthTypes:      []string{"oauth2"},
		Catalog: &catalog.Catalog{
			Name: "roadmap",
			Operations: []catalog.CatalogOperation{{
				ID:             "echo",
				Transport:      catalog.TransportPlugin,
				AllowedRoles:   []string{"admin"},
				RequiredScopes: []string{"remote.scope"},
			}},
		},
	}
	manager, err := NewManager([]Target{{
		Name: "roadmap",
		Spec: remoteSpec,
		RuntimeEnv: func(string) (RuntimeEnv, error) {
			return RuntimeEnv{
				Env:          map[string]string{"GESTALT_PLUGIN_INVOKER_SOCKET": "tls://gestalt.example.test:443"},
				AllowedHosts: []string{"gestalt.example.test"},
			}, nil
		},
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	ts := httptest.NewServer(providerDevTestHandler(t, manager, p))
	t.Cleanup(ts.Close)

	client := Client{BaseURL: ts.URL, HTTPClient: ts.Client()}
	session, err := client.CreateSession(context.Background(), CreateSessionRequest{Providers: []AttachProvider{{
		Name:   "roadmap",
		Spec:   spec,
		Config: map[string]any{"remote": true},
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(session.Providers) != 1 {
		t.Fatalf("session providers = %#v, want one", session.Providers)
	}
	if got := session.Providers[0].Env["GESTALT_PLUGIN_INVOKER_SOCKET"]; got != "tls://gestalt.example.test:443" {
		t.Fatalf("runtime env = %q, want relay target", got)
	}

	dispatchCtx, dispatchCancel := context.WithCancel(context.Background())
	defer dispatchCancel()
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- client.RunDispatcher(dispatchCtx, session.ID, map[string]proto.IntegrationProviderClient{
			"roadmap": local,
		})
	}()

	resolveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prov, ok, err := manager.ResolveProviderOverride(resolveCtx, p, "roadmap")
	if err != nil {
		t.Fatalf("ResolveProviderOverride: %v", err)
	}
	if !ok {
		t.Fatal("ResolveProviderOverride ok = false, want true")
	}
	if got := prov.ConnectionMode(); got != core.ConnectionModeUser {
		t.Fatalf("ConnectionMode = %q, want remote %q", got, core.ConnectionModeUser)
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 1 {
		t.Fatalf("Catalog operations = %#v, want one", cat)
	}
	if got := cat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("Catalog AllowedRoles = %#v, want remote [admin]", got)
	}
	if got := cat.Operations[0].RequiredScopes; len(got) != 1 || got[0] != "remote.scope" {
		t.Fatalf("Catalog RequiredScopes = %#v, want remote [remote.scope]", got)
	}
	sessionCat, attempted, err := core.CatalogForRequest(resolveCtx, prov, "remote-token")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if !attempted {
		t.Fatal("CatalogForRequest attempted = false, want true")
	}
	if got := sessionCat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("session Catalog AllowedRoles = %#v, want remote [admin]", got)
	}
	if got := sessionCat.Operations[0].RequiredScopes; len(got) != 1 || got[0] != "remote.scope" {
		t.Fatalf("session Catalog RequiredScopes = %#v, want remote [remote.scope]", got)
	}

	result, err := prov.Execute(resolveCtx, "echo", map[string]any{"message": "hello"}, "remote-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Body != `{"message":"hello","operation":"echo","token":"remote-token"}` {
		t.Fatalf("Execute body = %s", result.Body)
	}

	local.mu.Lock()
	startName := local.startName
	startConfig := fmt.Sprint(local.startConfig)
	local.mu.Unlock()
	if startName != "roadmap" {
		t.Fatalf("StartProvider name = %q, want roadmap", startName)
	}
	if !strings.Contains(startConfig, "remote:true") {
		t.Fatalf("StartProvider config = %s, want remote:true", startConfig)
	}

	dispatchCancel()
	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("dispatcher error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not stop")
	}
}

func providerDevTestHandler(t *testing.T, manager *Manager, p *principal.Principal) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(PathSessions, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := manager.CreateSession(r.Context(), p, req)
		if err != nil {
			writeProviderDevTestError(w, err)
			return
		}
		writeProviderDevTestJSON(w, http.StatusCreated, resp)
	})
	mux.HandleFunc(PathSessions+"/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, PathSessions+"/")
		parts := strings.Split(rest, "/")
		if len(parts) == 2 && parts[1] == "poll" && r.Method == http.MethodGet {
			ctx, cancel := context.WithTimeout(r.Context(), DefaultPollTimeout)
			defer cancel()
			resp, ok, err := manager.PollSession(ctx, p, parts[0])
			if err != nil {
				writeProviderDevTestError(w, err)
				return
			}
			if !ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeProviderDevTestJSON(w, http.StatusOK, resp)
			return
		}
		if len(parts) == 3 && parts[1] == "calls" && r.Method == http.MethodPost {
			var req CompleteCallRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := manager.CompleteCall(p, parts[0], parts[2], req); err != nil {
				writeProviderDevTestError(w, err)
				return
			}
			writeProviderDevTestJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if len(parts) == 1 && r.Method == http.MethodDelete {
			if err := manager.CloseSession(p, parts[0]); err != nil {
				writeProviderDevTestError(w, err)
				return
			}
			writeProviderDevTestJSON(w, http.StatusOK, map[string]string{"status": "closed"})
			return
		}
		http.NotFound(w, r)
	})
	return mux
}

func writeProviderDevTestJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProviderDevTestError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.InvalidArgument:
			code = http.StatusBadRequest
		case codes.Unauthenticated:
			code = http.StatusUnauthorized
		case codes.PermissionDenied:
			code = http.StatusForbidden
		case codes.NotFound:
			code = http.StatusNotFound
		case codes.FailedPrecondition:
			code = http.StatusPreconditionFailed
		}
		err = fmt.Errorf("%s", st.Message())
	}
	http.Error(w, err.Error(), code)
}

type recordingIntegrationClient struct {
	mu                     sync.Mutex
	startName              string
	startConfig            map[string]any
	supportsSessionCatalog bool
	sessionCatalog         *proto.Catalog
}

func (c *recordingIntegrationClient) GetMetadata(context.Context, *emptypb.Empty, ...grpc.CallOption) (*proto.ProviderMetadata, error) {
	return &proto.ProviderMetadata{SupportsSessionCatalog: c.supportsSessionCatalog}, nil
}

func (c *recordingIntegrationClient) StartProvider(_ context.Context, req *proto.StartProviderRequest, _ ...grpc.CallOption) (*proto.StartProviderResponse, error) {
	c.mu.Lock()
	c.startName = req.GetName()
	if req.GetConfig() != nil {
		c.startConfig = req.GetConfig().AsMap()
	}
	c.mu.Unlock()
	return &proto.StartProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (c *recordingIntegrationClient) Execute(_ context.Context, req *proto.ExecuteRequest, _ ...grpc.CallOption) (*proto.OperationResult, error) {
	params := req.GetParams().AsMap()
	body := fmt.Sprintf(`{"message":%q,"operation":%q,"token":%q}`, params["message"], req.GetOperation(), req.GetToken())
	return &proto.OperationResult{Status: http.StatusOK, Body: body}, nil
}

func (c *recordingIntegrationClient) ResolveHTTPSubject(context.Context, *proto.ResolveHTTPSubjectRequest, ...grpc.CallOption) (*proto.ResolveHTTPSubjectResponse, error) {
	return nil, status.Error(codes.Unimplemented, "resolve http subject is not implemented")
}

func (c *recordingIntegrationClient) GetSessionCatalog(context.Context, *proto.GetSessionCatalogRequest, ...grpc.CallOption) (*proto.GetSessionCatalogResponse, error) {
	if !c.supportsSessionCatalog {
		return nil, status.Error(codes.Unimplemented, "session catalog is not implemented")
	}
	return &proto.GetSessionCatalogResponse{Catalog: c.sessionCatalog}, nil
}

func (c *recordingIntegrationClient) PostConnect(context.Context, *proto.PostConnectRequest, ...grpc.CallOption) (*proto.PostConnectResponse, error) {
	return nil, status.Error(codes.Unimplemented, "post connect is not implemented")
}
