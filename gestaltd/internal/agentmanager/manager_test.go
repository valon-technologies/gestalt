package agentmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type catalogCountingProvider struct {
	coretesting.StubIntegration
	catalogCalls int
}

func (p *catalogCountingProvider) Catalog() *catalog.Catalog {
	p.catalogCalls++
	return p.CatalogVal
}

type sessionCatalogProvider struct {
	catalogCountingProvider
	sessionCatalog *catalog.Catalog
}

func (p *sessionCatalogProvider) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return p.sessionCatalog, nil
}

type tokenErrorInvoker struct {
	providerName string
	err          error
}

func (i tokenErrorInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return nil, nil
}

func (i tokenErrorInvoker) ResolveToken(ctx context.Context, _ *principal.Principal, providerName, _, _ string) (context.Context, string, error) {
	if i.providerName != "" && providerName != i.providerName {
		return ctx, "", nil
	}
	return ctx, "", i.err
}

type singleAgentControl struct {
	name     string
	provider coreagent.Provider
}

func (c singleAgentControl) ResolveProvider(name string) (coreagent.Provider, error) {
	if name != "" && name != c.name {
		return nil, NewAgentProviderNotAvailableError(name)
	}
	return c.provider, nil
}

func (c singleAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	if name != "" && name != c.name {
		return "", nil, NewAgentProviderNotAvailableError(name)
	}
	return c.name, c.provider, nil
}

func (c singleAgentControl) ProviderNames() []string {
	return []string{c.name}
}

type idempotentSessionProvider struct {
	coreagent.UnimplementedProvider
	session        *coreagent.Session
	createRequests []coreagent.CreateSessionRequest
}

func (p *idempotentSessionProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.createRequests = append(p.createRequests, req)
	return cloneAgentManagerSession(p.session), nil
}

func (p *idempotentSessionProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	if p.session == nil || req.SessionID != p.session.ID {
		return nil, core.ErrNotFound
	}
	return cloneAgentManagerSession(p.session), nil
}

