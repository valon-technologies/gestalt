package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

// accountIdentityMetadataKey is the reserved MetadataJSON key for Connection
// account recognition facts (SCIM-style). Only host code may write this key;
// provider/discovery metadata must not set it. Runtime connection-param
// surfaces must strip it via core.ConnectionParamsFromMetadataJSON.
const accountIdentityMetadataKey = core.AccountIdentityMetadataKey
const accountKeyMetadataKey = core.AccountKeyMetadataKey

const oauthIdentityProbeTimeout = 3 * time.Second

// identityFact is one recognition attribute for a linked account (SCIM multi-valued style).
type identityFact struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type oauthAccountIdentity struct {
	AccountID string
	Facts     []identityFact
}

// accountIdentity is the projected Connection identity payload.
type accountIdentity struct {
	Facts []identityFact `json:"facts"`
}

// connectionParamIdentityBinding maps stored connection-param metadata keys to
// identity fact kinds. Order is significant for determinism when multiple
// params are present. Opaque ids (e.g. cloud_id) are intentionally omitted.
//
// Longer-term these bindings should be declared on ConnectionParamDef /
// provider manifests (identityKind); this table is the host fallback until then.
var connectionParamIdentityBindings = []struct {
	Param string
	Kind  string
}{
	{Param: "subdomain", Kind: "subdomain"},
	{Param: "host", Kind: "host"},
	{Param: "api_host", Kind: "host"},
	{Param: "organization_id", Kind: "organization"},
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

// Only facts that identify an account, rather than describe it, participate
// in the canonical key. In particular, display names and discovered site
// labels must never split one account into multiple records.
var accountKeyFactKinds = map[string]struct{}{
	"email":        {},
	"login":        {},
	"workspace":    {},
	"host":         {},
	"subdomain":    {},
	"organization": {},
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

func accountKeyFromMetadataJSON(metadataJSON string) string {
	m, err := parseMetadataMap(metadataJSON)
	if err != nil {
		return ""
	}
	if key := strings.TrimSpace(m[accountKeyMetadataKey]); key != "" {
		return key
	}
	id, err := parseAccountIdentity(metadataJSON)
	if err != nil {
		return ""
	}
	return accountKeyFromIdentity(id)
}

func accountKeyFromProviderID(integration, providerID string) string {
	integration = strings.TrimSpace(integration)
	providerID = strings.TrimSpace(providerID)
	if integration == "" || providerID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(integration + "\x00" + providerID))
	return "provider:v1:" + hex.EncodeToString(digest[:])
}

func accountKeyFromIdentity(id *accountIdentity) string {
	if id == nil || len(id.Facts) == 0 {
		return ""
	}
	factsByKind := make(map[string]string, len(id.Facts))
	for _, fact := range id.Facts {
		kind := strings.ToLower(strings.TrimSpace(fact.Kind))
		value := strings.TrimSpace(fact.Value)
		if _, ok := accountKeyFactKinds[kind]; !ok || value == "" {
			continue
		}
		if _, ok := factsByKind[kind]; !ok {
			factsByKind[kind] = canonicalAccountKeyValue(kind, value)
		}
	}
	// Prefer the provider account's user identifier. Workspace/tenant facts
	// provide the boundary needed when the same login can exist in multiple
	// workspaces. Extra facts such as a later OAuth email probe must not change
	// the key for an already-linked account.
	parts := make([]string, 0, 2)
	if login := factsByKind["login"]; login != "" {
		parts = append(parts, "login="+login)
		if workspace := factsByKind["workspace"]; workspace != "" {
			parts = append(parts, "workspace="+workspace)
		}
	} else if email := factsByKind["email"]; email != "" {
		parts = append(parts, "email="+email)
		if tenant := firstAccountTenantFact(factsByKind); tenant != "" {
			parts = append(parts, tenant)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "v1:" + hex.EncodeToString(digest[:])
}

func firstAccountTenantFact(facts map[string]string) string {
	for _, kind := range []string{"workspace", "organization", "subdomain", "host"} {
		if value := facts[kind]; value != "" {
			return kind + "=" + value
		}
	}
	return ""
}

func canonicalAccountKeyValue(kind, value string) string {
	if kind == "email" || kind == "host" || kind == "subdomain" {
		return strings.ToLower(value)
	}
	return value
}

func setAccountKey(metadataJSON, key string) (string, error) {
	m, err := parseMetadataMap(metadataJSON)
	if err != nil {
		return "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		delete(m, accountKeyMetadataKey)
	} else {
		m[accountKeyMetadataKey] = key
	}
	return marshalMetadataMap(m)
}

func normalizeIdentityFacts(facts []identityFact) []identityFact {
	out := dedupeIdentityFacts(facts)
	if len(out) == 0 {
		return nil
	}
	return ensurePrimaryIdentityFact(out)
}

// dedupeIdentityFacts trims/dedupes and keeps at most one Primary from the
// input (last wins). It does not auto-assign primary — callers that merge
// partial batches must leave that to a final ensurePrimaryIdentityFact.
func dedupeIdentityFacts(facts []identityFact) []identityFact {
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
	return out
}

func clearPrimaryIdentityFlags(facts []identityFact) []identityFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]identityFact, len(facts))
	for i, f := range facts {
		out[i] = identityFact{Kind: f.Kind, Value: f.Value}
	}
	return out
}

