package main

import (
	"os"
	_ "time/tzdata"

	"github.com/valon-technologies/gestalt/server/internal/daemon"
)

var version = "dev"

func main() {
	os.Exit(daemon.Main(daemon.Options{
		Version: version,
		Args:    os.Args[1:],
	}))
}
