package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/testutil/fakebun"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	cfg, err := fakebun.LoadExecutableConfig(exePath)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("unexpected fake bun args: %s", strings.Join(args, " "))
	}
	if args[0] == "install" {
		return runInstall(cfg.Install, args[1:])
	}

	var errs []error
	if cfg.Runtime != nil {
		if err := runRuntime(*cfg.Runtime, args); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Errorf("runtime: %w", err))
		}
	}
	if cfg.Build != nil {
		if err := runBuild(*cfg.Build, args); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Errorf("build: %w", err))
		}
	}
	if len(errs) == 0 {
		return fmt.Errorf("unexpected fake bun args: %s", strings.Join(args, " "))
	}
	return errors.Join(errs...)
}

func runInstall(cfg *fakebun.InstallConfig, args []string) error {
	if cfg == nil {
		return fmt.Errorf("unexpected bun install args: %s", strings.Join(args, " "))
	}

	cwd := ""
	frozen := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 >= len(args) {
				return fmt.Errorf("missing bun install cwd")
			}
			cwd = args[i+1]
			i++
		case "--frozen-lockfile":
			frozen = true
		default:
			return fmt.Errorf("unexpected bun install args: %s", strings.Join(args, " "))
		}
	}

	if cwd == "" {
		return fmt.Errorf("missing bun install --cwd")
	}
	if cfg.ExpectedCwd != "" && cwd != cfg.ExpectedCwd {
		return fmt.Errorf("unexpected bun install cwd: %s != %s", cwd, cfg.ExpectedCwd)
	}
	if cfg.RequireFrozenLockfile && !frozen {
		return fmt.Errorf("missing bun install --frozen-lockfile")
	}

	nodeModulesDir := filepath.Join(cwd, "node_modules")
	if err := os.MkdirAll(nodeModulesDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(nodeModulesDir, ".installed"), nil, 0o644)
}

func runRuntime(cfg fakebun.RuntimeConfig, args []string) error {
	inv, err := parseInvocation(cfg.Mode, args)
	if err != nil {
		return err
	}
	if cfg.ExpectedCwd != "" && inv.cwd != cfg.ExpectedCwd {
		return fmt.Errorf("unexpected runtime cwd: %s != %s", inv.cwd, cfg.ExpectedCwd)
	}
	if cfg.ExpectedEntry != "" && inv.entry != cfg.ExpectedEntry {
		return fmt.Errorf("unexpected runtime entry: %s != %s", inv.entry, cfg.ExpectedEntry)
	}
	if len(inv.tail) != 2 {
		return fmt.Errorf("unexpected runtime args: %s", strings.Join(inv.tail, " "))
	}
	if cfg.ExpectedRoot != "" && inv.tail[0] != cfg.ExpectedRoot {
		return fmt.Errorf("unexpected runtime root: %s != %s", inv.tail[0], cfg.ExpectedRoot)
	}
	if cfg.ExpectedTarget != "" && inv.tail[1] != cfg.ExpectedTarget {
		return fmt.Errorf("unexpected runtime target: %s != %s", inv.tail[1], cfg.ExpectedTarget)
	}

	catalogPath := os.Getenv("GESTALT_PLUGIN_WRITE_CATALOG")
	if cfg.RequireAnyOutput && catalogPath == "" {
		return fmt.Errorf("missing catalog export path")
	}
	if cfg.RequireCatalog && catalogPath == "" {
		return fmt.Errorf("missing GESTALT_PLUGIN_WRITE_CATALOG")
	}
	if catalogPath != "" && cfg.Catalog != "" {
		if err := os.WriteFile(catalogPath, []byte(cfg.Catalog), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func runBuild(cfg fakebun.BuildConfig, args []string) error {
	inv, err := parseInvocation(cfg.Mode, args)
	if err != nil {
		return err
	}
	if cfg.ExpectedCwd != "" && inv.cwd != cfg.ExpectedCwd {
		return fmt.Errorf("unexpected build cwd: %s != %s", inv.cwd, cfg.ExpectedCwd)
	}
	if cfg.ExpectedEntry != "" && inv.entry != cfg.ExpectedEntry {
		return fmt.Errorf("unexpected build entry: %s != %s", inv.entry, cfg.ExpectedEntry)
	}
	if len(inv.tail) != 6 {
		return fmt.Errorf("unexpected build args: %s", strings.Join(inv.tail, " "))
	}

	sourceDir := inv.tail[0]
	target := inv.tail[1]
	output := inv.tail[2]
	name := inv.tail[3]
	goos := inv.tail[4]
	goarch := inv.tail[5]

	if cfg.ExpectedSourceDir != "" && sourceDir != cfg.ExpectedSourceDir {
		return fmt.Errorf("unexpected build source dir: %s != %s", sourceDir, cfg.ExpectedSourceDir)
	}
	if cfg.ExpectedTarget != "" && target != cfg.ExpectedTarget {
		return fmt.Errorf("unexpected build target: %s != %s", target, cfg.ExpectedTarget)
	}
	if cfg.ExpectedPluginName != "" && name != cfg.ExpectedPluginName {
		return fmt.Errorf("unexpected build plugin name: %s != %s", name, cfg.ExpectedPluginName)
	}
	if len(cfg.AllowedPlatforms) > 0 {
		allowed := false
		for _, platform := range cfg.AllowedPlatforms {
			if goos == platform.GOOS && goarch == platform.GOARCH {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("unexpected target platform: %s/%s", goos, goarch)
		}
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	if cfg.CopyBinaryFrom != "" {
		return copyFile(cfg.CopyBinaryFrom, output)
	}
	return os.WriteFile(output, []byte(cfg.BinaryContent), 0o755)
}

type invocation struct {
	cwd   string
	entry string
	tail  []string
}

func parseInvocation(mode fakebun.InvocationMode, args []string) (invocation, error) {
	if mode == "" {
		mode = fakebun.InvocationDirect
	}
	if len(args) < 3 || args[0] != "--cwd" {
		return invocation{}, fmt.Errorf("unexpected fake bun args: %s", strings.Join(args, " "))
	}

	cwd := args[1]
	switch mode {
	case fakebun.InvocationDirect:
		entry := args[2]
		tail := args[3:]
		if len(tail) > 0 && tail[0] == "--" {
			tail = tail[1:]
		}
		return invocation{cwd: cwd, entry: entry, tail: tail}, nil
	case fakebun.InvocationRun:
		if len(args) < 4 || args[2] != "run" {
			return invocation{}, fmt.Errorf("unexpected fake bun args: %s", strings.Join(args, " "))
		}
		entry := args[3]
		tail := args[4:]
		if len(tail) > 0 && tail[0] == "--" {
			tail = tail[1:]
		}
		return invocation{cwd: cwd, entry: entry, tail: tail}, nil
	default:
		return invocation{}, fmt.Errorf("unsupported fake bun invocation mode %q", mode)
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
