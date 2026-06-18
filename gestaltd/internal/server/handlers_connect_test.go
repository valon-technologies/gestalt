package server

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestCredentialMaterialContextUsesCredentialSubjectWithoutActorMetadata(t *testing.T) {
	t.Parallel()

	actor := &principal.Principal{
		SubjectID:   "user:someone@example.com",
		UserID:      "someone-id",
		DisplayName: "Someone Example",
		Identity:    &core.UserIdentity{Email: "someone@example.com", DisplayName: "Someone Example"},
		Kind:        principal.KindUser,
	}
	tm := credentialMaterial{SubjectID: "service_account:manual-bot"}

	ctx := credentialMaterialContext(context.Background(), actor, tm)
	got := principal.FromContext(ctx)
	if got == nil || got.SubjectID != "service_account:manual-bot" {
		t.Fatalf("principal = %+v, want service_account:manual-bot subject", got)
	}
	if got.UserID != "" || got.DisplayName != "" || got.Identity != nil {
		t.Fatalf("credential principal carried actor metadata: %+v", got)
	}
}

func TestCredentialMaterialContextFallsBackToActorPrincipal(t *testing.T) {
	t.Parallel()

	tm := credentialMaterial{
		ActorSubjectID: "user:someone@example.com",
		ActorUserID:    "someone-id",
	}
	ctx := credentialMaterialContext(context.Background(), nil, tm)
	got := principal.FromContext(ctx)
	if got == nil || got.SubjectID != "user:someone@example.com" || got.UserID != "someone-id" {
		t.Fatalf("principal = %+v, want reconstructed actor", got)
	}
}
