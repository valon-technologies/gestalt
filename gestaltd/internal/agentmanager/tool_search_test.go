package agentmanager

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestSearchToolsRanksExactOperationMatchesAheadOfTitleMatches(t *testing.T) {
	t.Parallel()

	manager := newAgentToolSearchRankingManager(t, "source_control", []catalog.CatalogOperation{
		{
			ID:          "reviews.search",
			Title:       "Pulls List",
			Description: "Search review metadata.",
			ReadOnly:    true,
		},
		{
			ID:          "pulls.list",
			Title:       "Fetch Reviews",
			Description: "Fetch review branches.",
			ReadOnly:    true,
		},
	})
	resp := searchAgentToolsForRanking(t, manager, "pulls.list")
	if got := resp.Tools[0].Target.Operation; got != "pulls.list" {
		t.Fatalf("top search result = %q, want pulls.list: %#v", got, resp.Tools)
	}
}

func TestSearchToolsRanksDescriptionOnlyIntent(t *testing.T) {
	t.Parallel()

	manager := newAgentToolSearchRankingManager(t, "payments", []catalog.CatalogOperation{
		{
			ID:          "accounts.list",
			Title:       "List Accounts",
			Description: "List account records.",
			ReadOnly:    true,
		},
		{
			ID:          "adjustments.create",
			Title:       "Create Adjustment",
			Description: "Create a refund for a customer payment.",
		},
	})
	resp := searchAgentToolsForRanking(t, manager, "refund customer payment")
	if got := resp.Tools[0].Target.Operation; got != "adjustments.create" {
		t.Fatalf("top search result = %q, want adjustments.create: %#v", got, resp.Tools)
	}
}

func TestSearchToolsRanksPullRequestAliasTowardListingIntent(t *testing.T) {
	t.Parallel()

	manager := newAgentToolSearchRankingManager(t, "codehost", []catalog.CatalogOperation{
		{
			ID:          "pulls.merge",
			Title:       "Merge Pull Request",
			Description: "Merge a pull request into the base branch.",
		},
		{
			ID:          "pulls.list",
			Title:       "List Pull Requests",
			Description: "List open pull requests for a repository.",
			ReadOnly:    true,
		},
	})
	resp := searchAgentToolsForRanking(t, manager, "open prs")
	if got := resp.Tools[0].Target.Operation; got != "pulls.list" {
		t.Fatalf("top search result = %q, want pulls.list: %#v", got, resp.Tools)
	}
}

func TestSearchToolsRanksMessageAliasTowardDMIntent(t *testing.T) {
	t.Parallel()

	manager := newAgentToolSearchRankingManager(t, "chat", []catalog.CatalogOperation{
		{
			ID:          "channels.post",
			Title:       "Post Channel Update",
			Description: "Post an update to a shared channel.",
		},
		{
			ID:          "messages.send",
			Title:       "Send Message",
			Description: "Send a private message to a user.",
		},
	})
	resp := searchAgentToolsForRanking(t, manager, "dm user")
	if got := resp.Tools[0].Target.Operation; got != "messages.send" {
		t.Fatalf("top search result = %q, want messages.send: %#v", got, resp.Tools)
	}
}

func TestSearchToolsRanksTaskAliasTowardIssueIntent(t *testing.T) {
	t.Parallel()

	manager := newAgentToolSearchRankingManager(t, "tracker", []catalog.CatalogOperation{
		{
			ID:          "projects.list",
			Title:       "List Projects",
			Description: "List project workspaces.",
			ReadOnly:    true,
		},
		{
			ID:          "tasks.search",
			Title:       "Search Tasks",
			Description: "Search task records assigned to users.",
			ReadOnly:    true,
		},
	})
	resp := searchAgentToolsForRanking(t, manager, "assigned issues")
	if got := resp.Tools[0].Target.Operation; got != "tasks.search" {
		t.Fatalf("top search result = %q, want tasks.search: %#v", got, resp.Tools)
	}
}

func TestSearchToolsDoesNotTreatPullRequestListingAsMergeIntent(t *testing.T) {
	t.Parallel()

	manager := newAgentToolSearchRankingManager(t, "codehost", []catalog.CatalogOperation{
		{
			ID:          "pulls.list",
			Title:       "List Pull Requests",
			Description: "List open pull requests for a repository.",
			ReadOnly:    true,
		},
		{
			ID:          "branches.combine",
			Title:       "Combine Branches",
			Description: "Merge branch changes into a repository.",
		},
	})
	resp := searchAgentToolsForRanking(t, manager, "merge")
	if got := resp.Tools[0].Target.Operation; got != "branches.combine" {
		t.Fatalf("top search result = %q, want branches.combine: %#v", got, resp.Tools)
	}
}

func newAgentToolSearchRankingManager(t *testing.T, name string, ops []catalog.CatalogOperation) *Manager {
	t.Helper()

	provider := &catalogCountingProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        name,
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				DisplayName: name,
				Operations:  ops,
			},
		},
	}
	return New(Config{Providers: testutil.NewProviderRegistry(t, provider)})
}

func searchAgentToolsForRanking(t *testing.T, manager *Manager, query string) *coreagent.SearchToolsResponse {
	t.Helper()

	resp, err := manager.SearchTools(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
	}, coreagent.SearchToolsRequest{
		Query:      query,
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("SearchTools returned %d tools, want 1: %#v", len(resp.Tools), resp.Tools)
	}
	return resp
}
