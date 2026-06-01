package operator

import (
	"fmt"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// LockPlatform is an os/arch pair for lock materialization or archive hashing.
type LockPlatform struct {
	GOOS   string
	GOARCH string
}

func lockMaterializationPlatform(platforms []LockPlatform) string {
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

func lockCheckMaterializationPlatform(platforms []LockPlatform, committed *Lockfile) string {
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

func bootstrapCommittedLockPaths(configPaths []string, state StatePaths) (*config.Config, lifecyclePaths, error) {
	cfg, err := loadConfigForLifecycle(configPaths, state, true)
	if err != nil {
		return nil, lifecyclePaths{}, fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayRemotePluginConfigPaths(configPaths, cfg); err != nil {
		return nil, lifecyclePaths{}, fmt.Errorf("loading config: %v", err)
	}
	if err := applyAppScope(cfg, state); err != nil {
		return nil, lifecyclePaths{}, err
	}
	return cfg, resolveLifecyclePaths(configPaths, cfg, state), nil
}

func peekCommittedLockfile(configPaths []string, state StatePaths) (lifecyclePaths, *Lockfile, error) {
	_, paths, err := bootstrapCommittedLockPaths(configPaths, state)
	if err != nil {
		return lifecyclePaths{}, nil, err
	}
	committed, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		return paths, nil, err
	}
	return paths, committed, nil
}

func resolveLockCheckPlatform(configPaths []string, state StatePaths, platforms []LockPlatform) (string, error) {
	if len(platforms) > 0 {
		return lockMaterializationPlatform(platforms), nil
	}
	paths, committed, err := peekCommittedLockfile(configPaths, state)
	if err != nil {
		if os.IsNotExist(err) {
			return lockMaterializationPlatform(nil), nil
		}
		return "", fmt.Errorf("read lockfile at %s: %w", paths.lockfilePath, err)
	}
	return lockCheckMaterializationPlatform(platforms, committed), nil
}

func lockPlatformMismatchError(committed *Lockfile, platforms []LockPlatform) error {
	if committed == nil || len(platforms) == 0 {
		return nil
	}
	stored := strings.TrimSpace(committed.MaterializationPlatform)
	if stored == "" {
		return nil
	}
	requested := lockMaterializationPlatform(platforms)
	if resolveMaterializationPlatform(stored) == requested {
		return nil
	}
	return fmt.Errorf(
		"lockfile was materialized for platform %q; re-run check with `--platform %s` (not %q)",
		stored,
		stored,
		requested,
	)
}
