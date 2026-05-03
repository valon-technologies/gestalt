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
	pythonSDKDirEnvVar  = "GESTALT_PYTHON_SDK_DIR"
)

var ErrNoPythonProviderPackage = errors.New("no Python provider package found")
var ErrNoPythonSourceComponentPackage = errors.New("no Python source component package found")

var pythonIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func DetectPythonProviderTarget(root string) (string, error) {
	target, err := detectPythonProjectTarget(root, "provider")
	if err != nil {
		if errors.Is(err, ErrNoPythonSourceComponentPackage) {
			return "", ErrNoPythonProviderPackage
		}
		return "", err
	}
	return target, nil
}

func DetectPythonComponentTarget(root, kind string) (string, error) {
	kind = providermanifestv1.NormalizeKind(kind)
	if err := validateSourceComponentKind(kind); err != nil {
		return "", err
	}
	if _, err := pythonRuntimeKind(kind); err != nil {
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
		return "", fmt.Errorf("%s tool.gestalt.%s: %w", pythonProjectFile, key, err)
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
	env, err := pythonBackendEnv(os.Environ())
	if err != nil {
		return "", fmt.Errorf("prepare Python release build environment: %w", err)
	}
	cmd.Env = env
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

func pythonBackendEnv(base []string) ([]string, error) {
	env, err := pythonBackendEnvMap()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(base)+len(env))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key == "PYTHONPATH" {
			continue
		}
		out = append(out, entry)
	}
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out, nil
}

func pythonBackendEnvMap() (map[string]string, error) {
	value, err := pythonBackendPythonPath()
	if err != nil {
		return nil, err
	}
	return map[string]string{"PYTHONPATH": value}, nil
}

func pythonBackendPythonPath() (string, error) {
	sdkPath, err := explicitPythonSDKPath()
	if err != nil {
		return "", err
	}
	if sdkPath == "" {
		return "", nil
	}
	value := sdkPath
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		value += string(os.PathListSeparator) + existing
	}
	return value, nil
}

func explicitPythonSDKPath() (string, error) {
	raw := strings.TrimSpace(os.Getenv(pythonSDKDirEnvVar))
	if raw == "" {
		return "", nil
	}
	path, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("%s %q: resolve absolute path: %w", pythonSDKDirEnvVar, raw, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s %q: %w", pythonSDKDirEnvVar, path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s %q is not a directory", pythonSDKDirEnvVar, path)
	}
	if _, err := os.Stat(filepath.Join(path, pythonProjectFile)); err != nil {
		return "", fmt.Errorf("%s %q: missing %s: %w", pythonSDKDirEnvVar, path, pythonProjectFile, err)
	}
	if info, err := os.Stat(filepath.Join(path, "gestalt")); err != nil {
		return "", fmt.Errorf("%s %q: missing gestalt package: %w", pythonSDKDirEnvVar, path, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("%s %q: gestalt package is not a directory", pythonSDKDirEnvVar, path)
	}
	return path, nil
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
	pythonRuntimeKindIntegration    = "integration"
	pythonRuntimeKindAuthentication = "authentication"
	pythonRuntimeKindCache          = "cache"
	pythonRuntimeKindIndexedDB      = "indexeddb"
	pythonRuntimeKindS3             = "s3"
	pythonRuntimeKindWorkflow       = "workflow"
	pythonRuntimeKindAgent          = "agent"
	pythonRuntimeKindSecrets        = "secrets"
)

func pythonRuntimeKind(kind string) (string, error) {
	kind = providermanifestv1.NormalizeKind(kind)
	switch kind {
	case providermanifestv1.KindPlugin:
		return pythonRuntimeKindIntegration, nil
	case providermanifestv1.KindAuthentication:
		return pythonRuntimeKindAuthentication, nil
	case providermanifestv1.KindCache:
		return pythonRuntimeKindCache, nil
	case providermanifestv1.KindIndexedDB:
		return pythonRuntimeKindIndexedDB, nil
	case providermanifestv1.KindS3:
		return pythonRuntimeKindS3, nil
	case providermanifestv1.KindWorkflow:
		return pythonRuntimeKindWorkflow, nil
	case providermanifestv1.KindAgent:
		return pythonRuntimeKindAgent, nil
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
