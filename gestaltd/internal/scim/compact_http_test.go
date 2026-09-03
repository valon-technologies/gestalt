package scim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

type authz struct {
	mu  sync.Mutex
	rel map[string]*proto.Relationship
}

func newAuthz() *authz { return &authz{rel: map[string]*proto.Relationship{}} }
func (a *authz) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}
func (a *authz) CheckAccessMany(_ context.Context, r *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	o := &proto.CheckAccessManyResponse{}
	for range r.Requests {
		o.Decisions = append(o.Decisions, &proto.CheckAccessResponse{Allowed: true})
	}
	return o, nil
}
func (a *authz) ListRelationships(_ context.Context, r *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	o := &proto.ListRelationshipsResponse{}
	for _, x := range a.rel {
		if f := r.GetFilter(); f != nil {
			if f.Relation != "" && f.Relation != x.Tuple.Relation {
				continue
			}
			if f.Resource != nil && !gproto.Equal(f.Resource, x.Tuple.Resource) {
				continue
			}
			if f.Target != nil && !gproto.Equal(f.Target, x.Tuple.Target) {
				continue
			}
		}
		o.Relationships = append(o.Relationships, gproto.Clone(x).(*proto.Relationship))
	}
	return o, nil
}
func key(t *proto.RelationshipTuple) string { return t.String() }
func (a *authz) AddRelationship(_ context.Context, r *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rel[key(r.Relationship.Tuple)] = gproto.Clone(r.Relationship).(*proto.Relationship)
	return &proto.AddRelationshipResponse{Relationship: r.Relationship}, nil
}
func (a *authz) DeleteRelationship(_ context.Context, r *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.rel, key(r.RelationshipTuple))
	return &proto.DeleteRelationshipResponse{}, nil
}
func (*authz) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}
func (*authz) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}
func (*authz) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}
func (*authz) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}
func (*authz) Ping(context.Context) error { return nil }
func (*authz) Close() error               { return nil }
func setup(t *testing.T) (http.Handler, *authz, *coredata.Services) {
	db := &coretesting.StubIndexedDB{}
	svc, e := coredata.New(db)
	if e != nil {
		t.Fatal(e)
	}
	a := newAuthz()
	cfg := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{"rippling": {Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "token"}}, ActiveUserRelationships: []config.SCIMRelationshipConfig{{Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "employees"}}}}}}
	s, e := scim.NewService(svc.DB, a, "https://gestalt.test", cfg)
	if e != nil {
		t.Fatal(e)
	}
	return scim.NewHandler(s), a, svc
}
func req(t *testing.T, h http.Handler, m, p string, b any) *httptest.ResponseRecorder {
	var x []byte
	if b != nil {
		x, _ = json.Marshal(b)
	}
	r := httptest.NewRequest(m, p, bytes.NewReader(x))
	r.Header.Set("Authorization", "Bearer token")
	if b != nil {
		r.Header.Set("Content-Type", "application/scim+json")
	}
	o := httptest.NewRecorder()
	h.ServeHTTP(o, r)
	return o
}
func TestCompactSCIMUserAndGroupContract(t *testing.T) {
	t.Parallel()
	h, _, _ := setup(t)
	u := req(t, h, "POST", "/scim/v2/Users", map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "alice@valon.com", "active": true, "emails": []any{map[string]any{"value": "alice@valon.com", "type": "work", "primary": true}}})
	if u.Code != 201 {
		t.Fatal(u.Code, u.Body.String())
	}
	var user scim.User
	_ = json.Unmarshal(u.Body.Bytes(), &user)
	if !user.Active {
		t.Fatal("active user did not receive configured projection")
	}
	g := req(t, h, "POST", "/scim/v2/Groups", map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Employees", "members": []any{map[string]any{"value": user.ID}}})
	if g.Code != 201 {
		t.Fatal(g.Code, g.Body.String())
	}
	var group scim.Group
	_ = json.Unmarshal(g.Body.Bytes(), &group)
	if len(group.Members) != 1 || group.Members[0].Value != user.ID {
		t.Fatalf("members=%#v", group.Members)
	}
	get := req(t, h, "GET", "/scim/v2/Groups/"+group.ID, nil)
	if get.Code != 200 {
		t.Fatal(get.Code)
	}
	if get.Header().Get("ETag") == "" {
		t.Fatal("missing ETag")
	}
}
func TestCompactSCIMNamespaceAndETag(t *testing.T) {
	t.Parallel()
	h, _, _ := setup(t)
	a := req(t, h, "POST", "/scim/v2/Users", map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "a@valon.com"})
	var u scim.User
	_ = json.Unmarshal(a.Body.Bytes(), &u)
	b := req(t, h, "GET", "/scim/v2/Users/"+u.ID, nil)
	if b.Code != 200 {
		t.Fatal(b.Code)
	}
	r := httptest.NewRequest("PUT", "/scim/v2/Users/"+u.ID, bytes.NewBufferString(`{"schemas":["`+scim.UserSchemaURN+`"],"userName":"a@valon.com"}`))
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("Content-Type", "application/scim+json")
	r.Header.Set("If-Match", `W/"stale"`)
	o := httptest.NewRecorder()
	h.ServeHTTP(o, r)
	if o.Code != 412 {
		t.Fatalf("stale ETag=%d", o.Code)
	}
}

func TestSCIMResourcesStoreDoesNotDuplicateLiveAuthorizationState(t *testing.T) {
	t.Parallel()
	h, _, services := setup(t)
	user := req(t, h, "POST", "/scim/v2/Users", map[string]any{"schemas": []string{scim.UserSchemaURN}, "userName": "metadata@valon.com", "active": true})
	if user.Code != http.StatusCreated {
		t.Fatal(user.Code, user.Body.String())
	}
	var createdUser scim.User
	if err := json.Unmarshal(user.Body.Bytes(), &createdUser); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(user.Body.Bytes(), []byte(`"name"`)) {
		t.Fatalf("empty User name should be omitted: %s", user.Body.String())
	}
	group := req(t, h, "POST", "/scim/v2/Groups", map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Metadata", "members": []map[string]any{{"value": createdUser.ID}}})
	if group.Code != http.StatusCreated {
		t.Fatal(group.Code, group.Body.String())
	}
	rows, err := services.DB.ObjectStore(coredata.StoreSCIMResources).GetAll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, ok := row["external_id"]; ok {
			t.Fatalf("empty external_id should not be persisted: %#v", row)
		}
		if _, ok := row["profile"]; ok {
			t.Fatalf("empty User profile should not be persisted: %#v", row)
		}
		for _, forbidden := range []string{"active", "members", "groups", "version", "deleted", "pending", "applied_relationships", "ownership", "managed_by", "retry_at"} {
			if _, ok := row[forbidden]; ok {
				t.Fatalf("SCIM metadata row stores live state %q: %#v", forbidden, row)
			}
		}
	}
}

var _ core.AuthorizationProvider = (*authz)(nil)
var _ = config.ServerSCIMConfig{}