func ensurePrimaryIdentityFact(facts []identityFact) []identityFact {
	if len(facts) == 0 {
		return nil
	}
	hasPrimary := false
	for _, f := range facts {
		if f.Primary {
			hasPrimary = true
			break
		}
	}
	if hasPrimary {
		return facts
	}
	facts[selectPrimaryFactIndex(facts)].Primary = true
	return facts
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
		// Corrupt host blob must not block discovery completion.
		id = nil
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
	seenKinds := make(map[string]struct{})
	for _, binding := range connectionParamIdentityBindings {
		if v := strings.TrimSpace(m[binding.Param]); v != "" {
			if _, ok := seenKinds[binding.Kind]; ok {
				// Prefer earlier bindings (e.g. host over api_host).
				continue
			}
			seenKinds[binding.Kind] = struct{}{}
			facts = append(facts, identityFact{Kind: binding.Kind, Value: v})
		}
	}
	return facts
}

func mergeIdentityFacts(base []identityFact, extra ...identityFact) []identityFact {
	// Dedupe only — never auto-assign primary on partial merges, or an early
	// batch (e.g. subdomain) permanently wins over a later higher-rank fact
	// (e.g. email).
	return dedupeIdentityFacts(append(append([]identityFact{}, base...), extra...))
}

func (s *Server) enrichAccountIdentity(ctx context.Context, tm credentialMaterial) credentialMaterial {
	storedAccountKey := accountKeyStoredInMetadataJSON(tm.MetadataJSON)
	existing, err := parseAccountIdentity(tm.MetadataJSON)
	if err != nil {
		// Corrupt identity blob: drop it and rebuild from known sources.
		existing = nil
	}
	var facts []identityFact
	if existing != nil {
		// Drop stored primary flags so this enrich pass can re-rank by kind
		// order (and honor new explicit primaries from OAuth probes).
		facts = append(facts, clearPrimaryIdentityFlags(existing.Facts)...)
	}
	facts = mergeIdentityFacts(facts, identityFactsFromConnectionParams(tm.MetadataJSON)...)

	if len(tm.Fields) > 0 {
		if email := strings.TrimSpace(tm.Fields["email"]); email != "" {
			facts = mergeIdentityFacts(facts, identityFact{Kind: "email", Value: email})
		}
	}

	// OAuth probes are integration-scoped and never run for opaque/manual
	// field credentials (AccessToken may hold a raw secret there).
	if len(tm.Fields) == 0 {
		if token := strings.TrimSpace(tm.AccessToken); token != "" {
			providerIdentity := fetchOAuthAccountIdentity(ctx, tm.Integration, token)
			facts = mergeIdentityFacts(facts, providerIdentity.Facts...)
			if tm.ProviderAccountID == "" {
				tm.ProviderAccountID = providerIdentity.AccountID
			}
		}
	}

	if len(facts) > 0 {
		merged, err := setAccountIdentity(tm.MetadataJSON, &accountIdentity{Facts: facts})
		if err != nil {
			slog.WarnContext(ctx, "account identity enrichment skipped", "integration", tm.Integration, "error", err)
			return tm
		}
		tm.MetadataJSON = merged
	}

	// Preserve an existing key when a provider later returns a richer set of
	// facts. This makes the identity stable across reconnects and leaves the
	// display projection free to evolve.
	if storedAccountKey == "" {
		key := accountKeyFromProviderID(tm.Integration, tm.ProviderAccountID)
		if key != "" {
			if merged, err := setAccountKey(tm.MetadataJSON, key); err == nil {
				tm.MetadataJSON = merged
			} else {
				slog.WarnContext(ctx, "account key enrichment skipped", "integration", tm.Integration, "error", err)
			}
		}
	}
	return tm
}

func accountKeyStoredInMetadataJSON(metadataJSON string) string {
	m, err := parseMetadataMap(metadataJSON)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(m[accountKeyMetadataKey])
}

