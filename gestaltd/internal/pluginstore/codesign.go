package pluginstore

import (
	"fmt"
	"os/exec"
	"runtime"
)

// adhocCodesignDarwin ad-hoc signs a Mach-O binary on macOS to strip the
// linker-signed flag (0x20000) that Go's linker sets. macOS 26+ rejects
// binaries with this flag. This is a no-op on non-macOS platforms and when
// the path is empty.
func adhocCodesignDarwin(path string) error {
	if runtime.GOOS != "darwin" || path == "" {
		return nil
	}
	out, err := exec.Command("codesign", "--force", "--sign", "-", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign %s: %w\n%s", path, err, out)
	}
	return nil
}
