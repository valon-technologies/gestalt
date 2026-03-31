//go:build !linux && !darwin

package sandbox

import (
	"fmt"
	"os/exec"
)

func Wrap(_ *Policy, _ *exec.Cmd) (*exec.Cmd, func(), error) {
	return nil, nil, fmt.Errorf("sandbox not supported on this platform")
}

func RunSubcommand(_ []string) error {
	return fmt.Errorf("sandbox subcommand is not supported on this platform")
}

func DefaultReadOnlyPaths() []string {
	return nil
}
