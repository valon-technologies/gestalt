package scim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coredb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	gproto "google.golang.org/protobuf/proto"
)

const (
	testBaseURL      = "https://gestalt.example"
	testCurrentToken = "rippling-current-token"
	testNextToken    = "rippling-next-token"
)

type testListResponse struct {
	Schemas      []string    `json:"schemas"`
	TotalResults int         `json:"totalResults"`
	StartIndex   int         `json:"startIndex"`
	ItemsPerPage int         `json:"itemsPerPage"`
	Resources    []scim.User `json:"Resources"`
}

type testErrorResponse struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	SCIMType string   `json:"scimType"`
	Detail   string   `json:"detail"`
}

func testSCIMConfig(clients map[string]config.SCIMClientConfig) config.ServerSCIMConfig {
	return config.ServerSCIMConfig{
		Clients:       clients,
		RetryInterval: "5ms",
		DriftInterval: "1h",
	}
}

func ripplingClient(domains []string, relationships ...config.SCIMRelationshipConfig) config.SCIMClientConfig {
	return config.SCIMClientConfig{
		Credentials:              []config.SCIMCredentialConfig{{ID: "current", BearerToken: testCurrentToken}, {ID: "next", BearerToken: testNextToken}},
		AuthoritativeUserDomains: domains,
		ActiveUserRelationships:  relationships,
	}
}

func employeeProjection() config.SCIMRelationshipConfig {
	return config.SCIMRelationshipConfig{
		Relation: "member",
		Resource: config.AuthorizationResourceDef{Type: "group", ID: "valon-employees"},
	}
}

func newSCIMService(t *testing.T, db coredb.IndexedDB, authorization core.AuthorizationProvider, cfg config.ServerSCIMConfig, opts ...scim.ServiceOptions) (*scim.Service, *coredata.Services, http.Handler) {
	t.Helper()
	if db == nil {
		db = &coretesting.StubIndexedDB{}
	}
	services, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	var service *scim.Service
	if len(opts) == 0 {
		service, err = scim.NewService(services.DB, authorization, testBaseURL, cfg)
	} else {
		service, err = scim.NewServiceWithOptions(services.DB, authorization, testBaseURL, cfg, opts[0])
	}
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, services, scim.NewHandler(service)
}

