package providerpkg

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

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	pythonProjectFile   = "pyproject.toml"
	pythonRuntimeModule = "gestalt._runtime"
	pythonBuildModule   = "gestalt._build"
	pythonEnvVarPrefix  = "GESTALT_PYTHON_"
)

var ErrNoPythonProviderPackage = errors.New("no Python provider package found")
var ErrNoPythonSourceComponentPackage = errors.New("no Python source component package found")

var pythonIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func DetectPythonProviderTarget(root string) (string, error) {
	target, err := detectPythonProjectTarget(root, "plugin")
	if err != nil {
		if errors.Is(err, ErrNoPythonSourceComponentPackage) {
			return "", ErrNoPythonProviderPackage
		}
		return "", err
	}
	return target, nil
}

func DetectPythonComponentTarget(root, kind string) (string, error) {
	if err := validateSourceComponentKind(kind); err != nil {
		return "", err
	}
	return detectPythonProjectTarget(root, kind)
}

func detectPythonProjectTarget(root, key string) (string, error) {
	projectPath := filepath.Join(root, pythonProjectFile)
	if _, err := os.Stat(projectPath); err != nil {
		if os.IsNotExist(err) {
			if key == "plugin" {
				return "", ErrNoPythonProviderPackage
			}
			return "", ErrNoPythonSourceComponentPackage
		}
		return "", fmt.Errorf("stat %s: %w", pythonProjectFile, err)
	}

	data, err := os.ReadFile(projectPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", pythonProjectFile, err)
	}

	target, err := pythonProjectTarget(data, key)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", pythonProjectFile, err)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		if key == "plugin" {
			return "", ErrNoPythonProviderPackage
		}
		return "", ErrNoPythonSourceComponentPackage
	}
	if _, _, err := SplitPythonProviderTarget(target); err != nil {
		label := key
		if key == "plugin" {
			label = "provider/plugin"
		}
		return "", fmt.Errorf("%s tool.gestalt.%s: %w", pythonProjectFile, label, err)
	}
	return target, nil
}

func pythonProviderExecutionCommand(root, target string) (string, []string, func(), error) {
	return pythonExecutionCommand(root, target, pythonRuntimeKindIntegration)
}

func pythonComponentExecutionCommand(root, target, runtimeKind string) (string, []string, func(), error) {
	return pythonExecutionCommand(root, target, runtimeKind)
}

func pythonExecutionCommand(root, target, runtimeKind string) (string, []string, func(), error) {
	interpreter, err := DetectPythonInterpreter(root, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", nil, nil, err
	}
	return interpreter, []string{"-m", pythonRuntimeModule, root, target, runtimeKind}, nil, nil
}

func BuildPythonProviderBinary(sourceDir, binaryPath, pluginName, target, goos, goarch string) (string, error) {
	return BuildPythonComponentBinary(sourceDir, binaryPath, pluginName, target, pythonRuntimeKindIntegration, goos, goarch)
}

func BuildPythonComponentBinary(sourceDir, binaryPath, pluginName, target, runtimeKind, goos, goarch string) (string, error) {
	interpreter, err := DetectPythonInterpreter(sourceDir, goos, goarch)
	if err != nil {
		return "", fmt.Errorf("detect Python release interpreter for %s/%s: %w", goos, goarch, err)
	}

	cmd := exec.Command(interpreter, "-m", pythonBuildModule, sourceDir, target, binaryPath, pluginName, runtimeKind, goos, goarch)
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), pythonBackendEnv()...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("python release build: %w (ensure gestalt and PyInstaller are installed in the selected Python environment)", err)
	}
	return "", nil
}

func DetectPythonInterpreter(root, goos, goarch string) (string, error) {
	for _, candidate := range pythonInterpreterCandidates(root, goos, goarch) {
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
	envVar := pythonInterpreterEnvVar(goos, goarch)
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		return "", fmt.Errorf("detect Python interpreter: %w", exec.ErrNotFound)
	}
	return "", fmt.Errorf("detect Python interpreter: %w (set %s or provide a target-specific virtualenv)", exec.ErrNotFound, envVar)
}

func pythonBackendImportPaths() []string {
	if sdkPath := localPythonSDKPath(); sdkPath != "" {
		return []string{sdkPath}
	}
	return nil
}

func pythonBackendEnv() []string {
	env := pythonBackendEnvMap()
	if len(env) == 0 {
		return nil
	}
	return []string{"PYTHONPATH=" + env["PYTHONPATH"]}
}

