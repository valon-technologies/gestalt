package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gestaltmcp "github.com/valon-technologies/gestalt/server/services/apps/mcp"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

// AUTHORIZATION CONFORMANCE SUITE (plan step G4).
//
// Every case below drives a real server surface -- mounted UI, the /apps
// catalog, app admin, the app-admin member roster, user lookup, operation
// invocation, and MCP tools/list plus tools/call -- against one shared
// authorization evaluator, so the surfaces are proven to agree with each other
// rather than each with its own stub.
//
// WHAT THE EVALUATOR IS. The production relationship-graph evaluator ships in
// the out-of-repo gestalt-providers module and cannot be built or run from this
// module in CI (see the header of conformance_evaluator_test.go for the full
// reasoning). The suite therefore runs against conformanceEvaluator, a single
// in-repo reference implementation of the documented semantics, reached through
// the single constructor newConformanceAuthorization.
//
// WHAT THIS PROVES: that every server surface asks the same question of the
// same evaluator, projects the answer the same way, honors subject sets,
// default roles, policy aliases, action-to-relation mapping and pagination
// consistently, and fails closed on cyclic, malformed, and under-specified
// provider responses.
//
// WHAT THIS DOES NOT PROVE: that the real provider implements those semantics.
// That remains an assumption until the real evaluator is exercised, which the
// plan's Gate A canary and the T2 shadow evaluation are responsible for.

const (
	conformanceEmployeeUserID = "6a1f0d4c-3e5b-4a7c-9d2e-8f0a1b2c3d04"
	conformanceAdminUserID    = "7b2e1c5d-4f6a-4b8d-8e3f-9a0b1c2d3e05"
	conformanceExternalUserID = "8c3d2e6f-5a7b-4c9e-9f4a-0b1c2d3e4f06"
	conformanceOutsiderUserID = "9d4e3f70-6b8c-4dae-8a5b-1c2d3e4f5a07"

	conformanceEmployeeToken = "conformance-employee-token"
	conformanceAdminToken    = "conformance-admin-token"
	conformanceExternalToken = "conformance-external-token"
	conformanceOutsiderToken = "conformance-outsider-token"

	// conformanceTalentPolicy is a custom authorizationPolicy alias. Apps bound
	// to it are authorized against a dedicated resource type of the same name
	// rather than against the shared "app" type.
	conformanceTalentPolicy = "talentTeamPolicy"

	conformanceMountedApp = "sampleApp"
	conformanceOtherApp   = "otherApp"
	conformanceTalentApp  = "talent-team"
	conformanceAdminApp   = "g-issues"

	// Role-gated mounts sit on their own paths, separate from the apps' static
	// mounts, so both mount shapes can be exercised on one server.
	conformanceGatedSamplePath = "/gated-sample"
	conformanceGatedTalentPath = "/gated-talent"

	conformanceGroup       = "engineering"
	conformanceNestedGroup = "engineering-leads"

	conformanceListOperation = "items.list"

	conformancePublicBaseURL = "https://gestalt.test"
)

func conformanceSubject(userID string) string { return principal.UserSubjectID(userID) }

// conformanceModel is the active authorization model every case shares. The
// wildcard action mirrors the model the plan keeps in production:
//
//	actions:
//	  "*":
//	    relations: [viewer, user, editor, admin]
//
// appDefaultRole is the "app" resource type's defaultRole, which is what lets an
// employee through with no relationship of their own. Passing "" models the
// post-Stack-B world where defaultRole has been removed.
func conformanceModel(appDefaultRole string) []conformanceResourceType {
	return []conformanceResourceType{
		{
			Name:        "app",
			DefaultRole: appDefaultRole,
			Actions: []conformanceAction{
				{Name: conformanceWildcardAction, Relations: []string{"viewer", "user", "editor", "admin"}},
			},
		},
		{
			// A dedicated resource type for the custom policy alias. It
			// deliberately declares a different relation set than "app", so a
			// grant on the wrong type cannot accidentally satisfy it.
			Name: conformanceTalentPolicy,
			Actions: []conformanceAction{
				{Name: conformanceWildcardAction, Relations: []string{"viewer", "admin"}},
			},
		},
		{
			Name: testUserLookupResource,
			Actions: []conformanceAction{
				{Name: conformanceWildcardAction, Relations: []string{testUserLookupRole}},
			},
		},
	}
}

// conformanceHarness is one running server plus the evaluator behind it.
type conformanceHarness struct {
	ts       *httptest.Server
	authz    *conformanceEvaluator
	services *coredata.Services
}

// conformanceConfig describes the server one case needs.
type conformanceConfig struct {
	spec conformanceSpec
	// authorization overrides the evaluator entirely. Only the fail-closed
	// provider-behavior cases set it.
	authorization core.AuthorizationProvider
	// withInvocation wires an authorization-aware broker plus the two surfaces
	// that invoke through it: MCP and the public REST operation endpoint.
	withInvocation bool
}