func scimRequest(t *testing.T, handler http.Handler, method, path, token string, body any, headers ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/scim+json")
	}
	if len(headers) > 0 {
		for key, value := range headers[0] {
			req.Header.Set(key, value)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return value
}

func createUser(t *testing.T, handler http.Handler, token, userName string, active bool, extra map[string]any) (*scim.User, *httptest.ResponseRecorder) {
	t.Helper()
	payload := map[string]any{
		"schemas":  []string{scim.UserSchemaURN},
		"userName": userName,
		"active":   active,
	}
	for key, value := range extra {
		payload[key] = value
	}
	recorder := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", token, payload)
	if recorder.Code < 200 || recorder.Code >= 300 {
		return nil, recorder
	}
	user := decodeResponse[scim.User](t, recorder)
	return &user, recorder
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestSCIMUsersHTTPContract(t *testing.T) {
	t.Parallel()

	_, _, handler := newSCIMService(t, nil, nil, testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))

	for _, token := range []string{"", "wrong"} {
		response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Schemas", token, nil)
		payload := decodeResponse[testErrorResponse](t, response)
		if response.Code != http.StatusUnauthorized || response.Header().Get("Content-Type") != "application/scim+json" || payload.Status != "401" || len(payload.Schemas) != 1 || payload.Schemas[0] != scim.ErrorSchemaURN {
			t.Fatalf("unauthorized token %q = %d %#v", token, response.Code, payload)
		}
	}
	for _, token := range []string{testCurrentToken, testNextToken} {
		response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Schemas", token, nil)
		if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(scim.UserSchemaURN)) {
			t.Fatalf("Schemas with rotating token = %d %s", response.Code, response.Body.String())
		}
	}

	alicePayload := map[string]any{
		"schemas":     []string{scim.UserSchemaURN, "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"},
		"externalId":  "employee-123",
		"userName":    "Alice@Valon.com",
		"active":      true,
		"displayName": "Alice Example",
		"name":        map[string]any{"givenName": "Alice", "familyName": "Example"},
		"emails":      []map[string]any{{"value": "Alice@Valon.com", "type": "work", "primary": true}},
		"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User": map[string]any{"department": "Ignored"},
	}
	createdRecorder := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, alicePayload)
	created := decodeResponse[scim.User](t, createdRecorder)
	if createdRecorder.Code != http.StatusCreated || created.ID == "" || created.UserName != "Alice@Valon.com" || !created.Active || created.Meta.Version != `W/"1"` {
		t.Fatalf("POST = %d %#v", createdRecorder.Code, created)
	}
	if got := createdRecorder.Header().Get("Location"); got != testBaseURL+"/scim/v2/Users/"+created.ID {
		t.Fatalf("Location = %q", got)
	}
	if got := createdRecorder.Header().Get("ETag"); got != created.Meta.Version {
		t.Fatalf("ETag = %q", got)
	}

	for _, filter := range []string{
		`externalId eq "employee-123"`,
		`userName eq "alice@valon.com"`,
		`emails.value eq "alice@valon.com"`,
		`emails[type eq "work"].value eq "alice@valon.com"`,
		`userName eq "alice@valon.com" and externalId eq "employee-123"`,
	} {
		response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(filter), testCurrentToken, nil)
		list := decodeResponse[testListResponse](t, response)
		if response.Code != http.StatusOK || list.TotalResults != 1 || list.Resources[0].ID != created.ID {
			t.Fatalf("filter %q = %d %#v", filter, response.Code, list)
		}
	}
	empty := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`userName eq "random-entra-probe@invalid.example"`), testCurrentToken, nil)
	if list := decodeResponse[testListResponse](t, empty); empty.Code != http.StatusOK || list.TotalResults != 0 || list.Resources == nil {
		t.Fatalf("empty filter = %d %s", empty.Code, empty.Body.String())
	}
	unsupportedFilter := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`active eq "true"`), testCurrentToken, nil)
	if payload := decodeResponse[testErrorResponse](t, unsupportedFilter); unsupportedFilter.Code != http.StatusBadRequest || payload.Status != "400" || payload.SCIMType != "invalidValue" {
		t.Fatalf("unsupported filter = %d %#v", unsupportedFilter.Code, payload)
	}
	wrongMediaType := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{"userName": "invalid@valon.com"}, map[string]string{"Content-Type": "application/json"})
	if payload := decodeResponse[testErrorResponse](t, wrongMediaType); wrongMediaType.Code != http.StatusBadRequest || payload.Status != "400" {
		t.Fatalf("wrong media type = %d %#v", wrongMediaType.Code, payload)
	}

	bob, bobRecorder := createUser(t, handler, testCurrentToken, "bob@valon.com", true, nil)
	carol, carolRecorder := createUser(t, handler, testCurrentToken, "carol@valon.com", true, nil)
	if bobRecorder.Code != http.StatusCreated || carolRecorder.Code != http.StatusCreated {
		t.Fatalf("users without externalId = %d/%d", bobRecorder.Code, carolRecorder.Code)
	}
	page := decodeResponse[testListResponse](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?startIndex=2&count=1", testCurrentToken, nil))
	if page.TotalResults != 3 || page.StartIndex != 2 || page.ItemsPerPage != 1 {
		t.Fatalf("pagination = %#v", page)
	}
	firstPage := decodeResponse[testListResponse](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?startIndex=0&count=1", testCurrentToken, nil))
	if firstPage.TotalResults != 3 || firstPage.StartIndex != 1 || firstPage.ItemsPerPage != 1 {
		t.Fatalf("startIndex below one = %#v", firstPage)
	}
	emptyPage := decodeResponse[testListResponse](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?count=-1", testCurrentToken, nil))
	if emptyPage.TotalResults != 3 || emptyPage.StartIndex != 1 || emptyPage.ItemsPerPage != 0 || emptyPage.Resources == nil {
		t.Fatalf("negative count = %#v", emptyPage)
	}

	patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{
		{"op": "RePlAcE", "path": "active", "value": false},
		{"op": "replace", "path": `emails[type eq "work"].value`, "value": "Alice@Valon.com"},
		{"op": "replace", "value": map[string]any{"displayName": "Alice Updated"}},
	}}
	patchedRecorder := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+created.ID, testCurrentToken, patch, map[string]string{"If-Match": created.Meta.Version})
	patched := decodeResponse[scim.User](t, patchedRecorder)
	if patchedRecorder.Code != http.StatusOK || patched.Active || patched.DisplayName != "Alice Updated" || patched.Meta.Version != `W/"2"` {
		t.Fatalf("PATCH = %d %#v", patchedRecorder.Code, patched)
	}
	replayed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+created.ID, testCurrentToken, patch, map[string]string{"If-Match": created.Meta.Version})
	if replayedUser := decodeResponse[scim.User](t, replayed); replayed.Code != http.StatusOK || replayedUser.Meta.Version != patched.Meta.Version {
		t.Fatalf("replayed PATCH = %d %#v", replayed.Code, replayedUser)
	}
	differentPatch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "displayName", "value": "Different Update"}}}
	stale := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+created.ID, testCurrentToken, differentPatch, map[string]string{"If-Match": created.Meta.Version})
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("different stale PATCH = %d %s", stale.Code, stale.Body.String())
	}

	put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "externalId": "employee-123", "userName": "Alice@Valon.com", "emails": []map[string]any{{"value": "Alice@Valon.com", "type": "work", "primary": true}}}
	putRecorder := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+created.ID, testCurrentToken, put, map[string]string{"If-Match": "*"})
	if putUser := decodeResponse[scim.User](t, putRecorder); putRecorder.Code != http.StatusOK || putUser.Active {
		t.Fatalf("PUT missing active = %d %#v", putRecorder.Code, putUser)
	}
	reactivate := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "active", "value": true}}}
	reactivatedRecorder := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+created.ID, testCurrentToken, reactivate, map[string]string{"If-Match": "*"})
	if reactivated := decodeResponse[scim.User](t, reactivatedRecorder); reactivatedRecorder.Code != http.StatusOK || !reactivated.Active || reactivated.ID != created.ID {
		t.Fatalf("reactivation = %d %#v", reactivatedRecorder.Code, reactivated)
	}

	conflictingEmail := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "value": map[string]any{"emails": []map[string]any{{"value": "bob@valon.com", "type": "work", "primary": true}}}}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+created.ID, testCurrentToken, conflictingEmail); response.Code != http.StatusConflict {
		t.Fatalf("email conflict = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, alicePayload); response.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d %s", response.Code, response.Body.String())
	}

	for _, id := range []string{created.ID, bob.ID, carol.ID} {
		if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Users/"+id, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d %s", id, response.Code, response.Body.String())
		}
		if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Users/"+id, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
			t.Fatalf("replayed DELETE %s = %d %s", id, response.Code, response.Body.String())
		}
		if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+id, testCurrentToken, nil); response.Code != http.StatusNotFound {
			t.Fatalf("GET tombstone %s = %d", id, response.Code)
		}
	}
	recreatedRecorder := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, alicePayload)
	recreated := decodeResponse[scim.User](t, recreatedRecorder)
	if recreatedRecorder.Code != http.StatusCreated || recreated.ID == created.ID {
		t.Fatalf("recreated user = %d %#v", recreatedRecorder.Code, recreated)
	}
}

