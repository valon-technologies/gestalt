package appregistry

import (
	"errors"
)

func isTerminalFinalizeError(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrPublishUploadMismatch),
		errors.Is(err, ErrPublishVersionConflict),
		errors.Is(err, ErrPublishDeclarationInvalid),
		errors.Is(err, ErrPublishAppIdentityMismatch),
		errors.Is(err, ErrPublishRegistryNotEnrolled),
		errors.Is(err, ErrPublishArtifactLimit),
		errors.Is(err, ErrPublishRequiredPlatform),
		errors.Is(err, ErrObjectPreconditionFailed):
		return true
	default:
		return isTerminalPublishConflict(err)
	}
}
