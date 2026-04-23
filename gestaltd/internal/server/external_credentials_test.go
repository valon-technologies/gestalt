package server

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestSubjectHasOtherExternalIdentityLinkTreatsTypedNilProviderAsMissing(t *testing.T) {
	t.Parallel()

	var tokens *coredata.TokenService
	srv := &Server{externalCredentials: tokens}

	got, err := srv.subjectHasOtherExternalIdentityLink(context.Background(), "user:test", externalIdentityRef{}, "")
	if err != nil {
		t.Fatalf("subjectHasOtherExternalIdentityLink error = %v, want nil", err)
	}
	if got {
		t.Fatal("subjectHasOtherExternalIdentityLink = true, want false")
	}
}
