package appregistry_test

import (
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestInstallValidationReasonFrom(t *testing.T) {
	t.Parallel()

	reasonErr := &appregistry.InstallValidationError{
		Reason: appregistry.InstallValidationPlatformArtifactMissing,
		Detail: "registry version has no artifact for platform linux/amd64",
	}

	reason, ok := appregistry.InstallValidationReasonFrom(reasonErr)
	if !ok {
		t.Fatal("InstallValidationReasonFrom = false")
	}
	if reason != appregistry.InstallValidationPlatformArtifactMissing {
		t.Fatalf("reason = %q", reason)
	}
	if !errors.Is(reasonErr, appregistry.ErrInstallValidationFailed) {
		t.Fatalf("errors.Is = false, want ErrInstallValidationFailed")
	}
}
