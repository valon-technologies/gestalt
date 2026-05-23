package daemon

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
)

type exitCodeError struct {
	code int
}

func (e exitCodeError) Error() string {
	return fmt.Sprintf("process exited with status %d", e.code)
}

func exitCode(err error) (int, bool) {
	var exitErr exitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.code, true
	}
	return 0, false
}

// Main is the gestaltd process entrypoint. It runs the CLI and returns the
// process exit code. Callers should typically invoke it from main via os.Exit.
//
// Exit policy:
//   - Run succeeds: 0
//   - flag.ErrHelp: 0 without logging
//   - exitCodeError (intentional child process exit passthrough): child code without logging
//   - all other errors: log "gestaltd exited" and return 1
func Main(opts Options) int {
	return mainExitCode(Run(opts))
}

func mainExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if code, ok := exitCode(err); ok {
		return code
	}
	slog.Error("gestaltd exited", "error", err)
	return 1
}
