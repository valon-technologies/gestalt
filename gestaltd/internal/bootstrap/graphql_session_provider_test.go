package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/graphql"
)

func TestGraphQLSessionCatalogProviderLoadsCatalogOnDemand(t *testing.T) {
	t.Parallel()

	schema := graphql.Schema{
		QueryType: &graphql.TypeName{Name: "Query"},
		Types: []graphql.FullType{
			{
				Kind: "OBJECT",
				Name: "Query",
				Fields: []graphql.Field{{
					Name: "viewer",
					Type: graphql.TypeRef{Kind: "OBJECT", Name: stringPtr("Viewer")},
				}},
			},
			{
				Kind: "OBJECT",
				Name: "Viewer",
				Fields: []graphql.Field{
					{Name: "id", Type: graphql.TypeRef{Kind: "SCALAR", Name: stringPtr("ID")}},
				},
			},
			{Kind: "SCALAR", Name: "ID"},
		},
	}

	var (
		introspectionCalls atomic.Int32
		executionCalls     atomic.Int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(payload.Query, "__schema") {
			introspectionCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"__schema": schema,
				},
			})
			return
		}
		executionCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"viewer": map[string]any{"id": "user-123"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	base, err := declarative.Build(graphql.StaticDefinition("linear", srv.URL), declarative.ConnectionDef{})
	if err != nil {
		t.Fatalf("declarative.Build: %v", err)
	}

	wrapped := wrapGraphQLSessionCatalogProvider(base, "linear", srv.URL, map[string]*config.OperationOverride{
		"viewer": {
			Tags:    []string{"profile"},
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{SelectionSet: "id"},
		},
	}, nil)
	if got := len(wrapped.Catalog().Operations); got != 0 {
		t.Fatalf("static catalog ops = %d, want 0", got)
	}
	if got := introspectionCalls.Load(); got != 0 {
		t.Fatalf("introspection calls before request = %d, want 0", got)
	}

	scp, ok := wrapped.(core.SessionCatalogProvider)
	if !ok {
		t.Fatal("expected wrapped provider to implement SessionCatalogProvider")
	}
	cat, err := scp.CatalogForRequest(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if got := introspectionCalls.Load(); got != 1 {
		t.Fatalf("introspection calls after CatalogForRequest = %d, want 1", got)
	}
	viewer, ok := graphQLCatalogOperation(cat, "viewer")
	if !ok {
		t.Fatalf("session catalog operations = %#v, want viewer", cat.Operations)
	}
	if viewer.Transport != "graphql" {
		t.Fatalf("viewer transport = %q, want %q", viewer.Transport, "graphql")
	}
	if got, want := viewer.Tags, []string{"profile"}; !slices.Equal(got, want) {
		t.Fatalf("viewer tags = %#v, want %#v", got, want)
	}

	result, err := wrapped.Execute(context.Background(), "viewer", nil, "test-token")
	if err != nil {
		t.Fatalf("Execute(viewer): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if got := introspectionCalls.Load(); got != 2 {
		t.Fatalf("introspection calls after Execute = %d, want 2", got)
	}
	if got := executionCalls.Load(); got != 1 {
		t.Fatalf("execution calls after Execute = %d, want 1", got)
	}
}

func TestConfiguredStaticGraphQLProviderSkipsSessionCatalog(t *testing.T) {
	t.Parallel()

	type graphQLRequestPayload struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}

	var (
		mu                 sync.Mutex
		seenRequests       []graphQLRequestPayload
		introspectionCalls atomic.Int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload graphQLRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.Contains(payload.Query, "__schema") {
			introspectionCalls.Add(1)
			http.Error(w, "introspection should not be requested", http.StatusTeapot)
			return
		}
		mu.Lock()
		seenRequests = append(seenRequests, payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		data := map[string]any{"searchRecords": []any{}}
		if strings.Contains(payload.Query, "record(") {
			data = map[string]any{"record": map[string]any{"id": payload.Variables["id"]}}
		} else if strings.Contains(payload.Query, "status") {
			data = map[string]any{"status": "ok"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)

	prov, _, err := buildConfiguredSpecProvider(
		context.Background(),
		"exampleGraphQL",
		config.ResolvedSpecSurface{
			Surface: config.SpecSurfaceGraphQL,
			URL:     srv.URL,
			Connection: config.ConnectionDef{
				Auth: config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeNone},
			},
		},
		providerMetadata{},
		specProviderConfig{
			allowedOperations: map[string]*config.OperationOverride{
				"status": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						OperationType: "query",
						Arguments:     graphQLTestArgs(),
					},
				},
				"record": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						OperationType: "query",
						Arguments: graphQLTestArgs(providermanifestv1.ManifestGraphQLArgument{
							Name: "id",
							Type: "String!",
						}),
						SelectionSet: "id",
					},
				},
				"searchRecords": {
					Alias:        "records.search",
					Description:  "Search record metadata.",
					AllowedRoles: []string{"viewer"},
					Tags:         []string{"records"},
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						OperationType: "query",
						Arguments: graphQLTestArgs(providermanifestv1.ManifestGraphQLArgument{
							Name:        "recordType",
							Type:        "RecordType",
							Description: "Optional record type filter.",
						}),
						SelectionSet: "records { id name }",
					},
				},
			},
		},
		Deps{},
	)
	if err != nil {
		t.Fatalf("buildConfiguredSpecProvider: %v", err)
	}
	if _, ok := prov.(core.SessionCatalogProvider); ok {
		t.Fatal("static GraphQL provider implements SessionCatalogProvider; want static catalog only")
	}
	searchOp, ok := graphQLCatalogOperation(prov.Catalog(), "records.search")
	if !ok {
		t.Fatalf("catalog operations = %#v, want records.search", prov.Catalog().Operations)
	}
	if got, want := searchOp.AllowedRoles, []string{"viewer"}; !slices.Equal(got, want) {
		t.Fatalf("records.search roles = %#v, want %#v", got, want)
	}
	if got, want := searchOp.Tags, []string{"records"}; !slices.Equal(got, want) {
		t.Fatalf("records.search tags = %#v, want %#v", got, want)
	}
	if len(searchOp.Parameters) != 1 || searchOp.Parameters[0].Name != "recordType" || searchOp.Parameters[0].Type != "string" || searchOp.Parameters[0].Required {
		t.Fatalf("records.search parameters = %#v, want optional string recordType", searchOp.Parameters)
	}
	recordOp, ok := graphQLCatalogOperation(prov.Catalog(), "record")
	if !ok {
		t.Fatalf("catalog operations = %#v, want record", prov.Catalog().Operations)
	}
	if len(recordOp.Parameters) != 1 || recordOp.Parameters[0].Name != "id" || recordOp.Parameters[0].Type != "string" || !recordOp.Parameters[0].Required {
		t.Fatalf("record parameters = %#v, want required string id", recordOp.Parameters)
	}

	result, err := prov.Execute(context.Background(), "records.search", map[string]any{"recordType": "ACTIVE"}, "")
	if err != nil {
		t.Fatalf("Execute(records.search): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	result, err = prov.Execute(context.Background(), "record", map[string]any{"id": "rec_123"}, "")
	if err != nil {
		t.Fatalf("Execute(record): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	result, err = prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if got := introspectionCalls.Load(); got != 0 {
		t.Fatalf("introspection calls = %d, want 0", got)
	}

	mu.Lock()
	gotRequests := append([]graphQLRequestPayload(nil), seenRequests...)
	mu.Unlock()
	wantRequests := []graphQLRequestPayload{
		{
			Query:     "query($recordType: RecordType) { searchRecords(recordType: $recordType) { records { id name } } }",
			Variables: map[string]any{"recordType": "ACTIVE"},
		},
		{
			Query:     "query($id: String!) { record(id: $id) { id } }",
			Variables: map[string]any{"id": "rec_123"},
		},
		{
			Query: "query { status }",
		},
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestConfiguredStaticGraphQLProviderRejectsInvalidStaticConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		operations map[string]*config.OperationOverride
		wantErr    string
	}{
		{
			name: "all graphql operations must opt in with arguments",
			operations: map[string]*config.OperationOverride{
				"status": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Arguments: graphQLTestArgs(),
					},
				},
				"viewer": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						SelectionSet: "id",
					},
				},
			},
			wantErr: `allowed operation "viewer" must set graphql.arguments`,
		},
		{
			name: "invalid argument type",
			operations: map[string]*config.OperationOverride{
				"searchRecords": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Arguments: graphQLTestArgs(providermanifestv1.ManifestGraphQLArgument{
							Name: "recordType",
							Type: "RecordType!!",
						}),
						SelectionSet: "records { id }",
					},
				},
			},
			wantErr: `unexpected input "!"`,
		},
		{
			name: "invalid argument name",
			operations: map[string]*config.OperationOverride{
				"searchRecords": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Arguments: graphQLTestArgs(providermanifestv1.ManifestGraphQLArgument{
							Name: "record-type",
							Type: "String",
						}),
						SelectionSet: "records { id }",
					},
				},
			},
			wantErr: `argument "record-type" is not a valid GraphQL name`,
		},
		{
			name: "empty nested selection",
			operations: map[string]*config.OperationOverride{
				"viewer": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Arguments:    graphQLTestArgs(),
						SelectionSet: "user { }",
					},
				},
			},
			wantErr: "empty selection set",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := buildConfiguredSpecProvider(
				context.Background(),
				"exampleGraphQL",
				config.ResolvedSpecSurface{
					Surface: config.SpecSurfaceGraphQL,
					URL:     "https://graphql.example/query",
					Connection: config.ConnectionDef{
						Auth: config.ConnectionAuthDef{Type: providermanifestv1.AuthTypeNone},
					},
				},
				providerMetadata{},
				specProviderConfig{allowedOperations: tc.operations},
				Deps{},
			)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("buildConfiguredSpecProvider error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

type graphQLPostConnectProvider struct {
	metadata map[string]string
}

func (p *graphQLPostConnectProvider) Name() string        { return "linear" }
func (p *graphQLPostConnectProvider) DisplayName() string { return "Linear" }
func (p *graphQLPostConnectProvider) Description() string { return "Linear provider" }
func (p *graphQLPostConnectProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeUser
}
func (p *graphQLPostConnectProvider) AuthTypes() []string { return []string{"oauth"} }
func (p *graphQLPostConnectProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *graphQLPostConnectProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *graphQLPostConnectProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *graphQLPostConnectProvider) ConnectionForOperation(string) string        { return "" }
func (p *graphQLPostConnectProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{Name: "linear"}
}
func (p *graphQLPostConnectProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
}
func (p *graphQLPostConnectProvider) InvokeGraphQL(_ context.Context, _ core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{"data":{}}`}, nil
}
func (p *graphQLPostConnectProvider) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/start?state=" + state
}
func (p *graphQLPostConnectProvider) ExchangeCode(_ context.Context, _ string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "access-token"}, nil
}
func (p *graphQLPostConnectProvider) RefreshToken(_ context.Context, _ string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "refreshed-token"}, nil
}
func (p *graphQLPostConnectProvider) PostConnect(_ context.Context, _ *core.ExternalCredential) (map[string]string, error) {
	return p.metadata, nil
}

func TestGraphQLSessionCatalogProviderPreservesPostConnectCapability(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"gestalt.external_identity.type": "slack_identity",
		"gestalt.external_identity.id":   "team:T123:user:U456",
	}
	wrapped := wrapGraphQLSessionCatalogProvider(&graphQLPostConnectProvider{metadata: want}, "linear", "https://example.com/graphql", nil, nil)

	if _, ok := wrapped.(core.OAuthProvider); !ok {
		t.Fatal("expected wrapped provider to preserve oauth support")
	}
	if !core.SupportsPostConnect(wrapped) {
		t.Fatal("expected wrapped provider to preserve post-connect support")
	}

	got, supported, err := core.PostConnect(context.Background(), wrapped, &core.ExternalCredential{
		Integration: "slack",
		Connection:  "default",
		AccessToken: "tok",
	})
	if err != nil {
		t.Fatalf("PostConnect: %v", err)
	}
	if !supported {
		t.Fatal("expected core.PostConnect to report support")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PostConnect metadata = %#v, want %#v", got, want)
	}
}

func stringPtr(value string) *string {
	return &value
}

func graphQLTestArgs(args ...providermanifestv1.ManifestGraphQLArgument) *[]providermanifestv1.ManifestGraphQLArgument {
	if args == nil {
		args = []providermanifestv1.ManifestGraphQLArgument{}
	}
	return &args
}
