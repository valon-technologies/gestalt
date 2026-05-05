package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"
	_ "time/tzdata"

	"github.com/valon-technologies/gestalt/server/internal/daemon"
)

var version = "dev"

func main() {
	if err := daemon.Run(daemon.Options{
		Version: version,
		Args:    os.Args[1:],
	}); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		if code, ok := daemon.ExitCode(err); ok {
			os.Exit(code)
		}
		slog.Error("gestaltd exited", "error", err)
		os.Exit(1)
	}
}