// newConformanceHarness starts a server whose every authorization boundary is
// answered by one evaluator. The mounts, apps, policy aliases and MCP tools are
// identical in every case so the surfaces are directly comparable.
func newConformanceHarness(t *testing.T, cfg conformanceConfig) *conformanceHarness {
	t.Helper()

	authz := newConformanceAuthorization(t, cfg.spec)
	var authorization core.AuthorizationProvider = authz
	if cfg.authorization != nil {
		authorization = cfg.authorization
	}

	services := testutil.NewStubServices(t)
	providers := testutil.NewProviderRegistry(t,
		conformanceStubApp(conformanceMountedApp),
		conformanceStubApp(conformanceOtherApp),
	)
	policies := map[string]string{conformanceTalentApp: conformanceTalentPolicy}

	// The catalog reports an app's mounted path from its declared static mount,
	// so the listed apps are configured the way production configures them.
	staticRoot := t.TempDir()
	writeTestUIAsset(t, filepath.Join(staticRoot, "index.html"), "<html>conformance</html>")
	staticApp := func(mount, policy string) *config.ProviderEntry {
		return &config.ProviderEntry{
			Static:              &config.AppStaticConfig{Mount: mount},
			ResolvedStaticRoot:  staticRoot,
			AuthorizationPolicy: policy,
		}
	}
	appDefs := map[string]*config.ProviderEntry{
		conformanceMountedApp: staticApp("/sample", ""),
		conformanceOtherApp:   staticApp("/other", ""),
		conformanceTalentApp:  staticApp("/talent", conformanceTalentPolicy),
		conformanceAdminApp:   {},
	}

	ts := newTestServer(t, func(sc *server.Config) {
		sc.Auth = conformanceAuthStub()
		sc.Authorization = authorization
		sc.Services = services
		sc.Providers = providers
		sc.AppDefs = appDefs
		sc.AuthorizationPolicies = policies
		// Role-gated mounts live on their own paths so the catalog keeps using
		// the static mounts above. AllowedRoles is what forces the evaluator to
		// name the relation that authorized the request.
		sc.MountedUIs = []server.MountedUI{
			conformanceMount(conformanceMountedApp, conformanceGatedSamplePath, ""),
			conformanceMount(conformanceTalentApp, conformanceGatedTalentPath, conformanceTalentPolicy),
		}
		if cfg.withInvocation {
			broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials,
				invocation.WithAuthorizationProvider(authorization),
				invocation.WithProviderKinds(testProviderKindsFromAppDefs(appDefs)),
				invocation.WithAuthorizationPolicies(policies),
			)
			sc.Invoker = broker

			// The public REST operation endpoint invokes through the same
			// broker, so an HTTP operation call and an MCP tool call reach the
			// same evaluator decision.
			generated, err := publicrpc.NewGeneratedRegistry()
			if err != nil {
				t.Fatalf("NewGeneratedRegistry: %v", err)
			}
			transport := providergateway.NewProviderGatewayTransport()
			transport.SetIdentityProvider(sc.Auth)
			transport.SetPublicMethods(generated)
			transport.SetAuthorizationProvider(authorization)
			transport.SetPublicBaseURL(conformancePublicBaseURL)
			sc.PublicBaseURL = conformancePublicBaseURL
			sc.PublicGatewayTransport = transport

			sc.MCPHandler = gestaltmcp.NewStatelessHTTPHandler(gestaltmcp.Config{
				Invoker:          broker,
				TokenResolver:    broker,
				OperationAccess:  broker,
				Providers:        providers,
				AllowedProviders: []string{conformanceMountedApp},
				IncludeREST:      map[string]bool{conformanceMountedApp: true},
			})
		}
	})
	testutil.CloseOnCleanup(t, ts)

	return &conformanceHarness{ts: ts, authz: authz, services: services}
}

// conformanceMount is a role-gated mounted UI. AllowedRoles is what forces the
// evaluator to name the relation that authorized the request instead of merely
// answering "allowed".
func conformanceMount(appName, path, policy string) server.MountedUI {
	return server.MountedUI{
		Name:                "conformance:" + appName,
		Path:                path,
		AppName:             appName,
		AuthorizationPolicy: policy,
		AppLevelAuth:        true,
		AllowedRoles:        []string{"viewer", "admin"},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(appName + "-shell"))
		}),
	}
}

func conformanceStubApp(name string) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        name,
		DN:       name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: name,
			Operations: []catalog.CatalogOperation{{
				ID:          conformanceListOperation,
				Description: "List items",
				Method:      http.MethodGet,
				Path:        "/items",
				Transport:   catalog.TransportREST,
			}},
		},
		ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{
				Status:  http.StatusOK,
				Body:    []byte(`{"invoked":"` + op + `"}`),
				Headers: map[string][]string{"Content-Type": {"application/json"}},
			}, nil
		},
	}
}

// conformanceAuthStub maps every credential the suite uses -- browser session
// cookies, API tokens, and CLI-exchanged tokens -- onto canonical user subjects.
// A CLI exchange mints "cli:<token>", which introspects to the same subject as
// the session it was exchanged from, so the three credential shapes are
// distinguishable in the test yet identical to authorization.
func conformanceAuthStub() *coretesting.StubAuthProvider {
	subjects := map[string]string{
		conformanceEmployeeToken: conformanceSubject(conformanceEmployeeUserID),
		conformanceAdminToken:    conformanceSubject(conformanceAdminUserID),
		conformanceExternalToken: conformanceSubject(conformanceExternalUserID),
		conformanceOutsiderToken: conformanceSubject(conformanceOutsiderUserID),
	}
	resolve := func(token string) (string, bool) {
		token = strings.TrimSpace(token)
		if subject, ok := subjects[token]; ok {
			return subject, true
		}
		if exchanged, ok := strings.CutPrefix(token, "cli:"); ok {
			subject, ok := subjects[exchanged]
			return subject, ok
		}
		return "", false
	}

	stub := testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
		subject, ok := resolve(token)
		if !ok {
			return &core.IntrospectResponse{Active: false}, nil
		}
		return testIntrospectActive(subject, ""), nil
	})
	stub.TokenFn = func(_ context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
		if req == nil || strings.TrimSpace(req.GrantType) != core.GrantTypeTokenExchange {
			return nil, fmt.Errorf("unsupported grant_type")
		}
		subjectToken := strings.TrimSpace(req.SubjectToken)
		if _, ok := resolve(subjectToken); !ok {
			return nil, fmt.Errorf("inactive subject token")
		}
		return &core.TokenResponse{
			AccessToken: "cli:" + subjectToken,
			TokenType:   "Bearer",
			ExpiresIn:   int(30 * 24 * time.Hour / time.Second),
			GrantID:     "conformance-grant-" + subjectToken,
			Scope:       strings.TrimSpace(req.Scope),
		}, nil
	}
	return stub
}

// --- request helpers -------------------------------------------------------

// conformanceCredential is how a request authenticates. Exactly one of cookie
// or bearer is set, which is what makes the browser and token surfaces
// comparable on identical requests.
type conformanceCredential struct {
	bearer string
	cookie string
}

func conformanceBearer(token string) conformanceCredential {
	return conformanceCredential{bearer: token}
}
func conformanceCookie(token string) conformanceCredential {
	return conformanceCredential{cookie: token}
}

func (h *conformanceHarness) do(t *testing.T, method, path string, cred conformanceCredential, body io.Reader) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, h.ts.URL+path, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	switch {
	case cred.bearer != "":
		req.Header.Set("Authorization", "Bearer "+cred.bearer)
	case cred.cookie != "":
		req.AddCookie(&http.Cookie{Name: "session_token", Value: cred.cookie})
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(raw)
}

