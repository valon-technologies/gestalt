//go:build !unix

package daemon

import "os/exec"

func processExitCode(exitErr *exec.ExitError) int {
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
