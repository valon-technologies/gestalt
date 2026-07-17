package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAuthorizationAPICheckAccess(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	resp := doAuthorizationJSONRequest(t, http.MethodPost, ts.URL+"/api/v1/authorization/check-access", `{
		"subject": {"type": "subject", "id": "user:alice"},
		"action": {"name": "view"},
		"resource": {"type": "group", "id": "engineering"}
	}`)
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if got := authz.checkAccessRequest.GetSubject().GetId(); got != "user:alice" {
		t.Fatalf("subject id = %q, want user:alice", got)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["allowed"] != true || body["modelId"] != "model-1" {
		t.Fatalf("response = %#v, want allowed true model-1", body)
	}
}

func TestAuthorizationAPICheckAccessDeniedIncludesAllowedFalse(t *testing.T) {
	authz := &authorizationAPITestProvider{checkAccessDenied: true}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	resp := doAuthorizationJSONRequest(t, http.MethodPost, ts.URL+"/api/v1/authorization/check-access", `{
		"subject": {"type": "subject", "id": "user:alice"},
		"action": {"name": "admin"},
		"resource": {"type": "group", "id": "engineering"}
	}`)
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	allowed, ok := body["allowed"].(bool)
	if !ok || allowed {
		t.Fatalf("allowed = %#v, want explicit false", body["allowed"])
	}
}

func TestAuthorizationAPIListRelationships(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	resp := doAuthorizationJSONRequest(t, http.MethodGet, ts.URL+"/api/v1/authorization/relationships?subjectType=subject&subjectId=user%3Aalice&relation=member&resourceType=group&resourceId=engineering&sourceLayer=runtime&pageSize=25&pageToken=next", "")
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	req := authz.listRelationshipsRequest
	if req.GetPageSize() != 25 || req.GetPageToken() != "next" {
		t.Fatalf("pagination = (%d, %q), want (25, next)", req.GetPageSize(), req.GetPageToken())
	}
	filter := req.GetFilter()
	if filter.GetTarget().GetSubject().GetType() != "subject" || filter.GetTarget().GetSubject().GetId() != "user:alice" {
		t.Fatalf("target subject = %#v", filter.GetTarget().GetSubject())
	}
	if filter.GetRelation() != "member" || filter.GetResource().GetType() != "group" || filter.GetResource().GetId() != "engineering" {
		t.Fatalf("filter = %#v", filter)
	}
	if filter.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
		t.Fatalf("source layer = %v, want runtime", filter.GetSourceLayer())
	}
}

func TestAuthorizationAPIListRelationshipsRejectsPartialEntityFilters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "subject type only", query: "subjectType=subject"},
		{name: "subject id only", query: "subjectId=user%3Aalice"},
		{name: "resource type only", query: "resourceType=group"},
		{name: "resource id only", query: "resourceId=engineering"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authz := &authorizationAPITestProvider{}
			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Authorization = authz
			})
			defer ts.Close()
			t.Parallel()

			resp := doAuthorizationJSONRequest(t, http.MethodGet, ts.URL+"/api/v1/authorization/relationships?"+tc.query, "")
			defer closeResponseBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
			}
			if authz.listRelationshipsRequest != nil {
				t.Fatalf("ListRelationships request = %#v, want nil", authz.listRelationshipsRequest)
			}
		})
	}
}

func TestAuthorizationAPIRelationshipMutationsNotExposed(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		resp := doAuthorizationJSONRequest(t, method, ts.URL+"/api/v1/authorization/relationships", `{
			"relationshipTuple": {
				"target": {"subject": {"type": "subject", "id": "user:alice"}},
				"relation": "member",
				"resource": {"type": "group", "id": "engineering"}
			}
		}`)
		closeResponseBody(t, resp)
		switch resp.StatusCode {
		case http.StatusNotFound, http.StatusMethodNotAllowed:
		default:
			t.Fatalf("%s status = %d, want 404 or 405", method, resp.StatusCode)
		}
	}
}

