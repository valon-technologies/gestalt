package providerdev

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
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
	spec := pluginservice.StaticProviderSpec{
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
	remoteSpec := pluginservice.StaticProviderSpec{
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
		Config: map[string]any{
			"remote": true,
		},
		RuntimeEnv: func(string) (RuntimeEnv, error) {
			return RuntimeEnv{
				Env: map[string]string{"GESTALT_PLUGIN_INVOKER_SOCKET": "tls://gestalt.example.test:443"},
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
	localUI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Local-Handler", "observed")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, "local ui %s?%s range=%s", r.URL.Path, r.URL.RawQuery, r.Header.Get("Range"))
	})
	session, err := client.CreateSession(context.Background(), CreateSessionRequest{Providers: []AttachProvider{{
		Name: "roadmap",
		Spec: spec,
		UI:   true,
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
		dispatchDone <- client.RunDispatcher(dispatchCtx, session.AttachID, map[string]proto.IntegrationProviderClient{
			"roadmap": local,
		}, WithUIHandlers(map[string]http.Handler{"roadmap": localUI}))
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

	uiResp, ok, err := manager.ServeUIAsset(resolveCtx, p, "roadmap", UIAssetRequest{
		Method:   http.MethodGet,
		Path:     "/sync",
		RawQuery: "tab=preview",
		Header: http.Header{
			"Range": []string{"bytes=0-3"},
		},
	})
	if err != nil {
		t.Fatalf("ServeUIAsset: %v", err)
	}
	if !ok {
		t.Fatal("ServeUIAsset ok = false, want true")
	}
	if got := uiResp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("ui content-type = %q, want text/html", got)
	}
	uiBody, err := base64.StdEncoding.DecodeString(uiResp.Body)
	if err != nil {
		t.Fatalf("decode ui body: %v", err)
	}
	if string(uiBody) != "local ui /sync?tab=preview range=bytes=0-3" {
		t.Fatalf("ui body = %q", uiBody)
	}

	uiResp, ok, err = manager.ServeUIAsset(resolveCtx, p, "roadmap", UIAssetRequest{
		Method: http.MethodGet,
		Path:   "/empty-header",
	})
	if err != nil {
		t.Fatalf("ServeUIAsset without headers: %v", err)
	}
	if !ok {
		t.Fatal("ServeUIAsset without headers ok = false, want true")
	}
	uiBody, err = base64.StdEncoding.DecodeString(uiResp.Body)
	if err != nil {
		t.Fatalf("decode ui body without headers: %v", err)
	}
	if string(uiBody) != "local ui /empty-header? range=" {
		t.Fatalf("ui body without headers = %q", uiBody)
	}

	uiResp, ok, err = manager.ServeUIAsset(resolveCtx, p, "roadmap", UIAssetRequest{
		Method: http.MethodHead,
		Path:   "/head",
	})
	if err != nil {
		t.Fatalf("ServeUIAsset HEAD: %v", err)
	}
	if !ok {
		t.Fatal("ServeUIAsset HEAD ok = false, want true")
	}
	uiBody, err = base64.StdEncoding.DecodeString(uiResp.Body)
	if err != nil {
		t.Fatalf("decode ui HEAD body: %v", err)
	}
	if len(uiBody) != 0 {
		t.Fatalf("ui HEAD body = %q, want empty", uiBody)
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

func TestCompleteCallTreatsOKErrorCodeAsProtocolError(t *testing.T) {
	t.Parallel()

	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	dispatcherSecret := "dispatcher-secret"
	call := &rpcCall{id: "call-1", response: make(chan rpcResponse, 1)}
	session := &Session{
		id:                   "session-1",
		dispatcherSecretHash: hashDispatcherSecret(dispatcherSecret),
		owner:                p.SubjectID,
		pending:              map[string]*rpcCall{"call-1": call},
		done:                 make(chan struct{}),
		closeDone:            make(chan struct{}),
		lastSeen:             time.Now(),
	}
	manager := &Manager{
		sessions: map[string]*Session{"session-1": session},
	}

	if err := manager.CompleteCallWithDispatcherSecretOnly("session-1", "call-1", dispatcherSecret, CompleteCallRequest{
		Error: &RPCError{Code: int32(codes.OK), Message: "unexpected ok"},
	}); err != nil {
		t.Fatalf("CompleteCallWithDispatcherSecretOnly: %v", err)
	}

	select {
	case resp := <-call.response:
		if got := status.Code(resp.err); got != codes.InvalidArgument {
			t.Fatalf("response error code = %s, want %s", got, codes.InvalidArgument)
		}
		if got := resp.payload; got != nil {
			t.Fatalf("response payload = %x, want nil", got)
		}
	case <-time.After(time.Second):
		t.Fatal("call response was not delivered")
	}
}

func TestCreateSessionMatchesProviderBySource(t *testing.T) {
	t.Parallel()

	manager, err := NewManager([]Target{{
		Name:   "workplaceHub",
		Source: "github.com/valon-technologies/valon-tools/plugins/workplace-hub",
		Spec: pluginservice.StaticProviderSpec{
			Name: "workplaceHub",
		},
		Config: map[string]any{"remote": true},
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	resp, err := manager.CreateSession(context.Background(), p, CreateSessionRequest{Providers: []AttachProvider{{
		Source: "github.com/valon-technologies/valon-tools/plugins/workplace-hub",
		Spec: pluginservice.StaticProviderSpec{
			Name: "local-workplace-hub",
		},
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("session providers = %#v, want one", resp.Providers)
	}
	if got := resp.Providers[0].Name; got != "workplaceHub" {
		t.Fatalf("resolved provider name = %q, want workplaceHub", got)
	}

	names, err := manager.ResolveAttachProviderNames(CreateSessionRequest{Providers: []AttachProvider{{
		Source: "github.com/valon-technologies/valon-tools/plugins/workplace-hub",
	}}})
	if err != nil {
		t.Fatalf("ResolveAttachProviderNames: %v", err)
	}
	if len(names) != 1 || names[0] != "workplaceHub" {
		t.Fatalf("ResolveAttachProviderNames = %#v, want [workplaceHub]", names)
	}
}

func TestHTTPTransportCreateSessionExplicitConfigOverridesRemoteConfig(t *testing.T) {
	t.Parallel()

	manager, err := NewManager([]Target{{
		Name:   "roadmap",
		Config: map[string]any{"remote": true},
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	ts := httptest.NewServer(providerDevTestHandler(t, manager, p))
	t.Cleanup(ts.Close)

	client := Client{BaseURL: ts.URL, HTTPClient: ts.Client()}
	localConfig := map[string]any{"local": true}
	resp, err := client.CreateSession(context.Background(), CreateSessionRequest{Providers: []AttachProvider{{
		Name:   "roadmap",
		Config: &localConfig,
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	manager.mu.RLock()
	session := manager.sessions[resp.AttachID]
	manager.mu.RUnlock()
	if session == nil {
		t.Fatalf("session %q was not recorded", resp.AttachID)
	}
	target := session.targets["roadmap"]
	if target == nil {
		t.Fatal("session target roadmap was not recorded")
	}
	if got := target.target.Config["remote"]; got != nil {
		t.Fatalf("attached config remote = %#v, want omitted", got)
	}
	if got := target.target.Config["local"]; got != true {
		t.Fatalf("attached config local = %#v, want true", got)
	}
}

func TestHTTPTransportRequiresDispatcherSecretForDispatcherTraffic(t *testing.T) {
	t.Parallel()

	manager, err := NewManager([]Target{{Name: "roadmap"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	ts := httptest.NewServer(providerDevTestHandler(t, manager, p))
	t.Cleanup(ts.Close)

	client := Client{BaseURL: ts.URL, HTTPClient: ts.Client()}
	session, err := client.CreateSession(context.Background(), CreateSessionRequest{Providers: []AttachProvider{{Name: "roadmap"}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.DispatcherSecret == "" {
		t.Fatal("DispatcherSecret is empty")
	}

	pollReq, err := http.NewRequest(http.MethodGet, ts.URL+PathAttachments+"/"+session.AttachID+"/poll", nil)
	if err != nil {
		t.Fatalf("build poll request: %v", err)
	}
	pollResp, err := ts.Client().Do(pollReq)
	if err != nil {
		t.Fatalf("poll without dispatcher secret: %v", err)
	}
	_ = pollResp.Body.Close()
	if pollResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("poll status = %d, want 401", pollResp.StatusCode)
	}

	completeReq, err := http.NewRequest(http.MethodPost, ts.URL+PathAttachments+"/"+session.AttachID+"/calls/call-1", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build complete request: %v", err)
	}
	completeReq.Header.Set("Content-Type", "application/json")
	completeResp, err := ts.Client().Do(completeReq)
	if err != nil {
		t.Fatalf("complete without dispatcher secret: %v", err)
	}
	_ = completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("complete status = %d, want 401", completeResp.StatusCode)
	}

	closeReq, err := http.NewRequest(http.MethodDelete, ts.URL+PathAttachments+"/"+session.AttachID, nil)
	if err != nil {
		t.Fatalf("build close request: %v", err)
	}
	closeResp, err := ts.Client().Do(closeReq)
	if err != nil {
		t.Fatalf("close without dispatcher secret: %v", err)
	}
	_ = closeResp.Body.Close()
	if closeResp.StatusCode != http.StatusOK {
		t.Fatalf("close status = %d, want 200", closeResp.StatusCode)
	}
}

func TestCreateAttachAuthorizationRejectsWhenPendingLimitReached(t *testing.T) {
	t.Parallel()

	manager, err := NewManager([]Target{{Name: "roadmap"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	req := CreateSessionRequest{Providers: []AttachProvider{{Name: "roadmap"}}}
	now := time.Now()
	for i := 0; i < DefaultMaxAttachAuthorizations; i++ {
		if _, _, _, err := manager.CreateAttachAuthorization(req, now); err != nil {
			t.Fatalf("CreateAttachAuthorization %d: %v", i, err)
		}
	}
	_, _, _, err = manager.CreateAttachAuthorization(req, now)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("CreateAttachAuthorization over limit code = %v, want %v (err=%v)", status.Code(err), codes.ResourceExhausted, err)
	}
}

func TestHTTPTransportListsRedactedAttachmentMetadata(t *testing.T) {
	t.Parallel()

	manager, err := NewManager([]Target{{
		Name:   "roadmap",
		Source: "github.com/acme/plugins/roadmap",
		UIPath: "/roadmap",
		RuntimeEnv: func(string) (RuntimeEnv, error) {
			return RuntimeEnv{
				Env: map[string]string{"SECRET": "do-not-return"},
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
		Name: "roadmap",
		UI:   true,
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for _, path := range []string{PathAttachments, PathAttachments + "/" + session.AttachID} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		payload, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, resp.StatusCode, payload)
		}
		body := string(payload)
		for _, forbidden := range []string{session.DispatcherSecret, "do-not-return", "allowedHosts", "SECRET"} {
			if forbidden != "" && strings.Contains(body, forbidden) {
				t.Fatalf("GET %s leaked %q in %s", path, forbidden, body)
			}
		}
		if !strings.Contains(body, `"attachId":"`+session.AttachID+`"`) {
			t.Fatalf("GET %s body missing attachId %q: %s", path, session.AttachID, body)
		}
	}
}

func TestCreateSessionRejectsAmbiguousProviderSource(t *testing.T) {
	t.Parallel()

	const source = "github.com/acme/plugins/shared"
	manager, err := NewManager([]Target{{
		Name:   "first",
		Source: source,
	}, {
		Name:   "second",
		Source: source,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	_, err = manager.CreateSession(context.Background(), p, CreateSessionRequest{Providers: []AttachProvider{{
		Source: source,
	}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateSession error = %v, want InvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "pass --name") {
		t.Fatalf("CreateSession error = %v, want pass --name hint", err)
	}
}

func TestPollSessionDropsCanceledQueuedCall(t *testing.T) {
	t.Parallel()

	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	dispatcherSecret := "dispatcher-secret"
	session := &Session{
		id:                   "session-1",
		dispatcherSecretHash: hashDispatcherSecret(dispatcherSecret),
		owner:                p.SubjectID,
		calls:                make(chan *rpcCall, 1),
		pending:              map[string]*rpcCall{},
		done:                 make(chan struct{}),
		closeDone:            make(chan struct{}),
		lastSeen:             time.Now(),
	}
	manager := &Manager{
		sessions: map[string]*Session{"session-1": session},
	}

	invokeCtx, cancel := context.WithCancel(context.Background())
	invokeDone := make(chan error, 1)
	go func() {
		invokeDone <- session.invoke(invokeCtx, "roadmap", "Execute", &emptypb.Empty{}, &emptypb.Empty{})
	}()

	deadline := time.Now().Add(time.Second)
	for len(session.calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(session.calls) == 0 {
		t.Fatal("invoke did not enqueue a call")
	}

	cancel()
	if err := <-invokeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("invoke error = %v, want %v", err, context.Canceled)
	}

	pollCtx, pollCancel := context.WithTimeout(context.Background(), time.Second)
	defer pollCancel()
	resp, ok, err := manager.PollSessionWithDispatcherSecretOnly(pollCtx, "session-1", dispatcherSecret)
	if err != nil {
		t.Fatalf("PollSessionWithDispatcherSecretOnly: %v", err)
	}
	if ok || resp != nil {
		t.Fatalf("PollSession = (%#v, %t), want no call", resp, ok)
	}
	if got := len(session.pending); got != 0 {
		t.Fatalf("pending calls = %d, want 0", got)
	}
	if got := len(session.calls); got != 0 {
		t.Fatalf("queued calls = %d, want 0", got)
	}
}

func TestSessionCloseReturnsSameErrorToConcurrentCallers(t *testing.T) {
	t.Parallel()

	expected := errors.New("close failed")
	prov := &blockingCloseProvider{
		StubIntegration: coretesting.StubIntegration{N: "roadmap"},
		started:         make(chan struct{}),
		release:         make(chan struct{}),
		err:             expected,
	}
	session := &Session{
		targets: map[string]*attachedTarget{
			"roadmap": {provider: prov},
		},
		pending:   map[string]*rpcCall{},
		done:      make(chan struct{}),
		closeDone: make(chan struct{}),
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- session.Close()
	}()
	<-prov.started

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- session.Close()
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second Close returned before cleanup finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(prov.release)

	if err := <-firstDone; !errors.Is(err, expected) {
		t.Fatalf("first Close error = %v, want %v", err, expected)
	}
	if err := <-secondDone; !errors.Is(err, expected) {
		t.Fatalf("second Close error = %v, want %v", err, expected)
	}
}

func providerDevTestHandler(t *testing.T, manager *Manager, p *principal.Principal) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	handleCreate := func(w http.ResponseWriter, r *http.Request) {
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
	}
	mux.HandleFunc(PathAttachments, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreate(w, r)
		case http.MethodGet:
			resp, err := manager.ListSessions(p)
			if err != nil {
				writeProviderDevTestError(w, err)
				return
			}
			writeProviderDevTestJSON(w, http.StatusOK, map[string]any{"attachments": resp})
		default:
			http.NotFound(w, r)
		}
	})
	handleAttachment := func(basePath string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			rest := strings.TrimPrefix(r.URL.Path, basePath+"/")
			parts := strings.Split(rest, "/")
			if basePath == PathAttachments && len(parts) == 1 && r.Method == http.MethodGet {
				resp, err := manager.GetSession(p, parts[0])
				if err != nil {
					writeProviderDevTestError(w, err)
					return
				}
				writeProviderDevTestJSON(w, http.StatusOK, resp)
				return
			}
			if len(parts) == 2 && parts[1] == "poll" && r.Method == http.MethodGet {
				ctx, cancel := context.WithTimeout(r.Context(), DefaultPollTimeout)
				defer cancel()
				resp, ok, err := manager.PollSessionWithDispatcherSecretOnly(ctx, parts[0], r.Header.Get(HeaderDispatcherSecret))
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
				if err := manager.CompleteCallWithDispatcherSecretOnly(parts[0], parts[2], r.Header.Get(HeaderDispatcherSecret), req); err != nil {
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
		}
	}
	mux.HandleFunc(PathAttachments+"/", handleAttachment(PathAttachments))
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

type blockingCloseProvider struct {
	coretesting.StubIntegration
	started chan struct{}
	release chan struct{}
	err     error
}

func (p *blockingCloseProvider) Close() error {
	close(p.started)
	<-p.release
	return p.err
}
