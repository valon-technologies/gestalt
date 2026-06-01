package operator

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func lockMaterializationPlatform(platforms []struct{ GOOS, GOARCH string }) string {
	if len(platforms) == 0 {
		return providerpkg.CurrentPlatformString()
	}
	return providerpkg.PlatformString(platforms[0].GOOS, platforms[0].GOARCH)
}

func resolveMaterializationPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return providerpkg.CurrentPlatformString()
	}
	return platform
}

func pathsMaterializationPlatform(paths lifecyclePaths) string {
	return resolveMaterializationPlatform(paths.lockMaterializationPlatform)
}

func materializationPlatformForLockfile(platform string) string {
	platform = resolveMaterializationPlatform(platform)
	if platform == providerpkg.CurrentPlatformString() {
		return ""
	}
	return platform
}

func lockCheckMaterializationPlatform(platforms []struct{ GOOS, GOARCH string }, committed *Lockfile) string {
	if len(platforms) > 0 {
		return lockMaterializationPlatform(platforms)
	}
	if committed != nil {
		if p := strings.TrimSpace(committed.MaterializationPlatform); p != "" {
			return resolveMaterializationPlatform(p)
		}
	}
	return lockMaterializationPlatform(nil)
}