func (h *conformanceHarness) get(t *testing.T, path string, cred conformanceCredential) (int, string) {
	t.Helper()
	return h.do(t, http.MethodGet, path, cred, nil)
}

// mountedUIWithin asserts the mounted UI answers at all within the deadline.
// It is how the cyclic and malformed cases prove the evaluator stays bounded:
// a traversal that never terminates shows up as a client timeout rather than as
// a hung test binary.
func (h *conformanceHarness) mountedUIWithin(
	t *testing.T, path string, cred conformanceCredential, deadline time.Duration,
) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.ts.URL+path+"/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cred.bearer)
	client := &http.Client{Timeout: deadline}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("mounted UI did not answer within %s: %v", deadline, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(raw)
}

// mountedUI is the browser-facing mounted app shell for one app.
func (h *conformanceHarness) mountedUI(t *testing.T, path string, cred conformanceCredential) (int, string) {
	t.Helper()
	return h.get(t, path+"/", cred)
}

// mountedPaths reads the /apps catalog and returns each app's mounted path.
// An app the caller may not open is listed with an empty mounted path.
func (h *conformanceHarness) mountedPaths(t *testing.T, cred conformanceCredential) map[string]string {
	t.Helper()
	status, body := h.get(t, "/api/v1/apps", cred)
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/apps status = %d: %s", status, body)
	}
	var listed []listedIntegration
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatalf("decode /apps: %v (body=%s)", err, body)
	}
	out := make(map[string]string, len(listed))
	for _, integration := range listed {
		out[integration.Name] = integration.MountedPath
	}
	return out
}

// members reads an app's admin member roster, which is both the app-admin gate
// and the paginated relationship read.
func (h *conformanceHarness) members(t *testing.T, appName string, cred conformanceCredential) (int, []memberEmailRow) {
	t.Helper()
	status, body := h.get(t, "/api/v1/apps/"+appName+"/admin/members", cred)
	if status != http.StatusOK {
		return status, nil
	}
	var rows []memberEmailRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode members: %v (body=%s)", err, body)
	}
	return status, rows
}

// mcp issues one JSON-RPC call against the mounted MCP endpoint.
func (h *conformanceHarness) mcp(t *testing.T, cred conformanceCredential, payload map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal mcp payload: %v", err)
	}
	status, body := h.do(t, http.MethodPost, "/mcp", cred, bytes.NewReader(encoded))
	if status != http.StatusOK {
		t.Fatalf("POST /mcp status = %d: %s", status, body)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode mcp response: %v (body=%s)", err, body)
	}
	return envelope
}