func TestSCIMCreateSupportsTerminalTransactionalMisses(t *testing.T) {
	t.Parallel()

	db := &transactionFaultDB{IndexedDB: &coretesting.StubIndexedDB{}}
	_, _, handler := newSCIMService(t, db, nil, testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))
	db.arm(transactionFaultTerminalMiss)

	_, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create with empty uniqueness indexes = %d %s", response.Code, response.Body.String())
	}

	_, response = createUser(t, handler, testCurrentToken, "alice@valon.com", true, map[string]any{"displayName": "Different Alice"})
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d %s", response.Code, response.Body.String())
	}
}

func TestSCIMCredentialRotationAndClientNamespaces(t *testing.T) {
	t.Parallel()

	_, _, handler := newSCIMService(t, nil, nil, testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
		"entra":    {Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}}},
	}))
	active := true
	payload := map[string]any{"schemas": []string{scim.UserSchemaURN}, "externalId": "shared", "userName": "same@example.com", "active": active}
	ripplingResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testNextToken, payload)
	entraResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", "entra-token", payload)
	ripplingUser := decodeResponse[scim.User](t, ripplingResponse)
	entraUser := decodeResponse[scim.User](t, entraResponse)
	if ripplingResponse.Code != http.StatusCreated || entraResponse.Code != http.StatusCreated || ripplingUser.ID == entraUser.ID {
		t.Fatalf("namespaced creates = %d/%d %#v/%#v", ripplingResponse.Code, entraResponse.Code, ripplingUser, entraUser)
	}
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+ripplingUser.ID, "entra-token", nil); response.Code != http.StatusNotFound {
		t.Fatalf("cross-namespace GET = %d", response.Code)
	}
}

func TestSCIMProjectionFailureRecoversAfterRestart(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	authorization := newRecordingAuthorization()
	authorization.setFailures(true, false)
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}, employeeProjection()),
	})
	service, services, handler := newSCIMService(t, db, authorization, cfg)
	_, failed := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	errorPayload := decodeResponse[testErrorResponse](t, failed)
	if failed.Code != http.StatusServiceUnavailable || failed.Header().Get("Retry-After") != "1" || errorPayload.Status != "503" {
		t.Fatalf("projection failure = %d %#v headers=%v", failed.Code, errorPayload, failed.Header())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	request := accessRequest(coreUser.ID)
	if response, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), request); err != nil || response.Allowed {
		t.Fatalf("pending projection access = %#v, %v", response, err)
	}

	authorization.setFailures(false, false)
	restarted, _, restartedHandler := newSCIMService(t, db, authorization, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	restarted.Start(ctx)
	var recovered scim.User
	eventually(t, func() bool {
		response := scimRequest(t, restartedHandler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`userName eq "alice@valon.com"`), testCurrentToken, nil)
		if response.Code != http.StatusOK {
			return false
		}
		list := decodeResponse[testListResponse](t, response)
		if list.TotalResults != 1 {
			return false
		}
		recovered = list.Resources[0]
		return true
	})
	replayed := scimRequest(t, restartedHandler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{
		"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true,
	})
	replayedUser := decodeResponse[scim.User](t, replayed)
	if replayed.Code != http.StatusCreated || replayedUser.ID != recovered.ID || replayedUser.Meta.Version != recovered.Meta.Version {
		t.Fatalf("replayed recovered create = %d %#v, want id=%q version=%q", replayed.Code, replayedUser, recovered.ID, recovered.Meta.Version)
	}
	if response, err := scim.WrapAuthorization(authorization, services.Users, restarted).CheckAccess(context.Background(), request); err != nil || !response.Allowed {
		t.Fatalf("recovered access = %#v, %v", response, err)
	}
	if relationship := authorization.relationshipForUser(coreUser.ID); relationship == nil || relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || relationship.GetProperties().AsMap()["managedBy"] != "scim" {
		t.Fatalf("projected relationship = %#v", relationship)
	}
}

