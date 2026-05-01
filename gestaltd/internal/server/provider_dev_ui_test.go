package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestProviderDevMountedUIHandlerOverridesFallback(t *testing.T) {
	t.Parallel()

	manager, err := providerdev.NewManager([]providerdev.Target{{Name: "roadmap"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p := &principal.Principal{SubjectID: "user:user-123", UserID: "user-123", Kind: principal.KindUser}
	session, err := manager.CreateSession(context.Background(), p, providerdev.CreateSessionRequest{Providers: []providerdev.AttachProvider{{
		Name: "roadmap",
		UI:   true,
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	dispatchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dispatchDone := make(chan error, 1)
	go func() {
		call, ok, err := manager.PollSessionWithDispatcherSecretOnly(dispatchCtx, session.AttachID, session.DispatcherSecret)
		if err != nil || !ok {
			dispatchDone <- err
			return
		}
		var assetReq providerdev.UIAssetRequest
		payload, err := base64.StdEncoding.DecodeString(call.RequestBase64)
		if err == nil {
			err = json.Unmarshal(payload, &assetReq)
		}
		if err != nil {
			dispatchDone <- err
			return
		}
		resp := providerdev.UIAssetResponse{
			Status: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/html; charset=utf-8"},
			},
			Body: base64.StdEncoding.EncodeToString([]byte("local ui " + assetReq.Path + "?" + assetReq.RawQuery + " range=" + assetReq.Header.Get("Range"))),
		}
		respPayload, err := json.Marshal(resp)
		if err != nil {
			dispatchDone <- err
			return
		}
		dispatchDone <- manager.CompleteCallWithDispatcherSecretOnly(session.AttachID, call.CallID, session.DispatcherSecret, providerdev.CompleteCallRequest{
			ResponseBase64: base64.StdEncoding.EncodeToString(respPayload),
		})
	}()

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("remote fallback"))
	})
	s := &Server{providerDevSessions: manager}
	handler := s.mountedUIHandler(MountedUI{
		Name:       "roadmap",
		Path:       "/roadmap",
		PluginName: "roadmap",
		Public:     true,
		Handler:    fallback,
	})
	req := httptest.NewRequest(http.MethodGet, "/roadmap/sync?tab=preview", nil)
	req.Header.Set("Range", "bytes=0-3")
	req = req.WithContext(principal.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "local ui /sync?tab=preview range=bytes=0-3" {
		t.Fatalf("body = %q, want local ui body", got)
	}
	postCtx, postCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer postCancel()
	postReq := httptest.NewRequest(http.MethodPost, "/roadmap/sync", nil)
	postReq = postReq.WithContext(principal.WithPrincipal(postCtx, p))
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if got := postRec.Body.String(); got != "remote fallback" {
		t.Fatalf("POST body = %q, want fallback body", got)
	}
	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("dispatch provider dev ui: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for provider dev ui dispatch")
	}
}

func TestProviderDevMountedUIHandlerUsesMountedAuthRuntime(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	serverAuth := &coretesting.StubAuthProvider{
		N: "server",
		ValidateTokenFn: func(context.Context, string) (*core.UserIdentity, error) {
			return nil, principal.ErrInvalidToken
		},
	}
	altAuth := &coretesting.StubAuthProvider{
		N: "alt",
		ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token != "alt-session" {
				return nil, principal.ErrInvalidToken
			}
			return &core.UserIdentity{Email: "alt@example.com"}, nil
		},
	}
	s := &Server{
		users: services.Users,
		auth:  serverAuth,
		resolver: principal.NewResolver(
			serverAuth,
			services.Users,
			services.APITokens,
		),
		authProviders: map[string]core.AuthenticationProvider{
			"alt": altAuth,
		},
		authResolvers: map[string]*principal.Resolver{
			"alt": principal.NewResolver(altAuth, services.Users, services.APITokens),
		},
		pluginDefs: map[string]*config.ProviderEntry{
			"roadmap": {RouteAuth: &config.RouteAuthDef{Provider: "alt"}},
		},
	}
	mounted := MountedUI{
		Name:       "roadmap",
		Path:       "/roadmap",
		PluginName: "roadmap",
	}
	req := httptest.NewRequest(http.MethodGet, "/roadmap/", nil)
	req.Header.Set("Authorization", "Bearer alt-session")
	p := s.providerDevUIPrincipal(req, mounted)
	if p == nil || p.SubjectID == "" {
		t.Fatalf("providerDevUIPrincipal = %#v, want principal from mounted auth runtime", p)
	}

	manager, err := providerdev.NewManager([]providerdev.Target{{Name: "roadmap"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	session, err := manager.CreateSession(context.Background(), p, providerdev.CreateSessionRequest{Providers: []providerdev.AttachProvider{{
		Name: "roadmap",
		UI:   true,
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s.providerDevSessions = manager

	dispatchDone := make(chan error, 1)
	go func() {
		call, ok, err := manager.PollSessionWithDispatcherSecretOnly(context.Background(), session.AttachID, session.DispatcherSecret)
		if err != nil || !ok {
			dispatchDone <- err
			return
		}
		resp := providerdev.UIAssetResponse{
			Status: http.StatusOK,
			Body:   base64.StdEncoding.EncodeToString([]byte("local alt ui")),
		}
		respPayload, err := json.Marshal(resp)
		if err != nil {
			dispatchDone <- err
			return
		}
		dispatchDone <- manager.CompleteCallWithDispatcherSecretOnly(session.AttachID, call.CallID, session.DispatcherSecret, providerdev.CompleteCallRequest{
			ResponseBase64: base64.StdEncoding.EncodeToString(respPayload),
		})
	}()

	handler := s.mountedUIHandler(MountedUI{
		Name:       "roadmap",
		Path:       "/roadmap",
		PluginName: "roadmap",
		Public:     true,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("remote fallback"))
		}),
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != "local alt ui" {
		t.Fatalf("body = %q, want local alt ui", got)
	}
	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("dispatch provider dev ui: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for provider dev ui dispatch")
	}
}

func TestProviderDevMountedUIHandlerUsesAnonymousForNoAuthMountedUI(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	noneAuth := &coretesting.StubAuthProvider{N: "none"}
	resolver := principal.NewResolver(noneAuth, services.Users, services.APITokens)
	s := &Server{
		users:              services.Users,
		auth:               noneAuth,
		resolver:           resolver,
		noAuth:             true,
		anonymousPrincipal: resolver.ResolveEmail(anonymousEmail),
		authProviders: map[string]core.AuthenticationProvider{
			"none": noneAuth,
		},
		authResolvers: map[string]*principal.Resolver{
			"none": resolver,
		},
		pluginDefs: map[string]*config.ProviderEntry{
			"roadmap": {RouteAuth: &config.RouteAuthDef{Provider: "none"}},
		},
	}
	mounted := MountedUI{
		Name:       "roadmap",
		Path:       "/roadmap",
		PluginName: "roadmap",
	}
	p := s.providerDevUIPrincipal(httptest.NewRequest(http.MethodGet, "/roadmap/", nil), mounted)
	if p == nil || p.SubjectID == "" {
		t.Fatalf("providerDevUIPrincipal = %#v, want enriched anonymous principal", p)
	}

	manager, err := providerdev.NewManager([]providerdev.Target{{Name: "roadmap"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	session, err := manager.CreateSession(context.Background(), p, providerdev.CreateSessionRequest{Providers: []providerdev.AttachProvider{{
		Name: "roadmap",
		UI:   true,
	}}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s.providerDevSessions = manager

	dispatchDone := make(chan error, 1)
	go func() {
		call, ok, err := manager.PollSessionWithDispatcherSecretOnly(context.Background(), session.AttachID, session.DispatcherSecret)
		if err != nil || !ok {
			dispatchDone <- err
			return
		}
		respPayload, err := json.Marshal(providerdev.UIAssetResponse{
			Status: http.StatusOK,
			Body:   base64.StdEncoding.EncodeToString([]byte("local anonymous ui")),
		})
		if err != nil {
			dispatchDone <- err
			return
		}
		dispatchDone <- manager.CompleteCallWithDispatcherSecretOnly(session.AttachID, call.CallID, session.DispatcherSecret, providerdev.CompleteCallRequest{
			ResponseBase64: base64.StdEncoding.EncodeToString(respPayload),
		})
	}()

	handler := s.mountedUIHandler(MountedUI{
		Name:       "roadmap",
		Path:       "/roadmap",
		PluginName: "roadmap",
		Public:     true,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("remote fallback"))
		}),
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmap/", nil))
	if got := rec.Body.String(); got != "local anonymous ui" {
		t.Fatalf("body = %q, want local anonymous ui", got)
	}
	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("dispatch provider dev ui: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for provider dev ui dispatch")
	}
}

func TestWriteProviderDevUIAssetInvalidBodyDoesNotForwardRemoteHeaders(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeProviderDevUIAsset(rec, &providerdev.UIAssetResponse{
		Status: http.StatusOK,
		Header: http.Header{
			"Cache-Control":  []string{"max-age=3600"},
			"Content-Length": []string{"999"},
			"Content-Type":   []string{"text/html; charset=utf-8"},
		},
		Body: "not-base64",
	})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Fatalf("content-length = %q, want no upstream content length", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("cache-control = %q, want no upstream cache header", got)
	}
}

func TestWriteProviderDevUIAssetReplacesExistingHeaders(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rec.Header().Add("Content-Type", "application/json")
	rec.Header().Add("Content-Type", "text/plain")
	rec.Header().Set("X-Frame-Options", "DENY")
	writeProviderDevUIAsset(rec, &providerdev.UIAssetResponse{
		Status: http.StatusOK,
		Header: http.Header{
			"Content-Type":    []string{"text/html; charset=utf-8"},
			"Set-Cookie":      []string{"a=1", "b=2"},
			"X-Frame-Options": []string{"SAMEORIGIN"},
		},
		Body: base64.StdEncoding.EncodeToString([]byte("ok")),
	})

	if got := rec.Header().Values("Content-Type"); len(got) != 1 || got[0] != "text/html; charset=utf-8" {
		t.Fatalf("content-type values = %#v, want only local ui value", got)
	}
	if got := rec.Header().Values("X-Frame-Options"); len(got) != 1 || got[0] != "SAMEORIGIN" {
		t.Fatalf("x-frame-options values = %#v, want only local ui value", got)
	}
	if got := rec.Header().Values("Set-Cookie"); len(got) != 2 || got[0] != "a=1" || got[1] != "b=2" {
		t.Fatalf("set-cookie values = %#v, want both local ui cookies", got)
	}
}

func TestMaxBodyMiddlewareAllowsLargeProviderDevCallCompletions(t *testing.T) {
	t.Parallel()

	handler := maxBodyMiddleware(defaultMaxBodyBytes)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	largeBody := bytes.Repeat([]byte("x"), defaultMaxBodyBytes+1024)

	providerDevAttachmentReq := httptest.NewRequest(http.MethodPost, "/api/v1/provider-dev/attachments/attach-1/calls/call-1", bytes.NewReader(largeBody))
	providerDevAttachmentRec := httptest.NewRecorder()
	handler.ServeHTTP(providerDevAttachmentRec, providerDevAttachmentReq)
	if providerDevAttachmentRec.Code != http.StatusNoContent {
		t.Fatalf("provider dev attachment completion status = %d, want %d", providerDevAttachmentRec.Code, http.StatusNoContent)
	}

	extraPathReq := httptest.NewRequest(http.MethodPost, "/api/v1/provider-dev/attachments/attach-1/calls/call-1/extra", bytes.NewReader(largeBody))
	extraPathRec := httptest.NewRecorder()
	handler.ServeHTTP(extraPathRec, extraPathReq)
	if extraPathRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("provider dev extra path status = %d, want %d", extraPathRec.Code, http.StatusRequestEntityTooLarge)
	}

	normalReq := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", bytes.NewReader(largeBody))
	normalRec := httptest.NewRecorder()
	handler.ServeHTTP(normalRec, normalReq)
	if normalRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("normal endpoint status = %d, want %d", normalRec.Code, http.StatusRequestEntityTooLarge)
	}
}
