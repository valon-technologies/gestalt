package coredata

import (
	"context"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type Services struct {
	Users                          *UserService
	Groups                         *GroupService
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
	ConnectionInstancePreferences  *ConnectionInstancePreferenceService
	AppAccessProfiles              *AppAccessProfileService
	AppAllowedOperations           *AppAllowedOperationsService
	SCIMConfig                     *SCIMConfigService
	ManagedSecrets                 *ManagedSecretService
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
		for _, store := range []struct {
			name   string
			schema *idb.ObjectStoreOptions
		}{
			{StoreUsers, &UsersSchema},
			{StoreManagedSubjects, &ManagedSubjectsSchema},
			{StoreAppSHAs, &AppSHAsSchema},
			{StoreAppVersionChangeRequests, &AppVersionChangeRequestsSchema},
			{StoreAppVersionInstallLocks, &AppVersionInstallLocksSchema},
			{StoreGestaltdSourceVersionState, &GestaltdSourceVersionStateSchema},
			{StoreAppRollouts, &AppRolloutsSchema},
			{StoreAppInstanceMaterializations, &AppInstanceMaterializationsSchema},
			{StoreAppAutoDeploySettings, &AppAutoDeploySettingsSchema},
			{StoreAppVersionRolloutOutcomes, &AppVersionRolloutOutcomesSchema},
			{StoreGestaltdInstanceHeartbeats, &GestaltdInstanceHeartbeatsSchema},
			{StoreAppVersionRecoveryObservations, &AppVersionRecoveryObservationsSchema},
			{StoreRemoteRegistrations, &RemoteRegistrationsSchema},
			{StoreRemoteProviders, &RemoteProvidersSchema},
			{StoreConnectionInstancePreferences, &ConnectionInstancePreferencesSchema},
			{StoreAppAccessProfiles, &AppAccessProfilesSchema},
			{StoreAppAllowedOperations, &AppAllowedOperationsSchema},
			{StoreSCIMResources, &SCIMResourcesSchema},
			{StoreSCIMConfig, &SCIMConfigSchema},
			{StoreSCIMSecrets, &SCIMSecretsSchema},
			{StoreManagedSecrets, &ManagedSecretsSchema},
			{StoreManagedSecretVersions, &ManagedSecretVersionsSchema},
			{StoreManagedSecretAuditLogs, &ManagedSecretAuditLogsSchema},
		} {
			if _, err := ds.CreateObjectStore(ctx, store.name, *store.schema); err != nil {
				return nil, fmt.Errorf("create %s store: %w", store.name, err)
			}
		}
	} else if err := ensureDeferredAppRegistryStores(ctx, ds); err != nil {
		return nil, err
	} else if err := ensureSCIMStores(ctx, ds); err != nil {
		return nil, err
	} else if err := ensureManagedSecretStores(ctx, ds); err != nil {
		return nil, err
	}
	if err := ensureGroupsStore(ctx, ds); err != nil {
		return nil, err
	}
	users := NewUserService(ds)
	groups := NewGroupService(ds)
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
	connectionInstancePreferences := NewConnectionInstancePreferenceService(ds)
	appAccessProfiles := NewAppAccessProfileService(ds)
	appAllowedOperations := NewAppAllowedOperationsService(ds)
	scimConfig := NewSCIMConfigService(ds)
	managedSecrets := NewManagedSecretService(ds)
	return &Services{
		ExternalCredentials:            nil,
		Users:                          users,
		Groups:                         groups,
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
		ConnectionInstancePreferences:  connectionInstancePreferences,
		AppAccessProfiles:              appAccessProfiles,
		AppAllowedOperations:           appAllowedOperations,
		SCIMConfig:                     scimConfig,
		ManagedSecrets:                 managedSecrets,
		DB:                             ds,
	}, nil
}

func ensureManagedSecretStores(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreManagedSecrets, ManagedSecretsSchema); err != nil {
		return fmt.Errorf("ensure managed_secrets store: %w", err)
	}
	if _, err := ds.CreateObjectStore(ctx, StoreManagedSecretVersions, ManagedSecretVersionsSchema); err != nil {
		return fmt.Errorf("ensure managed_secret_versions store: %w", err)
	}
	if _, err := ds.CreateObjectStore(ctx, StoreManagedSecretAuditLogs, ManagedSecretAuditLogsSchema); err != nil {
		return fmt.Errorf("ensure managed_secret_audit_logs store: %w", err)
	}
	return nil
}

func ensureSCIMStores(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreSCIMResources, SCIMResourcesSchema); err != nil {
		return fmt.Errorf("ensure scim_resources store: %w", err)
	}
	if _, err := ds.CreateObjectStore(ctx, StoreSCIMConfig, SCIMConfigSchema); err != nil {
		return fmt.Errorf("ensure scim_config store: %w", err)
	}
	if _, err := ds.CreateObjectStore(ctx, StoreSCIMSecrets, SCIMSecretsSchema); err != nil {
		return fmt.Errorf("ensure scim_secrets store: %w", err)
	}
	return nil
}

func ensureGroupsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreGroups, GroupsSchema); err != nil {
		return fmt.Errorf("ensure groups store: %w", err)
	}
	return nil
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
	if err := ensureAppAccessProfilesStore(ctx, ds); err != nil {
		return err
	}
	if err := ensureAppAllowedOperationsStore(ctx, ds); err != nil {
		return err
	}
	return nil
}

func ensureAppAllowedOperationsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreAppAllowedOperations, AppAllowedOperationsSchema); err != nil {
		return fmt.Errorf("ensure app_allowed_operations store: %w", err)
	}
	return nil
}

func ensureAppAccessProfilesStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreAppAccessProfiles, AppAccessProfilesSchema); err != nil {
		return fmt.Errorf("ensure app_access_profiles store: %w", err)
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
