package fakebun

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

type InvocationMode string

const (
	InvocationDirect InvocationMode = "direct"
	InvocationRun    InvocationMode = "run"
)

type Config struct {
	Install *InstallConfig `json:"install,omitempty"`
	Runtime *RuntimeConfig `json:"runtime,omitempty"`
	Build   *BuildConfig   `json:"build,omitempty"`
}

type InstallConfig struct {
	ExpectedCwd           string   `json:"expected_cwd,omitempty"`
	ExpectedCwds          []string `json:"expected_cwds,omitempty"`
	RequireFrozenLockfile bool     `json:"require_frozen_lockfile,omitempty"`
}

type RuntimeConfig struct {
	Mode             InvocationMode `json:"mode,omitempty"`
	ExpectedCwd      string         `json:"expected_cwd,omitempty"`
	ExpectedEntry    string         `json:"expected_entry,omitempty"`
	ExpectedRoot     string         `json:"expected_root,omitempty"`
	ExpectedTarget   string         `json:"expected_target,omitempty"`
	RequireAnyOutput bool           `json:"require_any_output,omitempty"`
	RequireCatalog   bool           `json:"require_catalog,omitempty"`
	Catalog          string         `json:"catalog,omitempty"`
}

type BuildConfig struct {
	Mode               InvocationMode `json:"mode,omitempty"`
	ExpectedCwd        string         `json:"expected_cwd,omitempty"`
	ExpectedEntry      string         `json:"expected_entry,omitempty"`
	ExpectedSourceDir  string         `json:"expected_source_dir,omitempty"`
	ExpectedTarget     string         `json:"expected_target,omitempty"`
	ExpectedPluginName string         `json:"expected_plugin_name,omitempty"`
	AllowedPlatforms   []Platform     `json:"allowed_platforms,omitempty"`
	BinaryContent      string         `json:"binary_content,omitempty"`
	CopyBinaryFrom     string         `json:"copy_binary_from,omitempty"`
}

type Platform struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

var (
	buildHelperOnce sync.Once
	helperBinary    string
	helperBinaryErr error
)

func NewExecutable(t testing.TB, cfg Config) string {
	t.Helper()

	sharedBinary, err := builtHelperBinary()
	if err != nil {
		t.Fatalf("build fake bun helper: %v", err)
	}

	dstPath := filepath.Join(t.TempDir(), "bun"+executableSuffix())
	if err := copyFile(sharedBinary, dstPath); err != nil {
		t.Fatalf("copy fake bun helper: %v", err)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal fake bun config: %v", err)
	}
	if err := os.WriteFile(ConfigPath(dstPath), data, 0o644); err != nil {
		t.Fatalf("write fake bun config: %v", err)
	}
	return dstPath
}

func LoadExecutableConfig(exePath string) (Config, error) {
	data, err := os.ReadFile(ConfigPath(exePath))
	if err != nil {
		return Config{}, fmt.Errorf("read fake bun config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse fake bun config: %w", err)
	}
	return cfg, nil
}

func ConfigPath(exePath string) string {
	return exePath + ".json"
}

func LocalTypeScriptSDKPath() string {
	root, err := repoRoot()
	if err != nil {
		return ""
	}
	path := filepath.Join(root, "sdk", "typescript")
	if _, err := os.Stat(filepath.Join(path, "package.json")); err != nil {
		return ""
	}
	return path
}

func builtHelperBinary() (string, error) {
	buildHelperOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "gestalt-fake-bun-*")
		if err != nil {
			helperBinaryErr = fmt.Errorf("create temp dir: %w", err)
			return
		}

		helperBinary = filepath.Join(tmpDir, "bun"+executableSuffix())
		root, err := repoRoot()
		if err != nil {
			helperBinaryErr = err
			return
		}

		cmd := exec.Command(goBinaryPath(), "build", "-o", helperBinary, "./internal/testutil/testdata/cmd/fakebun")
		cmd.Dir = filepath.Join(root, "gestaltd")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			helperBinaryErr = fmt.Errorf("go build fake bun helper: %w", err)
			return
		}
	})
	return helperBinary, helperBinaryErr
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..")), nil
}

func goBinaryPath() string {
	if resolved, err := exec.LookPath("go"); err == nil {
		return resolved
	}
	return "go"
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
