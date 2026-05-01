package testutil

import (
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func NewStubServices(t *testing.T) *coredata.Services {
	t.Helper()
	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("NewStubServices: %v", err)
	}
	AttachStubExternalCredentials(svc)
	return svc
}

func AttachStubExternalCredentials(svc *coredata.Services) *coretesting.StubExternalCredentialProvider {
	if svc == nil {
		return nil
	}
	provider := coretesting.NewStubExternalCredentialProvider()
	svc.ExternalCredentials = provider
	return provider
}
