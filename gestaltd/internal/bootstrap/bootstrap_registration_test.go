package bootstrap

import (
	"context"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpoauth"
)

type registrationAwareDB struct {
	coretesting.StubIndexedDB
}

func (s *registrationAwareDB) GetRegistration(context.Context, string, string) (*mcpoauth.Registration, error) {
	return nil, nil
}

func (s *registrationAwareDB) StoreRegistration(context.Context, *mcpoauth.Registration) error {
	return nil
}

func (s *registrationAwareDB) DeleteRegistration(context.Context, string, string) error {
	return nil
}

func TestBuildRegistrationStorePrefersIndexedDBRegistrationStore(t *testing.T) {
	t.Parallel()

	db := &registrationAwareDB{}
	svc, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	got := buildRegistrationStore(Deps{Services: svc})
	if got != db {
		t.Fatalf("buildRegistrationStore returned %T, want original registration store", got)
	}
}
