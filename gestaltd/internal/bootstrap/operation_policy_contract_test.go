package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	manifest "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestStoredPermissionsCannotReplaceGraphQLExecutionDefinitions(t *testing.T) {
	t.Parallel()
	const document = "query Viewer { viewer { id } }"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Query, OperationName string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid query", 400)
			return
		}
		if strings.Join(strings.Fields(request.Query), " ") != document || request.OperationName != "Viewer" {
			t.Errorf("upstream received changed execution definition: %+v", request)
			http.Error(w, "unexpected query", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"user-1"}}}`))
	}))
	t.Cleanup(upstream.Close)
	services := testutil.NewStubServices(t)
	// Existing persisted records are accepted without migration, but only their
	// permission fields are meaningful. They cannot replace executable queries.
	if err := services.DB.ObjectStore(coredata.StoreAppAllowedOperations).Put(context.Background(), idb.Record{
		"id": "linear", "app": "linear",
		"operations_json": `{"viewer":{"allowedRoles":["viewer"],"alias":"wrong","graphql":{"document":"mutation { deleteAll }"}}}`,
		"removed_json":    `[]`,
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Apps: map[string]*config.ProviderEntry{
		"linear": {ResolvedManifest: &manifest.Manifest{
			Kind: manifest.KindApp, DisplayName: "Linear",
			Spec: &manifest.Spec{
				Surfaces: &manifest.ProviderSurfaces{GraphQL: &manifest.GraphQLSurface{URL: upstream.URL}},
				AllowedOperations: map[string]*manifest.ManifestOperationOverride{
					"viewer": {GraphQL: &manifest.ManifestGraphQLOperation{Document: document, OperationName: "Viewer"}},
				},
			},
		}},
	}}
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{Services: services})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })
	provider, err := providers.Get("linear")
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Execute(context.Background(), "viewer", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 200 || string(result.Body) != `{"viewer":{"id":"user-1"}}` {
		t.Fatalf("GraphQL execution = %+v", result)
	}
}
