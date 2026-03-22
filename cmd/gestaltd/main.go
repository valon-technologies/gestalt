package main

import (
	"errors"
	"flag"
	"log"
	"os"

	_ "github.com/valon-technologies/gestalt/plugins/integrations/datadog"
	_ "github.com/valon-technologies/gestalt/plugins/integrations/hex"
	_ "github.com/valon-technologies/gestalt/plugins/integrations/slack"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}
