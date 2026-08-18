package appregistry

import "errors"

var (
	ErrPublishDeclarationInvalid  = errors.New("publish declaration is invalid")
	ErrPublishVersionConflict     = errors.New("publish version conflict")
	ErrPublishUnavailable         = errors.New("app registry publish is unavailable")
	ErrPublishUploadMissing       = errors.New("publish upload is missing")
	ErrPublishUploadMismatch      = errors.New("publish upload mismatch")
	ErrPublishArtifactLimit       = errors.New("publish artifact limit exceeded")
	ErrPublishRequiredPlatform    = errors.New("required publish platform missing")
	ErrPublishAppIdentityMismatch = errors.New("publish app identity mismatch")
	ErrPublishRegistryNotEnrolled = errors.New("app is not enrolled in the registry")
	ErrPublishIDMismatch          = errors.New("publish id mismatch")
	ErrPublishReconcileMismatch   = errors.New("published registry entry does not match publish declaration")
	ErrPublishIdentityMismatch    = errors.New("published entry identity mismatch")
)

func PublishHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrPublishUnavailable):
		return 503
	case errors.Is(err, ErrPublishDeclarationInvalid),
		errors.Is(err, ErrPublishRequiredPlatform),
		errors.Is(err, ErrPublishArtifactLimit),
		errors.Is(err, ErrPublishAppIdentityMismatch),
		errors.Is(err, ErrPublishUploadMissing),
		errors.Is(err, ErrPublishIDMismatch):
		return 400
	case errors.Is(err, ErrPublishVersionConflict),
		errors.Is(err, ErrPublishUploadMismatch),
		errors.Is(err, ErrPublishReconcileMismatch),
		errors.Is(err, ErrPublishIdentityMismatch),
		errors.Is(err, ErrRegistryEntryConflict),
		errors.Is(err, ErrIndexVersionConflict),
		errors.Is(err, ErrObjectPreconditionFailed):
		return 409
	case errors.Is(err, ErrPublishRegistryNotEnrolled):
		return 404
	default:
		return 502
	}
}
