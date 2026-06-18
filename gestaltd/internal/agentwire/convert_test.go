package agentwire

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func TestToolRefProtoRoundTripCarriesRunAs(t *testing.T) {
	t.Parallel()

	ref := coreagent.ToolRef{
		App:         "notion",
		Operation:   "search",
		Connection:  "support",
		Instance:    "default",
		Title:       "Search support Notion",
		Description: "Search support pages",
		RunAs: &core.RunAsSubject{
			SubjectID: " service_account:gestalt-support-notion ",
		},
	}

	encoded := ToolRefToProto(ref)
	if got := encoded.GetRunAs().GetId(); got != "service_account:gestalt-support-notion" {
		t.Fatalf("encoded runAs subject id = %q, want service_account:gestalt-support-notion", got)
	}

	decoded := ToolRefFromProto(encoded)
	if !core.RunAsSubjectsEqual(decoded.RunAs, core.NormalizeRunAsSubject(ref.RunAs)) {
		t.Fatalf("decoded runAs = %#v, want %#v", decoded.RunAs, core.NormalizeRunAsSubject(ref.RunAs))
	}
}
