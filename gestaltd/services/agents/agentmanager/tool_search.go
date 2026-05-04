package agentmanager

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type agentToolSearchDocument struct {
	Plugin      string `json:"plugin"`
	Operation   string `json:"operation"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Parameters  string `json:"parameters"`
	Tags        string `json:"tags"`
}

func mentionedAgentToolSearchProviders(query string, providerNames []string) []string {
	queryTokenSets := agentToolSearchTokenSets(query)
	if len(queryTokenSets) == 0 {
		return nil
	}
	mentioned := make([]string, 0, len(providerNames))
	for _, providerName := range providerNames {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			continue
		}
		providerTokenSets := agentToolSearchTokenSets(providerName)
		if len(providerTokenSets) == 0 {
			continue
		}
		if (len(providerTokenSets) == 1 && agentToolSearchTokenSetContainsAny(queryTokenSets, providerTokenSets[0])) ||
			agentToolSearchContainsTokenPhrase(queryTokenSets, providerTokenSets) {
			mentioned = append(mentioned, providerName)
		}
	}
	sort.Strings(mentioned)
	return mentioned
}

func rankAgentToolSearchCandidates(query string, candidates []agentToolSearchCandidate) ([]agentToolSearchCandidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	searchText := agentToolSearchText(query)
	if searchText == "" {
		out := append([]agentToolSearchCandidate(nil), candidates...)
		sortAgentToolSearchCandidates(out)
		return out, nil
	}

	searchIndex, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		return nil, err
	}
	defer func() { _ = searchIndex.Close() }()

	for i := range candidates {
		if err := searchIndex.Index(strconv.Itoa(i), agentToolSearchDoc(candidates[i])); err != nil {
			return nil, err
		}
	}

	searchReq := bleve.NewSearchRequest(agentToolSearchQuery(searchText))
	searchReq.Size = len(candidates)
	searchResp, err := searchIndex.Search(searchReq)
	if err != nil {
		return nil, err
	}

	ranked := make([]agentToolSearchRankedCandidate, 0, len(searchResp.Hits))
	for _, hit := range searchResp.Hits {
		candidateIndex, err := strconv.Atoi(hit.ID)
		if err != nil || candidateIndex < 0 || candidateIndex >= len(candidates) {
			continue
		}
		ranked = append(ranked, agentToolSearchRankedCandidate{
			candidate: candidates[candidateIndex],
			score:     hit.Score,
			index:     candidateIndex,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if cmp := compareAgentToolSearchCandidates(ranked[i].candidate, ranked[j].candidate); cmp != 0 {
			return cmp < 0
		}
		return ranked[i].index < ranked[j].index
	})

	out := make([]agentToolSearchCandidate, 0, len(ranked))
	for i := range ranked {
		candidate := ranked[i].candidate
		candidate.score = ranked[i].score
		out = append(out, candidate)
	}
	return out, nil
}

type agentToolSearchRankedCandidate struct {
	candidate agentToolSearchCandidate
	score     float64
	index     int
}

func agentToolSearchQuery(searchText string) blevequery.Query {
	queries := []blevequery.Query{
		agentToolSearchMatchQuery("plugin", searchText, 12),
		agentToolSearchMatchQuery("operation", searchText, 9),
		agentToolSearchMatchQuery("title", searchText, 8),
		agentToolSearchMatchQuery("parameters", searchText, 5),
		agentToolSearchMatchQuery("description", searchText, 4),
		agentToolSearchMatchQuery("tags", searchText, 3),
	}
	return bleve.NewDisjunctionQuery(queries...)
}

func agentToolSearchMatchQuery(field, searchText string, boost float64) *blevequery.MatchQuery {
	q := bleve.NewMatchQuery(searchText)
	q.SetField(field)
	q.SetBoost(boost)
	return q
}

func agentToolSearchDoc(candidate agentToolSearchCandidate) agentToolSearchDocument {
	op := candidate.operation
	cat := candidate.catalog
	var catalogName, catalogDisplayName, catalogDescription string
	if cat != nil {
		catalogName = cat.Name
		catalogDisplayName = cat.DisplayName
		catalogDescription = cat.Description
	}
	return agentToolSearchDocument{
		Plugin:      agentToolSearchText(candidate.ref.Plugin + " " + catalogName + " " + catalogDisplayName),
		Operation:   agentToolSearchText(candidate.ref.Operation + " " + op.ID + " " + op.ProviderID + " " + op.Path),
		Title:       agentToolSearchText(op.Title),
		Description: agentToolSearchText(catalogDescription + " " + op.Description),
		Parameters:  agentToolSearchText(agentToolSearchParameterText(op)),
		Tags:        agentToolSearchText(strings.Join(op.Tags, " ")),
	}
}

func agentToolSearchParameterText(op catalog.CatalogOperation) string {
	parts := make([]string, 0, len(op.Parameters)*3)
	for _, param := range op.Parameters {
		parts = append(parts, param.Name, param.WireName, param.Description)
	}
	parts = append(parts, agentToolSearchSchemaText(op.InputSchema))
	return strings.Join(parts, " ")
}

func agentToolSearchSchemaText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	var parts []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for key, value := range typed {
				parts = append(parts, key)
				walk(value)
			}
		case []any:
			for _, value := range typed {
				walk(value)
			}
		case string:
			parts = append(parts, typed)
		}
	}
	walk(value)
	return strings.Join(parts, " ")
}

func sortAgentToolSearchCandidates(candidates []agentToolSearchCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareAgentToolSearchCandidates(candidates[i], candidates[j]) < 0
	})
}

func compareAgentToolSearchCandidates(a, b agentToolSearchCandidate) int {
	if a.ref.Plugin != b.ref.Plugin {
		return strings.Compare(a.ref.Plugin, b.ref.Plugin)
	}
	if a.ref.Operation != b.ref.Operation {
		return strings.Compare(a.ref.Operation, b.ref.Operation)
	}
	if a.ref.Connection != b.ref.Connection {
		return strings.Compare(a.ref.Connection, b.ref.Connection)
	}
	if a.ref.Instance != b.ref.Instance {
		return strings.Compare(a.ref.Instance, b.ref.Instance)
	}
	return strings.Compare(string(a.ref.CredentialMode), string(b.ref.CredentialMode))
}

func agentToolSearchText(value string) string {
	return strings.Join(uniqueAgentToolSearchTokens(value), " ")
}

func uniqueAgentToolSearchTokens(value string) []string {
	raw := splitAgentToolSearchTokens(value)
	out := make([]string, 0, len(raw)*2)
	seen := make(map[string]struct{}, len(raw)*2)
	for _, token := range raw {
		if agentToolSearchStopWord(token) {
			continue
		}
		for _, normalized := range agentToolSearchTokenVariants(token) {
			if normalized == "" || agentToolSearchStopWord(normalized) {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}

func agentToolSearchTokenSets(value string) []map[string]bool {
	raw := splitAgentToolSearchTokens(value)
	out := make([]map[string]bool, 0, len(raw))
	for _, token := range raw {
		variants := uniqueAgentToolSearchTokens(token)
		if len(variants) == 0 {
			continue
		}
		set := make(map[string]bool, len(variants))
		for _, variant := range variants {
			set[variant] = true
		}
		out = append(out, set)
	}
	return out
}

func agentToolSearchTokenSetContainsAny(haystack []map[string]bool, needle map[string]bool) bool {
	for _, tokenSet := range haystack {
		if agentToolSearchTokenSetsOverlap(tokenSet, needle) {
			return true
		}
	}
	return false
}

func agentToolSearchContainsTokenPhrase(haystack, needle []map[string]bool) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for start := 0; start <= len(haystack)-len(needle); start++ {
		match := true
		for offset := range needle {
			if !agentToolSearchTokenSetsOverlap(haystack[start+offset], needle[offset]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func agentToolSearchTokenSetsOverlap(a, b map[string]bool) bool {
	for token := range a {
		if b[token] {
			return true
		}
	}
	return false
}

func splitAgentToolSearchTokens(value string) []string {
	var b strings.Builder
	var prev rune
	for _, r := range value {
		if unicode.IsUpper(r) && prev != 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			b.WriteRune(' ')
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(' ')
		}
		prev = r
	}
	return strings.Fields(b.String())
}

func agentToolSearchTokenVariants(token string) []string {
	token = strings.TrimSpace(strings.ToLower(token))
	if token == "" {
		return nil
	}
	variants := []string{token}
	switch {
	case strings.HasSuffix(token, "ies") && len(token) > 4:
		variants = append(variants, strings.TrimSuffix(token, "ies")+"y")
	case strings.HasSuffix(token, "s") && len(token) > 3 && !strings.HasSuffix(token, "ss"):
		variants = append(variants, strings.TrimSuffix(token, "s"))
	}
	switch token {
	case "issue", "issues":
		variants = append(variants, "ticket", "tickets")
	case "ticket", "tickets":
		variants = append(variants, "issue", "issues")
	}
	return variants
}

func agentToolSearchStopWord(token string) bool {
	switch token {
	case "a", "an", "and", "are", "as", "at", "be", "by", "for", "from", "get", "i", "in", "is", "it", "me", "my", "of", "on", "or", "please", "show", "the", "to", "use", "with":
		return true
	default:
		return false
	}
}
