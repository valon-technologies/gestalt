package main

import (
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
	for _, sdkPath := range pythonReleaseImportPaths() {
		args = append(args, "--paths", sdkPath)
	}
	args = append(args, launcherPath)

	cmd := exec.Command(interpreter, args...)
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), pythonReleaseEnv()...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("python release build: %w (ensure PyInstaller is installed in the selected Python environment)", err)
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

func pythonReleaseImportPaths() []string {
	paths := []string{}
	if sdkPath := localPythonSDKPath(); sdkPath != "" {
		paths = append(paths, sdkPath)
	}
	return paths
}

func pythonReleaseEnv() []string {
	paths := pythonReleaseImportPaths()
	if len(paths) == 0 {
		return nil
	}
	value := strings.Join(paths, string(os.PathListSeparator))
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		value += string(os.PathListSeparator) + existing
	}
	return []string{"PYTHONPATH=" + value}
}

func localPythonSDKPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "sdk", "python"))
	if _, err := os.Stat(filepath.Join(path, "pyproject.toml")); err != nil {
		return ""
	}
	return path
}
