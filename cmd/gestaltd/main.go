package main

import (
	"log"
	"os"

	_ "github.com/valon-technologies/gestalt/plugins/integrations/datadog"
	_ "github.com/valon-technologies/gestalt/plugins/integrations/hex"
	_ "github.com/valon-technologies/gestalt/plugins/integrations/slack"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
