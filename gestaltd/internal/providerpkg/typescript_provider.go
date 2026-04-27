package providerpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	typeScriptProjectFile  = "package.json"
	typeScriptRuntimeBin   = "gestalt-ts-runtime"
	typeScriptBuildBin     = "gestalt-ts-build"
	typeScriptBunEnvVar    = "GESTALT_BUN"
	typeScriptSDKDirEnvVar = "GESTALT_TYPESCRIPT_SDK_DIR"
	typeScriptProviderKey  = "provider"
	typeScriptPluginKey    = "plugin"
	typeScriptAuthKey      = "auth"
	typeScriptCacheKey     = "cache"
	typeScriptIndexedDBKey = "indexeddb"
	typeScriptS3Key        = "s3"
	typeScriptWorkflowKey  = "workflow"
	typeScriptAgentKey     = "agent"
)

var ErrNoTypeScriptProviderPackage = errors.New("no TypeScript provider package found")

var typeScriptExportPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

type typeScriptPackageConfig struct {
	Gestalt map[string]any `json:"gestalt"`
}

func DetectTypeScriptProviderTarget(root string) (string, error) {
	return detectTypeScriptTarget(root, "integration", ErrNoTypeScriptProviderPackage)
}

func DetectTypeScriptComponentTarget(root, kind string) (string, error) {
	providerKind, err := typeScriptComponentKind(kind)
	if err != nil {
		return "", err
	}
	return detectTypeScriptTarget(root, providerKind, ErrNoSourceComponentPackage)
}

func detectTypeScriptTarget(root, expectedKind string, missingErr error) (string, error) {
	projectPath := filepath.Join(root, typeScriptProjectFile)
	data, err := os.ReadFile(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", missingErr
		}
		return "", fmt.Errorf("read %s: %w", typeScriptProjectFile, err)
	}

	var config typeScriptPackageConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse %s: %w", typeScriptProjectFile, err)
	}

	if rawProvider, ok := config.Gestalt[typeScriptProviderKey]; ok {
		kind, target, err := parseTypeScriptProviderConfig(rawProvider)
		if err != nil {
			return "", fmt.Errorf("%s gestalt.%s: %w", typeScriptProjectFile, typeScriptProviderKey, err)
		}
		if kind == expectedKind {
			return formatTypeScriptRuntimeTarget(kind, target), nil
		}
	}
	return "", missingErr
}

func typeScriptExecutionCommand(root, target string) (string, []string, func(), error) {
	bunPath, err := DetectBunExecutable()
	if err != nil {
		return "", nil, nil, err
	}
	if err := ensureTypeScriptProjectDependencies(bunPath, root); err != nil {
		return "", nil, nil, err
	}
	sdkPath, err := prepareLocalTypeScriptSDK(bunPath)
	if err != nil {
		return "", nil, nil, err
	}
	if sdkPath != "" {
		return bunPath, []string{
			"--cwd", sdkPath,
			filepath.Join(sdkPath, "src", "runtime.ts"),
			"--",
			root,
			target,
		}, nil, nil
	}
	return bunPath, []string{
		"run",
		"--cwd", root,
		typeScriptRuntimeBin,
		"--",
		root,
		target,
	}, nil, nil
}

func BuildTypeScriptProviderBinary(sourceDir, binaryPath, pluginName, target, goos, goarch string) (string, error) {
	return buildTypeScriptBinary(sourceDir, binaryPath, pluginName, target, goos, goarch)
}

func BuildTypeScriptComponentBinary(sourceDir, binaryPath, kind, target, goos, goarch string) (string, error) {
	if err := validateSourceComponentKind(kind); err != nil {
		return "", err
	}
	return buildTypeScriptBinary(sourceDir, binaryPath, sourcePluginName(sourceDir), target, goos, goarch)
}

