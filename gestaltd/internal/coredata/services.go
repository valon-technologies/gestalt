package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                   *UserService
	ExternalCredentials     core.ExternalCredentialProvider
	ManagedSubjects         *ManagedSubjectService
	AppInstallations        *AppInstallationService
	AppInstallationEvents   *AppInstallationEventService
	DB                      indexeddb.IndexedDB
}

// NewOptions configures coredata bootstrap behavior.
type NewOptions struct {
	// SkipSchemaBootstrap skips CreateObjectStore for host control-plane stores.
	// Use when main-db is delegated to a remote gestaltd that already owns schema.
	SkipSchemaBootstrap bool
}

func New(ds indexeddb.IndexedDB) (*Services, error) {
	return NewWithOptions(context.Background(), ds, NewOptions{})
}

func NewWithContext(ctx context.Context, ds indexeddb.IndexedDB) (*Services, error) {
	return NewWithOptions(ctx, ds, NewOptions{})
}

func NewWithOptions(ctx context.Context, ds indexeddb.IndexedDB, opts NewOptions) (*Services, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !opts.SkipSchemaBootstrap {
		if _, err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err != nil {
			return nil, fmt.Errorf("create users store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreManagedSubjects, ManagedSubjectsSchema); err != nil {
			return nil, fmt.Errorf("create managed_subjects store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppSHAs, AppSHAsSchema); err != nil {
			return nil, fmt.Errorf("create app_shas store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppInstallations, AppInstallationsSchema); err != nil {
			return nil, fmt.Errorf("create app_installations store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppInstallationEvents, AppInstallationEventsSchema); err != nil {
			return nil, fmt.Errorf("create app_installation_events store: %w", err)
		}
	}
	users := NewUserService(ds)
	managedSubjects := NewManagedSubjectService(ds)
	appInstallations := NewAppInstallationService(ds)
	appInstallationEvents := NewAppInstallationEventService(ds)
	return &Services{
		ExternalCredentials:   nil,
		Users:                 users,
		ManagedSubjects:       managedSubjects,
		AppInstallations:      appInstallations,
		AppInstallationEvents: appInstallationEvents,
		DB:                    ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}