func pythonBackendEnvMap() map[string]string {
	paths := pythonBackendImportPaths()
	if len(paths) == 0 {
		return nil
	}
	value := strings.Join(paths, string(os.PathListSeparator))
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		value += string(os.PathListSeparator) + existing
	}
	return map[string]string{"PYTHONPATH": value}
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

func pythonInterpreterCandidates(root, goos, goarch string) []string {
	candidates := []string{
		os.Getenv(pythonInterpreterEnvVar(goos, goarch)),
	}
	suffix := goos + "-" + goarch
	if goos == "windows" {
		candidates = append(candidates,
			filepath.Join(root, ".venv-"+suffix, "Scripts", "python.exe"),
			filepath.Join(root, "venv-"+suffix, "Scripts", "python.exe"),
			filepath.Join(root, ".venv", suffix, "Scripts", "python.exe"),
			filepath.Join(root, "venv", suffix, "Scripts", "python.exe"),
		)
	} else {
		candidates = append(candidates,
			filepath.Join(root, ".venv-"+suffix, "bin", "python"),
			filepath.Join(root, "venv-"+suffix, "bin", "python"),
			filepath.Join(root, ".venv", suffix, "bin", "python"),
			filepath.Join(root, "venv", suffix, "bin", "python"),
		)
	}
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		candidates = append(candidates, pythonGenericInterpreterCandidates(root)...)
	}
	return candidates
}

func pythonGenericInterpreterCandidates(root string) []string {
	if runtime.GOOS == "windows" {
		return []string{
			os.Getenv("GESTALT_PYTHON"),
			filepath.Join(root, ".venv", "Scripts", "python.exe"),
			filepath.Join(root, "venv", "Scripts", "python.exe"),
			"py",
			"python",
		}
	}
	return []string{
		os.Getenv("GESTALT_PYTHON"),
		filepath.Join(root, ".venv", "bin", "python"),
		filepath.Join(root, "venv", "bin", "python"),
		"python3",
		"python",
	}
}

func pythonInterpreterEnvVar(goos, goarch string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", "/", "_")
	return pythonEnvVarPrefix + strings.ToUpper(replacer.Replace(goos)) + "_" + strings.ToUpper(replacer.Replace(goarch))
}

func SplitPythonProviderTarget(target string) (module string, attr string, err error) {
	raw := strings.TrimSpace(target)
	module, attr, hasAttr := strings.Cut(raw, ":")
	module = strings.TrimSpace(module)
	attr = strings.TrimSpace(attr)
	switch {
	case module == "":
		return "", "", fmt.Errorf("module is required")
	case !isPythonModulePath(module):
		return "", "", fmt.Errorf("module must be a dot-separated Python identifier path")
	case hasAttr && attr == "":
		return "", "", fmt.Errorf("attribute is required")
	case hasAttr && !isPythonIdentifier(attr):
		return "", "", fmt.Errorf("attribute must be a Python identifier")
	default:
		return module, attr, nil
	}
}

const (
	pythonRuntimeKindIntegration = "integration"
	pythonRuntimeKindAuth        = "auth"
	pythonRuntimeKindFileAPI     = "fileapi"
	pythonRuntimeKindIndexedDB   = "indexeddb"
	pythonRuntimeKindSecrets     = "secrets"
)

func pythonRuntimeKind(kind string) (string, error) {
	switch kind {
	case providermanifestv1.KindPlugin:
		return pythonRuntimeKindIntegration, nil
	case providermanifestv1.KindAuth:
		return pythonRuntimeKindAuth, nil
	case providermanifestv1.KindFileAPI:
		return pythonRuntimeKindFileAPI, nil
	case providermanifestv1.KindIndexedDB:
		return pythonRuntimeKindIndexedDB, nil
	case providermanifestv1.KindSecrets:
		return pythonRuntimeKindSecrets, nil
	default:
		return "", fmt.Errorf("unsupported Python runtime kind %q", kind)
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

func pythonProjectTarget(data []byte, wantedKey string) (string, error) {
	if wantedKey == "plugin" {
		target, err := pythonProjectTargetValue(data, "provider")
		if err != nil || target != "" {
			return target, err
		}
	}
	return pythonProjectTargetValue(data, wantedKey)
}

func pythonProjectTargetValue(data []byte, wantedKey string) (string, error) {
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
		if !ok || strings.TrimSpace(key) != wantedKey {
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
