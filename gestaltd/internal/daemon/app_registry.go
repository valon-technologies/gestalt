package daemon

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func runAppRegistry(args []string, gestaltdVersion string) error {
	if len(args) == 0 {
		printAppRegistryUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printAppRegistryUsage(os.Stderr)
		return flag.ErrHelp
	case "publish":
		return runAppRegistryPublish(args[1:], gestaltdVersion)
	case "pending":
		return runAppRegistryPending(args[1:])
	case "retention":
		return runAppRegistryRetention(args[1:])
	default:
		return fmt.Errorf("unknown app registry command %q", args[0])
	}
}

func printAppRegistryUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  publish     Publish an installable app version to the app registry")
	writeUsageLine(w, "  pending     Record or clear in-flight publish state in pending.json and failed.json")
	writeUsageLine(w, "  retention   Prune published app registry versions by retention policy")
}