// mcpToolNames lists the tools the caller may see.
func (h *conformanceHarness) mcpToolNames(t *testing.T, cred conformanceCredential) []string {
	t.Helper()
	envelope := h.mcp(t, cred, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{},
	})
	if rpcErr, ok := envelope["error"]; ok {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	result, _ := envelope["result"].(map[string]any)
	rawTools, _ := result["tools"].([]any)
	names := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		tool, _ := raw.(map[string]any)
		if name, ok := tool["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func requireWorkspaceFrontDoor(t *testing.T, names []string) {
	t.Helper()
	want := gestaltmcp.WorkspaceFrontDoorToolNames()
	if !slices.Equal(names, want) {
		t.Fatalf("tools/list = %v, want workspace front door %v", names, want)
	}
}

func conformanceFlattenedListTool() string {
	return conformanceMountedApp + "_items_list"
}

func (h *conformanceHarness) mcpSearchOperations(t *testing.T, cred conformanceCredential, query, app string) []string {
	t.Helper()
	args := map[string]any{}
	if query != "" {
		args["query"] = query
	}
	if app != "" {
		args["app"] = app
	}
	envelope := h.mcp(t, cred, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": gestaltmcp.SearchToolName, "arguments": args},
	})
	if rpcErr, ok := envelope["error"]; ok {
		t.Fatalf("gestalt_search error: %v", rpcErr)
	}
	result, _ := envelope["result"].(map[string]any)
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("gestalt_search tool error: %v", result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return nil
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	var body struct {
		Results []struct {
			Operation string `json:"operation"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("decode gestalt_search: %v body=%s", err, text)
	}
	names := make([]string, 0, len(body.Results))
	for _, hit := range body.Results {
		names = append(names, hit.Operation)
	}
	return names
}

// mcpCallSucceeds reports whether tools/call for the list operation ran. A
// denied call comes back as a JSON-RPC error or an isError result.
func (h *conformanceHarness) mcpCallSucceeds(t *testing.T, cred conformanceCredential, toolName string) bool {
	t.Helper()
	envelope := h.mcp(t, cred, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": toolName, "arguments": map[string]any{}},
	})
	if _, ok := envelope["error"]; ok {
		return false
	}
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		return false
	}
	isError, _ := result["isError"].(bool)
	return !isError
}

// invokeOperation calls one app operation over the public REST surface, which
// is the non-MCP operation-invocation path. It reports whether the call was
// authorized.
func (h *conformanceHarness) invokeOperation(t *testing.T, appName string, cred conformanceCredential) bool {
	t.Helper()
	status, body := h.do(t, http.MethodPost,
		"/api/v2/app/"+appName+"/operations/"+conformanceListOperation,
		cred, strings.NewReader(`{"params":{}}`))
	switch status {
	case http.StatusOK:
		return true
	case http.StatusForbidden, http.StatusUnauthorized:
		return false
	default:
		t.Fatalf("POST operation status = %d: %s", status, body)
		return false
	}
}

// --- grant shapes ----------------------------------------------------------

// TestConformanceGrantShapes covers the grant topologies the plan enumerates.
// Every case asks the same role-gated mounted UI the same question, so the only
// variable is the shape of the relationship graph behind the answer.
func TestConformanceGrantShapes(t *testing.T) {
	t.Parallel()

	subject := conformanceSubject(conformanceExternalUserID)
	cred := conformanceBearer(conformanceExternalToken)

	cases := []struct {
		name          string
		relationships []*proto.Relationship
		wantStatus    int
	}{
		{
			name:          "direct grant",
			relationships: []*proto.Relationship{conformanceDirectGrant(subject, "viewer", "app", conformanceMountedApp)},
			wantStatus:    http.StatusOK,
		},
		{
			name: "one level subject set",
			relationships: []*proto.Relationship{
				conformanceGroupMember(subject, conformanceGroup),
				conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "nested subject set",
			relationships: []*proto.Relationship{
				conformanceGroupMember(subject, conformanceNestedGroup),
				conformanceNestedGroupMember(conformanceNestedGroup, conformanceGroup),
				conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "grant outside the mount's allowed roles",
			relationships: []*proto.Relationship{
				conformanceGroupMember(subject, conformanceGroup),
				conformanceGroupGrant(conformanceGroup, "editor", "app", conformanceMountedApp),
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:          "no grant at all",
			relationships: nil,
			wantStatus:    http.StatusForbidden,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
				ResourceTypes: conformanceModel(""),
				Relationships: testCase.relationships,
			}})
			status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred)
			if status != testCase.wantStatus {
				t.Fatalf("mounted UI status = %d, want %d: %s", status, testCase.wantStatus, body)
			}
		})
	}
}

// TestConformanceCyclicGrantsFailClosedAndStayBounded asserts the property that
// matters for a cyclic graph: the request terminates and denies. It does not
// assert a particular traversal strategy.
func TestConformanceCyclicGrantsFailClosedAndStayBounded(t *testing.T) {
	t.Parallel()

	// Two groups whose membership is defined entirely in terms of each other,
	// plus a self-referential third. No subject is a member of any of them.
	harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
		Relationships: []*proto.Relationship{
			conformanceNestedGroupMember(conformanceGroup, conformanceNestedGroup),
			conformanceNestedGroupMember(conformanceNestedGroup, conformanceGroup),
			conformanceNestedGroupMember("self-loop", "self-loop"),
			conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			conformanceGroupGrant("self-loop", "viewer", "app", conformanceMountedApp),
			// A direct grant to a different subject, so the resource is not
			// trivially empty.
			conformanceDirectGrant(conformanceSubject(conformanceAdminUserID), "viewer", "app", conformanceMountedApp),
		},
	}})

	status, body := harness.mountedUIWithin(
		t, conformanceGatedSamplePath, conformanceBearer(conformanceExternalToken), 30*time.Second,
	)
	if status != http.StatusForbidden {
		t.Fatalf("cyclic graph status = %d, want 403: %s", status, body)
	}

	// The granted subject still gets in, proving the cycle did not poison the
	// whole resource.
	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, conformanceBearer(conformanceAdminToken)); status != http.StatusOK {
		t.Fatalf("direct grant holder status = %d, want 200: %s", status, body)
	}

	// Termination came from cycle detection, not from the traversal budget's
	// emergency brake. If this ever flips, the cycle handling regressed even
	// though the denial above still looks correct.
	if _, _, _, budgetExhausted := harness.authz.counters(); budgetExhausted {
		t.Fatal("cycle terminated only by exhausting the traversal budget")
	}
}

// TestConformanceCatalogAsksOneBatchedQuestionPerListing proves the catalog
// reaches its per-app decisions through one batched evaluator call rather than
// one call per app, which is what keeps listing and invocation on the same
// decision path without an N-round-trip cost.
func TestConformanceCatalogAsksOneBatchedQuestionPerListing(t *testing.T) {
	t.Parallel()

	external := conformanceSubject(conformanceExternalUserID)
	harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
		Relationships: []*proto.Relationship{
			conformanceGroupMember(external, conformanceGroup),
			conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceOtherApp),
		},
	}})
	cred := conformanceBearer(conformanceExternalToken)

	paths := harness.mountedPaths(t, cred)
	for _, app := range []string{conformanceMountedApp, conformanceOtherApp} {
		if paths[app] == "" {
			t.Fatalf("group-granted app %q lost its mounted path", app)
		}
	}
	single, batched, _, _ := harness.authz.counters()
	if batched == 0 {
		t.Fatal("catalog listing made no batched evaluator call")
	}
	if single > batched {
		t.Fatalf("catalog listing made %d per-item calls against %d batched calls", single, batched)
	}
}

// TestConformanceMalformedGrantsFailClosed feeds the evaluator tuples a real
// provider could persist but cannot interpret. Every one must be ignored rather
// than trusted, and none may crash the request.
func TestConformanceMalformedGrantsFailClosed(t *testing.T) {
	t.Parallel()

	subject := conformanceSubject(conformanceExternalUserID)
	resource := &proto.Resource{Type: "app", Id: conformanceMountedApp}

	malformed := []*proto.Relationship{
		// No tuple at all.
		{},
		// Tuple with no target.
		{Tuple: &proto.RelationshipTuple{Relation: "viewer", Resource: resource}},
		// Target with neither a subject nor a subject set.
		{Tuple: &proto.RelationshipTuple{Target: &proto.RelationshipTarget{}, Relation: "viewer", Resource: resource}},
		// Subject set with no resource.
		{Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{
				SubjectSet: &proto.SubjectSet{Relation: "member"},
			}},
			Relation: "viewer",
			Resource: resource,
		}},
		// Subject set with a blank relation.
		{Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{
				SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: "group", Id: conformanceGroup}},
			}},
			Relation: "viewer",
			Resource: resource,
		}},
		// Blank relation on the grant itself.
		{Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{Type: "subject", Id: subject},
			}},
			Resource: resource,
		}},
		// Grant with no resource.
		{Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{Type: "subject", Id: subject},
			}},
			Relation: "viewer",
		}},
		// A grant naming a relation the model's action does not allow.
		conformanceDirectGrant(subject, "not-a-modeled-relation", "app", conformanceMountedApp),
		// A grant on a resource type the model does not declare.
		conformanceDirectGrant(subject, "viewer", "undeclaredType", conformanceMountedApp),
	}

	harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
		Relationships: malformed,
	}})

	status, body := harness.mountedUIWithin(
		t, conformanceGatedSamplePath, conformanceBearer(conformanceExternalToken), 30*time.Second,
	)
	if status != http.StatusForbidden {
		t.Fatalf("malformed grants status = %d, want 403: %s", status, body)
	}
}

// TestConformancePaginatedGrantsAreReadWhole proves the app-admin roster follows
// next_page_token: the evaluator hands back one grant per page, so a surface
// that stopped after the first page would report one member instead of all.
func TestConformancePaginatedGrantsAreReadWhole(t *testing.T) {
	t.Parallel()

	admin := conformanceSubject(conformanceAdminUserID)
	relationships := []*proto.Relationship{
		conformanceDirectGrant(admin, "admin", "app", conformanceAdminApp),
	}
	const extraMembers = 6
	for i := range extraMembers {
		relationships = append(relationships, conformanceDirectGrant(
			fmt.Sprintf("user:conformance-page-%d", i), "viewer", "app", conformanceAdminApp,
		))
	}

	harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
		Relationships: relationships,
		ListPageSize:  1,
	}})

	status, rows := harness.members(t, conformanceAdminApp, conformanceBearer(conformanceAdminToken))
	if status != http.StatusOK {
		t.Fatalf("members status = %d, want 200", status)
	}
	if len(rows) != len(relationships) {
		t.Fatalf("members = %d rows, want %d; pagination was not followed", len(rows), len(relationships))
	}
	_, _, listCalls, _ := harness.authz.counters()
	if listCalls < len(relationships) {
		t.Fatalf("ListRelationships calls = %d, want at least %d pages", listCalls, len(relationships))
	}
}

// --- roles, policies, and resource types -----------------------------------

// TestConformanceGroupDerivedAdmin proves app administration is reachable purely
// through group membership, on both the app-admin gate and the role-gated mount.
func TestConformanceGroupDerivedAdmin(t *testing.T) {
	t.Parallel()

	admin := conformanceSubject(conformanceAdminUserID)
	harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
		Relationships: []*proto.Relationship{
			conformanceGroupMember(admin, conformanceNestedGroup),
			conformanceNestedGroupMember(conformanceNestedGroup, conformanceGroup),
			conformanceGroupGrant(conformanceGroup, "admin", "app", conformanceAdminApp),
			conformanceGroupGrant(conformanceGroup, "admin", "app", conformanceMountedApp),
		},
	}})

	cred := conformanceBearer(conformanceAdminToken)
	if status, _ := harness.members(t, conformanceAdminApp, cred); status != http.StatusOK {
		t.Fatalf("app admin status = %d, want 200", status)
	}
	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("mounted UI status = %d, want 200: %s", status, body)
	}
}

// TestConformancePolicyAliasUsesDedicatedResourceType proves a custom
// authorizationPolicy redirects the question to its own resource type: a grant
// on the alias opens the app, and the same grant on the shared "app" type does
// not.
func TestConformancePolicyAliasUsesDedicatedResourceType(t *testing.T) {
	t.Parallel()

	subject := conformanceSubject(conformanceExternalUserID)
	cred := conformanceBearer(conformanceExternalToken)

	t.Run("grant on the alias resource type allows", func(t *testing.T) {
		t.Parallel()

		harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceGroupMember(subject, conformanceGroup),
				conformanceGroupGrant(conformanceGroup, "viewer", conformanceTalentPolicy, conformanceTalentPolicy),
			},
		}})
		if status, body := harness.mountedUI(t, conformanceGatedTalentPath, cred); status != http.StatusOK {
			t.Fatalf("policy-aliased mount status = %d, want 200: %s", status, body)
		}
	})

	t.Run("grant on the shared app type does not leak into the alias", func(t *testing.T) {
		t.Parallel()

		harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceDirectGrant(subject, "viewer", "app", conformanceTalentApp),
				conformanceDirectGrant(subject, "viewer", "app", conformanceMountedApp),
			},
		}})
		if status, body := harness.mountedUI(t, conformanceGatedTalentPath, cred); status != http.StatusForbidden {
			t.Fatalf("policy-aliased mount status = %d, want 403: %s", status, body)
		}
		// The same grant shape on a non-aliased app still works, so the denial
		// above is the alias mapping and not a broken grant.
		if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
			t.Fatalf("non-aliased mount status = %d, want 200: %s", status, body)
		}
	})

	t.Run("app admin resolves the app key through the policy map", func(t *testing.T) {
		t.Parallel()

		// App admin looks the resource up from the app key, so this case
		// exercises the shared app key -> policy alias -> resource mapping
		// rather than the mount's own configured policy.
		aliased := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceDirectGrant(subject, "admin", conformanceTalentPolicy, conformanceTalentPolicy),
			},
		}})
		if status, _ := aliased.members(t, conformanceTalentApp, cred); status != http.StatusOK {
			t.Fatalf("policy-aliased app admin status = %d, want 200", status)
		}

		unaliased := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceDirectGrant(subject, "admin", "app", conformanceTalentApp),
			},
		}})
		if status, _ := unaliased.members(t, conformanceTalentApp, cred); status != http.StatusForbidden {
			t.Fatalf("admin on the unaliased app resource status = %d, want 403", status)
		}
	})

	t.Run("alias resource type enforces its own relation set", func(t *testing.T) {
		t.Parallel()

		// "editor" authorizes the shared "app" type but is not a relation the
		// talent policy's action accepts.
		harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceDirectGrant(subject, "editor", conformanceTalentPolicy, conformanceTalentPolicy),
			},
		}})
		if status, body := harness.mountedUI(t, conformanceGatedTalentPath, cred); status != http.StatusForbidden {
			t.Fatalf("policy-aliased mount status = %d, want 403: %s", status, body)
		}
	})
}

// --- population matrix ------------------------------------------------------

// TestConformanceEmployeeUsesDefaultRole is the current production behavior:
// under a model whose "app" type carries a defaultRole, an employee with no
// relationship of their own reaches the app, on every surface.
func TestConformanceEmployeeUsesDefaultRole(t *testing.T) {
	t.Parallel()

	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel("viewer"),
		},
	})
	cred := conformanceBearer(conformanceEmployeeToken)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("mounted UI status = %d, want 200: %s", status, body)
	}
	if got := harness.mountedPaths(t, cred)[conformanceMountedApp]; got == "" {
		t.Fatal("defaultRole employee lost the mounted path in /apps")
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
	if !harness.mcpCallSucceeds(t, cred, conformanceFlattenedListTool()) {
		t.Fatal("defaultRole employee could not call an MCP tool")
	}
	if !harness.invokeOperation(t, conformanceMountedApp, cred) {
		t.Fatal("defaultRole employee could not invoke an operation")
	}
}

// TestConformanceEmployeeWithoutDefaultRoleNeedsAGrant is the post-Stack-B
// world: with defaultRole removed, the same employee is denied unless a
// membership grant carries them.
func TestConformanceEmployeeWithoutDefaultRoleNeedsAGrant(t *testing.T) {
	t.Parallel()

	employee := conformanceSubject(conformanceEmployeeUserID)
	cred := conformanceBearer(conformanceEmployeeToken)

	denied := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
	}})
	if status, body := denied.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusForbidden {
		t.Fatalf("no-default employee status = %d, want 403: %s", status, body)
	}

	granted := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
		ResourceTypes: conformanceModel(""),
		Relationships: []*proto.Relationship{
			conformanceGroupMember(employee, conformanceGroup),
			conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
		},
	}})
	if status, body := granted.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("membership-granted employee status = %d, want 200: %s", status, body)
	}
}

// TestConformanceElevatedAdminPassesEveryGate proves an admin relation satisfies
// both the viewer-or-admin mount and the admin-only app-admin gate through one
// evaluator, with no surface inventing its own role ordering.
func TestConformanceElevatedAdminPassesEveryGate(t *testing.T) {
	t.Parallel()

	admin := conformanceSubject(conformanceAdminUserID)
	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceDirectGrant(admin, "admin", "app", conformanceMountedApp),
				conformanceDirectGrant(admin, "admin", "app", conformanceAdminApp),
			},
		},
	})
	cred := conformanceBearer(conformanceAdminToken)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("mounted UI status = %d, want 200: %s", status, body)
	}
	if status, _ := harness.members(t, conformanceAdminApp, cred); status != http.StatusOK {
		t.Fatalf("app admin status = %d, want 200", status)
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
}

// TestConformanceUngrantedExternalIsDeniedEverywhere is the baseline external
// case: no relationship, no defaultRole, denied on every surface.
func TestConformanceUngrantedExternalIsDeniedEverywhere(t *testing.T) {
	t.Parallel()

	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec:           conformanceSpec{ResourceTypes: conformanceModel("")},
	})
	cred := conformanceBearer(conformanceOutsiderToken)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusForbidden {
		t.Fatalf("mounted UI status = %d, want 403: %s", status, body)
	}
	if got := harness.mountedPaths(t, cred)[conformanceMountedApp]; got != "" {
		t.Fatalf("/apps exposed mounted path %q to an ungranted external user", got)
	}
	if status, _ := harness.members(t, conformanceAdminApp, cred); status != http.StatusForbidden {
		t.Fatalf("app admin status = %d, want 403", status)
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
	if ops := harness.mcpSearchOperations(t, cred, "items", conformanceMountedApp); len(ops) != 0 {
		t.Fatalf("gestalt_search exposed %v to an ungranted external user", ops)
	}
	if harness.mcpCallSucceeds(t, cred, conformanceFlattenedListTool()) {
		t.Fatal("tools/call succeeded for an ungranted external user")
	}
	if harness.invokeOperation(t, conformanceMountedApp, cred) {
		t.Fatal("operation invocation succeeded for an ungranted external user")
	}
}

// TestConformanceGrantedExternalIsAllowedOnEverySurface is the mirror image, and
// the case that proves the surfaces agree: one runtime grant opens the mount,
// the catalog, invocation, and MCP together.
func TestConformanceGrantedExternalIsAllowedOnEverySurface(t *testing.T) {
	t.Parallel()

	external := conformanceSubject(conformanceExternalUserID)
	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceGroupMember(external, conformanceGroup),
				conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			},
		},
	})
	cred := conformanceBearer(conformanceExternalToken)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("mounted UI status = %d, want 200: %s", status, body)
	}
	paths := harness.mountedPaths(t, cred)
	if paths[conformanceMountedApp] == "" {
		t.Fatal("/apps hid the mounted path from a granted external user")
	}
	if paths[conformanceOtherApp] != "" {
		t.Fatalf("/apps exposed ungranted app %q", conformanceOtherApp)
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
	if ops := harness.mcpSearchOperations(t, cred, "items", conformanceMountedApp); len(ops) == 0 {
		t.Fatal("gestalt_search hid every operation from a granted external user")
	}
	if !harness.mcpCallSucceeds(t, cred, conformanceFlattenedListTool()) {
		t.Fatal("tools/call denied a granted external user")
	}
	if !harness.invokeOperation(t, conformanceMountedApp, cred) {
		t.Fatal("operation invocation denied a granted external user")
	}
	if harness.invokeOperation(t, conformanceOtherApp, cred) {
		t.Fatal("operation invocation succeeded on an ungranted app")
	}
}

// TestConformanceRevokedExternalIsDeniedPromptly removes a live grant and
// reasserts every surface. No surface may keep serving from a cached decision.
func TestConformanceRevokedExternalIsDeniedPromptly(t *testing.T) {
	t.Parallel()

	external := conformanceSubject(conformanceExternalUserID)
	membership := conformanceGroupMember(external, conformanceGroup)
	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				membership,
				conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			},
		},
	})
	cred := conformanceBearer(conformanceExternalToken)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("pre-revoke mounted UI status = %d, want 200: %s", status, body)
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
	toolName := conformanceFlattenedListTool()

	harness.authz.revoke(t, membership)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusForbidden {
		t.Fatalf("post-revoke mounted UI status = %d, want 403: %s", status, body)
	}
	if got := harness.mountedPaths(t, cred)[conformanceMountedApp]; got != "" {
		t.Fatalf("post-revoke /apps still exposed mounted path %q", got)
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
	if ops := harness.mcpSearchOperations(t, cred, "items", conformanceMountedApp); len(ops) != 0 {
		t.Fatalf("post-revoke gestalt_search still exposed %v", ops)
	}
	if harness.mcpCallSucceeds(t, cred, toolName) {
		t.Fatal("post-revoke tools/call still succeeded")
	}
	if harness.invokeOperation(t, conformanceMountedApp, cred) {
		t.Fatal("post-revoke operation invocation still succeeded")
	}

	// Re-granting restores access, proving the denial tracked the grant rather
	// than a one-way latch.
	harness.authz.grant(t, membership)
	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
		t.Fatalf("re-granted mounted UI status = %d, want 200: %s", status, body)
	}
}

// TestConformanceExternalAppAdminIsIsolated proves administering one app confers
// nothing on another app, and does not turn into a general viewer role.
func TestConformanceExternalAppAdminIsIsolated(t *testing.T) {
	t.Parallel()

	external := conformanceSubject(conformanceExternalUserID)
	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceDirectGrant(external, "admin", "app", conformanceAdminApp),
			},
		},
	})
	cred := conformanceBearer(conformanceExternalToken)

	if status, _ := harness.members(t, conformanceAdminApp, cred); status != http.StatusOK {
		t.Fatalf("own app admin status = %d, want 200", status)
	}
	if status, _ := harness.members(t, conformanceMountedApp, cred); status != http.StatusForbidden {
		t.Fatalf("other app admin status = %d, want 403", status)
	}
	if status, _ := harness.members(t, conformanceOtherApp, cred); status != http.StatusForbidden {
		t.Fatalf("third app admin status = %d, want 403", status)
	}
	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusForbidden {
		t.Fatalf("mounted UI of an unadministered app status = %d, want 403: %s", status, body)
	}
	if got := harness.mountedPaths(t, cred)[conformanceMountedApp]; got != "" {
		t.Fatalf("/apps exposed an unadministered app's mounted path %q", got)
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cred))
	if ops := harness.mcpSearchOperations(t, cred, "items", conformanceMountedApp); len(ops) != 0 {
		t.Fatalf("gestalt_search exposed %v to an admin of a different app", ops)
	}
}

// TestConformanceUserLookupNeedsTheEmployeeOperatorRole proves app-scoped
// administration does not become a user directory: the roster still lists the
// grants, but identities resolve only for the explicit operator role, which may
// itself be held through a group.
func TestConformanceUserLookupNeedsTheEmployeeOperatorRole(t *testing.T) {
	t.Parallel()

	admin := conformanceSubject(conformanceAdminUserID)

	rosterEmails := func(t *testing.T, extra ...*proto.Relationship) []memberEmailRow {
		t.Helper()
		harness := newConformanceHarness(t, conformanceConfig{spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
		}})
		member := seedUser(t, harness.services, "member@valon.com")
		harness.authz.grant(t, conformanceDirectGrant(admin, "admin", "app", conformanceAdminApp))
		harness.authz.grant(t, conformanceDirectGrant(
			conformanceSubject(member.ID), "viewer", "app", conformanceAdminApp,
		))
		for _, relationship := range extra {
			harness.authz.grant(t, relationship)
		}
		status, rows := harness.members(t, conformanceAdminApp, conformanceBearer(conformanceAdminToken))
		if status != http.StatusOK {
			t.Fatalf("members status = %d, want 200", status)
		}
		if len(rows) != 2 {
			t.Fatalf("members = %#v, want 2 rows", rows)
		}
		return rows
	}

	t.Run("app admin alone cannot enumerate users", func(t *testing.T) {
		t.Parallel()

		for _, row := range rosterEmails(t) {
			if row.Email != "" {
				t.Fatalf("app-scoped admin resolved an email without the operator role: %#v", row)
			}
		}
	})

	t.Run("group derived operator role restores lookup", func(t *testing.T) {
		t.Parallel()

		rows := rosterEmails(t,
			conformanceGroupMember(admin, conformanceGroup),
			conformanceGroupGrant(conformanceGroup, testUserLookupRole, testUserLookupResource, testUserLookupResource),
		)
		found := false
		for _, row := range rows {
			if row.Email == "member@valon.com" {
				found = true
			}
		}
		if !found {
			t.Fatalf("operator role did not resolve member email: %#v", rows)
		}
	})
}

// --- credential surfaces ----------------------------------------------------

// TestConformanceCredentialSurfacesAgree walks one user through the browser
// session, the CLI token exchange, and the resulting API token, then through
// MCP, and requires the same authorization answer from all of them -- first
// while granted, then after the grant is revoked.
func TestConformanceCredentialSurfacesAgree(t *testing.T) {
	t.Parallel()

	external := conformanceSubject(conformanceExternalUserID)
	membership := conformanceGroupMember(external, conformanceGroup)
	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				membership,
				conformanceGroupGrant(conformanceGroup, "viewer", "app", conformanceMountedApp),
			},
		},
	})

	browser := conformanceCookie(conformanceExternalToken)
	apiToken := conformanceBearer(conformanceExternalToken)

	// The CLI exchanges the browser session for a long-lived API token through
	// the real /api/v1/tokens surface.
	status, body := harness.do(t, http.MethodPost, "/api/v1/tokens", browser,
		strings.NewReader(`{"name":"conformance-cli","scopes":"`+conformanceMountedApp+`"}`))
	if status != http.StatusCreated {
		t.Fatalf("CLI token exchange status = %d, want 201: %s", status, body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decode created token: %v (body=%s)", err, body)
	}
	if created.Token == "" {
		t.Fatalf("CLI token exchange returned no token: %s", body)
	}
	cliToken := conformanceBearer(created.Token)

	surfaces := map[string]conformanceCredential{
		"browser session": browser,
		"CLI exchanged":   cliToken,
		"API token":       apiToken,
	}
	for name, cred := range surfaces {
		if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusOK {
			t.Fatalf("%s: granted mounted UI status = %d, want 200: %s", name, status, body)
		}
		if got := harness.mountedPaths(t, cred)[conformanceMountedApp]; got == "" {
			t.Fatalf("%s: /apps hid the mounted path from a granted user", name)
		}
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cliToken))
	toolName := conformanceFlattenedListTool()
	if !harness.mcpCallSucceeds(t, cliToken, toolName) {
		t.Fatal("CLI exchanged token: tools/call denied a granted user")
	}
	for _, name := range []string{"CLI exchanged", "API token"} {
		if !harness.invokeOperation(t, conformanceMountedApp, surfaces[name]) {
			t.Fatalf("%s: operation invocation denied a granted user", name)
		}
	}

	harness.authz.revoke(t, membership)

	for name, cred := range surfaces {
		if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusForbidden {
			t.Fatalf("%s: revoked mounted UI status = %d, want 403: %s", name, status, body)
		}
		if got := harness.mountedPaths(t, cred)[conformanceMountedApp]; got != "" {
			t.Fatalf("%s: /apps still exposed mounted path %q after revoke", name, got)
		}
	}
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, cliToken))
	if ops := harness.mcpSearchOperations(t, cliToken, "items", conformanceMountedApp); len(ops) != 0 {
		t.Fatalf("CLI exchanged token: gestalt_search still exposed %v after revoke", ops)
	}
	if harness.mcpCallSucceeds(t, cliToken, toolName) {
		t.Fatal("CLI exchanged token: tools/call still succeeded after revoke")
	}
	for _, name := range []string{"CLI exchanged", "API token"} {
		if harness.invokeOperation(t, conformanceMountedApp, surfaces[name]) {
			t.Fatalf("%s: operation invocation still succeeded after revoke", name)
		}
	}
}

// TestConformanceMCPListingAndCallAgree proves the workspace front door is
// listed to every authenticated caller, while app operations stay fail-closed
// on search and invoke using the same evaluator as the REST surface.
func TestConformanceMCPListingAndCallAgree(t *testing.T) {
	t.Parallel()

	external := conformanceSubject(conformanceExternalUserID)
	harness := newConformanceHarness(t, conformanceConfig{
		withInvocation: true,
		spec: conformanceSpec{
			ResourceTypes: conformanceModel(""),
			Relationships: []*proto.Relationship{
				conformanceGroupMember(external, conformanceGroup),
				conformanceGroupGrant(conformanceGroup, "user", "app", conformanceMountedApp),
			},
		},
	})

	granted := conformanceBearer(conformanceExternalToken)
	ungranted := conformanceBearer(conformanceOutsiderToken)

	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, granted))
	requireWorkspaceFrontDoor(t, harness.mcpToolNames(t, ungranted))
	if !harness.mcpCallSucceeds(t, granted, gestaltmcp.SearchToolName) {
		t.Fatal("tools/list offered gestalt_search but tools/call denied it")
	}

	toolName := conformanceFlattenedListTool()
	if ops := harness.mcpSearchOperations(t, granted, "items", conformanceMountedApp); len(ops) == 0 {
		t.Fatal("gestalt_search hid the granted list operation")
	}
	if !harness.mcpCallSucceeds(t, granted, toolName) {
		t.Fatalf("granted caller could not invoke %q", toolName)
	}

	if ops := harness.mcpSearchOperations(t, ungranted, "items", conformanceMountedApp); len(ops) != 0 {
		t.Fatalf("gestalt_search offered %v to an ungranted caller", ops)
	}
	if harness.mcpCallSucceeds(t, ungranted, toolName) {
		t.Fatalf("tools/call ran %q for an ungranted caller", toolName)
	}
}

// --- provider-response conformance -----------------------------------------

// allowWithoutRelationsProvider is the failure mode G3a flagged: a provider that
// answers "allowed" without naming the relation that authorized it. A role-gated
// surface has nothing to check the answer against and must deny.
type allowWithoutRelationsProvider struct {
	core.AuthorizationProvider

	resourceTypes []conformanceResourceType
}

func (p *allowWithoutRelationsProvider) CheckAccess(
	_ context.Context, _ *proto.CheckAccessRequest,
) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (p *allowWithoutRelationsProvider) CheckAccessMany(
	_ context.Context, req *proto.CheckAccessManyRequest,
) (*proto.CheckAccessManyResponse, error) {
	decisions := make([]*proto.CheckAccessResponse, 0, len(req.GetRequests()))
	for range req.GetRequests() {
		decisions = append(decisions, &proto.CheckAccessResponse{Allowed: true})
	}
	return &proto.CheckAccessManyResponse{Decisions: decisions}, nil
}

func (p *allowWithoutRelationsProvider) ListRelationships(
	_ context.Context, _ *proto.ListRelationshipsRequest,
) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *allowWithoutRelationsProvider) ListActiveModelResourceTypes(
	_ context.Context, req *proto.ListActiveModelResourceTypesRequest,
) (*proto.ListActiveModelResourceTypesResponse, error) {
	name := strings.TrimSpace(req.GetFilter().GetName())
	out := []*proto.AuthorizationModelResourceType{}
	for _, resourceType := range p.resourceTypes {
		if name != "" && resourceType.Name != name {
			continue
		}
		entry := &proto.AuthorizationModelResourceType{Name: resourceType.Name, DefaultRole: resourceType.DefaultRole}
		for _, action := range resourceType.Actions {
			entry.Actions = append(entry.Actions, &proto.ModelAction{Name: action.Name, Relations: action.Relations})
		}
		out = append(out, entry)
	}
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: out}, nil
}

// TestConformanceAllowWithoutMatchedRelationsIsNotARole is the explicit guard
// for the risk G3a flagged. `allowed: true` with an empty matched_relations list
// carries no role, so it must not satisfy a role-gated mount or app admin.
func TestConformanceAllowWithoutMatchedRelationsIsNotARole(t *testing.T) {
	t.Parallel()

	harness := newConformanceHarness(t, conformanceConfig{
		authorization: &allowWithoutRelationsProvider{resourceTypes: conformanceModel("")},
		spec:          conformanceSpec{ResourceTypes: conformanceModel("")},
	})
	cred := conformanceBearer(conformanceOutsiderToken)

	if status, body := harness.mountedUI(t, conformanceGatedSamplePath, cred); status != http.StatusForbidden {
		t.Fatalf("role-gated mount status = %d, want 403 on a relation-less allow: %s", status, body)
	}
	if status, _ := harness.members(t, conformanceAdminApp, cred); status != http.StatusForbidden {
		t.Fatalf("app admin status = %d, want 403 on a relation-less allow", status)
	}
	if status, body := harness.mountedUI(t, conformanceGatedTalentPath, cred); status != http.StatusForbidden {
		t.Fatalf("policy-aliased mount status = %d, want 403 on a relation-less allow: %s", status, body)
	}
}
