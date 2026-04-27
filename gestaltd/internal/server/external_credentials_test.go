package server

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestSubjectHasOtherExternalIdentityLinkTreatsTypedNilProviderAsMissing(t *testing.T) {
	t.Parallel()

	var provider *coretesting.StubExternalCredentialProvider
	srv := &Server{externalCredentials: provider}

	got, err := srv.subjectHasOtherExternalIdentityLink(context.Background(), "user:test", externalIdentityRef{}, "")
	if err != nil {
		t.Fatalf("subjectHasOtherExternalIdentityLink error = %v, want nil", err)
	}
	if got {
		t.Fatal("subjectHasOtherExternalIdentityLink = true, want false")
	}
}
