package operator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

const (
	localConfigDirName     = ".gestaltd"
	defaultProviderRepo    = "github.com/valon-technologies/gestalt-providers"
	defaultProviderVersion = "0.0.1-alpha.1"
	keychainService        = "gestaltd"
	keychainEncryptionKey  = "encryption-key"
)

func DefaultLocalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, localConfigDirName, "config.yaml")
}

func LocalConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, localConfigDirName, "config.yaml"),
	}
}

func GenerateDefaultConfig(configDir string) (string, error) {
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate encryption key: %w", err)
	}
	hexKey := hex.EncodeToString(key)

	dbPath := filepath.Join(configDir, "gestalt.db")
	providersDir := os.Getenv("GESTALT_PROVIDERS_DIR")

	if isInteractive() && tryStoreInKeychain(hexKey) {
		slog.Info("stored encryption key in OS keychain", "service", keychainService)
		var cfg string
		if providersDir != "" {
			cfg = defaultLocalSourceKeychainConfig(providersDir, dbPath)
		} else {
			cfg = defaultManagedKeychainConfig(dbPath)
		}
		if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
			return "", fmt.Errorf("write config: %w", err)
		}
		slog.Info("generated default config", "path", configPath, "secrets", "keychain")
		return configPath, nil
	}

	var cfg string
	if providersDir != "" {
		cfg = defaultLocalSourceConfig(providersDir, dbPath, hexKey)
	} else {
		cfg = defaultManagedConfig(dbPath, hexKey)
	}

	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	slog.Info("generated default config", "path", configPath, "secrets", "env")
	return configPath, nil
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func tryStoreInKeychain(hexKey string) bool {
	return keyring.Set(keychainService, keychainEncryptionKey, hexKey) == nil
}

func defaultManagedConfig(dbPath, encryptionKey string) string {
	return fmt.Sprintf(`datastore:
  provider:
    source:
      ref: %s/datastore/sqlite
      version: %s
  config:
    path: %q
secrets:
  provider: env
server:
  port: 8080
  encryption_key: %q
`, defaultProviderRepo, defaultProviderVersion, dbPath, encryptionKey)
}

func defaultManagedKeychainConfig(dbPath string) string {
	return fmt.Sprintf(`datastore:
  provider:
    source:
      ref: %s/datastore/sqlite
      version: %s
  config:
    path: %q
secrets:
  provider: keychain
server:
  port: 8080
  encryption_key: secret://encryption-key
`, defaultProviderRepo, defaultProviderVersion, dbPath)
}

func defaultLocalSourceConfig(providersDir, dbPath, encryptionKey string) string {
	return fmt.Sprintf(`datastore:
  provider:
    source:
      path: %q
  config:
    path: %q
secrets:
  provider: env
server:
  port: 8080
  encryption_key: %q
`, filepath.Join(providersDir, "datastore", "sqlite", "plugin.yaml"), dbPath, encryptionKey)
}

func defaultLocalSourceKeychainConfig(providersDir, dbPath string) string {
	return fmt.Sprintf(`datastore:
  provider:
    source:
      path: %q
  config:
    path: %q
secrets:
  provider: keychain
server:
  port: 8080
  encryption_key: secret://encryption-key
`, filepath.Join(providersDir, "datastore", "sqlite", "plugin.yaml"), dbPath)
}
