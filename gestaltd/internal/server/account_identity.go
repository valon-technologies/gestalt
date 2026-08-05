package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// accountIdentityMetadataKey is the reserved MetadataJSON key for Connection
// account recognition facts (SCIM-style). Values are JSON, not provider params,
// and are skipped by validateProviderMetadata.
const accountIdentityMetadataKey = "account_identity"

// identityFact is one recognition attribute for a linked account (SCIM multi-valued style).
type identityFact struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// accountIdentity is the projected Connection identity payload.
type accountIdentity struct {
	Facts []identityFact `json:"facts"`
}

// Connection-param metadata keys that are safe, human-meaningful signifiers.
var connectionParamIdentityKinds = map[string]string{
	"subdomain":       "subdomain",
	"host":            "host",
	"api_host":        "host",
	"organization_id": "organization",
}

// Prefer these kinds when assigning primary if none is flagged.
var identityPrimaryKindOrder = []string{
	"email",
	"login",
	"workspace",
	"site",
	"host",
	"subdomain",
	"organization",
	"display_name",
}

func parseMetadataMap(metadataJSON string) (map[string]string, error) {
	m := make(map[string]string)
	if strings.TrimSpace(metadataJSON) == "" {
		return m, nil
	}
	if err := json.Unmarshal([]byte(metadataJSON), &m); err != nil {
		return nil, fmt.Errorf("corrupt MetadataJSON: %w", err)
	}
	return m, nil
}

func marshalMetadataMap(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(b), nil
}

