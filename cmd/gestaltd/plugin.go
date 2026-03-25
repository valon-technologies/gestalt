package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/pluginstore"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
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
	case "package":
		return runPluginPackage(args[1:])
	case "install":
		return runPluginInstall(args[1:])
	case "inspect":
		return runPluginInspect(args[1:])
	case "list":
		return runPluginList(args[1:])
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
	fmt.Fprintf(os.Stdout, "packaged %s -> %s\n", sourceDir, *output)
	return nil
}

func runPluginInstall(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin install", flag.ContinueOnError)
	fs.Usage = func() { printPluginInstallUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to the Gestalt config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: gestaltd plugin install [--config PATH] <package>")
	}
	store := pluginstore.New(resolveConfigPath(*configPath))
	installed, err := store.Install(fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "installed %s -> %s\n", installed.Ref.String(), installed.ExecutablePath)
	return nil
}

func runPluginInspect(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin inspect", flag.ContinueOnError)
	fs.Usage = func() { printPluginInspectUsage(fs.Output()) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: gestaltd plugin inspect <package-or-manifest>")
	}
	_, manifest, source, err := pluginpkg.LoadManifestFromPath(fs.Arg(0))
	if err != nil {
		return err
	}
	printManifestSummary(os.Stdout, source, manifest)
	return nil
}

func runPluginList(args []string) error {
	fs := flag.NewFlagSet("gestaltd plugin list", flag.ContinueOnError)
	fs.Usage = func() { printPluginListUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to the Gestalt config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	store := pluginstore.New(resolveConfigPath(*configPath))
	installed, err := store.List()
	if err != nil {
		return err
	}
	for _, item := range installed {
		writeUsageLine(os.Stdout, fmt.Sprintf("%s %s", item.Ref.String(), item.ExecutablePath))
	}
	return nil
}

func printPluginUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  package     Build a plugin package archive")
	writeUsageLine(w, "  install     Install a plugin package into the local store")
	writeUsageLine(w, "  inspect     Inspect a plugin package or installed manifest")
	writeUsageLine(w, "  list        List installed plugins")
}

func printPluginPackageUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin package --input PATH --output PATH")
	writeUsageLine(w, "")
	writeUsageLine(w, "Package a plugin manifest or build directory into a distributable archive.")
}

func printPluginInstallUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin install [--config PATH] <package>")
	writeUsageLine(w, "")
	writeUsageLine(w, "Install a packaged plugin into the project-local store.")
}

func printPluginInspectUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin inspect <package-or-manifest>")
	writeUsageLine(w, "")
	writeUsageLine(w, "Inspect a plugin package or installed manifest without executing it.")
}

func printPluginListUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd plugin list [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "List installed plugins for a config-backed project store.")
}

func printManifestSummary(w io.Writer, source string, manifest *pluginmanifestv1.Manifest) {
	writeUsageLine(w, fmt.Sprintf("source: %s", source))
	writeUsageLine(w, fmt.Sprintf("id: %s", manifest.ID))
	writeUsageLine(w, fmt.Sprintf("version: %s", manifest.Version))
	writeUsageLine(w, fmt.Sprintf("kinds: %s", strings.Join(pluginpkg.Kinds(manifest), ", ")))
	if manifest.Provider != nil {
		writeUsageLine(w, fmt.Sprintf("provider protocol: %d-%d", manifest.Provider.Protocol.Min, manifest.Provider.Protocol.Max))
		if manifest.Provider.ConfigSchemaPath != "" {
			writeUsageLine(w, fmt.Sprintf("config schema: %s", manifest.Provider.ConfigSchemaPath))
		}
	}
	for _, artifact := range manifest.Artifacts {
		writeUsageLine(w, fmt.Sprintf("artifact: %s/%s %s", artifact.OS, artifact.Arch, artifact.Path))
	}
}
