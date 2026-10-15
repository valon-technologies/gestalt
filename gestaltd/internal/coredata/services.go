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
		if err := createObjectStores(ctx, ds, "create", []objectStoreSpec{
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
		}...); err != nil {
			return nil, err
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

type objectStoreSpec struct {
	name   string
	schema *idb.ObjectStoreOptions
}

func createObjectStores(ctx context.Context, ds indexeddb.IndexedDB, action string, stores ...objectStoreSpec) error {
	for _, store := range stores {
		if _, err := ds.CreateObjectStore(ctx, store.name, *store.schema); err != nil {
			return fmt.Errorf("%s %s store: %w", action, store.name, err)
		}
	}
	return nil
}

func ensureManagedSecretStores(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure",
		objectStoreSpec{StoreManagedSecrets, &ManagedSecretsSchema},
		objectStoreSpec{StoreManagedSecretVersions, &ManagedSecretVersionsSchema},
		objectStoreSpec{StoreManagedSecretAuditLogs, &ManagedSecretAuditLogsSchema},
	)
}

func ensureSCIMStores(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure",
		objectStoreSpec{StoreSCIMResources, &SCIMResourcesSchema},
		objectStoreSpec{StoreSCIMConfig, &SCIMConfigSchema},
		objectStoreSpec{StoreSCIMSecrets, &SCIMSecretsSchema},
	)
}

func ensureGroupsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure", objectStoreSpec{StoreGroups, &GroupsSchema})
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}

func ensureDeferredAppRegistryStores(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure",
		objectStoreSpec{StoreAppAutoDeploySettings, &AppAutoDeploySettingsSchema},
		objectStoreSpec{StoreAppVersionRolloutOutcomes, &AppVersionRolloutOutcomesSchema},
		objectStoreSpec{StoreGestaltdInstanceHeartbeats, &GestaltdInstanceHeartbeatsSchema},
		objectStoreSpec{StoreAppVersionRecoveryObservations, &AppVersionRecoveryObservationsSchema},
		objectStoreSpec{StoreAppAccessProfiles, &AppAccessProfilesSchema},
		objectStoreSpec{StoreAppAllowedOperations, &AppAllowedOperationsSchema},
	)
}

func ensureAppAllowedOperationsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure", objectStoreSpec{StoreAppAllowedOperations, &AppAllowedOperationsSchema})
}

func ensureAppAccessProfilesStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure", objectStoreSpec{StoreAppAccessProfiles, &AppAccessProfilesSchema})
}

func ensureAppAutoDeploySettingsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure", objectStoreSpec{StoreAppAutoDeploySettings, &AppAutoDeploySettingsSchema})
}

func ensureGestaltdInstanceHeartbeatsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure", objectStoreSpec{StoreGestaltdInstanceHeartbeats, &GestaltdInstanceHeartbeatsSchema})
}

func ensureAppVersionRecoveryObservationsStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	return createObjectStores(ctx, ds, "ensure", objectStoreSpec{StoreAppVersionRecoveryObservations, &AppVersionRecoveryObservationsSchema})
}