func cloneAgentManagerSession(src *coreagent.Session) *coreagent.Session {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func TestCreateSessionReconcilesIdempotentProviderSessionID(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	provider := &idempotentSessionProvider{
		session: &coreagent.Session{
			ID:        "provider-session-1",
			State:     coreagent.SessionStateActive,
			CreatedBy: coreagent.Actor{SubjectID: principal.UserSubjectID("user-1")},
		},
	}
	manager := New(Config{
		Agent:           singleAgentControl{name: "simple", provider: provider},
		SessionMetadata: services.AgentSessions,
		RunMetadata:     services.AgentRunMetadata,
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName:    "simple",
		IdempotencyKey:  "workflow:github:run-1:session",
		ProviderOptions: map[string]any{"temperature": 0},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID != "provider-session-1" {
		t.Fatalf("CreateSession session ID = %q, want provider-session-1", session.ID)
	}
	if len(provider.createRequests) != 1 {
		t.Fatalf("CreateSession calls = %d, want 1", len(provider.createRequests))
	}
	if provider.createRequests[0].SessionID == session.ID {
		t.Fatalf("provider request session ID = returned session ID %q, want generated requested ID", session.ID)
	}

	ref, err := services.AgentSessions.Get(context.Background(), "provider-session-1")
	if err != nil {
		t.Fatalf("AgentSessions.Get: %v", err)
	}
	if ref.IdempotencyKey != "workflow:github:run-1:session" {
		t.Fatalf("metadata idempotency key = %q, want workflow key", ref.IdempotencyKey)
	}

	replayed, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName:   "simple",
		IdempotencyKey: "workflow:github:run-1:session",
	})
	if err != nil {
		t.Fatalf("CreateSession replay: %v", err)
	}
	if replayed.ID != "provider-session-1" {
		t.Fatalf("CreateSession replay ID = %q, want provider-session-1", replayed.ID)
	}
	if len(provider.createRequests) != 1 {
		t.Fatalf("CreateSession calls after replay = %d, want 1", len(provider.createRequests))
	}
}

func TestCreateSessionPreservesExistingMetadataWhenReconcilingProviderSession(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	archivedAt := time.Date(2026, time.April, 27, 12, 0, 0, 0, time.UTC)
	if _, err := services.AgentSessions.Put(context.Background(), &coreagent.SessionReference{
		ID:                  "provider-session-1",
		ProviderName:        "simple",
		SubjectID:           principal.UserSubjectID("user-1"),
		CredentialSubjectID: "credential:old",
		ArchivedAt:          &archivedAt,
	}); err != nil {
		t.Fatalf("AgentSessions.Put: %v", err)
	}
	provider := &idempotentSessionProvider{
		session: &coreagent.Session{
			ID:    "provider-session-1",
			State: coreagent.SessionStateArchived,
		},
	}
	manager := New(Config{
		Agent:           singleAgentControl{name: "simple", provider: provider},
		SessionMetadata: services.AgentSessions,
		RunMetadata:     services.AgentRunMetadata,
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName:   "simple",
		IdempotencyKey: "workflow:github:run-1:session",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID != "provider-session-1" {
		t.Fatalf("CreateSession session ID = %q, want provider-session-1", session.ID)
	}

	ref, err := services.AgentSessions.Get(context.Background(), "provider-session-1")
	if err != nil {
		t.Fatalf("AgentSessions.Get: %v", err)
	}
	if ref.ArchivedAt == nil || !ref.ArchivedAt.Equal(archivedAt) {
		t.Fatalf("metadata archived_at = %v, want %v", ref.ArchivedAt, archivedAt)
	}
	if ref.CredentialSubjectID != "credential:old" {
		t.Fatalf("metadata credential_subject_id = %q, want credential:old", ref.CredentialSubjectID)
	}
	if ref.IdempotencyKey != "workflow:github:run-1:session" {
		t.Fatalf("metadata idempotency key = %q, want workflow key", ref.IdempotencyKey)
	}
}

func TestCreateSessionWaitsForClaimedIdempotentSessionMetadata(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	subjectID := principal.UserSubjectID("user-1")
	provider := &idempotentSessionProvider{
		session: &coreagent.Session{
			ID:        "provider-session-1",
			State:     coreagent.SessionStateActive,
			CreatedBy: coreagent.Actor{SubjectID: subjectID},
		},
	}
	manager := New(Config{
		Agent:           singleAgentControl{name: "simple", provider: provider},
		SessionMetadata: services.AgentSessions,
		RunMetadata:     services.AgentRunMetadata,
	})
	const idempotencyKey = "workflow:github:run-1:session"
	claimedSessionID, claimed, err := services.AgentSessions.ClaimIdempotency(context.Background(), subjectID, "simple", idempotencyKey, "provider-session-1", time.Now())
	if err != nil {
		t.Fatalf("ClaimIdempotency: %v", err)
	}
	if !claimed || claimedSessionID != "provider-session-1" {
		t.Fatalf("ClaimIdempotency = (%q, %t), want (provider-session-1, true)", claimedSessionID, claimed)
	}
	putErr := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, err := services.AgentSessions.Put(context.Background(), &coreagent.SessionReference{
			ID:             "provider-session-1",
			ProviderName:   "simple",
			SubjectID:      subjectID,
			IdempotencyKey: idempotencyKey,
		})
		putErr <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := manager.CreateSession(ctx, &principal.Principal{SubjectID: subjectID}, coreagent.ManagerCreateSessionRequest{
		ProviderName:   "simple",
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := <-putErr; err != nil {
		t.Fatalf("AgentSessions.Put: %v", err)
	}
	if session.ID != "provider-session-1" {
		t.Fatalf("CreateSession ID = %q, want provider-session-1", session.ID)
	}
	if len(provider.createRequests) != 0 {
		t.Fatalf("provider CreateSession calls = %d, want 0", len(provider.createRequests))
	}
}

func TestCreateSessionWaitsForReconciledIdempotentSessionMetadata(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	subjectID := principal.UserSubjectID("user-1")
	provider := &idempotentSessionProvider{
		session: &coreagent.Session{
			ID:        "provider-session-1",
			State:     coreagent.SessionStateActive,
			CreatedBy: coreagent.Actor{SubjectID: subjectID},
		},
	}
	manager := New(Config{
		Agent:           singleAgentControl{name: "simple", provider: provider},
		SessionMetadata: services.AgentSessions,
		RunMetadata:     services.AgentRunMetadata,
	})
	const idempotencyKey = "workflow:github:run-1:session"
	claimedSessionID, claimed, err := services.AgentSessions.ClaimIdempotency(context.Background(), subjectID, "simple", idempotencyKey, "generated-session-1", time.Now())
	if err != nil {
		t.Fatalf("ClaimIdempotency: %v", err)
	}
	if !claimed || claimedSessionID != "generated-session-1" {
		t.Fatalf("ClaimIdempotency = (%q, %t), want (generated-session-1, true)", claimedSessionID, claimed)
	}
	putErr := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, err := services.AgentSessions.Put(context.Background(), &coreagent.SessionReference{
			ID:             "provider-session-1",
			ProviderName:   "simple",
			SubjectID:      subjectID,
			IdempotencyKey: idempotencyKey,
		})
		putErr <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := manager.CreateSession(ctx, &principal.Principal{SubjectID: subjectID}, coreagent.ManagerCreateSessionRequest{
		ProviderName:   "simple",
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := <-putErr; err != nil {
		t.Fatalf("AgentSessions.Put: %v", err)
	}
	if session.ID != "provider-session-1" {
		t.Fatalf("CreateSession ID = %q, want provider-session-1", session.ID)
	}
	if len(provider.createRequests) != 0 {
		t.Fatalf("provider CreateSession calls = %d, want 0", len(provider.createRequests))
	}
}

func TestCreateSessionRejectsIdempotentProviderSessionForDifferentSubject(t *testing.T) {
	t.Parallel()

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	provider := &idempotentSessionProvider{
		session: &coreagent.Session{
			ID:        "provider-session-1",
			State:     coreagent.SessionStateActive,
			CreatedBy: coreagent.Actor{SubjectID: principal.UserSubjectID("user-2")},
		},
	}
	manager := New(Config{
		Agent:           singleAgentControl{name: "simple", provider: provider},
		SessionMetadata: services.AgentSessions,
		RunMetadata:     services.AgentRunMetadata,
	})
	p := &principal.Principal{SubjectID: principal.UserSubjectID("user-1")}

	_, err = manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName:   "simple",
		IdempotencyKey: "workflow:github:run-1:session",
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("CreateSession error = %v, want not found", err)
	}
	if _, err := services.AgentSessions.Get(context.Background(), "provider-session-1"); err == nil {
		t.Fatal("AgentSessions.Get error = nil, want not found")
	}

	provider.session.CreatedBy = coreagent.Actor{SubjectID: principal.UserSubjectID("user-1")}
	session, err := manager.CreateSession(context.Background(), p, coreagent.ManagerCreateSessionRequest{
		ProviderName:   "simple",
		IdempotencyKey: "workflow:github:run-1:session",
	})
	if err != nil {
		t.Fatalf("CreateSession after rejected replay: %v", err)
	}
	if session.ID != "provider-session-1" {
		t.Fatalf("CreateSession after rejected replay ID = %q, want provider-session-1", session.ID)
	}
}

func TestSearchToolsSearchesAuthorizedCatalogWhenNoRefsDefined(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "search",
				Title:    "Search",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "search",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "docs" || resp.Tools[0].Target.Operation != "search" {
		t.Fatalf("tool target = %#v, want docs.search", resp.Tools[0].Target)
	}
}

func TestSearchToolsRestrictsToDefinedRefs(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "search",
				Title:    "Search",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "search",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "docs",
			Operation: "search",
		}},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "docs" || resp.Tools[0].Target.Operation != "search" {
		t.Fatalf("tool target = %#v, want docs.search", resp.Tools[0].Target)
	}
}

func TestSearchToolsOmitsHiddenOperationsUnlessExplicitlyScoped(t *testing.T) {
	t.Parallel()

	hidden := false
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{
					ID:       "chat.postMessage",
					Title:    "Post Message",
					ReadOnly: false,
				},
				{
					ID:      "events.reply",
					Title:   "Reply to Event",
					Visible: &hidden,
				},
			}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "reply",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 0 {
		t.Fatalf("SearchTools returned %d hidden tools, want 0", len(resp.Tools))
	}

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		ToolRefs: []coreagent.ToolRef{{Plugin: "slack", Operation: "events.reply"}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Target.Operation != "events.reply" {
		t.Fatalf("ResolveTools = %#v, want explicit hidden events.reply", tools)
	}
}

func TestSearchToolsGlobalWildcardKeepsSearchOpenWithExactRefs(t *testing.T) {
	t.Parallel()

	hidden := false
	slack := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply to Event",
				Visible: &hidden,
			}}},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "list_issues",
				Title:    "List Linear Issues",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, slack, linear)})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "linear issues",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "*"},
			{Plugin: "slack", Operation: "events.reply"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "linear" || resp.Tools[0].Target.Operation != "list_issues" {
		t.Fatalf("tool target = %#v, want linear.list_issues", resp.Tools[0].Target)
	}
}