// oauthIdentitySource selects the single identity probe family for an
// integration. Empty means no outbound OAuth identity probe.
func oauthIdentitySource(integration string) string {
	switch strings.TrimSpace(integration) {
	case "gmail":
		return "gmail"
	case "github":
		return "github"
	case "slack", "slack_v2":
		return "slack"
	case "bigquery", "gcs", "gcp_batch":
		return "google"
	default:
		if strings.HasPrefix(integration, "google_") {
			return "google"
		}
		return ""
	}
}

func fetchOAuthAccountIdentity(ctx context.Context, integration, accessToken string) oauthAccountIdentity {
	source := oauthIdentitySource(integration)
	if source == "" || strings.TrimSpace(accessToken) == "" {
		return oauthAccountIdentity{}
	}
	client := &http.Client{Timeout: oauthIdentityProbeTimeout}
	switch source {
	case "gmail":
		if identity := fetchGoogleUserInfoIdentity(ctx, client, accessToken); len(identity.Facts) > 0 || identity.AccountID != "" {
			return identity
		}
		return oauthAccountIdentity{Facts: fetchGmailProfileFacts(ctx, client, accessToken)}
	case "google":
		return fetchGoogleUserInfoIdentity(ctx, client, accessToken)
	case "slack":
		return fetchSlackAuthTestIdentity(ctx, client, accessToken)
	case "github":
		return fetchGitHubUserIdentity(ctx, client, accessToken)
	default:
		return oauthAccountIdentity{}
	}
}

func fetchOAuthAccountIdentityFacts(ctx context.Context, integration, accessToken string) []identityFact {
	return fetchOAuthAccountIdentity(ctx, integration, accessToken).Facts
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
	return fetchGoogleUserInfoIdentity(ctx, client, accessToken).Facts
}

func fetchGoogleUserInfoIdentity(ctx context.Context, client *http.Client, accessToken string) oauthAccountIdentity {
	obj, err := fetchJSONObject(ctx, client, http.MethodGet, "https://openidconnect.googleapis.com/userinfo", accessToken)
	if err != nil {
		return oauthAccountIdentity{}
	}
	var facts []identityFact
	accountID := stringField(obj, "sub")
	if email := stringField(obj, "email"); email != "" {
		facts = append(facts, identityFact{Kind: "email", Value: email, Primary: true})
	}
	if name := stringField(obj, "name"); name != "" {
		facts = append(facts, identityFact{Kind: "display_name", Value: name})
	}
	return oauthAccountIdentity{AccountID: accountID, Facts: facts}
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
	return fetchSlackAuthTestIdentity(ctx, client, accessToken).Facts
}

func fetchSlackAuthTestIdentity(ctx context.Context, client *http.Client, accessToken string) oauthAccountIdentity {
	obj, err := fetchJSONObject(ctx, client, http.MethodPost, "https://slack.com/api/auth.test", accessToken)
	if err != nil {
		return oauthAccountIdentity{}
	}
	if ok, _ := obj["ok"].(bool); !ok {
		return oauthAccountIdentity{}
	}
	var facts []identityFact
	teamID := stringField(obj, "team_id")
	userID := stringField(obj, "user_id")
	if team := stringField(obj, "team"); team != "" {
		facts = append(facts, identityFact{Kind: "workspace", Value: team, Primary: true})
	}
	if user := stringField(obj, "user"); user != "" {
		facts = append(facts, identityFact{Kind: "login", Value: user})
	}
	accountID := ""
	if teamID != "" && userID != "" {
		accountID = teamID + ":" + userID
	}
	return oauthAccountIdentity{AccountID: accountID, Facts: facts}
}

func fetchGitHubUserFacts(ctx context.Context, client *http.Client, accessToken string) []identityFact {
	return fetchGitHubUserIdentity(ctx, client, accessToken).Facts
}

func fetchGitHubUserIdentity(ctx context.Context, client *http.Client, accessToken string) oauthAccountIdentity {
	obj, err := fetchJSONObject(ctx, client, http.MethodGet, "https://api.github.com/user", accessToken)
	if err != nil {
		return oauthAccountIdentity{}
	}
	var facts []identityFact
	accountID := jsonScalarField(obj, "id", "node_id")
	if login := stringField(obj, "login"); login != "" {
		facts = append(facts, identityFact{Kind: "login", Value: login, Primary: true})
	}
	if name := stringField(obj, "name"); name != "" {
		facts = append(facts, identityFact{Kind: "display_name", Value: name})
	}
	if email := stringField(obj, "email"); email != "" {
		facts = append(facts, identityFact{Kind: "email", Value: email})
	}
	return oauthAccountIdentity{AccountID: accountID, Facts: facts}
}

func jsonScalarField(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch value := value.(type) {
		case string:
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		case float64:
			return fmt.Sprintf("%.0f", value)
		}
	}
	return ""
}
