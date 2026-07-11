package appregistry

import "errors"

// ErrInstallVersionLocked means another instance is already installing this app version.
var ErrInstallVersionLocked = errors.New("app version install already in progress")