func parseAccountIdentity(metadataJSON string) (*accountIdentity, error) {
	m, err := parseMetadataMap(metadataJSON)
	if err != nil {
		return nil, err
	}
	raw, ok := m[accountIdentityMetadataKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var id accountIdentity
	if err := json.Unmarshal([]byte(raw), &id); err != nil {
		return nil, fmt.Errorf("corrupt account_identity metadata: %w", err)
	}
	id.Facts = normalizeIdentityFacts(id.Facts)
	if len(id.Facts) == 0 {
		return nil, nil
	}
	return &id, nil
}

func setAccountIdentity(metadataJSON string, id *accountIdentity) (string, error) {
	m, err := parseMetadataMap(metadataJSON)
	if err != nil {
		return "", err
	}
	if id == nil || len(id.Facts) == 0 {
		delete(m, accountIdentityMetadataKey)
		return marshalMetadataMap(m)
	}
	id.Facts = normalizeIdentityFacts(id.Facts)
	if len(id.Facts) == 0 {
		delete(m, accountIdentityMetadataKey)
		return marshalMetadataMap(m)
	}
	b, err := json.Marshal(accountIdentity{Facts: id.Facts})
	if err != nil {
		return "", fmt.Errorf("marshal account_identity: %w", err)
	}
	m[accountIdentityMetadataKey] = string(b)
	return marshalMetadataMap(m)
}

func normalizeIdentityFacts(facts []identityFact) []identityFact {
	out := make([]identityFact, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	primaryIdx := -1
	for _, f := range facts {
		kind := strings.TrimSpace(f.Kind)
		value := strings.TrimSpace(f.Value)
		if kind == "" || value == "" {
			continue
		}
		key := kind + "\x00" + value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		fact := identityFact{Kind: kind, Value: value, Primary: f.Primary}
		if fact.Primary {
			if primaryIdx >= 0 {
				out[primaryIdx].Primary = false
			}
			primaryIdx = len(out)
		}
		out = append(out, fact)
	}
	if len(out) == 0 {
		return nil
	}
	if primaryIdx < 0 {
		out[selectPrimaryFactIndex(out)].Primary = true
	}
	return out
}

func selectPrimaryFactIndex(facts []identityFact) int {
	for _, kind := range identityPrimaryKindOrder {
		for i, f := range facts {
			if f.Kind == kind {
				return i
			}
		}
	}
	return 0
}

func identityFromMetadataJSON(metadataJSON string) *accountIdentity {
	id, err := parseAccountIdentity(metadataJSON)
	if err != nil || id == nil {
		return nil
	}
	return id
}

func mergeDiscoveryCandidateIdentity(metadataJSON, candidateName string) (string, error) {
	name := strings.TrimSpace(candidateName)
	if name == "" {
		return metadataJSON, nil
	}
	id, err := parseAccountIdentity(metadataJSON)
	if err != nil {
		return "", err
	}
	if id == nil {
		id = &accountIdentity{}
	}
	id.Facts = append(id.Facts, identityFact{Kind: "site", Value: name})
	return setAccountIdentity(metadataJSON, id)
}

func identityFactsFromConnectionParams(metadataJSON string) []identityFact {
	m, err := parseMetadataMap(metadataJSON)
	if err != nil {
		return nil
	}
	var facts []identityFact
	for key, kind := range connectionParamIdentityKinds {
		if v := strings.TrimSpace(m[key]); v != "" {
			facts = append(facts, identityFact{Kind: kind, Value: v})
		}
	}
	return facts
}

func mergeIdentityFacts(base []identityFact, extra ...identityFact) []identityFact {
	return normalizeIdentityFacts(append(append([]identityFact{}, base...), extra...))
}

func (s *Server) enrichAccountIdentity(ctx context.Context, tm credentialMaterial) (credentialMaterial, error) {
	existing, err := parseAccountIdentity(tm.MetadataJSON)
	if err != nil {
		// Corrupt identity blob: drop it and rebuild from known sources.
		existing = nil
	}
	var facts []identityFact
	if existing != nil {
		facts = append(facts, existing.Facts...)
	}
	facts = mergeIdentityFacts(facts, identityFactsFromConnectionParams(tm.MetadataJSON)...)

	if token := strings.TrimSpace(tm.AccessToken); token != "" {
		facts = mergeIdentityFacts(facts, fetchOAuthAccountIdentityFacts(ctx, token)...)
	}

	if len(tm.Fields) > 0 {
		if email := strings.TrimSpace(tm.Fields["email"]); email != "" {
			facts = mergeIdentityFacts(facts, identityFact{Kind: "email", Value: email})
		}
	}

	if len(facts) == 0 {
		return tm, nil
	}
	merged, err := setAccountIdentity(tm.MetadataJSON, &accountIdentity{Facts: facts})
	if err != nil {
		return tm, err
	}
	tm.MetadataJSON = merged
	return tm, nil
}

func fetchOAuthAccountIdentityFacts(ctx context.Context, accessToken string) []identityFact {
	client := &http.Client{Timeout: 10 * time.Second}
	if facts := fetchGoogleUserInfoFacts(ctx, client, accessToken); len(facts) > 0 {
		return facts
	}
	if facts := fetchGmailProfileFacts(ctx, client, accessToken); len(facts) > 0 {
		return facts
	}
	if facts := fetchSlackAuthTestFacts(ctx, client, accessToken); len(facts) > 0 {
		return facts
	}
	if facts := fetchGitHubUserFacts(ctx, client, accessToken); len(facts) > 0 {
		return facts
	}
	return nil
}

func fetchJSONObject(ctx context.Context, client *http.Client, method, url, accessToken string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func stringField(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

func fetchGoogleUserInfoFacts(ctx context.Context, client *http.Client, accessToken string) []identityFact {
	obj, err := fetchJSONObject(ctx, client, http.MethodGet, "https://openidconnect.googleapis.com/userinfo", accessToken)
	if err != nil {
		return nil
	}
	var facts []identityFact
	if email := stringField(obj, "email"); email != "" {
		facts = append(facts, identityFact{Kind: "email", Value: email, Primary: true})
	}
	if name := stringField(obj, "name"); name != "" {
		facts = append(facts, identityFact{Kind: "display_name", Value: name})
	}
	return facts
}

func fetchGmailProfileFacts(ctx context.Context, client *http.Client, accessToken string) []identityFact {
	obj, err := fetchJSONObject(ctx, client, http.MethodGet, "https://gmail.googleapis.com/gmail/v1/users/me/profile", accessToken)
	if err != nil {
		return nil
	}
	if email := stringField(obj, "emailAddress"); email != "" {
		return []identityFact{{Kind: "email", Value: email, Primary: true}}
	}
	return nil
}

func fetchSlackAuthTestFacts(ctx context.Context, client *http.Client, accessToken string) []identityFact {
	obj, err := fetchJSONObject(ctx, client, http.MethodPost, "https://slack.com/api/auth.test", accessToken)
	if err != nil {
		return nil
	}
	if ok, _ := obj["ok"].(bool); !ok {
		return nil
	}
	var facts []identityFact
	if team := stringField(obj, "team"); team != "" {
		facts = append(facts, identityFact{Kind: "workspace", Value: team, Primary: true})
	}
	if user := stringField(obj, "user"); user != "" {
		facts = append(facts, identityFact{Kind: "login", Value: user})
	}
	return facts
}

func fetchGitHubUserFacts(ctx context.Context, client *http.Client, accessToken string) []identityFact {
	obj, err := fetchJSONObject(ctx, client, http.MethodGet, "https://api.github.com/user", accessToken)
	if err != nil {
		return nil
	}
	var facts []identityFact
	if login := stringField(obj, "login"); login != "" {
		facts = append(facts, identityFact{Kind: "login", Value: login, Primary: true})
	}
	if name := stringField(obj, "name"); name != "" {
		facts = append(facts, identityFact{Kind: "display_name", Value: name})
	}
	if email := stringField(obj, "email"); email != "" {
		facts = append(facts, identityFact{Kind: "email", Value: email})
	}
	return facts
}
