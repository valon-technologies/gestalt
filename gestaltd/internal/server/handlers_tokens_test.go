package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateTokenRequestDecodesExpiresIn(t *testing.T) {
	t.Parallel()

	// With expiresIn present
	var req createTokenRequest
	if err := json.Unmarshal([]byte(`{"name":"ci","scopes":"my-app","expiresIn":7776000}`), &req); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if req.Name != "ci" || req.Scopes != "my-app" {
		t.Fatalf("name=%q scopes=%q, want ci/my-app", req.Name, req.Scopes)
	}
	if req.ExpiresIn == nil || *req.ExpiresIn != 7776000 {
		t.Fatalf("ExpiresIn = %v, want 7776000", req.ExpiresIn)
	}

	// Without expiresIn (omitted)
	var req2 createTokenRequest
	if err := json.Unmarshal([]byte(`{"name":"ci","scopes":"my-app"}`), &req2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if req2.ExpiresIn != nil {
		t.Fatalf("ExpiresIn = %v, want nil when omitted", req2.ExpiresIn)
	}
}

func TestCreateTokenRequestRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	var req createTokenRequest
	dec := json.NewDecoder(strings.NewReader(`{"name":"ci","scopes":"my-app","unknown":123}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err == nil {
		t.Fatal("Decode() error = nil, want unknown field error")
	}
}
