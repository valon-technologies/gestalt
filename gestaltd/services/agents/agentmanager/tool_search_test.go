package agentmanager

import "testing"

func TestAgentToolSearchTokenVariantsIncludeTicketIssueSynonyms(t *testing.T) {
	t.Parallel()

	tokens := uniqueAgentToolSearchTokens("tickets")
	for _, want := range []string{"tickets", "ticket", "issues", "issue"} {
		if !stringSliceContains(tokens, want) {
			t.Fatalf("uniqueAgentToolSearchTokens(tickets) = %#v, want %q", tokens, want)
		}
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