func TestSCIMPatchRetryReturnsRecoveredResult(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		withIfMatch bool
	}{
		{name: "without If-Match"},
		{name: "with stale If-Match", withIfMatch: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			db := &coretesting.StubIndexedDB{}
			authorization := newRecordingAuthorization()
			cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
				"rippling": ripplingClient([]string{"valon.com"}, employeeProjection()),
			})
			service, services, handler := newSCIMService(t, db, authorization, cfg)
			user, created := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
			if created.Code != http.StatusCreated {
				t.Fatalf("create = %d %s", created.Code, created.Body.String())
			}

			authorization.setFailures(false, true)
			patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{
				{"op": "add", "path": "emails", "value": map[string]any{"value": "alias@valon.com", "type": "other"}},
				{"op": "replace", "path": "active", "value": false},
			}}
			headers := map[string]string{}
			if testCase.withIfMatch {
				headers["If-Match"] = user.Meta.Version
			}
			failed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, patch, headers)
			if failed.Code != http.StatusServiceUnavailable || failed.Header().Get("Retry-After") != "1" {
				t.Fatalf("failed PATCH = %d %s headers=%v", failed.Code, failed.Body.String(), failed.Header())
			}
			coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
			if err != nil {
				t.Fatal(err)
			}
			if access, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || access.Allowed {
				t.Fatalf("pending deactivation access = %#v, %v", access, err)
			}

			authorization.setFailures(false, false)
			restarted, _, restartedHandler := newSCIMService(t, db, authorization, cfg)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			restarted.Start(ctx)
			eventually(t, func() bool {
				response := scimRequest(t, restartedHandler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil)
				return response.Code == http.StatusOK && decodeResponse[scim.User](t, response).Meta.Version == `W/"2"`
			})

			replayed := scimRequest(t, restartedHandler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, patch, headers)
			replayedUser := decodeResponse[scim.User](t, replayed)
			if replayed.Code != http.StatusOK || replayedUser.Meta.Version != `W/"2"` || len(replayedUser.Emails) != 1 || replayedUser.Emails[0].Value != "alias@valon.com" {
				t.Fatalf("replayed recovered PATCH = %d %#v", replayed.Code, replayedUser)
			}
		})
	}
}

func TestSCIMEligibilityAllowsSafePendingUpdate(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}, employeeProjection()),
	})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	user, created := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	projected := authorization.relationshipForUser(coreUser.ID)
	if projected == nil {
		t.Fatal("active user has no projected relationship")
	}
	authorization.removeRelationship(projected.GetTuple())
	authorization.setFailures(true, false)
	patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "displayName", "value": "Alice Updated"}}}
	failed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, patch)
	if failed.Code != http.StatusServiceUnavailable || failed.Header().Get("Retry-After") != "1" {
		t.Fatalf("failed safe update = %d %s headers=%v", failed.Code, failed.Body.String(), failed.Header())
	}
	if access, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || !access.Allowed {
		t.Fatalf("safe pending update access = %#v, %v", access, err)
	}
}

func TestSCIMOmittedActiveIsFailClosed(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}, employeeProjection()),
	})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	response := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{
		"schemas": []string{scim.UserSchemaURN}, "userName": "inactive@valon.com",
	})
	user := decodeResponse[scim.User](t, response)
	if response.Code != http.StatusCreated || user.Active {
		t.Fatalf("create without active = %d %#v", response.Code, user)
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "inactive@valon.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if authorization.additionCount() != 0 {
		t.Fatalf("inactive projection additions = %d", authorization.additionCount())
	}
	if access, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || access.Allowed {
		t.Fatalf("omitted-active access = %#v, %v", access, err)
	}
}

