package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/internal/pluginpkg"
)

func runPlugin(args []string) error {
	if len(args) == 0 {
		printPluginUsage(os.Stderr)
		return flag.ErrHelp
	}

	switch args[0] {
	case "-h", "--help", "help":
		printPluginUsage(os.Stderr)
		return flag.ErrHelp
	case "init":
		return runPluginInit(args[1:])
	case "package":
		return runPluginPackage(args[1:])
	default:
		return fmt.Errorf("unknown plugin command %q", args[0])
	}
}

func runPluginPackage(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin package", flag.ContinueOnError)
	fs.Usage = func() { printPluginPackageUsage(fs.Output()) }
	input := fs.String("input", "", "path to plugin manifest or build directory")
	output := fs.String("output", "", "path to write the packaged archive")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *input == "" || *output == "" {
		return fmt.Errorf("usage: gestaltd plugin package --input PATH --output PATH")
	}

	sourceDir := *input
	if info, err := os.Stat(*input); err != nil {
		return err
	} else if !info.IsDir() {
		if filepath.Base(*input) != pluginpkg.ManifestFile {
			return fmt.Errorf("plugin package input must be a directory or %s, got %q", pluginpkg.ManifestFile, *input)
		}
		sourceDir = filepath.Dir(*input)
	}

	if err := pluginpkg.CreatePackageFromDir(sourceDir, *output); err != nil {
		return err
	}
	writeUsageLine(os.Stdout, fmt.Sprintf("packaged %s -> %s", sourceDir, *output))
	return nil
}

func printPluginUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  init        Scaffold a plugin package directory")
	writeUsageLine(w, "  package     Build a plugin package archive")
}

func printPluginPackageUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin package --input PATH --output PATH")
	writeUsageLine(w, "")
	writeUsageLine(w, "Package a plugin manifest or build directory into a distributable archive.")
}
