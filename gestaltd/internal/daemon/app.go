package daemon

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func runApp(args []string) error {
	if len(args) == 0 {
		printAppUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printAppUsage(os.Stderr)
		return flag.ErrHelp
	case "publish":
		return runAppPublish(args[1:])
	default:
		return fmt.Errorf("unknown app command %q", args[0])
	}
}

func printAppUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  publish     Publish an installable app version to a configured app registry")
}