func TestSCIMEligibilityIgnoresAnotherClientPendingIntent(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}),
		"entra": {
			Credentials:             []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}},
			ActiveUserRelationships: []config.SCIMRelationshipConfig{employeeProjection()},
		},
	})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	if _, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil); response.Code != http.StatusCreated {
		t.Fatalf("authoritative create = %d %s", response.Code, response.Body.String())
	}
	authorization.setFailures(true, false)
	if _, response := createUser(t, handler, "entra-token", "alice@valon.com", true, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-authoritative pending create = %d %s", response.Code, response.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	if response, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || !response.Allowed {
		t.Fatalf("authoritative access with unrelated intent = %#v, %v", response, err)
	}
}

func TestSCIMAuthoritativeOwnershipSurvivesEmailDomainChange(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}),
	})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	changeEmail := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "userName", "value": "alice@example.com"}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, changeEmail); response.Code != http.StatusOK {
		t.Fatalf("email change = %d %s", response.Code, response.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	gate := scim.WrapAuthorization(authorization, services.Users, service)
	if response, err := gate.CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || !response.Allowed {
		t.Fatalf("active changed-domain access = %#v, %v", response, err)
	}
	deactivate := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "active", "value": false}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate); response.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", response.Code, response.Body.String())
	}
	if response, err := gate.CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || response.Allowed {
		t.Fatalf("inactive changed-domain access = %#v, %v", response, err)
	}
}

func TestSCIMProjectionDriftConvergesThroughBackgroundController(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	authorization := newRecordingAuthorization()
	initialCfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, services, initialHandler := newSCIMService(t, db, authorization, initialCfg)
	user, response := createUser(t, initialHandler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("initial sync = %d %s", response.Code, response.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	staticRelationship := projectedRelationship(coreUser.ID, proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG)
	authorization.setRelationship(staticRelationship)

	retryTicks := make(chan time.Time, 1)
	driftTicks := make(chan time.Time, 2)
	projectedCfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil, employeeProjection())})
	projectedCfg.DriftInterval = "1h"
	service, _, handler := newSCIMService(t, db, authorization, projectedCfg, scim.ServiceOptions{NewTicker: func(interval time.Duration) (<-chan time.Time, func()) {
		if interval == 5*time.Millisecond {
			return retryTicks, func() {}
		}
		return driftTicks, func() {}
	}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.Start(ctx)
	eventually(t, func() bool { return authorization.listCallCount() > 0 })
	if authorization.additionCount() != 0 {
		t.Fatalf("SCIM replaced static relationship with %d additions", authorization.additionCount())
	}
	get := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil)
	if stored := decodeResponse[scim.User](t, get); stored.Meta.Version != `W/"1"` {
		t.Fatalf("drift changed SCIM version: %#v", stored.Meta)
	}

	authorization.removeRelationship(staticRelationship.GetTuple())
	driftTicks <- time.Now()
	eventually(t, func() bool { return authorization.additionCount() == 1 })
	deactivate := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "active", "value": false}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate); response.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", response.Code, response.Body.String())
	}
	if authorization.deletionCount() != 1 {
		t.Fatalf("owned projection deletions = %d", authorization.deletionCount())
	}
}

func TestSCIMRetryTicksDoNotRunFullDriftScan(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil, employeeProjection())})
	retryTicks := make(chan time.Time, 2)
	driftTicks := make(chan time.Time, 2)
	service, _, handler := newSCIMService(t, nil, authorization, cfg, scim.ServiceOptions{NewTicker: func(interval time.Duration) (<-chan time.Time, func()) {
		if interval == 5*time.Millisecond {
			return retryTicks, func() {}
		}
		return driftTicks, func() {}
	}})
	if _, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil); response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	baseline := authorization.listCallCount()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.Start(ctx)
	eventually(t, func() bool { return authorization.listCallCount() == baseline+1 })
	retryTicks <- time.Now()
	driftTicks <- time.Now()
	eventually(t, func() bool { return authorization.listCallCount() >= baseline+2 })
	if got := authorization.listCallCount(); got != baseline+2 {
		t.Fatalf("relationship scans after retry and drift ticks = %d, want %d", got, baseline+2)
	}
}

