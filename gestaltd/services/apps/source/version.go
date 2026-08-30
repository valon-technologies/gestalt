package source

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/semvervalidate"
)

func ValidateVersion(version string) error {
	if !semvervalidate.Valid(version) {
		return fmt.Errorf("plugin source: invalid semver %q", version)
	}
	return nil
}