func buildTypeScriptBinary(sourceDir, binaryPath, pluginName, target, goos, goarch string) (string, error) {
	bunPath, err := DetectBunExecutable()
	if err != nil {
		return "", fmt.Errorf("detect Bun executable: %w", err)
	}
	if err := ensureTypeScriptProjectDependencies(bunPath, sourceDir); err != nil {
		return "", fmt.Errorf("prepare TypeScript provider dependencies: %w", err)
	}

	var args []string
	sdkPath, err := prepareLocalTypeScriptSDK(bunPath)
	if err != nil {
		return "", fmt.Errorf("prepare local TypeScript SDK: %w", err)
	}
	if sdkPath != "" {
		args = []string{
			"--cwd", sdkPath,
			filepath.Join(sdkPath, "src", "build.ts"),
			"--",
			sourceDir,
			target,
			binaryPath,
			pluginName,
			goos,
			goarch,
		}
	} else {
		args = []string{
			"run",
			"--cwd", sourceDir,
			typeScriptBuildBin,
			"--",
			sourceDir,
			target,
			binaryPath,
			pluginName,
			goos,
			goarch,
		}
	}

	cmd := exec.Command(bunPath, args...)
	cmd.Dir = sourceDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("TypeScript release build: %w (ensure Bun and @valon-technologies/gestalt are available)", err)
	}
	return "", nil
}

func typeScriptComponentKind(kind string) (string, error) {
	kind = providermanifestv1.NormalizeKind(kind)
	switch kind {
	case providermanifestv1.KindAuthentication:
		return "authentication", nil
	case providermanifestv1.KindCache:
		return "cache", nil
	case providermanifestv1.KindIndexedDB:
		return "indexeddb", nil
	case providermanifestv1.KindS3:
		return "s3", nil
	case providermanifestv1.KindWorkflow:
		return "workflow", nil
	case providermanifestv1.KindAgent:
		return "agent", nil
	case providermanifestv1.KindSecrets:
		return "secrets", nil
	default:
		return "", fmt.Errorf("unsupported source component kind %q", kind)
	}
}

func DetectBunExecutable() (string, error) {
	for _, candidate := range bunExecutableCandidates() {
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
	return "", fmt.Errorf("detect Bun executable: %w (set %s if Bun is installed outside PATH)", exec.ErrNotFound, typeScriptBunEnvVar)
}

func bunExecutableCandidates() []string {
	candidates := []string{
		os.Getenv(typeScriptBunEnvVar),
		"bun",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".bun", "bin", "bun"))
	}
	return candidates
}

func prepareLocalTypeScriptSDK(bunPath string) (string, error) {
	sdkPath := localTypeScriptSDKPath()
	if sdkPath == "" {
		return "", nil
	}
	if err := ensureLocalTypeScriptSDKDependencies(bunPath, sdkPath); err != nil {
		return "", err
	}
	return sdkPath, nil
}

func ensureTypeScriptProjectDependencies(bunPath, root string) error {
	return ensureTypeScriptDependencies(bunPath, root, "TypeScript provider")
}

func ensureLocalTypeScriptSDKDependencies(bunPath, sdkPath string) error {
	return ensureTypeScriptDependencies(bunPath, sdkPath, "local TypeScript SDK")
}

func ensureTypeScriptDependencies(bunPath, root, label string) error {
	nodeModulesPath := filepath.Join(root, "node_modules")
	if info, err := os.Stat(nodeModulesPath); err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("%s node_modules path %q is not a directory", label, nodeModulesPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s node_modules: %w", label, err)
	}

	args := []string{"install", "--cwd", root}
	lockfilePath := filepath.Join(root, "bun.lock")
	if _, err := os.Stat(lockfilePath); err == nil {
		args = append(args, "--frozen-lockfile")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s bun.lock: %w", label, err)
	}

	cmd := exec.Command(bunPath, args...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bun install for %s: %w", label, err)
	}
	return nil
}

func localTypeScriptSDKPath() string {
	if override := strings.TrimSpace(os.Getenv(typeScriptSDKDirEnvVar)); override != "" {
		path := filepath.Clean(override)
		if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
			return path
		}
		return ""
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "sdk", "typescript"))
	if _, err := os.Stat(filepath.Join(path, "package.json")); err != nil {
		return ""
	}
	return path
}

