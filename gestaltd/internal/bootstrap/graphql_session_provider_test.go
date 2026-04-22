package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/provider"
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

	base, err := provider.Build(graphql.StaticDefinition("linear", srv.URL), config.ConnectionDef{})
	if err != nil {
		t.Fatalf("provider.Build: %v", err)
	}

	wrapped := wrapGraphQLSessionCatalogProvider(base, "linear", srv.URL, nil, map[string]string{
		"viewer": "id",
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

func stringPtr(value string) *string {
	return &value
}
