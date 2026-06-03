package appaccess

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestInvocationGrantsFromDependenciesPreservesBroadAccessAndConfiguredMetadata(t *testing.T) {
	t.Parallel()

	grants := InvocationGrantsFromDependencies([]InvocationDependency{
		{
			App:            "frontPorchRestApi",
			Operation:      "vds.schemaVersions",
			CredentialMode: core.ConnectionModeSubject,
			RunAs: &core.RunAsSubject{
				SubjectID:   "service_account:data-schema-explorer",
				DisplayName: "Data Schema Explorer",
			},
			ApplyByDefault: true,
		},
		{
			App:            "frontPorchRestApi",
			Operation:      "vds.dataExport",
			CredentialMode: core.ConnectionModeNone,
			RunAs:          &core.RunAsSubject{SubjectID: "service_account:ignored"},
			ApplyByDefault: false,
		},
	})

	if !grants[InvocationGrantAllApps].AllOperations {
		t.Fatal("wildcard all-operations grant was not preserved")
	}
	frontPorch := grants["frontPorchRestApi"]
	if got := frontPorch.Operations["vds.schemaVersions"]; got != core.ConnectionModeSubject {
		t.Fatalf("schemaVersions credential mode = %q, want subject", got)
	}
	if got := frontPorch.Operations["vds.dataExport"]; got != core.ConnectionModeNone {
		t.Fatalf("dataExport credential mode = %q, want none", got)
	}
	delegation := frontPorch.OperationDelegations["vds.schemaVersions"]
	if delegation.RunAs == nil || delegation.RunAs.SubjectID != "service_account:data-schema-explorer" {
		t.Fatalf("schemaVersions delegation = %#v, want data-schema-explorer service account", delegation)
	}
	if _, ok := frontPorch.OperationDelegations["vds.dataExport"]; ok {
		t.Fatal("ApplyByDefault=false should not attach default delegation")
	}
}

func TestExactInvocationGrantsFromDependencies(t *testing.T) {
	t.Parallel()

	grants := ExactInvocationGrantsFromDependencies([]InvocationDependency{
		{
			App:            "worker",
			Operation:      "run",
			CredentialMode: core.ConnectionModeNone,
		},
		{
			App:     "linear",
			Surface: " graphql ",
		},
	})

	if _, ok := grants[InvocationGrantAllApps]; ok {
		t.Fatalf("exact grants included wildcard grant: %#v", grants)
	}
	if got := grants["worker"].Operations["run"]; got != core.ConnectionModeNone {
		t.Fatalf("worker.run credential mode = %q, want none", got)
	}
	if _, ok := grants["linear"].Surfaces["graphql"]; !ok {
		t.Fatalf("linear surfaces = %#v, want graphql", grants["linear"].Surfaces)
	}
}
