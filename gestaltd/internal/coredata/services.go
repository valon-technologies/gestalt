package coredata

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                          *UserService
	ExternalCredentials            core.ExternalCredentialProvider
	ManagedSubjects                *ManagedSubjectService
	AppVersionChangeRequests       *AppVersionChangeRequestService
	AppVersionInstallLocks         *AppVersionInstallLockService
	GestaltdSourceVersionState     *GestaltdSourceVersionService
	AppRollouts                    *AppRolloutService
	AppInstanceMaterializations    *AppInstanceMaterializationService
	AutoDeploySettings             *AutoDeploySettingsService
	AppVersionRolloutOutcomes      *AppVersionRolloutOutcomeService
	GestaltdInstanceHeartbeats     *GestaltdInstanceHeartbeatService
	AppVersionRecoveryObservations *AppVersionRecoveryObservationService
	RemoteRegistrations            *RemoteRegistrationService
	DB                             indexeddb.IndexedDB
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
		if _, err := ds.CreateObjectStore(ctx, StoreAppVersionChangeRequests, AppVersionChangeRequestsSchema); err != nil {
			return nil, fmt.Errorf("create app_version_change_requests store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppVersionInstallLocks, AppVersionInstallLocksSchema); err != nil {
			return nil, fmt.Errorf("create app_version_install_locks store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreGestaltdSourceVersionState, GestaltdSourceVersionStateSchema); err != nil {
			return nil, fmt.Errorf("create gestaltd_source_version_state store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppRollouts, AppRolloutsSchema); err != nil {
			return nil, fmt.Errorf("create app_rollouts store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppInstanceMaterializations, AppInstanceMaterializationsSchema); err != nil {
			return nil, fmt.Errorf("create app_instance_materializations store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppAutoDeploySettings, AppAutoDeploySettingsSchema); err != nil {
			return nil, fmt.Errorf("create app_auto_deploy_settings store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppVersionRolloutOutcomes, AppVersionRolloutOutcomesSchema); err != nil {
			return nil, fmt.Errorf("create app_version_rollout_outcomes store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreGestaltdInstanceHeartbeats, GestaltdInstanceHeartbeatsSchema); err != nil {
			return nil, fmt.Errorf("create gestaltd_instance_heartbeats store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreAppVersionRecoveryObservations, AppVersionRecoveryObservationsSchema); err != nil {
			return nil, fmt.Errorf("create app_version_recovery_observations store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreRemoteRegistrations, RemoteRegistrationsSchema); err != nil {
			return nil, fmt.Errorf("create remote_registrations store: %w", err)
		}
		if _, err := ds.CreateObjectStore(ctx, StoreRemoteProviders, RemoteProvidersSchema); err != nil {
			return nil, fmt.Errorf("create remote_providers store: %w", err)
		}
	} else if err := ensureDeferredAppRegistryStores(ctx, ds); err != nil {
		return nil, err
	}
	users := NewUserService(ds)
	managedSubjects := NewManagedSubjectService(ds)
	appVersionChangeRequests := NewAppVersionChangeRequestService(ds)
	appVersionInstallLocks := NewAppVersionInstallLockService(ds)
	gestaltdSourceVersions := NewGestaltdSourceVersionService(ds)
	appRollouts := NewAppRolloutService(ds)
	appInstanceMaterializations := NewAppInstanceMaterializationService(ds)
	autoDeploySettings := NewAutoDeploySettingsService(ds)
	appVersionRolloutOutcomes := NewAppVersionRolloutOutcomeService(ds)
	gestaltdInstanceHeartbeats := NewGestaltdInstanceHeartbeatService(ds)
	appVersionRecoveryObservations := NewAppVersionRecoveryObservationService(ds)
	remoteRegistrations := NewRemoteRegistrationService(ds)
	return &Services{
		ExternalCredentials:            nil,
		Users:                          users,
		ManagedSubjects:                managedSubjects,
		AppVersionChangeRequests:       appVersionChangeRequests,
		AppVersionInstallLocks:         appVersionInstallLocks,
		GestaltdSourceVersionState:     gestaltdSourceVersions,
		AppRollouts:                    appRollouts,
		AppInstanceMaterializations:    appInstanceMaterializations,
		AutoDeploySettings:             autoDeploySettings,
		AppVersionRolloutOutcomes:      appVersionRolloutOutcomes,
		GestaltdInstanceHeartbeats:     gestaltdInstanceHeartbeats,
		AppVersionRecoveryObservations: appVersionRecoveryObservations,
		RemoteRegistrations:            remoteRegistrations,
		DB:                             ds,
	}, nil
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}

func ensureDeferredAppRegistryStores(ctx context.Context, ds indexeddb.IndexedDB) error {
	if err := ensureAppAutoDeploySettingsStore(ctx, ds); err != nil {
		return err
	}
	if err := ensureAppVersionRolloutOutcomesStore(ctx, ds); err != nil {
		return err
	}
	if err := ensureGestaltdInstanceHeartbeatsStore(ctx, ds); err != nil {
		return err
	}
	if err := ensureAppVersionRecoveryObservationsStore(ctx, ds); err != nil {
		return err
	}
	return nil
}

func ensureAppAutoDeploySettingsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreAppAutoDeploySettings, AppAutoDeploySettingsSchema); err != nil {
		return fmt.Errorf("ensure app_auto_deploy_settings store: %w", err)
	}
	return nil
}

func ensureGestaltdInstanceHeartbeatsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreGestaltdInstanceHeartbeats, GestaltdInstanceHeartbeatsSchema); err != nil {
		return fmt.Errorf("ensure gestaltd_instance_heartbeats store: %w", err)
	}
	return nil
}

func ensureAppVersionRecoveryObservationsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreAppVersionRecoveryObservations, AppVersionRecoveryObservationsSchema); err != nil {
		return fmt.Errorf("ensure app_version_recovery_observations store: %w", err)
	}
	return nil
}
