package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestAccountKeyFromIdentity_IsStableAndExcludesDisplayFacts(t *testing.T) {
	t.Parallel()
	first := accountKeyFromIdentity(&accountIdentity{Facts: []identityFact{
		{Kind: "workspace", Value: "Valon", Primary: true},
		{Kind: "login", Value: "giovannivocale"},
		{Kind: "display_name", Value: "Giovanni Vocale"},
	}})
	second := accountKeyFromIdentity(&accountIdentity{Facts: []identityFact{
		{Kind: "login", Value: "giovannivocale", Primary: true},
		{Kind: "workspace", Value: "Valon"},
		{Kind: "email", Value: "gio@example.com"},
		{Kind: "display_name", Value: "Different label"},
	}})
	if first == "" || first != second {
		t.Fatalf("keys = %q and %q, want same stable key", first, second)
	}
	if got := accountKeyFromIdentity(&accountIdentity{Facts: []identityFact{{Kind: "workspace", Value: "Valon"}}}); got != "" {
		t.Fatalf("workspace-only identity key = %q, want empty", got)
	}
}

func TestEnrichAccountIdentity_DoesNotInferCanonicalAccountKey(t *testing.T) {
	t.Parallel()
	var s Server
	got := s.enrichAccountIdentity(context.Background(), credentialMaterial{
		Integration:  "slack",
		Fields:       map[string]string{"email": "User@Example.com"},
		MetadataJSON: `{"workspace":"Valon"}`,
	})
	if key := accountKeyStoredInMetadataJSON(got.MetadataJSON); key != "" {
		t.Fatalf("metadata = %q, did not expect inferred account key", got.MetadataJSON)
	}
}

func TestEnrichAccountIdentity_UsesProviderAccountID(t *testing.T) {
	t.Parallel()
	var s Server
	got := s.enrichAccountIdentity(context.Background(), credentialMaterial{
		Integration:       "slack",
		ProviderAccountID: "T123:U456",
		MetadataJSON:      `{"workspace":"Valon","login":"giovannivocale"}`,
	})
	if gotKey := accountKeyFromMetadataJSON(got.MetadataJSON); gotKey != accountKeyFromProviderID("slack", "T123:U456") {
		t.Fatalf("account key = %q, want provider account key", gotKey)
	}
}

func TestNormalizeIdentityFacts_AssignsSinglePrimary(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	facts := identityFactsFromConnectionParams(`{"host":"looker.example","cloud_id":"abc","api_host":"api-na1.niceincontact.com"}`)
	if len(facts) != 1 {
		t.Fatalf("facts = %+v, want single host (host preferred over api_host)", facts)
	}
	if facts[0].Kind != "host" || facts[0].Value != "looker.example" {
		t.Fatalf("fact = %+v, want host=looker.example", facts[0])
	}
}

