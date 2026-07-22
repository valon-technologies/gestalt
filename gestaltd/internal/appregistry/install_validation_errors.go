package appregistry

import (
	"errors"
	"fmt"
)

// InstallValidationReason identifies which install-time validation check failed.
type InstallValidationReason string

const (
	InstallValidationPlatformArtifactMissing InstallValidationReason = "platform_artifact_missing"

	InstallValidationGestaltdVersionIncompatible InstallValidationReason = "gestaltd_version_incompatible"
	InstallValidationGestaltdVersionInvalid      InstallValidationReason = "gestaltd_version_invalid"
	InstallValidationGestaltdVersionUnknown      InstallValidationReason = "gestaltd_version_unknown"

	InstallValidationDependencyNotInstalled       InstallValidationReason = "dependency_not_installed"
	InstallValidationDependencyVersionUnsatisfied InstallValidationReason = "dependency_version_unsatisfied"
	InstallValidationDependencyMetadataMissing    InstallValidationReason = "dependency_metadata_missing"
	InstallValidationDependencyOperationMissing   InstallValidationReason = "dependency_operation_missing"
	InstallValidationDependencyOperationSchema    InstallValidationReason = "dependency_operation_schema_mismatch"

	InstallValidationReverseDependentMetadataMissing    InstallValidationReason = "reverse_dependent_metadata_missing"
	InstallValidationReverseDependentVersionUnsatisfied InstallValidationReason = "reverse_dependent_version_unsatisfied"
	InstallValidationReverseDependentOperationMissing   InstallValidationReason = "reverse_dependent_operation_missing"
	InstallValidationReverseDependentOperationSchema    InstallValidationReason = "reverse_dependent_operation_schema_mismatch"
)

// InstallValidationError is a typed install-time validation failure.
type InstallValidationError struct {
	Reason InstallValidationReason
	Detail string
}

func (e *InstallValidationError) Error() string {
	if e == nil {
		return ErrInstallValidationFailed.Error()
	}
	if e.Detail == "" {
		return fmt.Sprintf("%s: %s", ErrInstallValidationFailed, e.Reason)
	}
	return fmt.Sprintf("%s: %s: %s", ErrInstallValidationFailed, e.Reason, e.Detail)
}

func (e *InstallValidationError) Unwrap() error {
	return ErrInstallValidationFailed
}

// InstallValidationReasonFrom returns the validation reason when err is an InstallValidationError.
func InstallValidationReasonFrom(err error) (InstallValidationReason, bool) {
	var validationErr *InstallValidationError
	if !errors.As(err, &validationErr) || validationErr == nil {
		return "", false
	}
	return validationErr.Reason, true
}

func installValidationError(reason InstallValidationReason, detail string) error {
	return &InstallValidationError{Reason: reason, Detail: detail}
}

func validationFetchError(reason InstallValidationReason, subject string, err error) error {
	if errors.Is(err, ErrRegistryDocumentNotFound) {
		return installValidationError(reason, fmt.Sprintf("%s: published metadata not found", subject))
	}
	return fmt.Errorf("%s: %w", subject, err)
}
