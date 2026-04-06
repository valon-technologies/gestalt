package pluginpkg

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const (
	pythonProjectFile   = "pyproject.toml"
	pythonRuntimeModule = "gestalt._runtime"
)

var ErrNoPythonProviderPackage = errors.New("no Python provider package found")

var pythonIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func DetectPythonProviderTarget(root string) (string, error) {
	projectPath := filepath.Join(root, pythonProjectFile)
	if _, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoPythonProviderPackage
		}
		return "", fmt.Errorf("stat %s: %w", pythonProjectFile, err)
	}

	data, err := os.ReadFile(projectPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", pythonProjectFile, err)
	}

	target, err := pythonProjectPluginTarget(data)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", pythonProjectFile, err)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ErrNoPythonProviderPackage
	}
	if _, _, err := SplitPythonProviderTarget(target); err != nil {
		return "", fmt.Errorf("%s tool.gestalt.plugin: %w", pythonProjectFile, err)
	}
	return target, nil
}

func pythonProviderExecutionCommand(root, target string) (string, []string, func(), error) {
	interpreter, err := DetectPythonInterpreter(root)
	if err != nil {
		return "", nil, nil, err
	}
	return interpreter, []string{"-m", pythonRuntimeModule, root, target}, nil, nil
}

func BuildPythonProviderBinary(sourceDir, binaryPath, pluginName, target string) error {
	module, attr, err := SplitPythonProviderTarget(target)
	if err != nil {
		return fmt.Errorf("invalid Python provider target %q: %w", target, err)
	}

	interpreter, err := DetectPythonInterpreter(sourceDir)
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
	if runtime.GOOS == "windows" {
		pyinstallerName = strings.TrimSuffix(pyinstallerName, ".exe")
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

func DetectPythonInterpreter(root string) (string, error) {
	for _, candidate := range pythonInterpreterCandidates(root) {
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("detect Python interpreter: %w", exec.ErrNotFound)
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
	if sdkPath := localPythonSDKPath(); sdkPath != "" {
		return []string{sdkPath}
	}
	return nil
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

func pythonInterpreterCandidates(root string) []string {
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(root, ".venv", "Scripts", "python.exe"),
			filepath.Join(root, "venv", "Scripts", "python.exe"),
			"py",
			"python",
		}
	}
	return []string{
		filepath.Join(root, ".venv", "bin", "python"),
		filepath.Join(root, "venv", "bin", "python"),
		"python3",
		"python",
	}
}

func SplitPythonProviderTarget(target string) (module string, attr string, err error) {
	module, attr, ok := strings.Cut(strings.TrimSpace(target), ":")
	module = strings.TrimSpace(module)
	attr = strings.TrimSpace(attr)
	switch {
	case !ok:
		return "", "", fmt.Errorf("must be in module:attribute form")
	case module == "":
		return "", "", fmt.Errorf("module is required")
	case attr == "":
		return "", "", fmt.Errorf("attribute is required")
	case !isPythonModulePath(module):
		return "", "", fmt.Errorf("module must be a dot-separated Python identifier path")
	case !isPythonIdentifier(attr):
		return "", "", fmt.Errorf("attribute must be a Python identifier")
	default:
		return module, attr, nil
	}
}

func isPythonModulePath(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !isPythonIdentifier(part) {
			return false
		}
	}
	return true
}

func isPythonIdentifier(value string) bool {
	return pythonIdentifierPattern.MatchString(value)
}

func pythonProjectPluginTarget(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	inGestaltSection := false
	for scanner.Scan() {
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			inGestaltSection = section == "tool.gestalt"
			continue
		}
		if !inGestaltSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "plugin" {
			continue
		}
		return parseTOMLString(value)
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func stripTOMLComment(line string) string {
	inBasicString := false
	inLiteralString := false
	escaped := false
	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case inBasicString:
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inBasicString = false
			}
		case inLiteralString:
			if r == '\'' {
				inLiteralString = false
			}
		default:
			switch r {
			case '#':
				return line[:i]
			case '"':
				inBasicString = true
			case '\'':
				inLiteralString = true
			}
		}
	}
	return line
}

func parseTOMLString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is required")
	}
	switch value[0] {
	case '"':
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted string: %w", err)
		}
		return parsed, nil
	case '\'':
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", fmt.Errorf("invalid literal string")
		}
		return value[1 : len(value)-1], nil
	default:
		return "", fmt.Errorf("must be a quoted string")
	}
}