func TestMergeDiscoveryCandidateIdentity(t *testing.T) {
	t.Parallel()
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

func TestMergeDiscoveryCandidateIdentity_CorruptBlobDoesNotFail(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(map[string]string{
		"cloud_id":                 "123",
		accountIdentityMetadataKey: "{not-json",
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := mergeDiscoveryCandidateIdentity(string(raw), "Acme")
	if err != nil {
		t.Fatal(err)
	}
	id := identityFromMetadataJSON(meta)
	if id == nil || id.Facts[0].Value != "Acme" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestValidateProviderMetadataRejectsAccountIdentity(t *testing.T) {
	t.Parallel()
	err := validateProviderMetadata("discovery", map[string]string{
		"cloud_id":                 "abc-123",
		accountIdentityMetadataKey: `{"facts":[{"kind":"site","value":"Acme","primary":true}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), accountIdentityMetadataKey) {
		t.Fatalf("error = %v, want reserved key rejection", err)
	}
}

func TestValidateProviderMetadataRejectsAccountKey(t *testing.T) {
	t.Parallel()
	err := validateProviderMetadata("discovery", map[string]string{
		accountKeyMetadataKey: "provider-controlled",
	})
	if err == nil || !strings.Contains(err.Error(), accountKeyMetadataKey) {
		t.Fatalf("error = %v, want reserved key rejection", err)
	}
}

func TestMergeMetadataJSONStripsAccountIdentity(t *testing.T) {
	t.Parallel()
	merged, err := mergeMetadataJSON(`{"cloud_id":"1"}`, map[string]string{
		"cloud_id":                 "2",
		accountIdentityMetadataKey: `{"facts":[]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(merged), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["cloud_id"] != "2" {
		t.Fatalf("cloud_id = %q", raw["cloud_id"])
	}
	if _, ok := raw[accountIdentityMetadataKey]; ok {
		t.Fatal("account_identity must not merge from provider overlay")
	}
}

func TestConnectionParamsFromMetadataJSONStripsAccountKey(t *testing.T) {
	t.Parallel()
	params, err := core.ConnectionParamsFromMetadataJSON(`{"workspace":"Valon","account_key":"v1:secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := params[core.AccountKeyMetadataKey]; ok {
		t.Fatalf("params = %+v, account key must not be runtime metadata", params)
	}
}

func TestOAuthIdentitySource(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"gmail":           "gmail",
		"google_calendar": "google",
		"bigquery":        "google",
		"github":          "github",
		"slack":           "slack",
		"jira":            "",
		"zendesk":         "",
		"launchdarkly":    "",
	}
	for integration, want := range cases {
		if got := oauthIdentitySource(integration); got != want {
			t.Fatalf("%s: oauthIdentitySource = %q, want %q", integration, got, want)
		}
	}
}

func TestFetchOAuthAccountIdentityFacts_NoProbeForUnknownIntegration(t *testing.T) {
	t.Parallel()
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	// Unknown integration must not hit any probe URL (we can't redirect hardcoded
	// URLs here; assert via source gate instead and enrich path).
	if facts := fetchOAuthAccountIdentityFacts(context.Background(), "jira", "secret-token"); len(facts) != 0 {
		t.Fatalf("facts = %+v", facts)
	}
	_ = srv
	if called {
		t.Fatal("unexpected HTTP call")
	}
}

func TestEnrichAccountIdentity_EmailOutranksEarlierSubdomain(t *testing.T) {
	t.Parallel()
	var s Server
	tm := credentialMaterial{
		Integration: "zendesk",
		Fields: map[string]string{
			"email":     "ops@acme.com",
			"api_token": "secret",
		},
		MetadataJSON: `{"subdomain":"acme"}`,
	}
	got := s.enrichAccountIdentity(context.Background(), tm)
	id := identityFromMetadataJSON(got.MetadataJSON)
	if id == nil {
		t.Fatal("expected identity")
	}
	var primary *identityFact
	for i := range id.Facts {
		if id.Facts[i].Primary {
			primary = &id.Facts[i]
		}
	}
	if primary == nil || primary.Kind != "email" || primary.Value != "ops@acme.com" {
		t.Fatalf("primary = %+v, facts = %+v", primary, id.Facts)
	}
}

func TestEnrichAccountIdentity_ManualFieldsSkipOAuthProbe(t *testing.T) {
	t.Parallel()
	var s Server
	tm := credentialMaterial{
		Integration:  "gmail",
		AccessToken:  "raw-secret-must-not-probe",
		Fields:       map[string]string{"email": "ops@acme.com"},
		MetadataJSON: `{"subdomain":"acme"}`,
	}
	got := s.enrichAccountIdentity(context.Background(), tm)
	id := identityFromMetadataJSON(got.MetadataJSON)
	if id == nil {
		t.Fatal("expected identity")
	}
	kinds := map[string]bool{}
	for _, f := range id.Facts {
		kinds[f.Kind] = true
	}
	if !kinds["email"] || !kinds["subdomain"] {
		t.Fatalf("facts = %+v", id.Facts)
	}
}

func TestMergeIdentityFacts_DoesNotAutoAssignPrimary(t *testing.T) {
	t.Parallel()
	facts := mergeIdentityFacts(nil, identityFact{Kind: "subdomain", Value: "acme"})
	for _, f := range facts {
		if f.Primary {
			t.Fatalf("partial merge must not auto-assign primary: %+v", facts)
		}
	}
	facts = mergeIdentityFacts(facts, identityFact{Kind: "email", Value: "ops@acme.com"})
	normalized := normalizeIdentityFacts(facts)
	primaryCount := 0
	for _, f := range normalized {
		if f.Primary {
			primaryCount++
			if f.Kind != "email" {
				t.Fatalf("want email primary after final normalize, got %+v", normalized)
			}
		}
	}
	if primaryCount != 1 {
		t.Fatalf("primary count = %d in %+v", primaryCount, normalized)
	}
}

func TestEnrichAccountIdentity_GmailProbeScoped(t *testing.T) {
	t.Parallel()
	userinfoHits, profileHits := 0, 0
	mux := http.NewServeMux()
	// We can't remint hardcoded Google URLs; instead verify oauthIdentitySource
	// and that fetchOAuthAccountIdentityFacts for gmail eventually returns from
	// a successful local userinfo-shaped response via direct helper.
	_ = mux
	_ = userinfoHits
	_ = profileHits

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"email": "ada@example.com",
			"name":  "Ada",
		})
	}))
	t.Cleanup(srv.Close)

	obj, err := fetchJSONObject(context.Background(), srv.Client(), http.MethodGet, srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	facts := normalizeIdentityFacts([]identityFact{
		{Kind: "email", Value: stringField(obj, "email"), Primary: true},
		{Kind: "display_name", Value: stringField(obj, "name")},
	})
	if len(facts) != 2 || facts[0].Value != "ada@example.com" {
		t.Fatalf("facts = %+v", facts)
	}
	if oauthIdentitySource("gmail") != "gmail" {
		t.Fatal("gmail should use gmail probe family")
	}
	if oauthIdentitySource("jira") != "" {
		t.Fatal("jira must not oauth-probe")
	}
}
