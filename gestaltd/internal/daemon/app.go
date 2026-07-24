package daemon

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const appPublishDeprecatedMessage = "gestaltd app publish is deprecated; use gestaltd app registry publish"

func runApp(args []string) error {
	if len(args) == 0 {
		printAppUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printAppUsage(os.Stderr)
		return flag.ErrHelp
	case "registry":
		return runAppRegistry(args[1:])
	case "publish":
		_, _ = fmt.Fprintln(os.Stderr, appPublishDeprecatedMessage)
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
	writeUsageLine(w, "  registry    Manage app registry catalogs and publishes")
	writeUsageLine(w, "  publish     Deprecated alias for registry publish")
}
