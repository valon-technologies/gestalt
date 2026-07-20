package appregistry

import "errors"

// ErrInstallVersionLocked means another instance is admitting a rollout for this app.
var ErrInstallVersionLocked = errors.New("app rollout admission already in progress")

// ErrAppRolloutActive means the app already has an enrolling or restarting rollout.
var ErrAppRolloutActive = errors.New("app already has an active rollout")

// ErrAppVersionAlreadyInstalled means the requested app version is already in the catalog.
var ErrAppVersionAlreadyInstalled = errors.New("app version is already installed")

// ErrAppCatalogNotEmpty means add was requested for an app already known to the fleet.
var ErrAppCatalogNotEmpty = errors.New("app already has fleet-known versions")

// ErrAppCatalogEmpty means upgrade was requested before the app was added.
var ErrAppCatalogEmpty = errors.New("app has no fleet-known versions")

// ErrAppRegistryBinding means the request does not match the app's deploy-time registry source.
var ErrAppRegistryBinding = errors.New("app registry binding does not match deploy config")

// ErrInstallTimedOut means install work exceeded the bounded post-lock timeout.
var ErrInstallTimedOut = errors.New("app version install timed out")
