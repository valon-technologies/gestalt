package bootstrap

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/mcpoauth"
)

type registrationAwareDatastore struct {
	coretesting.StubDatastore
}

func (s *registrationAwareDatastore) GetRegistration(context.Context, string, string) (*mcpoauth.Registration, error) {
	return nil, nil
}

func (s *registrationAwareDatastore) StoreRegistration(context.Context, *mcpoauth.Registration) error {
	return nil
}

func (s *registrationAwareDatastore) DeleteRegistration(context.Context, string, string) error {
	return nil
}

func TestBuildRegistrationStorePrefersDatastoreRegistrationStore(t *testing.T) {
	t.Parallel()

	store := &registrationAwareDatastore{}
	got := buildRegistrationStore(Deps{Datastore: store})
	if got != store {
		t.Fatalf("buildRegistrationStore returned %T, want original datastore registration store", got)
	}
}