func SplitTypeScriptProviderTarget(target string) (modulePath string, exportName string, err error) {
	raw := strings.TrimSpace(target)
	modulePath, exportName, _ = strings.Cut(raw, "#")
	modulePath = strings.TrimSpace(modulePath)
	exportName = strings.TrimSpace(exportName)
	if modulePath == "" {
		return "", "", fmt.Errorf("module path is required")
	}
	if exportName != "" && !typeScriptExportPattern.MatchString(exportName) {
		return "", "", fmt.Errorf("export must be a JavaScript identifier")
	}
	if err := validateTypeScriptModulePath(modulePath); err != nil {
		return "", "", err
	}
	return modulePath, exportName, nil
}

func parseTypeScriptProviderConfig(raw any) (kind string, target string, err error) {
	switch value := raw.(type) {
	case string:
		return parseTypeScriptProviderString(value)
	case map[string]any:
		rawKind, _ := value["kind"].(string)
		rawTarget, _ := value["target"].(string)
		kind = normalizeTypeScriptProviderKind(rawKind)
		if strings.TrimSpace(rawKind) != "" && kind == "" {
			return "", "", fmt.Errorf("unsupported provider kind %q", rawKind)
		}
		if strings.TrimSpace(rawTarget) == "" {
			return "", "", fmt.Errorf("target is required")
		}
		target = strings.TrimSpace(rawTarget)
		if _, _, err := SplitTypeScriptProviderTarget(target); err != nil {
			return "", "", err
		}
		return kind, target, nil
	default:
		return "", "", fmt.Errorf("must be a string or object")
	}
}

func parseTypeScriptProviderString(raw string) (kind string, target string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("target is required")
	}
	if prefix, rest, ok := parseTypeScriptKindPrefixedTarget(trimmed); ok {
		if _, _, err := SplitTypeScriptProviderTarget(rest); err != nil {
			return "", "", err
		}
		return prefix, rest, nil
	}
	if _, _, err := SplitTypeScriptProviderTarget(trimmed); err != nil {
		if prefix, _, found := strings.Cut(trimmed, ":"); found {
			prefix = strings.TrimSpace(prefix)
			if prefix != "" && !strings.HasPrefix(prefix, ".") && !strings.HasPrefix(prefix, "/") {
				return "", "", fmt.Errorf("unsupported provider kind %q", prefix)
			}
		}
		return "", "", err
	}
	return "integration", trimmed, nil
}

func parseTypeScriptKindPrefixedTarget(raw string) (kind string, target string, ok bool) {
	prefix, rest, found := strings.Cut(raw, ":")
	if !found {
		return "", "", false
	}
	if strings.TrimSpace(prefix) == "" {
		return "", "", false
	}
	kind = normalizeTypeScriptProviderKind(prefix)
	if kind == "" {
		return "", "", false
	}
	return kind, strings.TrimSpace(rest), true
}

func normalizeTypeScriptProviderKind(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "plugin":
		return "integration"
	case "authentication", "auth":
		return "authentication"
	case "cache":
		return "cache"
	case "indexeddb":
		return "indexeddb"
	case "s3":
		return "s3"
	case "workflow":
		return "workflow"
	case "agent":
		return "agent"
	case "secrets":
		return "secrets"
	case "telemetry":
		return "telemetry"
	default:
		return ""
	}
}

func formatTypeScriptRuntimeTarget(kind, target string) string {
	if kind == "integration" {
		return "plugin:" + strings.TrimSpace(target)
	}
	return kind + ":" + strings.TrimSpace(target)
}

func validateTypeScriptModulePath(value string) error {
	if !strings.HasPrefix(value, "./") && !strings.HasPrefix(value, "../") {
		return fmt.Errorf("module path must be relative")
	}
	cleanPath := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if path.IsAbs(cleanPath) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return fmt.Errorf("module path must stay within the plugin root")
	}
	ext := path.Ext(cleanPath)
	switch ext {
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
		return nil
	default:
		return fmt.Errorf("module path must end in a TypeScript or JavaScript file extension")
	}
}
