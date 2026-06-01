package operator

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// lockMaterializationPlatform returns the OS/arch used for committed-lock phase 1
// (materialization, static validation manifests). When --platform is omitted, the
// host platform is used. When multiple platforms are passed, phase 1 uses the first;
// downloadPlatformArchives still hashes the full list in phase 2.
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

// pathsMaterializationPlatform returns the OS/arch used for archive resolution and
// materialization. It reads lifecyclePaths.lockMaterializationPlatform, which is
// set only during committed lock / lock --check (see prepareCommittedLockAtPaths).
func pathsMaterializationPlatform(paths lifecyclePaths) string {
	return resolveMaterializationPlatform(paths.lockMaterializationPlatform)
}

// materializationPlatformForLockfile returns the platform string to persist in the
// committed lockfile. Omitted when phase 1 used the host platform so existing
// CI/host-native locks stay unchanged.
func materializationPlatformForLockfile(platform string) string {
	platform = resolveMaterializationPlatform(platform)
	if platform == providerpkg.CurrentPlatformString() {
		return ""
	}
	return platform
}

// lockCheckMaterializationPlatform selects the phase-1 platform for lock --check.
// Explicit --platform wins; otherwise the committed lockfile's stored platform is
// used so darwin developers can verify linux-target locks without passing flags.
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
