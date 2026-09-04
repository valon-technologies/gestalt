package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestPersistReconnectRequiredGrantOnlyUsesKnownInstance(t *testing.T) {
	t.Parallel()

	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	metadata := `{"account_key":"provider:v1:shared"}`
	for _, credential := range []*core.ExternalCredential{
		{
			ID:           "credential-old",
			Subject:      "user:1",
			Audience:     "provider:default",
			Qualifier:    "old-label",
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token"},
			CreatedAt:    time.Unix(1, 0),
		},
		{
			ID:           "credential-new",
			Subject:      "user:1",
			Audience:     "provider:default",
			Qualifier:    "new-label",
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "new-token"},
			CreatedAt:    time.Unix(2, 0),
		},
	} {
		if err := provider.UpsertCredential(ctx, credential); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{
		externalCredentials: provider,
		now:                 func() time.Time { return time.Unix(10, 0) },
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.test/?_connection=default&_instance=old-label", nil)
	request = request.WithContext(principal.WithPrincipal(request.Context(), &principal.Principal{
		SubjectID: "user:1",
		UserID:    "1",
		Kind:      principal.KindUser,
	}))

	s.persistReconnectRequiredGrant(request, "provider", invocation.ErrReconnectRequired)

	old, err := provider.GetCredential(ctx, "user:1", "provider:default", "old-label")
	if err != nil {
		t.Fatal(err)
	}
	if old.Grant == nil || old.Grant.RefreshErrorCount != 1 || old.Grant.ExpiresAt == nil || !old.Grant.ExpiresAt.Equal(time.Unix(10, 0)) {
		t.Fatalf("old credential = %+v, want reconnect-required state", old)
	}
	newCredential, err := provider.GetCredential(ctx, "user:1", "provider:default", "new-label")
	if err != nil {
		t.Fatal(err)
	}
	if newCredential.Grant == nil || newCredential.Grant.RefreshErrorCount != 0 {
		t.Fatalf("new credential = %+v, want untouched duplicate", newCredential)
	}

	noInstanceRequest := httptest.NewRequest(http.MethodPost, "http://example.test/?_connection=default", nil)
	noInstanceRequest = noInstanceRequest.WithContext(principal.WithPrincipal(noInstanceRequest.Context(), &principal.Principal{
		SubjectID: "user:1",
		UserID:    "1",
		Kind:      principal.KindUser,
	}))
	s.persistReconnectRequiredGrant(noInstanceRequest, "provider", invocation.ErrReconnectRequired)

	newCredential, err = provider.GetCredential(ctx, "user:1", "provider:default", "new-label")
	if err != nil {
		t.Fatal(err)
	}
	if newCredential.Grant == nil || newCredential.Grant.RefreshErrorCount != 0 {
		t.Fatalf("new credential = %+v, want no guessed duplicate marked", newCredential)
	}
}
