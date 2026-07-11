package appregistry

import "errors"

// ErrInstallVersionLocked means another instance is already installing this app version.
var ErrInstallVersionLocked = errors.New("app version install already in progress")

// ErrAppVersionAlreadyInstalled means the requested app version is already in the catalog.
var ErrAppVersionAlreadyInstalled = errors.New("app version is already installed")

// ErrInstallTimedOut means install work exceeded the bounded post-lock timeout.
var ErrInstallTimedOut = errors.New("app version install timed out")