func TestSCIMCrossReplicaConditionalMutation(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, firstHandler := newSCIMService(t, db, nil, cfg)
	_, _, secondHandler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, firstHandler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}

	start := make(chan struct{})
	statuses := make(chan *httptest.ResponseRecorder, 2)
	for i, handler := range []http.Handler{firstHandler, secondHandler} {
		go func(worker int, handler http.Handler) {
			<-start
			patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "displayName", "value": fmt.Sprintf("worker-%d", worker)}}}
			statuses <- scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, patch, map[string]string{"If-Match": user.Meta.Version})
		}(i, handler)
	}
	close(start)
	responses := []*httptest.ResponseRecorder{<-statuses, <-statuses}
	successes := 0
	for _, mutation := range responses {
		switch mutation.Code {
		case http.StatusOK:
			successes++
		case http.StatusPreconditionFailed:
		case http.StatusServiceUnavailable:
			if mutation.Header().Get("Retry-After") != "1" {
				t.Fatalf("503 missing Retry-After: %v", mutation.Header())
			}
		case http.StatusConflict:
			t.Fatalf("replica contention was misreported as uniqueness conflict: %s", mutation.Body.String())
		default:
			t.Fatalf("unexpected mutation status %d: %s", mutation.Code, mutation.Body.String())
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent mutations = %d, responses=%d/%d", successes, responses[0].Code, responses[1].Code)
	}
	committed := decodeResponse[scim.User](t, scimRequest(t, firstHandler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if committed.Meta.Version != `W/"2"` {
		t.Fatalf("committed version = %q", committed.Meta.Version)
	}
}

func TestSCIMIntentCollisionReturnsRetryableUnavailable(t *testing.T) {
	t.Parallel()

	db := &transactionFaultDB{IndexedDB: &coretesting.StubIndexedDB{}}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	db.arm(transactionFaultIntentAdd)
	patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "displayName", "value": "Alice Updated"}}}
	response = scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, patch)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("intent collision = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
}

func TestSCIMMutationCurrentUserReadFailureReturnsRetryableUnavailable(t *testing.T) {
	t.Parallel()

	db := &transactionFaultDB{IndexedDB: &coretesting.StubIndexedDB{}}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	db.arm(transactionFaultSCIMUserGet)
	patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "displayName", "value": "Alice Updated"}}}
	response = scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, patch)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("datastore failure = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
}

func TestSCIMCreateUserLinkRaceReturnsRetryableUnavailable(t *testing.T) {
	t.Parallel()

	db := &transactionFaultDB{IndexedDB: &coretesting.StubIndexedDB{}}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, services, handler := newSCIMService(t, db, nil, cfg)
	db.arm(transactionFaultCoreUserAdd)
	_, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("user link race = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
	if _, err := services.Users.FindOrCreateUser(context.Background(), "alice@valon.com"); err != nil {
		t.Fatalf("create concurrent Gestalt user: %v", err)
	}
	_, response = createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("retry create = %d %s", response.Code, response.Body.String())
	}
}

func TestSCIMAuthorizationBoundary(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}, employeeProjection()),
	})
	service, services, handler := newSCIMService(t, db, authorization, cfg)
	if _, response := createUser(t, handler, testCurrentToken, "active@valon.com", true, nil); response.Code != http.StatusCreated {
		t.Fatalf("active create = %d %s", response.Code, response.Body.String())
	}
	if _, response := createUser(t, handler, testCurrentToken, "inactive@valon.com", false, nil); response.Code != http.StatusCreated {
		t.Fatalf("inactive create = %d %s", response.Code, response.Body.String())
	}
	active, _ := services.Users.FindUserByEmail(context.Background(), "active@valon.com")
	inactive, _ := services.Users.FindUserByEmail(context.Background(), "inactive@valon.com")
	missing, _ := services.Users.FindOrCreateUser(context.Background(), "missing@valon.com")
	exempt, _ := services.Users.FindOrCreateUser(context.Background(), "person@example.com")
	requests := []*proto.CheckAccessRequest{
		accessRequest(active.ID),
		accessRequest(inactive.ID),
		accessRequest(missing.ID),
		accessRequest(exempt.ID),
		{Subject: &proto.Subject{Type: "subject", Id: "service-account:sync"}, Action: &proto.Action{Name: "read"}, Resource: &proto.Resource{Type: "app", Id: "docs"}},
	}
	gate := scim.WrapAuthorization(authorization, services.Users, service)
	want := []bool{true, false, false, true, true}
	for i, request := range requests {
		response, err := gate.CheckAccess(context.Background(), request)
		if err != nil || response.Allowed != want[i] {
			t.Fatalf("CheckAccess[%d] = %#v, %v, want %v", i, response, err, want[i])
		}
	}
	batch, err := gate.CheckAccessMany(context.Background(), &proto.CheckAccessManyRequest{Requests: requests})
	if err != nil || len(batch.Decisions) != len(want) {
		t.Fatalf("CheckAccessMany = %#v, %v", batch, err)
	}
	for i, decision := range batch.Decisions {
		if decision.Allowed != want[i] {
			t.Fatalf("CheckAccessMany[%d] = %v, want %v", i, decision.Allowed, want[i])
		}
	}

	db.Err = errors.New("datastore unavailable")
	if response, err := gate.CheckAccess(context.Background(), accessRequest(active.ID)); err != nil || response.Allowed {
		t.Fatalf("datastore failure did not fail closed: %#v, %v", response, err)
	}
	db.Err = nil
	managed := projectedRelationship(active.ID, proto.SourceLayer_SOURCE_LAYER_RUNTIME)
	if _, err := gate.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: managed}); err == nil {
		t.Fatal("ordinary add to SCIM-managed relationship succeeded")
	}
	if _, err := gate.DeleteRelationship(context.Background(), &proto.DeleteRelationshipRequest{RelationshipTuple: managed.Tuple}); err == nil {
		t.Fatal("ordinary delete from SCIM-managed relationship succeeded")
	}
}

