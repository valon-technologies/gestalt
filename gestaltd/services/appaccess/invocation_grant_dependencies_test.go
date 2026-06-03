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
