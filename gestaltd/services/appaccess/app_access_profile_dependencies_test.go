package appaccess

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestExactAppAccessProfilesFromDependencies(t *testing.T) {
	t.Parallel()

	profiles := ExactAppAccessProfilesFromDependencies([]AppAccessDependency{
		{
			App:            "documents",
			Operation:      "documents.read",
			CredentialMode: core.ConnectionModeSubject,
			RunAs: &core.RunAsSubject{
				SubjectID: "service_account:document-reader",
			},
			ApplyByDefault: true,
		},
		{
			App:            "documents",
			Operation:      "documents.export",
			CredentialMode: core.ConnectionModeNone,
			RunAs:          &core.RunAsSubject{SubjectID: "service_account:ignored"},
			ApplyByDefault: false,
		},
		{
			App:            "graph",
			Surface:        " GraphQL ",
			CredentialMode: core.ConnectionModeSubject,
		},
	})

	if _, ok := profiles[AppAccessAllApps]; ok {
		t.Fatalf("exact profiles included wildcard profile: %#v", profiles)
	}
	documents := profiles["documents"]
	if got := documents.Operations["documents.read"]; got != core.ConnectionModeSubject {
		t.Fatalf("documents.read credential mode = %q, want subject", got)
	}
	if got := documents.Operations["documents.export"]; got != core.ConnectionModeNone {
		t.Fatalf("documents.export credential mode = %q, want none", got)
	}
	delegation := documents.OperationDelegations["documents.read"]
	if delegation.RunAs == nil || delegation.RunAs.SubjectID != "service_account:document-reader" {
		t.Fatalf("documents.read delegation = %#v, want document-reader service account", delegation)
	}
	if _, ok := documents.OperationDelegations["documents.export"]; ok {
		t.Fatal("ApplyByDefault=false should not attach default delegation")
	}
	graph := profiles["graph"]
	if got := graph.Surfaces["graphql"]; got != core.ConnectionModeSubject {
		t.Fatalf("graphql surface credential mode = %q, want subject", got)
	}
}