func TestSCIMAuthorizationGateAppliesAtInvocationBroker(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}),
	})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	var executeCalls int
	provider := &coretesting.StubIntegration{
		N:        "docs",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID: "documents.read", Method: http.MethodGet,
		}}},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			executeCalls++
			return &core.OperationResult{Status: http.StatusOK}, nil
		},
	}
	broker := invocation.NewBroker(
		testutil.NewProviderRegistry(t, provider),
		services.Users,
		services.ExternalCredentials,
		invocation.WithAuthorizationProvider(scim.WrapAuthorization(authorization, services.Users, service)),
		invocation.WithProviderKinds(map[string]invocation.ProviderKind{"docs": invocation.ProviderKindApp}),
	)
	identity := &principal.Principal{
		SubjectID: principal.UserSubjectID(coreUser.ID),
		UserID:    coreUser.ID,
		Kind:      principal.KindUser,
	}
	if _, err := broker.Invoke(context.Background(), identity, "docs", "", "documents.read", nil); err != nil {
		t.Fatalf("active Invoke: %v", err)
	}
	deactivate := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "active", "value": false}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate); response.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", response.Code, response.Body.String())
	}
	if _, err := broker.Invoke(context.Background(), identity, "docs", "", "documents.read", nil); !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("inactive Invoke error = %v, want ErrAuthorizationDenied", err)
	}
	if executeCalls != 1 {
		t.Fatalf("provider execute calls = %d, want 1", executeCalls)
	}
}

func accessRequest(coreUserID string) *proto.CheckAccessRequest {
	return &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: "user:" + coreUserID},
		Action:   &proto.Action{Name: "read"},
		Resource: &proto.Resource{Type: "app", Id: "docs"},
	}
}

type recordingAuthorization struct {
	mu          sync.Mutex
	failAdd     bool
	failDelete  bool
	listCalls   int
	additions   int
	deletions   int
	relations   map[string]*proto.Relationship
	checkCalled int
}

func newRecordingAuthorization() *recordingAuthorization {
	return &recordingAuthorization{relations: make(map[string]*proto.Relationship)}
}

func (a *recordingAuthorization) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	a.mu.Lock()
	a.checkCalled++
	a.mu.Unlock()
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (a *recordingAuthorization) CheckAccessMany(_ context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	a.mu.Lock()
	a.checkCalled += len(req.Requests)
	a.mu.Unlock()
	response := &proto.CheckAccessManyResponse{}
	for range req.Requests {
		response.Decisions = append(response.Decisions, &proto.CheckAccessResponse{Allowed: true})
	}
	return response, nil
}

func (a *recordingAuthorization) ListRelationships(_ context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listCalls++
	response := &proto.ListRelationshipsResponse{}
	for _, relationship := range a.relations {
		if req != nil && req.Filter != nil {
			if req.Filter.Target != nil && !gproto.Equal(req.Filter.Target, relationship.Tuple.Target) {
				continue
			}
			if req.Filter.Relation != "" && req.Filter.Relation != relationship.Tuple.Relation {
				continue
			}
			if req.Filter.Resource != nil && !gproto.Equal(req.Filter.Resource, relationship.Tuple.Resource) {
				continue
			}
		}
		response.Relationships = append(response.Relationships, gproto.Clone(relationship).(*proto.Relationship))
	}
	return response, nil
}

func (a *recordingAuthorization) AddRelationship(_ context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failAdd {
		return nil, errors.New("injected add failure")
	}
	a.additions++
	a.relations[relationshipKey(req.Relationship.Tuple)] = gproto.Clone(req.Relationship).(*proto.Relationship)
	return &proto.AddRelationshipResponse{Relationship: req.Relationship}, nil
}

func (a *recordingAuthorization) DeleteRelationship(_ context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failDelete {
		return nil, errors.New("injected delete failure")
	}
	a.deletions++
	delete(a.relations, relationshipKey(req.RelationshipTuple))
	return &proto.DeleteRelationshipResponse{}, nil
}

func (a *recordingAuthorization) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (a *recordingAuthorization) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (a *recordingAuthorization) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (a *recordingAuthorization) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (a *recordingAuthorization) Ping(context.Context) error { return nil }
func (a *recordingAuthorization) Close() error               { return nil }

func (a *recordingAuthorization) setFailures(add, delete bool) {
	a.mu.Lock()
	a.failAdd = add
	a.failDelete = delete
	a.mu.Unlock()
}

func (a *recordingAuthorization) setRelationship(relationship *proto.Relationship) {
	a.mu.Lock()
	a.relations[relationshipKey(relationship.Tuple)] = gproto.Clone(relationship).(*proto.Relationship)
	a.mu.Unlock()
}

