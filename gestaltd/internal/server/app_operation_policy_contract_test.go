package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// Two already-serving instances share only durable data. Saving through one
// instance must affect the other's discovery and invocation without rebuilding
// providers or requiring a special reset/restart request.
func TestAppOperationPermissionsHTTPContractAcrossInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	services := testutil.NewStubServices(t)
	const app = conformanceMountedApp
	authorization := newConformanceAuthorization(t, conformanceSpec{
		ResourceTypes: conformanceModel("viewer"),
		Relationships: []*proto.Relationship{
			conformanceDirectGrant(conformanceSubject(conformanceAdminUserID), "admin", "app", app),
		},
	})
	var urls []string
	var brokers []*invocation.Broker
	for range 2 {
		provider := conformanceStubApp(app)
		provider.CatalogVal.Operations = append(provider.CatalogVal.Operations, catalog.CatalogOperation{
			ID: "items.retrieve", Method: "GET", Title: "Retrieve item", Description: "Untouched metadata",
		})
		providers := testutil.NewProviderRegistry(t, provider)
		broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials,
			invocation.WithAuthorizationProvider(authorization),
			invocation.WithAppOperationPolicies(services.AppAllowedOperations),
		)
		brokers = append(brokers, broker)
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Services = services
			cfg.Auth = conformanceAuthStub()
			cfg.Authorization = authorization
			cfg.Providers = providers
			cfg.Invoker = broker
			cfg.OperationAccessChecker = broker
			// nil AllowedOperations deliberately means an unrestricted catalog.
			cfg.AppDefs = map[string]*config.ProviderEntry{app: {}}
		})
		t.Cleanup(ts.Close)
		urls = append(urls, ts.URL)
	}
	adminPath := "/api/v1/apps/" + app + "/admin/allowed-operations"
	request := func(instance int, method, path, token, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, urls[instance]+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s instance %d = %d, want %d: %s", method, path, instance, resp.StatusCode, want, data)
		}
		return data
	}
	rows := func(instance int) map[string][]string {
		t.Helper()
		data := request(instance, "GET", adminPath, conformanceAdminToken, "", 200)
		var response struct {
			Operations []struct {
				ID           string
				AllowedRoles []string
			}
		}
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		result := make(map[string][]string)
		for _, operation := range response.Operations {
			result[operation.ID] = operation.AllowedRoles
		}
		return result
	}
	assertInvoke := func(instance int, user, operation string, allowed bool) {
		t.Helper()
		p := &principal.Principal{Kind: principal.KindUser, UserID: user, SubjectID: principal.UserSubjectID(user)}
		result, err := brokers[instance].Invoke(ctx, p, app, "", operation, nil)
		if allowed {
			if err != nil || result.Status != 200 || !strings.Contains(string(result.Body), operation) {
				t.Fatalf("invoke %s instance %d = %+v, %v", operation, instance, result, err)
			}
		} else if !errors.Is(err, invocation.ErrAuthorizationDenied) {
			t.Fatalf("invoke %s instance %d = %v, want denied", operation, instance, err)
		}
	}
	if got := rows(0); len(got) != 2 {
		t.Fatalf("unrestricted admin catalog = %v", got)
	}
	request(0, "PUT", adminPath, conformanceEmployeeToken, `{"operations":{}}`, 403)
	request(0, "PUT", adminPath, conformanceAdminToken,
		`{"operations":{"items.list":{"allowedRoles":["admin"]}}}`, 200)
	for instance := range 2 {
		got := rows(instance)
		if len(got) != 2 || len(got["items.list"]) != 1 || got["items.list"][0] != "admin" {
			t.Fatalf("saved policy catalog instance %d = %v", instance, got)
		}
		assertInvoke(instance, conformanceEmployeeUserID, "items.list", false)
		assertInvoke(instance, conformanceAdminUserID, "items.list", true)
		assertInvoke(instance, conformanceEmployeeUserID, "items.retrieve", true)
		adminCatalog := request(instance, "GET", "/api/v1/apps/"+app+"/operations", conformanceAdminToken, "", 200)
		if !strings.Contains(string(adminCatalog), `"allowedRoles":["admin"]`) {
			t.Fatalf("discovery must advertise effective role requirements: %s", adminCatalog)
		}
		data := request(instance, "GET", "/api/v1/apps/"+app+"/operations", conformanceEmployeeToken, "", 200)
		if strings.Contains(string(data), "items.list") || !strings.Contains(string(data), "Untouched metadata") {
			t.Fatalf("viewer discovery = %s", data)
		}
	}
	for _, body := range []string{
		`{"operations":{"items.list":{"allowedRoles":["viewer"],"graphql":{"document":"mutation { deleteAll }"}}}}`,
		`{"operations":{"items.list":{"allowedRoles":["viewer"],"alias":"other"}}}`,
		`{"operations":{"items.list":{"allowedRoles":[]}}}`,
		`{"operations":{"items.list":null}}`,
		`{"operations":{"missing":{"allowedRoles":["admin"]}}}`,
		`{"operations":{" items.list ":{"allowedRoles":["admin"]}}}`,
		`{"operations":{"items.list":{"allowedRoles":["viewer"]}},"removed":["items.list"]}`,
		`{"operations":{}} {}`,
	} {
		request(1, "PUT", adminPath, conformanceAdminToken, body, 400)
	}
	assertInvoke(0, conformanceEmployeeUserID, "items.list", false)
	request(1, "PUT", adminPath, conformanceAdminToken, `{"operations":{},"removed":["items.retrieve"]}`, 200)
	request(0, "PUT", adminPath, conformanceAdminToken, `{"operations":{}}`, 200)
	for instance := range 2 {
		if got := rows(instance); len(got) != 1 {
			t.Fatalf("removed operation returned by admin catalog: %v", got)
		}
		assertInvoke(instance, conformanceAdminUserID, "items.retrieve", false)
		assertInvoke(instance, conformanceEmployeeUserID, "items.list", false)
	}
	request(0, "PUT", adminPath, conformanceAdminToken,
		`{"operations":{"items.retrieve":{"allowedRoles":["viewer","admin"]}}}`, 200)
	assertInvoke(1, conformanceEmployeeUserID, "items.retrieve", true)

	accessPath := "/api/v1/apps/" + app + "/access"
	request(1, "GET", accessPath, conformanceAdminToken, "", 200)
	for _, body := range []string{`{`, `{"enabledOperations":["missing"]}`} {
		request(1, "PUT", accessPath, conformanceAdminToken, body, 400)
	}
	savedAccess := request(0, "PUT", accessPath, conformanceAdminToken,
		`{"enabledOperations":["items.retrieve"]}`, 200)
	policyRecord, err := services.DB.ObjectStore(coredata.StoreAppAllowedOperations).Get(ctx, app)
	if err != nil {
		t.Fatal(err)
	}

	// An unreadable durable record must fail closed and remain distinguishable
	// from upstream failures or permission denials on every HTTP surface.
	if err := services.DB.ObjectStore(coredata.StoreAppAllowedOperations).Put(ctx, idb.Record{
		"id": app, "app": app, "operations_json": "{", "removed_json": "[]",
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		adminPath, "/api/v1/apps/" + app + "/operations", "/api/v1/" + app + "/items.list", accessPath,
	} {
		request(1, "GET", path, conformanceAdminToken, "", 503)
	}
	request(1, "PUT", accessPath, conformanceAdminToken, `{"enabledOperations":[]}`, 503)

	// A failed save must leave the user's choices intact after recovery.
	if err := services.DB.ObjectStore(coredata.StoreAppAllowedOperations).Put(ctx, policyRecord); err != nil {
		t.Fatal(err)
	}
	if got := request(1, "GET", accessPath, conformanceAdminToken, "", 200); string(got) != string(savedAccess) {
		t.Fatalf("access settings changed during outage: got %s, want %s", got, savedAccess)
	}
}