func TestSearchToolsGlobalWildcardFindsProviderQualifiedCatalogOperation(t *testing.T) {
	t.Parallel()

	hidden := false
	slack := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply to Event",
				Visible: &hidden,
			}}},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:          "issues",
					Description: "All issues. Returns a paginated list of issues visible to the authenticated user.",
					ReadOnly:    true,
				}},
			},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, slack, linear)})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "Linear list issues assigned to me",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "*"},
			{Plugin: "slack", Operation: "events.reply"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "linear" || resp.Tools[0].Target.Operation != "issues" {
		t.Fatalf("tool target = %#v, want linear.issues", resp.Tools[0].Target)
	}
}

func TestAgentRunPermissionsKeepsAPITokenRestrictionsForHTTPWildcard(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "linear",
		Operations: []string{"issues"},
	}})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		Source:           principal.SourceAPIToken,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	got := agentRunPermissions(ctx, p, "slack", []coreagent.ToolRef{{Plugin: "*"}})
	if len(got) != 1 || got[0].Plugin != "linear" || len(got[0].Operations) != 1 || got[0].Operations[0] != "issues" {
		t.Fatalf("agentRunPermissions = %#v, want API token permissions preserved", got)
	}
}

func TestAgentRunPermissionsClearsHTTPResolvedUserWildcardRestrictions(t *testing.T) {
	t.Parallel()

	perms := principal.CompilePermissions([]core.AccessPermission{{
		Plugin:     "slack",
		Operations: []string{"events.reply"},
	}})
	p := &principal.Principal{
		SubjectID:        principal.UserSubjectID("user-1"),
		UserID:           "user-1",
		Kind:             principal.KindUser,
		TokenPermissions: perms,
		Scopes:           principal.PermissionPlugins(perms),
	}
	ctx := invocation.WithInvocationSurface(context.Background(), invocation.InvocationSurfaceHTTP)

	if got := agentRunPermissions(ctx, p, "slack", []coreagent.ToolRef{{Plugin: "*"}}); got != nil {
		t.Fatalf("agentRunPermissions = %#v, want nil permissions for resolved user wildcard search", got)
	}
}

