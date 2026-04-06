package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
)

func buildPythonReleaseBinary(sourceDir, binaryPath, pluginName, target string) error {
	module, attr, err := pluginpkg.SplitPythonProviderTarget(target)
	if err != nil {
		return fmt.Errorf("invalid Python provider target %q: %w", target, err)
	}

	interpreter, err := pluginpkg.DetectPythonInterpreter(sourceDir)
	if err != nil {
		return fmt.Errorf("detect Python release interpreter: %w", err)
	}
	if err := validatePythonReleaseEnvironment(interpreter); err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "gestalt-python-release-*")
	if err != nil {
		return fmt.Errorf("create Python release workdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	launcherPath := filepath.Join(workDir, "launcher.py")
	if err := os.WriteFile(launcherPath, []byte(pythonReleaseLauncherSource(module, attr, pluginName)), 0o644); err != nil {
		return fmt.Errorf("write Python release launcher: %w", err)
	}

	pyinstallerName := filepath.Base(binaryPath)
	if runtime.GOOS == windowsOS {
		pyinstallerName = strings.TrimSuffix(pyinstallerName, windowsExecutableSuffix)
	}

	args := []string{
		"-m", "PyInstaller",
		"--noconfirm",
		"--clean",
		"--onefile",
		"--distpath", filepath.Dir(binaryPath),
		"--workpath", filepath.Join(workDir, "build"),
		"--specpath", filepath.Join(workDir, "spec"),
		"--name", pyinstallerName,
		"--hidden-import", module,
		"--paths", sourceDir,
	}
	args = append(args, launcherPath)

	cmd := exec.Command(interpreter, args...)
	cmd.Dir = sourceDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("python release build: %w", err)
	}
	return nil
}

func pythonReleaseLauncherSource(module, attr, pluginName string) string {
	return fmt.Sprintf(`from __future__ import annotations

import importlib
import os

from gestalt._runtime import serve

_gestalt_module = importlib.import_module(%q)
_gestalt_plugin = getattr(_gestalt_module, %q)

_gestalt_plugin.name = %q

if __name__ == "__main__":
    catalog_path = os.environ.get("GESTALT_PLUGIN_WRITE_CATALOG")
    if catalog_path:
        _gestalt_plugin.write_catalog(catalog_path)
        raise SystemExit(0)
    serve(_gestalt_plugin)
`, module, attr, pluginName)
}

func validatePythonReleaseEnvironment(interpreter string) error {
	cmd := exec.Command(
		interpreter,
		"-c",
		"import gestalt; import PyInstaller",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(output.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf(
			"python release environment must have gestalt and PyInstaller installed (typically from the internal wheel or private index): %s",
			msg,
		)
	}
	return nil
}