func (a *recordingAuthorization) removeRelationship(tuple *proto.RelationshipTuple) {
	a.mu.Lock()
	delete(a.relations, relationshipKey(tuple))
	a.mu.Unlock()
}

func (a *recordingAuthorization) relationshipForUser(coreUserID string) *proto.Relationship {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, relationship := range a.relations {
		if relationship.GetTuple().GetTarget().GetSubject().GetId() == "user:"+coreUserID {
			return gproto.Clone(relationship).(*proto.Relationship)
		}
	}
	return nil
}

func (a *recordingAuthorization) listCallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listCalls
}

func (a *recordingAuthorization) additionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.additions
}

func (a *recordingAuthorization) deletionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deletions
}

func projectedRelationship(coreUserID string, source proto.SourceLayer) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target:   &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreUserID}}},
			Relation: "member",
			Resource: &proto.Resource{Type: "group", Id: "valon-employees"},
		},
		SourceLayer: source,
	}
}

func relationshipKey(tuple *proto.RelationshipTuple) string {
	target := tuple.GetTarget()
	targetKey := "subject\x00" + target.GetSubject().GetType() + "\x00" + target.GetSubject().GetId()
	if subjectSet := target.GetSubjectSet(); subjectSet != nil {
		targetKey = "subject-set\x00" + subjectSet.GetResource().GetType() + "\x00" + subjectSet.GetResource().GetId() + "\x00" + subjectSet.GetRelation()
	}
	return targetKey + "\x00" + tuple.GetRelation() + "\x00" + tuple.GetResource().GetType() + "\x00" + tuple.GetResource().GetId()
}

var _ core.AuthorizationProvider = (*recordingAuthorization)(nil)

type transactionFault int

const (
	transactionFaultIntentAdd transactionFault = iota + 1
	transactionFaultSCIMUserGet
	transactionFaultCoreUserAdd
	transactionFaultTerminalMiss
)

type transactionFaultDB struct {
	coredb.IndexedDB
	mu      sync.Mutex
	fault   transactionFault
	armed   bool
	tripped bool
}

func (d *transactionFaultDB) arm(fault transactionFault) {
	d.mu.Lock()
	d.fault = fault
	d.armed = true
	d.tripped = false
	d.mu.Unlock()
}

func (d *transactionFaultDB) trip(fault transactionFault) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.armed || d.tripped || d.fault != fault {
		return false
	}
	d.tripped = true
	return true
}

func (d *transactionFaultDB) active(fault transactionFault) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.armed && d.fault == fault
}

func (d *transactionFaultDB) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	tx, err := d.IndexedDB.Transaction(ctx, stores, mode, opts)
	if err != nil {
		return nil, err
	}
	return &transactionFaultTransaction{Transaction: tx, db: d}, nil
}

type transactionFaultTransaction struct {
	idb.Transaction
	db *transactionFaultDB
}

func (t *transactionFaultTransaction) ObjectStore(name string) idb.TransactionObjectStore {
	return &transactionFaultStore{TransactionObjectStore: t.Transaction.ObjectStore(name), tx: t.Transaction, db: t.db, name: name}
}

type transactionFaultStore struct {
	idb.TransactionObjectStore
	tx   idb.Transaction
	db   *transactionFaultDB
	name string
}

func (s *transactionFaultStore) Index(name string) idb.TransactionIndex {
	return &transactionFaultIndex{TransactionIndex: s.TransactionObjectStore.Index(name), tx: s.tx, db: s.db}
}

func (s *transactionFaultStore) Get(ctx context.Context, id string) (idb.Record, error) {
	if s.name == coredata.StoreSCIMUsers && s.db.trip(transactionFaultSCIMUserGet) {
		return nil, errors.New("injected datastore failure")
	}
	record, err := s.TransactionObjectStore.Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) && s.db.active(transactionFaultTerminalMiss) {
		_ = s.tx.Abort(ctx)
	}
	return record, err
}

func (s *transactionFaultStore) Add(ctx context.Context, record idb.Record) error {
	switch s.name {
	case coredata.StoreSCIMProjectionIntents:
		if s.db.trip(transactionFaultIntentAdd) {
			return idb.ErrAlreadyExists
		}
	case coredata.StoreUsers:
		if s.db.trip(transactionFaultCoreUserAdd) {
			return idb.ErrAlreadyExists
		}
	}
	return s.TransactionObjectStore.Add(ctx, record)
}

type transactionFaultIndex struct {
	idb.TransactionIndex
	tx idb.Transaction
	db *transactionFaultDB
}

func (i *transactionFaultIndex) Get(ctx context.Context, query any) (idb.Record, error) {
	record, err := i.TransactionIndex.Get(ctx, query)
	if errors.Is(err, idb.ErrNotFound) && i.db.active(transactionFaultTerminalMiss) {
		_ = i.tx.Abort(ctx)
	}
	return record, err
}

var _ coredb.IndexedDB = (*transactionFaultDB)(nil)