func TestSearchToolsRejectsBlankToolRefPlugin(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "search",
				Title:    "Search",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "search",
		ToolRefs: []coreagent.ToolRef{{
			Operation: "search",
		}},
	})
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("SearchTools error = %v, want %v", err, invocation.ErrProviderNotFound)
	}
}

func TestSearchToolsDiscoversSessionCatalogOperations(t *testing.T) {
	t.Parallel()

	provider := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "docs",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
					ID:       "static_search",
					Title:    "Static Search",
					ReadOnly: true,
				}}},
			},
		},
		sessionCatalog: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID:          "dynamic_search",
			Title:       "Dynamic Search",
			Description: "Search the session catalog",
			ReadOnly:    true,
		}}},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "dynamic",
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "docs" || resp.Tools[0].Target.Operation != "dynamic_search" {
		t.Fatalf("tool target = %#v, want docs.dynamic_search", resp.Tools[0].Target)
	}
}

func TestSearchToolsSkipsUnavailablePluginScopedProviders(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:       "searchIssues",
					Title:    "Search Linear Issues",
					ReadOnly: true,
				}},
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby, linear),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "Linear",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "ashby"},
			{Plugin: "linear"},
		},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1", len(resp.Tools))
	}
	if resp.Tools[0].Target.Plugin != "linear" || resp.Tools[0].Target.Operation != "searchIssues" {
		t.Fatalf("tool target = %#v, want linear.searchIssues", resp.Tools[0].Target)
	}
}

func TestSearchToolsReturnsUnavailableWhenScopedSearchHasNoCandidates(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query:    "Ashby",
		ToolRefs: []coreagent.ToolRef{{Plugin: "ashby"}},
	})
	if !errors.Is(err, invocation.ErrNoCredential) {
		t.Fatalf("SearchTools error = %v, want ErrNoCredential", err)
	}
}

func TestSearchToolsKeepsExactOperationRefsStrict(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "candidate",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "ashby",
			Operation: "candidateSearch",
		}},
	})
	if !errors.Is(err, invocation.ErrNoCredential) {
		t.Fatalf("SearchTools error = %v, want ErrNoCredential", err)
	}
}

