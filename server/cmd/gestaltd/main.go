package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("gestaltd exited", "error", err)
		os.Exit(1)
	}
}
