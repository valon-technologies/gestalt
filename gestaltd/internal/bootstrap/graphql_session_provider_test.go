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
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/graphql"
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
			Tags: []string{"profile"},
		},
	})
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
		Query         string         `json:"query"`
		OperationName string         `json:"operationName,omitempty"`
		Variables     map[string]any `json:"variables"`
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
		} else if strings.Contains(payload.Query, "viewer") {
			data = map[string]any{"viewer": map[string]any{"id": "user_123"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)

	statusDocument := `query { status }`
	recordDocument := `query Record($id: String!) { record(id: $id) { id } }`
	searchRecordsDocument := `query SearchRecords(
  "Optional record type filter."
  $recordType: RecordType
) {
  searchRecords(recordType: $recordType) {
    records { id name }
  }
}`
	viewerDocument := `query { viewer { id } }`

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
						Document: statusDocument,
					},
				},
				"record": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Document:      recordDocument,
						OperationName: "Record",
					},
				},
				"searchRecords": {
					Alias:        "records.search",
					Description:  "Search record metadata.",
					AllowedRoles: []string{"viewer"},
					Tags:         []string{"records"},
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Document:      searchRecordsDocument,
						OperationName: "SearchRecords",
					},
				},
			},
		},
		Deps{},
	)
	if err != nil {
		t.Fatalf("buildConfiguredSpecProvider: %v", err)
	}
	if core.SupportsSessionCatalog(prov) {
		t.Fatal("static GraphQL provider reports session catalog support; want static catalog only")
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

	noArgProv, _, err := buildConfiguredSpecProvider(
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
						Document: statusDocument,
					},
				},
				"viewer": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Document: viewerDocument,
					},
				},
			},
		},
		Deps{},
	)
	if err != nil {
		t.Fatalf("build no-arg provider: %v", err)
	}
	if core.SupportsSessionCatalog(noArgProv) {
		t.Fatal("no-arg static GraphQL provider reports session catalog support; want static catalog only")
	}
	result, err = noArgProv.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(no-arg status): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	result, err = noArgProv.Execute(context.Background(), "viewer", nil, "")
	if err != nil {
		t.Fatalf("Execute(no-arg viewer): %v", err)
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
			Query: "query SearchRecords($recordType: RecordType){\n" +
				"  searchRecords(recordType: $recordType){\n" +
				"    records {\n" +
				"      id\n" +
				"      name\n" +
				"    }\n" +
				"  }\n" +
				"}",
			OperationName: "SearchRecords",
			Variables:     map[string]any{"recordType": "ACTIVE"},
		},
		{
			Query:         "query Record($id: String!){\n  record(id: $id){\n    id\n  }\n}",
			OperationName: "Record",
			Variables:     map[string]any{"id": "rec_123"},
		},
		{
			Query: "{\n  status\n}",
		},
		{
			Query: "{\n  status\n}",
		},
		{Query: "{\n  viewer {\n    id\n  }\n}"},
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
			name: "invalid variable type",
			operations: map[string]*config.OperationOverride{
				"searchRecords": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Document: `query Search($recordType: RecordType!!) { searchRecords(recordType: $recordType) { records { id } } }`,
					},
				},
			},
			wantErr: `graphql.document`,
		},
		{
			name: "invalid variable name",
			operations: map[string]*config.OperationOverride{
				"searchRecords": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Document: `query Search($1record: String) { searchRecords(recordType: $1record) { records { id } } }`,
					},
				},
			},
			wantErr: `graphql.document`,
		},
		{
			name: "empty nested selection",
			operations: map[string]*config.OperationOverride{
				"viewer": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						Document: `query Viewer { user { } }`,
					},
				},
			},
			wantErr: "graphql.document",
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

func stringPtr(value string) *string {
	return &value
}
