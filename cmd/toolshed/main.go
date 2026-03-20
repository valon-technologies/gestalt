package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/valon-technologies/toolshed/plugins/integrations/datadog"
	_ "github.com/valon-technologies/toolshed/plugins/integrations/hex"
	_ "github.com/valon-technologies/toolshed/plugins/integrations/slack"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runServe(args)
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "mcp":
		return runMCP(args[1:])
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}
