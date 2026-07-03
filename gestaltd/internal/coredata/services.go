package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users               *UserService
	ExternalCredentials core.ExternalCredentialProvider
	ManagedSubjects     *ManagedSubjectService
	DB                  indexeddb.IndexedDB
}

func New(ds indexeddb.IndexedDB) (*Services, error) {
	return NewWithContext(context.Background(), ds)
}

func NewWithContext(ctx context.Context, ds indexeddb.IndexedDB) (*Services, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := ds.CreateObjectStore(ctx, StoreUsers, UsersSchema); err != nil {
		return nil, fmt.Errorf("create users store: %w", err)
	}
	if _, err := ds.CreateObjectStore(ctx, StoreManagedSubjects, ManagedSubjectsSchema); err != nil {
		return nil, fmt.Errorf("create managed_subjects store: %w", err)
	}
	if _, err := ds.CreateObjectStore(ctx, StoreAppSHAs, AppSHAsSchema); err != nil {
		return nil, fmt.Errorf("create app_shas store: %w", err)
	}
	users := NewUserService(ds)
	managedSubjects := NewManagedSubjectService(ds)
	return &Services{
		ExternalCredentials: nil,
		Users:               users,
		ManagedSubjects:     managedSubjects,
		DB:                  ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}
