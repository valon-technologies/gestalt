package pluginsource

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func ValidateVersion(version string) error {
	if strings.HasPrefix(version, "v") {
		return fmt.Errorf("pluginsource: version must not have leading 'v': %q", version)
	}
	if _, err := semver.StrictNewVersion(version); err != nil {
		return fmt.Errorf("pluginsource: invalid semver %q: %w", version, err)
	}
	return nil
}
