package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeIdentityFacts_AssignsSinglePrimary(t *testing.T) {
	facts := normalizeIdentityFacts([]identityFact{
		{Kind: "display_name", Value: "Ada"},
		{Kind: "email", Value: "ada@example.com"},
		{Kind: "email", Value: "ada@example.com"}, // duplicate
	})
	if len(facts) != 2 {
		t.Fatalf("facts = %d, want 2", len(facts))
	}
	primaryCount := 0
	for _, f := range facts {
		if f.Primary {
			primaryCount++
			if f.Kind != "email" {
				t.Fatalf("primary kind = %q, want email", f.Kind)
			}
		}
	}
	if primaryCount != 1 {
		t.Fatalf("primary count = %d, want 1", primaryCount)
	}
}

func TestNormalizeIdentityFacts_KeepsExplicitPrimary(t *testing.T) {
	facts := normalizeIdentityFacts([]identityFact{
		{Kind: "email", Value: "a@example.com"},
		{Kind: "workspace", Value: "Acme", Primary: true},
	})
	if len(facts) != 2 {
		t.Fatalf("facts = %d, want 2", len(facts))
	}
	if !facts[1].Primary || facts[0].Primary {
		t.Fatalf("expected workspace primary, got %+v", facts)
	}
}

func TestSetAndParseAccountIdentityRoundTrip(t *testing.T) {
	meta, err := setAccountIdentity(`{"subdomain":"acme"}`, &accountIdentity{
		Facts: []identityFact{
			{Kind: "subdomain", Value: "acme", Primary: true},
			{Kind: "email", Value: "ops@acme.com"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(meta), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["subdomain"] != "acme" {
		t.Fatalf("subdomain = %q", raw["subdomain"])
	}
	if raw[accountIdentityMetadataKey] == "" {
		t.Fatal("expected account_identity key")
	}
	id := identityFromMetadataJSON(meta)
	if id == nil || len(id.Facts) != 2 {
		t.Fatalf("identity = %+v", id)
	}
}

func TestIdentityFactsFromConnectionParams(t *testing.T) {
	facts := identityFactsFromConnectionParams(`{"host":"looker.example","cloud_id":"abc","api_host":"api-na1.niceincontact.com"}`)
	kinds := map[string]string{}
	for _, f := range facts {
		kinds[f.Kind] = f.Value
	}
	if kinds["host"] == "" {
		t.Fatalf("expected host fact, got %+v", facts)
	}
	if _, ok := kinds["cloud_id"]; ok {
		t.Fatal("cloud_id must not become an identity fact")
	}
}

func TestMergeDiscoveryCandidateIdentity(t *testing.T) {
	meta, err := mergeDiscoveryCandidateIdentity(`{"cloud_id":"123"}`, "Acme Jira")
	if err != nil {
		t.Fatal(err)
	}
	id := identityFromMetadataJSON(meta)
	if id == nil || len(id.Facts) != 1 || id.Facts[0].Kind != "site" || id.Facts[0].Value != "Acme Jira" {
		t.Fatalf("identity = %+v", id)
	}
	if !id.Facts[0].Primary {
		t.Fatal("expected site primary")
	}
}

func TestFetchGoogleUserInfoFacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"email": "ada@example.com",
			"name":  "Ada Lovelace",
		})
	}))
	t.Cleanup(srv.Close)

	obj, err := fetchJSONObject(t.Context(), srv.Client(), http.MethodGet, srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	facts := normalizeIdentityFacts([]identityFact{
		{Kind: "email", Value: stringField(obj, "email"), Primary: true},
		{Kind: "display_name", Value: stringField(obj, "name")},
	})
	if len(facts) != 2 || !facts[0].Primary || facts[0].Value != "ada@example.com" {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestValidateProviderMetadataSkipsAccountIdentity(t *testing.T) {
	err := validateProviderMetadata("discovery", map[string]string{
		"cloud_id":                  "abc-123",
		accountIdentityMetadataKey: `[{"kind":"site","value":"Acme","primary":true}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
}