func TestAuthorizationAPIGetActiveModelRef(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	resp := doAuthorizationJSONRequest(t, http.MethodGet, ts.URL+"/api/v1/authorization/models/active", "")
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	var body map[string]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["model"]["id"] != "model-1" || body["model"]["version"] != "v1" {
		t.Fatalf("model = %#v, want model-1/v1", body["model"])
	}
}

func TestAuthorizationAPIPropagatesAuthenticatedPrincipal(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	svc := testutil.NewStubServices(t)
	user := seedUser(t, svc, "authorization-user@test.local")
	plaintext := scopedTestBearerToken(user.ID, "")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.Authorization = authz
		cfg.Services = svc
	})
	defer ts.Close()
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/authorization/models/active", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET active model: %v", err)
	}
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	wantSubjectID := principal.UserSubjectID(user.ID)
	if authz.subjectID != wantSubjectID {
		t.Fatalf("SubjectID = %q, want %q", authz.subjectID, wantSubjectID)
	}
	if authz.entry != invocation.EntryHTTP {
		t.Fatalf("entry = %q, want %q", authz.entry, invocation.EntryHTTP)
	}
}

func TestAuthorizationAPIListActiveModelResourceTypes(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()
	t.Parallel()

	resp := doAuthorizationJSONRequest(t, http.MethodGet, ts.URL+"/api/v1/authorization/models/active/resource-types?name=group&sourceLayer=static_config&pageSize=10&pageToken=next", "")
	defer closeResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	req := authz.listResourceTypesRequest
	if req.GetFilter().GetName() != "group" || req.GetFilter().GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG {
		t.Fatalf("filter = %#v", req.GetFilter())
	}
	if req.GetPageSize() != 10 || req.GetPageToken() != "next" {
		t.Fatalf("pagination = (%d, %q), want (10, next)", req.GetPageSize(), req.GetPageToken())
	}
}

func doAuthorizationJSONRequest(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if strings.TrimSpace(body) != "" {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

type authorizationAPITestProvider struct {
	core.AuthorizationProvider

	checkAccessRequest       *proto.CheckAccessRequest
	checkAccessDenied        bool
	listRelationshipsRequest *proto.ListRelationshipsRequest
	listResourceTypesRequest *proto.ListActiveModelResourceTypesRequest
	subjectID                string
	entry                    invocation.Entry
}

func (p *authorizationAPITestProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.checkAccessRequest = req
	return &proto.CheckAccessResponse{Allowed: !p.checkAccessDenied, ModelId: "model-1"}, nil
}

func (p *authorizationAPITestProvider) ListRelationships(_ context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	p.listRelationshipsRequest = req
	return &proto.ListRelationshipsResponse{
		Relationships: []*proto.Relationship{
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{
							Subject: &proto.Subject{Type: "subject", Id: "user:alice"},
						},
					},
					Relation: "member",
					Resource: &proto.Resource{Type: "group", Id: "engineering"},
				},
			},
		},
	}, nil
}

func (p *authorizationAPITestProvider) AddRelationship(_ context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{Relationship: req.GetRelationship()}, nil
}

func (p *authorizationAPITestProvider) DeleteRelationship(_ context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *authorizationAPITestProvider) GetActiveModelRef(ctx context.Context) (*proto.GetActiveModelRefResponse, error) {
	if caller := principal.FromContext(ctx); caller != nil {
		p.subjectID = caller.SubjectID
	}
	p.entry = invocation.EntryFromContext(ctx)
	return &proto.GetActiveModelRefResponse{
		Model: &proto.AuthorizationModelRef{
			Id:        "model-1",
			Version:   "v1",
			CreatedAt: timestamppb.Now(),
		},
	}, nil
}

func (p *authorizationAPITestProvider) ListActiveModelResourceTypes(_ context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	p.listResourceTypesRequest = req
	return &proto.ListActiveModelResourceTypesResponse{
		ModelId: "model-1",
		ResourceTypes: []*proto.AuthorizationModelResourceType{
			{Name: "group", SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG},
		},
	}, nil
}
