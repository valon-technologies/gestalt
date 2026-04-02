package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	sqliteEncryptionSaltSuffix = ".argon2id-salt"
	configEncryptionSaltDir    = ".gestalt"
	configEncryptionSaltFile   = "encryption-key.argon2id-salt"
	argon2SaltSizeBytes        = 16
)

func resolveServerEncryptionKeys(cfg *config.Config) ([]byte, []byte, error) {
	if cfg == nil || cfg.Server.EncryptionKey == "" {
		return nil, nil, nil
	}

	if key, err := hex.DecodeString(cfg.Server.EncryptionKey); err == nil && len(key) == 32 {
		return key, nil, nil
	}

	legacyKey := crypto.DeriveKey(cfg.Server.EncryptionKey)
	saltPath, ok, err := encryptionSaltPath(cfg)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return legacyKey, nil, nil
	}

	salt, err := loadOrCreateEncryptionSalt(saltPath)
	if err != nil {
		return nil, nil, err
	}
	return crypto.DeriveKeyWithSalt(cfg.Server.EncryptionKey, salt), legacyKey, nil
}

func encryptionSaltPath(cfg *config.Config) (string, bool, error) {
	if cfg.Datastore.Provider == "sqlite" {
		var sqliteCfg struct {
			Path string `yaml:"path"`
		}
		if cfg.Datastore.Config.Kind != 0 {
			if err := cfg.Datastore.Config.Decode(&sqliteCfg); err != nil {
				return "", false, fmt.Errorf("resolving sqlite encryption salt path: %w", err)
			}
		}
		if sqliteCfg.Path == "" {
			sqliteCfg.Path = "./gestalt.db"
		}
		return sqliteCfg.Path + sqliteEncryptionSaltSuffix, true, nil
	}
	if cfg.ResolvedConfigPath == "" {
		return "", false, nil
	}
	return filepath.Join(filepath.Dir(cfg.ResolvedConfigPath), configEncryptionSaltDir, configEncryptionSaltFile), true, nil
}

func loadOrCreateEncryptionSalt(path string) ([]byte, error) {
	salt, err := readEncryptionSalt(path)
	if err == nil {
		return salt, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	salt = make([]byte, argon2SaltSizeBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating encryption salt: %w", err)
	}
	if err := writeEncryptionSalt(path, salt); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readEncryptionSalt(path)
		}
		return nil, err
	}
	return salt, nil
}

func readEncryptionSalt(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	salt, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(salt) == 0 {
		return nil, fmt.Errorf("parsing encryption salt %s: invalid hex", path)
	}
	return salt, nil
}

func writeEncryptionSalt(path string, salt []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating encryption salt directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	data := append([]byte(hex.EncodeToString(salt)), '\n')
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("writing encryption salt %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing encryption salt %s: %w", path, err)
	}
	return nil
}