func TestSearchToolsKeepsMixedExactOperationRefsStrict(t *testing.T) {
	t.Parallel()

	ashby := &sessionCatalogProvider{
		catalogCountingProvider: catalogCountingProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "ashby",
				ConnMode: core.ConnectionModeUser,
			},
		},
	}
	linear := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: "Linear",
				Operations: []catalog.CatalogOperation{{
					ID:       "searchIssues",
					Title:    "Search Linear Issues",
					ReadOnly: true,
				}},
			},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, ashby, linear),
		Invoker:   tokenErrorInvoker{providerName: "ashby", err: invocation.ErrNoCredential},
	})

	_, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query: "Linear",
		ToolRefs: []coreagent.ToolRef{
			{Plugin: "linear"},
			{Plugin: "ashby", Operation: "candidateSearch"},
		},
	})
	if !errors.Is(err, invocation.ErrNoCredential) {
		t.Fatalf("SearchTools error = %v, want ErrNoCredential", err)
	}
}

func TestResolveToolsReturnsEmptyWhenNoRefsDefined(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:       "search",
				Title:    "Search",
				ReadOnly: true,
			}}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("ResolveTools returned %d tools, want 0", len(tools))
	}
}

func TestResolveToolsExpandsPluginOnlyRefs(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "docs",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
				{
					ID:       "search",
					Title:    "Search",
					ReadOnly: true,
				},
				{
					ID:       "summarize",
					Title:    "Summarize",
					ReadOnly: true,
				},
			}},
		},
	}
	manager := New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		ToolRefs: []coreagent.ToolRef{{Plugin: "docs"}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("ResolveTools returned %d tools, want 2", len(tools))
	}
	if tools[0].Target.Operation != "search" || tools[1].Target.Operation != "summarize" {
		t.Fatalf("ResolveTools operations = %q, %q; want search, summarize", tools[0].Target.Operation, tools[1].Target.Operation)
	}
}

func TestResolveToolsAppliesDeclaredInvokeCredentialMode(t *testing.T) {
	t.Parallel()

	hidden := false
	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:      "events.reply",
				Title:   "Reply",
				Visible: &hidden,
			}}},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]config.PluginInvocationDependency{
			"slackbot": {{
				Plugin:         "slack",
				Operation:      "events.reply",
				CredentialMode: providermanifestv1.ConnectionModeNone,
			}},
		},
	})

	tools, err := manager.ResolveTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.ResolveToolsRequest{
		CallerPluginName: "slackbot",
		ToolRefs: []coreagent.ToolRef{{
			Plugin:    "slack",
			Operation: "events.reply",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ResolveTools returned %d tools, want 1", len(tools))
	}
	if tools[0].Target.Plugin != "slack" || tools[0].Target.Operation != "events.reply" {
		t.Fatalf("tool target = %#v, want slack.events.reply", tools[0].Target)
	}
	if tools[0].Target.CredentialMode != core.ConnectionModeNone {
		t.Fatalf("tool credential mode = %q, want %q", tools[0].Target.CredentialMode, core.ConnectionModeNone)
	}
}

func TestResolveToolsRejectsUndeclaredCredentialMode(t *testing.T) {
	t.Parallel()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "slack",
			ConnMode: core.ConnectionModeUser,
			CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
				ID:    "events.reply",
				Title: "Reply",
			}}},
		},
	}
	manager := New(Config{
		Providers: testutil.NewProviderRegistry(t, provider),
		PluginInvokes: map[string][]config.PluginInvocationDependency{
			"slackbot": {{
				Plugin:         "slack",
				Operation:      "chat.postMessage",
				CredentialMode: providermanifestv1.ConnectionModeNone,
			}},
		},
	})

	for _, tc := range []struct {
		name             string
		callerPluginName string
	}{
		{name: "public request"},
		{name: "caller without matching invoke", callerPluginName: "slackbot"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manager.ResolveTools(context.Background(), &principal.Principal{
				SubjectID: principal.UserSubjectID("user-1"),
			}, coreagent.ResolveToolsRequest{
				CallerPluginName: tc.callerPluginName,
				ToolRefs: []coreagent.ToolRef{{
					Plugin:         "slack",
					Operation:      "events.reply",
					CredentialMode: core.ConnectionModeNone,
				}},
			})
			if !errors.Is(err, invocation.ErrAuthorizationDenied) {
				t.Fatalf("ResolveTools error = %v, want ErrAuthorizationDenied", err)
			}
		})
	}
}
