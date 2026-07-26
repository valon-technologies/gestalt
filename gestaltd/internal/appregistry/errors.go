package appregistry

import "errors"

// ErrInstallVersionLocked means another instance is admitting a rollout for this app.
var ErrInstallVersionLocked = errors.New("app rollout admission already in progress")

// ErrAppRolloutActive means the app already has an enrolling or restarting rollout.
var ErrAppRolloutActive = errors.New("app already has an active rollout")

// ErrAppVersionAlreadyInstalled means the requested app version is already in the catalog.
var ErrAppVersionAlreadyInstalled = errors.New("app version is already installed")

// ErrAppAlreadyAdded means add was used after the catalog already contained a version.
var ErrAppAlreadyAdded = errors.New("app already has fleet-known versions")

// ErrAppNotAdded means upgrade was used before the first catalog version was added.
var ErrAppNotAdded = errors.New("app has no fleet-known versions")

// ErrRegistrySourceMismatch means the request registry does not match deploy config.
var ErrRegistrySourceMismatch = errors.New("app registry does not match configured source")

// ErrInstallValidationFailed means the candidate failed install-time validation.
var ErrInstallValidationFailed = errors.New("install-time validation failed")

// ErrAppRegistryNotConfigured means the named registry is absent from gestaltd config.
var ErrAppRegistryNotConfigured = errors.New("app registry is not configured")

// ErrInstallTimedOut means install work exceeded the bounded post-lock timeout.
var ErrInstallTimedOut = errors.New("app version install timed out")

// ErrAppVersionExpired means a never-deployed version is past unused retention.
var ErrAppVersionExpired = errors.New("app version is expired")

// ErrAppVersionLocked means a historical version is permanently locked.
var ErrAppVersionLocked = errors.New("app version is permanently locked")
