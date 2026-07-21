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

// ErrInstallTimedOut means install work exceeded the bounded post-lock timeout.
var ErrInstallTimedOut = errors.New("app version install timed out")
