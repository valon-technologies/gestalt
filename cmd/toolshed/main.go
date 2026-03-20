package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/valon-technologies/gestalt/plugins/integrations/datadog"
	_ "github.com/valon-technologies/gestalt/plugins/integrations/hex"
	_ "github.com/valon-technologies/gestalt/plugins/integrations/slack"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

const usage = `Usage: toolshed <command> [flags]

Commands:
  serve     Start the API server (default)
  dev       Start API server + web UI for local development
  config    Validate configuration and print resolved values

Run 'toolshed <command> -help' for command-specific flags.
`

func run(args []string) error {
	if len(args) == 0 {
		return runServe(args)
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "dev":
		return runDev(args[1:])
	case "config":
		return runConfigCheck(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		if strings.HasPrefix(args[0], "-") {
			return runServe(args)
		}
		return fmt.Errorf("unknown command: %s", args[0])
	}
}
