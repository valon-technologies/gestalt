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
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

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
		Clients: clients,
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

func newSCIMService(t *testing.T, db coredb.IndexedDB, authorization core.AuthorizationProvider, cfg config.ServerSCIMConfig) (*scim.Service, *coredata.Services, http.Handler) {
	t.Helper()
	if db == nil {
		db = &coretesting.StubIndexedDB{}
	}
	services, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	service, err := scim.NewService(services.DB, authorization, testBaseURL, cfg)
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

func TestSCIMUsersHTTPContract(t *testing.T) {
	t.Parallel()

	_, _, handler := newSCIMService(t, nil, newRecordingAuthorization(), testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil, employeeProjection()),
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
	schemaItem := scimRequest(t, handler, http.MethodGet, "/scim/v2/Schemas/"+scim.UserSchemaURN, testCurrentToken, nil)
	if schemaItem.Code != http.StatusOK || schemaItem.Header().Get("Content-Location") == "" || !strings.HasSuffix(schemaItem.Header().Get("Content-Location"), "/Schemas/"+scim.UserSchemaURN) {
		t.Fatalf("individual Schema = %d headers=%v body=%s", schemaItem.Code, schemaItem.Header(), schemaItem.Body.String())
	}
	var schema struct {
		ID   string `json:"id"`
		Meta struct {
			Location string `json:"location"`
		} `json:"meta"`
		Attributes []struct {
			Name     string `json:"name"`
			Returned string `json:"returned"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(schemaItem.Body.Bytes(), &schema); err != nil || schema.ID != scim.UserSchemaURN || schema.Meta.Location != schemaItem.Header().Get("Content-Location") {
		t.Fatalf("individual Schema metadata = %#v, err=%v", schema, err)
	}
	for _, attribute := range schema.Attributes {
		if attribute.Name == "userName" && attribute.Returned != "default" {
			t.Fatalf("User.userName returned = %q", attribute.Returned)
		}
	}
	schemaCollection := scimRequest(t, handler, http.MethodGet, "/scim/v2/Schemas", testCurrentToken, nil)
	if bytes.Contains(schemaCollection.Body.Bytes(), []byte(`"name":"externalId"`)) {
		t.Fatalf("common externalId unexpectedly advertised as resource-specific schema attribute: %s", schemaCollection.Body.String())
	}
	serviceProvider := scimRequest(t, handler, http.MethodGet, "/scim/v2/ServiceProviderConfig", testCurrentToken, nil)
	if serviceProvider.Code != http.StatusOK || !bytes.Contains(serviceProvider.Body.Bytes(), []byte(`"patch":{"supported":false}`)) {
		t.Fatalf("ServiceProviderConfig = %d %s", serviceProvider.Code, serviceProvider.Body.String())
	}
	resourceTypes := scimRequest(t, handler, http.MethodGet, "/scim/v2/ResourceTypes/User", testCurrentToken, nil)
	if resourceTypes.Code != http.StatusOK || !bytes.Contains(resourceTypes.Body.Bytes(), []byte(`"endpoint":"/Users"`)) {
		t.Fatalf("ResourceTypes/User = %d %s", resourceTypes.Code, resourceTypes.Body.String())
	}
	missingResourceSchema := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{"userName": "missing-schema@valon.com"})
	if payload := decodeResponse[testErrorResponse](t, missingResourceSchema); missingResourceSchema.Code != http.StatusBadRequest || payload.SCIMType != "invalidSyntax" {
		t.Fatalf("missing User schema = %d %#v", missingResourceSchema.Code, payload)
	}
	wrongResourceSchema := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "userName": "wrong-schema@valon.com"})
	if payload := decodeResponse[testErrorResponse](t, wrongResourceSchema); wrongResourceSchema.Code != http.StatusBadRequest || payload.SCIMType != "invalidSyntax" {
		t.Fatalf("wrong User schema = %d %#v", wrongResourceSchema.Code, payload)
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
	createdRecorder := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users?attributes=name.givenName", testCurrentToken, alicePayload)
	created := decodeResponse[scim.User](t, createdRecorder)
	created.ID = strings.TrimPrefix(createdRecorder.Header().Get("Location"), testBaseURL+"/scim/v2/Users/")
	created.Meta.Location = createdRecorder.Header().Get("Location")
	created.Meta.Version = createdRecorder.Header().Get("ETag")
	if createdRecorder.Code != http.StatusCreated || created.ID == "" || created.UserName != "" || created.Meta.Version == "" || !bytes.Contains(createdRecorder.Body.Bytes(), []byte(`"givenName":"Alice"`)) || bytes.Contains(createdRecorder.Body.Bytes(), []byte(`"active":`)) {
		t.Fatalf("POST = %d %#v", createdRecorder.Code, created)
	}
	if got := createdRecorder.Header().Get("Location"); got != testBaseURL+"/scim/v2/Users/"+created.ID {
		t.Fatalf("Location = %q", got)
	}
	if got := createdRecorder.Header().Get("Content-Location"); got != created.Meta.Location {
		t.Fatalf("Content-Location = %q", got)
	}
	if got := createdRecorder.Header().Get("ETag"); got != created.Meta.Version {
		t.Fatalf("ETag = %q", got)
	}
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+created.ID, testCurrentToken, nil, map[string]string{"If-None-Match": created.Meta.Version}); response.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match = %d %s", response.Code, response.Body.String())
	}
	projected := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+created.ID+"?attributes=name.givenName", testCurrentToken, nil)
	if projected.Code != http.StatusOK || bytes.Contains(projected.Body.Bytes(), []byte(`"userName"`)) || bytes.Contains(projected.Body.Bytes(), []byte(`"active":`)) || bytes.Contains(projected.Body.Bytes(), []byte(`"familyName"`)) {
		t.Fatalf("attributes projection = %d %s", projected.Code, projected.Body.String())
	}
	qualifiedProjection := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+created.ID+"?attributes="+url.QueryEscape(scim.UserSchemaURN+":name.givenName"), testCurrentToken, nil)
	if qualifiedProjection.Code != http.StatusOK || !bytes.Contains(qualifiedProjection.Body.Bytes(), []byte(`"givenName":"Alice"`)) || bytes.Contains(qualifiedProjection.Body.Bytes(), []byte(`"familyName"`)) {
		t.Fatalf("schema-qualified attributes projection = %d %s", qualifiedProjection.Code, qualifiedProjection.Body.String())
	}
	excluded := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+created.ID+"?excludedAttributes=name.familyName", testCurrentToken, nil)
	if excluded.Code != http.StatusOK || bytes.Contains(excluded.Body.Bytes(), []byte(`"familyName"`)) {
		t.Fatalf("excludedAttributes projection = %d %s", excluded.Code, excluded.Body.String())
	}
	if both := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+created.ID+"?attributes=name&excludedAttributes=emails", testCurrentToken, nil); both.Code != http.StatusBadRequest {
		t.Fatalf("simultaneous projection parameters = %d %s", both.Code, both.Body.String())
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
	caseFoldedExternal := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`externalId eq "EMPLOYEE-123"`), testCurrentToken, nil)
	if list := decodeResponse[testListResponse](t, caseFoldedExternal); caseFoldedExternal.Code != http.StatusOK || list.TotalResults != 0 {
		t.Fatalf("case-sensitive externalId filter = %d %#v", caseFoldedExternal.Code, list)
	}
	empty := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`userName eq "random-entra-probe@invalid.example"`), testCurrentToken, nil)
	if list := decodeResponse[testListResponse](t, empty); empty.Code != http.StatusOK || list.TotalResults != 0 || list.Resources == nil {
		t.Fatalf("empty filter = %d %s", empty.Code, empty.Body.String())
	}
	unsupportedFilter := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`active eq "true"`), testCurrentToken, nil)
	if payload := decodeResponse[testErrorResponse](t, unsupportedFilter); unsupportedFilter.Code != http.StatusBadRequest || payload.Status != "400" || payload.SCIMType != "invalidFilter" {
		t.Fatalf("unsupported filter = %d %#v", unsupportedFilter.Code, payload)
	}
	patch := map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"}, "Operations": []map[string]any{{"op": "replace", "path": "active", "value": false}}}
	if payload := decodeResponse[testErrorResponse](t, scimRequest(t, handler, http.MethodPatch, "/scim/v2/Users/"+created.ID, testCurrentToken, patch)); payload.Status != "501" {
		t.Fatalf("unsupported PATCH = %#v", payload)
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

	put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "externalId": "employee-123", "userName": "Alice@Valon.com", "emails": []map[string]any{{"value": "Alice@Valon.com", "type": "work", "primary": true}}}
	putRecorder := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+created.ID, testCurrentToken, put, map[string]string{"If-Match": "*"})
	if putUser := decodeResponse[scim.User](t, putRecorder); putRecorder.Code != http.StatusOK || putUser.Active {
		t.Fatalf("PUT missing active = %d %#v", putRecorder.Code, putUser)
	}
	reactivate := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "Alice@Valon.com", "active": true, "emails": []map[string]any{{"value": "Alice@Valon.com", "type": "work", "primary": true}}}
	reactivatedRecorder := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+created.ID, testCurrentToken, reactivate, map[string]string{"If-Match": "*"})
	if reactivated := decodeResponse[scim.User](t, reactivatedRecorder); reactivatedRecorder.Code != http.StatusOK || !reactivated.Active || reactivated.ID != created.ID {
		t.Fatalf("reactivation = %d %#v", reactivatedRecorder.Code, reactivated)
	}

	conflictingEmail := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "Alice@Valon.com", "emails": []map[string]any{{"value": "bob@valon.com", "type": "work", "primary": true}}}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+created.ID, testCurrentToken, conflictingEmail); response.Code != http.StatusConflict {
		t.Fatalf("email conflict = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, alicePayload); response.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d %s", response.Code, response.Body.String())
	}
	_, multiplePrimary := createUser(t, handler, testCurrentToken, "multiple-primary@valon.com", true, map[string]any{"emails": []map[string]any{{"value": "multiple-primary@valon.com", "primary": true}, {"value": "other@valon.com", "primary": true}}})
	if payload := decodeResponse[testErrorResponse](t, multiplePrimary); multiplePrimary.Code != http.StatusBadRequest || payload.SCIMType != "invalidValue" {
		t.Fatalf("multiple primary emails = %d %#v", multiplePrimary.Code, payload)
	}
	primary, primaryResponse := createUser(t, handler, testCurrentToken, "primary-switch@valon.com", true, map[string]any{"emails": []map[string]any{{"value": "primary-switch@valon.com", "type": "work", "primary": true}, {"value": "home@valon.com", "type": "home"}}})
	if primaryResponse.Code != http.StatusCreated {
		t.Fatalf("primary test user = %d %s", primaryResponse.Code, primaryResponse.Body.String())
	}
	primaryPut := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "primary-switch@valon.com", "active": true, "emails": []map[string]any{{"value": "primary-switch@valon.com", "type": "work", "primary": false}, {"value": "home@valon.com", "type": "home", "primary": true}}}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+primary.ID, testCurrentToken, primaryPut, map[string]string{"If-Match": primary.Meta.Version}); response.Code != http.StatusOK {
		t.Fatalf("primary email PUT = %d %s", response.Code, response.Body.String())
	}
	primaryRead := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+primary.ID, testCurrentToken, nil))
	primaryCount := 0
	for _, email := range primaryRead.Emails {
		if email.Primary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		t.Fatalf("primary email count after PUT = %d (%#v)", primaryCount, primaryRead.Emails)
	}
	beforeNoOp := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+primary.ID, testCurrentToken, nil))
	noOp := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+primary.ID, testCurrentToken, primaryPut, map[string]string{"If-Match": beforeNoOp.Meta.Version}))
	if noOp.Meta.Version != beforeNoOp.Meta.Version || !noOp.Meta.LastModified.Equal(beforeNoOp.Meta.LastModified) {
		t.Fatalf("duplicate email add changed metadata = before %#v after %#v", beforeNoOp.Meta, noOp.Meta)
	}

	for _, id := range []string{created.ID, bob.ID, carol.ID} {
		if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Users/"+id, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d %s", id, response.Code, response.Body.String())
		}
		if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Users/"+id, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNotFound {
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

func TestSCIMCreateReturns503OnDatabaseFailure(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	_, _, handler := newSCIMService(t, db, nil, testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))
	db.Err = errors.New("database unavailable")
	response := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true})
	if payload := decodeResponse[testErrorResponse](t, response); response.Code != http.StatusServiceUnavailable || payload.Status != "503" {
		t.Fatalf("database failure = %d %#v", response.Code, payload)
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

func TestSCIMUserNameUniquenessIsCaseInsensitiveAcrossReplicas(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, firstHandler := newSCIMService(t, db, nil, cfg)
	_, _, secondHandler := newSCIMService(t, db, nil, cfg)
	start := make(chan struct{})
	responses := make(chan int, 2)
	for i, handler := range []http.Handler{firstHandler, secondHandler} {
		go func(handler http.Handler, userName string) {
			<-start
			request := scimRequest(t, handler, http.MethodPost, "/scim/v2/Users", testCurrentToken, map[string]any{
				"schemas": []string{scim.UserSchemaURN}, "userName": userName,
			})
			responses <- request.Code
		}(handler, []string{"Alice@Valon.com", "alice@valon.com"}[i])
	}
	close(start)
	statuses := []int{<-responses, <-responses}
	if (statuses[0] != http.StatusCreated || statuses[1] != http.StatusConflict) && (statuses[1] != http.StatusCreated || statuses[0] != http.StatusConflict) {
		t.Fatalf("case-variant create statuses = %v, want one 201 and one 409", statuses)
	}
}

func TestSCIMProjectionFailureLeavesLiveStateForClientRetry(t *testing.T) {
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
		t.Fatalf("provider gap access = %#v, %v", response, err)
	}

	authorization.setFailures(false, false)
	current := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`userName eq "alice@valon.com"`), testCurrentToken, nil)
	list := decodeResponse[testListResponse](t, current)
	if current.Code != http.StatusOK || list.TotalResults != 1 || list.Resources[0].Active {
		t.Fatalf("post-failure live User representation = %d %#v", current.Code, list)
	}
	if response, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), request); err != nil || response.Allowed {
		t.Fatalf("access remains denied until client retry = %#v, %v", response, err)
	}
	if relationship := authorization.relationshipForUser(coreUser.ID); relationship != nil {
		t.Fatalf("failed projection unexpectedly created relationship = %#v", relationship)
	}
	// A later explicit lifecycle mutation converges the provider.
	recoveredID := list.Resources[0].ID
	activate := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+recoveredID, testCurrentToken, activate); response.Code != http.StatusOK {
		t.Fatalf("explicit activation = %d %s", response.Code, response.Body.String())
	}
	if relationship := authorization.relationshipForUser(coreUser.ID); relationship == nil {
		t.Fatal("explicit reactivation did not project relationship")
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

func TestSCIMClientNamespacesDoNotShareEligibility(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient([]string{"valon.com"}, employeeProjection()),
		"entra":    {Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}}},
	})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	if _, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil); response.Code != http.StatusCreated {
		t.Fatalf("authoritative create = %d %s", response.Code, response.Body.String())
	}
	if _, response := createUser(t, handler, "entra-token", "alice@valon.com", true, nil); response.Code != http.StatusCreated {
		t.Fatalf("second-client create = %d %s", response.Code, response.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	if response, err := scim.WrapAuthorization(authorization, services.Users, service).CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || !response.Allowed {
		t.Fatalf("authoritative client projection was affected by other namespace = %#v, %v", response, err)
	}
}

func TestSCIMClientsCannotRelinkSharedCoreUser(t *testing.T) {
	t.Parallel()

	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
		"entra":    {Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}}},
	})
	_, _, handler := newSCIMService(t, nil, newRecordingAuthorization(), cfg)
	first, response := createUser(t, handler, testCurrentToken, "shared@valon.com", true, map[string]any{"displayName": "Shared"})
	if response.Code != http.StatusCreated {
		t.Fatalf("first create = %d %s", response.Code, response.Body.String())
	}
	if _, response = createUser(t, handler, "entra-token", "shared@valon.com", true, nil); response.Code != http.StatusCreated {
		t.Fatalf("second create = %d %s", response.Code, response.Body.String())
	}
	put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "moved@valon.com", "active": true, "displayName": "Changed"}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+first.ID, testCurrentToken, put); response.Code != http.StatusConflict {
		t.Fatalf("shared core relink = %d %s", response.Code, response.Body.String())
	}
}

func TestSCIMConcurrentClientsCannotRelinkSharedCoreUser(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
		"entra":    {Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}}},
	})
	_, services, firstHandler := newSCIMService(t, db, authorization, cfg)
	if _, response := createUser(t, firstHandler, testCurrentToken, "race-shared@valon.com", true, map[string]any{"displayName": "Shared"}); response.Code != http.StatusCreated {
		t.Fatalf("first create = %d %s", response.Code, response.Body.String())
	}
	_, _, secondHandler := newSCIMService(t, db, authorization, cfg)
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, attempt := range []struct{ token, displayName string }{{"entra-token", "Changed by Entra"}, {testCurrentToken, "Changed by Rippling"}} {
		go func(token, displayName string) {
			<-start
			responses <- scimRequest(t, secondHandler, http.MethodPost, "/scim/v2/Users", token, map[string]any{
				"schemas":     []string{scim.UserSchemaURN},
				"userName":    "race-shared@valon.com",
				"active":      true,
				"displayName": displayName,
			})
		}(attempt.token, attempt.displayName)
	}
	close(start)
	for range 2 {
		if response := <-responses; response.Code != http.StatusConflict {
			t.Fatalf("concurrent shared-core create = %d %s", response.Code, response.Body.String())
		}
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "race-shared@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	if coreUser.DisplayName != "Shared" {
		t.Fatalf("shared core display name changed = %q", coreUser.DisplayName)
	}
}

func TestSCIMAuthoritativeDomainGateFollowsCoreEmail(t *testing.T) {
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
	changeEmail := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@example.com", "active": true}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, changeEmail); response.Code != http.StatusOK {
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
	deactivate := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@example.com", "active": false}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate); response.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", response.Code, response.Body.String())
	}
	if response, err := gate.CheckAccess(context.Background(), accessRequest(coreUser.ID)); err != nil || !response.Allowed {
		t.Fatalf("non-authoritative changed-domain access = %#v, %v", response, err)
	}
}

func TestSCIMExternalAuthorizationWritesUpdateSCIMMetadata(t *testing.T) {
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
	projectedCfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil, employeeProjection())})
	service, _, handler := newSCIMService(t, db, authorization, projectedCfg)
	gate := scim.WrapAuthorization(authorization, services.Users, service)
	tuple := projectedRelationship(coreUser.ID, proto.SourceLayer_SOURCE_LAYER_RUNTIME).Tuple
	before := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if before.Active {
		t.Fatal("user unexpectedly active before provider relationship")
	}
	if _, err := gate.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: &proto.Relationship{Tuple: tuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}}); err != nil {
		t.Fatal(err)
	}
	afterAdd := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if !afterAdd.Active || !afterAdd.Meta.LastModified.After(before.Meta.LastModified) || afterAdd.Meta.Version == before.Meta.Version {
		t.Fatalf("external relationship did not update live User = %#v", afterAdd)
	}
	if _, err := gate.DeleteRelationship(context.Background(), &proto.DeleteRelationshipRequest{RelationshipTuple: tuple}); err != nil {
		t.Fatal(err)
	}
	afterDelete := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if afterDelete.Active || !afterDelete.Meta.LastModified.After(afterAdd.Meta.LastModified) || afterDelete.Meta.Version == afterAdd.Meta.Version {
		t.Fatalf("external relationship deletion did not update live User = %#v", afterDelete)
	}
}

func TestSCIMExternalNestedDeleteUpdatesAffectedUserMetadata(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "nested-external@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	childResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "External Child", "members": []map[string]any{{"value": user.ID}}})
	child := decodeResponse[scim.Group](t, childResponse)
	if childResponse.Code != http.StatusCreated {
		t.Fatalf("create child = %d %s", childResponse.Code, childResponse.Body.String())
	}
	parentResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "External Parent", "members": []map[string]any{{"value": child.ID, "type": "Group"}}})
	parent := decodeResponse[scim.Group](t, parentResponse)
	if parentResponse.Code != http.StatusCreated {
		t.Fatalf("create parent = %d %s", parentResponse.Code, parentResponse.Body.String())
	}
	before := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if len(before.Groups) != 2 {
		t.Fatalf("nested groups before external delete = %#v", before.Groups)
	}
	gate := scim.WrapAuthorization(authorization, services.Users, service)
	nested := &proto.RelationshipTuple{
		Target:   &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: "group", Id: child.ID}, Relation: "member"}}},
		Relation: "member", Resource: &proto.Resource{Type: "group", Id: parent.ID},
	}
	if _, err := gate.DeleteRelationship(context.Background(), &proto.DeleteRelationshipRequest{RelationshipTuple: nested}); err != nil {
		t.Fatal(err)
	}
	after := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if len(after.Groups) != 1 || after.Groups[0].Value != child.ID || !after.Meta.LastModified.After(before.Meta.LastModified) || after.Meta.Version == before.Meta.Version {
		t.Fatalf("nested external delete did not update user metadata: before=%#v after=%#v", before, after)
	}
}

func TestSCIMUserDisplayNameFollowsCoreUser(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, services, handler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, handler, testCurrentToken, "display@valon.com", true, map[string]any{"displayName": "SCIM Name"})
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	before := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if before.DisplayName != "SCIM Name" {
		t.Fatalf("initial displayName = %q", before.DisplayName)
	}
	if _, err := services.Users.FindOrCreateUserWithName(context.Background(), "display@valon.com", "Core Name"); err != nil {
		t.Fatal(err)
	}
	after := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if after.DisplayName != "Core Name" || after.Meta.Version == before.Meta.Version {
		t.Fatalf("core displayName update not reflected = before %#v after %#v", before, after)
	}
}

func TestSCIMRelationshipPaginationCoversAllMembersAndDeletes(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, nil, authorization, cfg)
	users := make([]*scim.User, 0, 3)
	for _, name := range []string{"alice@valon.com", "bob@valon.com", "carol@valon.com"} {
		user, response := createUser(t, handler, testCurrentToken, name, true, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", name, response.Code, response.Body.String())
		}
		users = append(users, user)
	}
	authorization.setPageSize(1)
	members := make([]map[string]any, 0, len(users))
	for _, user := range users {
		members = append(members, map[string]any{"value": user.ID})
	}
	created := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Paged", "members": members})
	group := decodeResponse[scim.Group](t, created)
	if created.Code != http.StatusCreated || len(group.Members) != len(users) {
		t.Fatalf("paged Group create = %d %#v", created.Code, group)
	}
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
		t.Fatalf("paged Group delete = %d %s", response.Code, response.Body.String())
	}
	for _, user := range users {
		read := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
		if len(read.Groups) != 0 {
			t.Fatalf("User.groups after paged delete = %#v", read.Groups)
		}
	}
}

func TestSCIMActivationRetriesFromActualProviderTuples(t *testing.T) {
	t.Parallel()
	projections := []config.SCIMRelationshipConfig{employeeProjection(), {Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "engineering"}}}
	for failAt := 1; failAt <= len(projections); failAt++ {
		t.Run(fmt.Sprintf("failure-%d", failAt), func(t *testing.T) {
			authorization := newRecordingAuthorization()
			cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient([]string{"valon.com"}, projections...)})
			_, _, handler := newSCIMService(t, nil, authorization, cfg)
			user, response := createUser(t, handler, testCurrentToken, "activate@valon.com", false, nil)
			if response.Code != http.StatusCreated {
				t.Fatalf("create = %d %s", response.Code, response.Body.String())
			}
			authorization.setFailureAt(failAt, 0)
			activate := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "activate@valon.com", "active": true}
			if failed := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, activate); failed.Code != http.StatusServiceUnavailable {
				t.Fatalf("partial activation = %d %s", failed.Code, failed.Body.String())
			}
			live := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
			if live.Active {
				t.Fatal("partial activation was reported active")
			}
			authorization.setFailures(false, false)
			retried := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, activate))
			if retried.Active == false {
				t.Fatalf("activation retry remained inactive: %#v", retried)
			}
		})
	}
}

func TestSCIMDeactivationRetriesFromActualProviderTuples(t *testing.T) {
	t.Parallel()
	projections := []config.SCIMRelationshipConfig{employeeProjection(), {Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "engineering"}}}
	for failAt := 1; failAt <= len(projections); failAt++ {
		t.Run(fmt.Sprintf("failure-%d", failAt), func(t *testing.T) {
			authorization := newRecordingAuthorization()
			cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient([]string{"valon.com"}, projections...)})
			_, _, handler := newSCIMService(t, nil, authorization, cfg)
			user, response := createUser(t, handler, testCurrentToken, "deactivate@valon.com", true, nil)
			if response.Code != http.StatusCreated {
				t.Fatalf("create = %d %s", response.Code, response.Body.String())
			}
			authorization.setFailureAt(0, failAt)
			deactivate := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "deactivate@valon.com", "active": false}
			if failed := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate); failed.Code != http.StatusServiceUnavailable {
				t.Fatalf("partial deactivation = %d %s", failed.Code, failed.Body.String())
			}
			live := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
			if (failAt == 1 && !live.Active) || (failAt > 1 && live.Active) {
				t.Fatalf("partial deactivation live state = %#v", live)
			}
			authorization.setFailures(false, false)
			retried := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate))
			if retried.Active {
				t.Fatalf("deactivation retry remained active: %#v", retried)
			}
		})
	}
}

func TestSCIMCrossReplicaConditionalMutation(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil, employeeProjection())})
	authorization := newRecordingAuthorization()
	_, _, firstHandler := newSCIMService(t, db, authorization, cfg)
	_, _, secondHandler := newSCIMService(t, db, authorization, cfg)
	user, response := createUser(t, firstHandler, testCurrentToken, "alice@valon.com", false, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}

	start := make(chan struct{})
	statuses := make(chan *httptest.ResponseRecorder, 2)
	for i, handler := range []http.Handler{firstHandler, secondHandler} {
		go func(worker int, handler http.Handler) {
			<-start
			put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true}
			statuses <- scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, put, map[string]string{"If-Match": user.Meta.Version})
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
	if committed.Meta.Version == "" {
		t.Fatalf("committed version is empty")
	}
}

func TestSCIMMutationReturns503OnResourceWriteFailure(t *testing.T) {
	t.Parallel()

	db := &transactionFaultDB{IndexedDB: &coretesting.StubIndexedDB{}}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	db.arm(transactionFaultSCIMResourcePut)
	put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true, "displayName": "Alice Updated"}
	response = scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, put)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("database failure = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
}

func TestSCIMMutationReturns503WhenStoreIsUnavailable(t *testing.T) {
	t.Parallel()

	db := &coretesting.StubIndexedDB{}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, handler, testCurrentToken, "database-failure@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	db.Err = errors.New("database unavailable")
	put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "database-failure@valon.com", "active": true, "displayName": "Updated"}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, put); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("database failure = %d %s", response.Code, response.Body.String())
	}
}

func TestSCIMMutationReadFailureReturns503(t *testing.T) {
	t.Parallel()

	db := &transactionFaultDB{IndexedDB: &coretesting.StubIndexedDB{}}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, db, nil, cfg)
	user, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	db.arm(transactionFaultSCIMResourceGet)
	put := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true, "displayName": "Alice Updated"}
	response = scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, put)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("datastore failure = %d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
}

func TestSCIMCreateUserLinkConflictReturnsUnavailable(t *testing.T) {
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
	if _, err := gate.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: managed}); err != nil {
		t.Fatalf("ordinary add to SCIM-managed relationship: %v", err)
	}
	if _, err := gate.DeleteRelationship(context.Background(), &proto.DeleteRelationshipRequest{RelationshipTuple: managed.Tuple}); err != nil {
		t.Fatalf("ordinary delete from SCIM-managed relationship: %v", err)
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
	if _, err := broker.Invoke(context.Background(), identity, "docs", "", "documents.read", nil); !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("unprojected Invoke error = %v", err)
	}
	deactivate := map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": false}
	if response := scimRequest(t, handler, http.MethodPut, "/scim/v2/Users/"+user.ID, testCurrentToken, deactivate); response.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", response.Code, response.Body.String())
	}
	if _, err := broker.Invoke(context.Background(), identity, "docs", "", "documents.read", nil); !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("inactive Invoke error = %v, want ErrAuthorizationDenied", err)
	}
	if executeCalls != 0 {
		t.Fatalf("provider execute calls = %d, want 0", executeCalls)
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
	mu           sync.Mutex
	failAdd      bool
	failDelete   bool
	listCalls    int
	additions    int
	deletions    int
	addCalls     int
	deleteCalls  int
	failAddAt    int
	failDeleteAt int
	pageSize     int32
	relations    map[string]*proto.Relationship
	checkCalled  int
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
	filtered := make([]*proto.Relationship, 0, len(a.relations))
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
		filtered = append(filtered, gproto.Clone(relationship).(*proto.Relationship))
	}
	sort.Slice(filtered, func(i, j int) bool { return relationshipKey(filtered[i].Tuple) < relationshipKey(filtered[j].Tuple) })
	pageSize := int32(len(filtered))
	if a.pageSize > 0 && (pageSize == 0 || a.pageSize < pageSize) {
		pageSize = a.pageSize
	}
	offset := 0
	if req != nil && req.PageToken != "" {
		offset, _ = strconv.Atoi(req.PageToken)
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + int(pageSize)
	if end > len(filtered) {
		end = len(filtered)
	}
	response := &proto.ListRelationshipsResponse{Relationships: filtered[offset:end]}
	if end < len(filtered) {
		response.NextPageToken = strconv.Itoa(end)
	}
	return response, nil
}

func (a *recordingAuthorization) AddRelationship(_ context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.addCalls++
	if a.failAdd || a.failAddAt > 0 && a.addCalls == a.failAddAt {
		return nil, errors.New("injected add failure")
	}
	a.additions++
	a.relations[relationshipKey(req.Relationship.Tuple)] = gproto.Clone(req.Relationship).(*proto.Relationship)
	return &proto.AddRelationshipResponse{Relationship: req.Relationship}, nil
}

func (a *recordingAuthorization) DeleteRelationship(_ context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleteCalls++
	if a.failDelete || a.failDeleteAt > 0 && a.deleteCalls == a.failDeleteAt {
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
	a.failAddAt = 0
	a.failDeleteAt = 0
	a.addCalls = 0
	a.deleteCalls = 0
	a.mu.Unlock()
}

func (a *recordingAuthorization) setFailureAt(add, delete int) {
	a.mu.Lock()
	a.failAdd = false
	a.failDelete = false
	a.failAddAt = add
	a.failDeleteAt = delete
	a.addCalls = 0
	a.deleteCalls = 0
	a.mu.Unlock()
}

func (a *recordingAuthorization) setPageSize(size int32) {
	a.mu.Lock()
	a.pageSize = size
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

func (a *recordingAuthorization) additionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.additions
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
	transactionFaultSCIMResourceGet transactionFault = iota + 1
	transactionFaultSCIMResourcePut
	transactionFaultCoreUserAdd
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

func (d *transactionFaultDB) ObjectStore(name string) idb.ObjectStore {
	return &transactionFaultObjectStore{ObjectStore: d.IndexedDB.ObjectStore(name), db: d, name: name}
}

type transactionFaultObjectStore struct {
	idb.ObjectStore
	db   *transactionFaultDB
	name string
}

func (s *transactionFaultObjectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	if s.name == coredata.StoreSCIMResources && s.db.trip(transactionFaultSCIMResourceGet) {
		return nil, errors.New("injected datastore failure")
	}
	return s.ObjectStore.Get(ctx, id)
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
	return &transactionFaultStore{TransactionObjectStore: t.Transaction.ObjectStore(name), db: t.db, name: name}
}

type transactionFaultStore struct {
	idb.TransactionObjectStore
	db   *transactionFaultDB
	name string
}

func (s *transactionFaultStore) Put(ctx context.Context, record idb.Record) error {
	if s.name == coredata.StoreSCIMResources && s.db.trip(transactionFaultSCIMResourcePut) {
		return errors.New("injected datastore failure")
	}
	return s.TransactionObjectStore.Put(ctx, record)
}

func (s *transactionFaultStore) Add(ctx context.Context, record idb.Record) error {
	switch s.name {
	case coredata.StoreUsers:
		if s.db.trip(transactionFaultCoreUserAdd) {
			return idb.ErrAlreadyExists
		}
	}
	return s.TransactionObjectStore.Add(ctx, record)
}

var _ coredb.IndexedDB = (*transactionFaultDB)(nil)
