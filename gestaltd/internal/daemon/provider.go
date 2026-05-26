package daemon

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func runProvider(args []string) error {
	if len(args) == 0 {
		printProviderUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printProviderUsage(os.Stderr)
		return flag.ErrHelp
	case "attach":
		return runProviderAttach(args[1:])
	case "add":
		return runProviderAdd(args[1:])
	case "info":
		return runProviderInfo(args[1:])
	case "list":
		return runProviderList(args[1:])
	case "package":
		return runProviderPackage(args[1:])
	case "publish":
		return runProviderPublish(args[1:])
	case "remove":
		return runProviderRemove(args[1:])
	case "repo":
		return runProviderRepo(args[1:])
	case "search":
		return runProviderSearch(args[1:])
	case "upgrade":
		return runProviderUpgrade(args[1:])
	case "validate":
		return runProviderValidate(args[1:])
	case "release":
		return runProviderRelease(args[1:])
	default:
		return fmt.Errorf("unknown provider command %q", args[0])
	}
}

func printProviderUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  attach      List, inspect, or detach remote provider-dev attachments")
	writeUsageLine(w, "  add         Add a provider package to config and update lock state")
	writeUsageLine(w, "  info        Show provider package metadata from configured repositories")
	writeUsageLine(w, "  list        List configured providers and lock status")
	writeUsageLine(w, "  package     Build provider release archives")
	writeUsageLine(w, "  publish     Publish provider release artifacts")
	writeUsageLine(w, "  release     Finalize provider release metadata from archives")
	writeUsageLine(w, "  remove      Remove a provider entry from config and update lock state")
	writeUsageLine(w, "  repo        Manage provider package repositories")
	writeUsageLine(w, "  search      Search configured provider package repositories")
	writeUsageLine(w, "  upgrade     Refresh a provider package lock or version constraint")
	writeUsageLine(w, "  validate    Validate a local source plugin inside a synthesized Gestalt config")
}
