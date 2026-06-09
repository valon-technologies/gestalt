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
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAuthorizationAPICheckAccess(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()

	resp := doAuthorizationJSONRequest(t, http.MethodPost, ts.URL+"/api/v1/authorization/check-access", `{
		"subject": {"type": "subject", "id": "user:alice"},
		"action": {"name": "view"},
		"resource": {"type": "group", "id": "engineering"}
	}`)
	defer resp.Body.Close()
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

func TestAuthorizationAPIListRelationships(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()

	resp := doAuthorizationJSONRequest(t, http.MethodGet, ts.URL+"/api/v1/authorization/relationships?subjectType=subject&subjectId=user%3Aalice&relation=member&resourceType=group&resourceId=engineering&sourceLayer=runtime&pageSize=25&pageToken=next", "")
	defer resp.Body.Close()
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

func TestAuthorizationAPIAddRelationship(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()

	resp := doAuthorizationJSONRequest(t, http.MethodPost, ts.URL+"/api/v1/authorization/relationships", `{
		"relationship": {
			"tuple": {
				"target": {"subject": {"type": "subject", "id": "user:alice"}},
				"relation": "member",
				"resource": {"type": "group", "id": "engineering"}
			},
			"sourceLayer": "SOURCE_LAYER_RUNTIME"
		}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if got := authz.addRelationshipRequest.GetRelationship().GetTuple().GetRelation(); got != "member" {
		t.Fatalf("relation = %q, want member", got)
	}
}

func TestAuthorizationAPIDeleteRelationship(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()

	resp := doAuthorizationJSONRequest(t, http.MethodDelete, ts.URL+"/api/v1/authorization/relationships", `{
		"relationshipTuple": {
			"target": {"subject": {"type": "subject", "id": "user:alice"}},
			"relation": "member",
			"resource": {"type": "group", "id": "engineering"}
		}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if got := authz.deleteRelationshipRequest.GetRelationshipTuple().GetResource().GetId(); got != "engineering" {
		t.Fatalf("resource id = %q, want engineering", got)
	}
}

func TestAuthorizationAPIGetActiveModelRef(t *testing.T) {
	authz := &authorizationAPITestProvider{}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	defer ts.Close()

	resp := doAuthorizationJSONRequest(t, http.MethodGet, ts.URL+"/api/v1/authorization/models/active", "")
	defer resp.Body.Close()
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

type authorizationAPITestProvider struct {
	core.AuthorizationProvider

	checkAccessRequest        *proto.CheckAccessRequest
	listRelationshipsRequest  *proto.ListRelationshipsRequest
	addRelationshipRequest    *proto.AddRelationshipRequest
	deleteRelationshipRequest *proto.DeleteRelationshipRequest
}

func (p *authorizationAPITestProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.checkAccessRequest = req
	return &proto.CheckAccessResponse{Allowed: true, ModelId: "model-1"}, nil
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
	p.addRelationshipRequest = req
	return &proto.AddRelationshipResponse{Relationship: req.GetRelationship()}, nil
}

func (p *authorizationAPITestProvider) DeleteRelationship(_ context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	p.deleteRelationshipRequest = req
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *authorizationAPITestProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{
		Model: &proto.AuthorizationModelRef{
			Id:        "model-1",
			Version:   "v1",
			CreatedAt: timestamppb.Now(),
		},
	}, nil
}
