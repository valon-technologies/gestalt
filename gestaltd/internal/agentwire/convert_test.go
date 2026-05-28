package agentwire

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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
			SubjectID:   " service_account:gestalt-support-notion ",
			DisplayName: " Gestalt Support Notion ",
			AuthSource:  " notion_service_account ",
		},
		RunAsExternalIdentity: &core.ExternalIdentityRef{
			Type: " notion_workspace ",
			ID:   " valon-support ",
		},
	}

	encoded := ToolRefToProto(ref)
	if got := encoded.GetRunAs().GetKind(); got != "service_account" {
		t.Fatalf("encoded runAs subject kind = %q, want service_account", got)
	}
	if got := encoded.GetRunAs().GetCredentialSubjectId(); got != "service_account:gestalt-support-notion" {
		t.Fatalf("encoded runAs credential subject = %q, want normalized default", got)
	}

	decoded := ToolRefFromProto(encoded)
	if !core.RunAsSubjectsEqual(decoded.RunAs, core.NormalizeRunAsSubject(ref.RunAs)) {
		t.Fatalf("decoded runAs = %#v, want %#v", decoded.RunAs, core.NormalizeRunAsSubject(ref.RunAs))
	}
	if !core.ExternalIdentityRefsEqual(decoded.RunAsExternalIdentity, core.NormalizeExternalIdentityRef(ref.RunAsExternalIdentity)) {
		t.Fatalf("decoded runAs external identity = %#v, want %#v", decoded.RunAsExternalIdentity, core.NormalizeExternalIdentityRef(ref.RunAsExternalIdentity))
	}
}

func TestToolRefFromProtoDropsMalformedRunAsExternalIdentity(t *testing.T) {
	t.Parallel()

	decoded := ToolRefFromProto(&proto.AgentToolRef{
		App:       "notion",
		Operation: "search",
		RunAs: &proto.SubjectContext{
			Id: "service_account:gestalt-support-notion",
		},
		RunAsExternalIdentity: &proto.ExternalIdentityContext{
			Type: "notion_workspace",
		},
	})

	if decoded.RunAs == nil || decoded.RunAs.SubjectKind != "service_account" {
		t.Fatalf("decoded runAs = %#v, want normalized subject", decoded.RunAs)
	}
	if decoded.RunAsExternalIdentity != nil {
		t.Fatalf("decoded malformed external identity = %#v, want nil", decoded.RunAsExternalIdentity)
	}
}

func TestToolRefsFromProtoSkipsNilRefs(t *testing.T) {
	t.Parallel()

	decoded := ToolRefsFromProto([]*proto.AgentToolRef{
		nil,
		{App: "github", Operation: "search"},
	})
	if len(decoded) != 1 {
		t.Fatalf("decoded refs = %#v, want one ref", decoded)
	}
	if decoded[0].App != "github" || decoded[0].Operation != "search" {
		t.Fatalf("decoded ref = %#v, want github search", decoded[0])
	}
}
