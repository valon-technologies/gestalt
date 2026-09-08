package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAccountAlreadyConnectedMatchesLogicalAccount(t *testing.T) {
	t.Parallel()

	provider := coretesting.NewStubExternalCredentialProvider()
	s := &Server{externalCredentials: provider}
	ctx := context.Background()
	if err := provider.CreateCredential(ctx, &core.ExternalCredential{
		ID:         "existing",
		Subject:    "user:1",
		Audience:   "slack:default",
		Qualifier:  "Valon",
		AccountKey: "slack:v1:T123:U456",
	}); err != nil {
		t.Fatal(err)
	}

	if !s.accountAlreadyConnected(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		AccountKey:   "slack:v1:T123:U456",
	}) {
		t.Fatal("expected matching account key to be reported as already connected")
	}
	if s.accountAlreadyConnected(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		AccountKey:   "slack:v1:T999:U888",
	}) {
		t.Fatal("expected different account key to be reported as new")
	}
}

func TestConnectionSetupFailureDescribesInstanceConflict(t *testing.T) {
	t.Parallel()

	status, message := connectionSetupFailure(&core.CredentialInstanceConflictError{
		Instance:         "Shared label",
		DifferentAccount: true,
	})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", status, http.StatusConflict)
	}
	want := `The instance name "Shared label" is already linked to another account. Choose a different instance name and try again.`
	if message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
}

func TestInstanceInfoDoesNotExposeInternalAccountKey(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(instanceInfo{AccountKey: "internal-only"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "accountKey") {
		t.Fatalf("instance info exposed internal account key: %s", encoded)
	}
}

func TestSelectCredentialGroupsForInstanceRetainsSameAccountDuplicates(t *testing.T) {
	t.Parallel()

	groups := []logicalCredentialGroup{{members: []matchedCredential{
		{credential: &core.ExternalCredential{AccountKey: "shared", Qualifier: "visible"}},
		{credential: &core.ExternalCredential{AccountKey: "shared", Qualifier: "hidden"}},
	}}}
	selected := selectCredentialGroupsForInstance(groups, "visible")
	if len(selected) != 1 || len(selected[0].members) != 2 {
		t.Fatalf("selected groups = %+v, want both same-account instances", selected)
	}
}

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
