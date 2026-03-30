package pluginsource

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

func ValidateVersion(version string) error {
	canonical := versionPrefix + version
	if !semver.IsValid(canonical) {
		return fmt.Errorf("pluginsource: invalid semver %q", version)
	}
	base := canonical
	if i := strings.IndexByte(base, '+'); i != -1 {
		base = base[:i]
	}
	if semver.Canonical(canonical) != base {
		return fmt.Errorf("pluginsource: invalid semver %q", version)
	}
	return nil
}
