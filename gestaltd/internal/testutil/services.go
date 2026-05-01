package testutil

import (
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type Services = coredata.Services

func NewStubServices(t *testing.T) *Services {
	t.Helper()
	svc, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("NewStubServices: %v", err)
	}
	AttachStubExternalCredentials(svc)
	return svc
}

func AttachStubExternalCredentials(svc *Services) *coretesting.StubExternalCredentialProvider {
	if svc == nil {
		return nil
	}
	provider := coretesting.NewStubExternalCredentialProvider()
	svc.ExternalCredentials = provider
	return provider
}
