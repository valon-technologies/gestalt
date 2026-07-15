package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const legacyDefaultRemoteName = "default"

// DefaultRemoteName returns the name of the remote marked default: true, if any.
func (c *Config) DefaultRemoteName() string {
	if c == nil {
		return ""
	}
	for name, remote := range c.Server.Remotes {
		if remote != nil && remote.Default {
			return name
		}
	}
	return ""
}

func canonicalizeRemotes(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if cfg.Server.Remotes == nil {
		cfg.Server.Remotes = map[string]*RemoteConfig{}
	}

	legacyURL := strings.TrimRight(strings.TrimSpace(cfg.Server.Remote), "/")
	legacyToken := strings.TrimSpace(cfg.Server.RemoteToken)

	if legacyURL != "" || legacyToken != "" {
		entry := cfg.Server.Remotes[legacyDefaultRemoteName]
		if entry == nil {
			entry = &RemoteConfig{}
			cfg.Server.Remotes[legacyDefaultRemoteName] = entry
		}
		entry.URL = strings.TrimRight(strings.TrimSpace(entry.URL), "/")
		entry.Token = strings.TrimSpace(entry.Token)
		if legacyURL != "" {
			if entry.URL != "" && entry.URL != legacyURL {
				return fmt.Errorf("config validation: server.remote conflicts with server.remotes.%s.url", legacyDefaultRemoteName)
			}
			if entry.URL == "" {
				entry.URL = legacyURL
			}
		}
		if legacyToken != "" {
			if entry.Token != "" && entry.Token != legacyToken {
				return fmt.Errorf("config validation: server.remoteToken conflicts with server.remotes.%s.token", legacyDefaultRemoteName)
			}
			if entry.Token == "" {
				entry.Token = legacyToken
			}
		}
		if entry.URL != "" && !entry.Default {
			hasDefault := false
			for name, remote := range cfg.Server.Remotes {
				if name == legacyDefaultRemoteName || remote == nil {
					continue
				}
				if remote.Default {
					hasDefault = true
					break
				}
			}
			if !hasDefault {
				entry.Default = true
			}
		}
	}

	cfg.Server.Remote = ""
	cfg.Server.RemoteToken = ""

	return finalizeRemoteMap(cfg)
}

func finalizeRemoteMap(cfg *Config) error {
	if cfg == nil || len(cfg.Server.Remotes) == 0 {
		return nil
	}
	normalized := make(map[string]*RemoteConfig, len(cfg.Server.Remotes))
	for name, remote := range cfg.Server.Remotes {
		if remote == nil {
			continue
		}
		canonical := strings.TrimSpace(name)
		if canonical == "" {
			return fmt.Errorf("config validation: server.remotes contains a blank remote name")
		}
		if name != canonical {
			return fmt.Errorf("config validation: server.remotes key %q must not include leading or trailing whitespace", name)
		}
		remote.URL = strings.TrimRight(strings.TrimSpace(remote.URL), "/")
		remote.Token = strings.TrimSpace(remote.Token)
		if _, exists := normalized[canonical]; exists {
			return fmt.Errorf("config validation: server.remotes.%s is duplicated", canonical)
		}
		normalized[canonical] = remote
	}
	cfg.Server.Remotes = normalized
	if len(cfg.Server.Remotes) == 0 {
		cfg.Server.Remotes = nil
	}
	return nil
}

func validateRemotesConfig(cfg *Config) error {
	if cfg == nil || len(cfg.Server.Remotes) == 0 {
		return nil
	}

	defaultCount := 0
	for name, remote := range cfg.Server.Remotes {
		if remote == nil {
			return fmt.Errorf("config validation: server.remotes.%s is required", name)
		}
		label := "server.remotes." + name + ".url"
		if strings.TrimSpace(remote.URL) == "" {
			return fmt.Errorf("config validation: %s is required", label)
		}
		if err := validateHTTPOriginURL(label, remote.URL); err != nil {
			return err
		}
		if remote.Default {
			defaultCount++
		}
		if defaultCount > 1 {
			return fmt.Errorf("config validation: at most one server.remotes entry may set default: true")
		}
		if !remote.Default && strings.TrimSpace(remote.Token) == "" {
			return fmt.Errorf("config validation: server.remotes.%s.token is required for non-default remotes", name)
		}
	}
	return nil
}

func validateAppRemoteRefs(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	entry.Remote = strings.TrimSpace(entry.Remote)
	if entry.Local && entry.Remote != "" {
		return fmt.Errorf("config validation: apps.%s cannot set both local: true and remote: %q", name, entry.Remote)
	}
	if entry.Remote == "" {
		return nil
	}
	if cfg.Server.Remotes == nil || cfg.Server.Remotes[entry.Remote] == nil {
		return fmt.Errorf("config validation: apps.%s.remote %q is not configured in server.remotes", name, entry.Remote)
	}
	return nil
}

// ValidateRemoteGestaltd checks serve-time remote requirements.
func ValidateRemoteGestaltd(cfg *Config) error {
	if cfg == nil || len(cfg.Server.Remotes) == 0 {
		return nil
	}
	for name, remote := range cfg.Server.Remotes {
		if remote == nil {
			continue
		}
		if strings.TrimSpace(remote.Token) == "" {
			if remote.Default {
				return fmt.Errorf("config validation: server.remotes.%s.token is required for the default remote", name)
			}
			return fmt.Errorf("config validation: server.remotes.%s.token is required", name)
		}
	}
	return nil
}

// ApplyServeRemoteOverrides applies gestaltd serve CLI overrides for remote
// gestaltd delegation and validates the resolved remote configuration.
func ApplyServeRemoteOverrides(cfg *Config, remote, remoteToken string) error {
	if cfg == nil {
		return nil
	}
	if err := canonicalizeRemotes(cfg); err != nil {
		return err
	}

	if remote != "" {
		name := cfg.DefaultRemoteName()
		if name == "" {
			name = legacyDefaultRemoteName
		}
		entry := cfg.Server.Remotes[name]
		if entry == nil {
			entry = &RemoteConfig{}
			if cfg.Server.Remotes == nil {
				cfg.Server.Remotes = map[string]*RemoteConfig{}
			}
			cfg.Server.Remotes[name] = entry
		}
		entry.Default = true
		url := strings.TrimRight(strings.TrimSpace(remote), "/")
		if err := validateHTTPOriginURL("server.remotes."+name+".url", url); err != nil {
			return err
		}
		entry.URL = url
	}

	if len(cfg.Server.Remotes) == 0 {
		if url, err := gestaltCLIConfigURL(); err != nil {
			return err
		} else if url != "" {
			cfg.Server.Remotes = map[string]*RemoteConfig{
				legacyDefaultRemoteName: {
					URL:     url,
					Default: true,
				},
			}
		}
	}

	if remoteToken != "" {
		defaultName := cfg.DefaultRemoteName()
		if defaultName == "" {
			return fmt.Errorf("config validation: --remote-token requires a default remote")
		}
		entry := cfg.Server.Remotes[defaultName]
		if entry == nil {
			return fmt.Errorf("config validation: server.remotes.%s is required", defaultName)
		}
		entry.Token = strings.TrimSpace(remoteToken)
	}

	if defaultName := cfg.DefaultRemoteName(); defaultName != "" {
		entry := cfg.Server.Remotes[defaultName]
		if entry != nil && strings.TrimSpace(entry.Token) == "" {
			token, err := defaultRemoteToken()
			if err != nil {
				return err
			}
			if strings.TrimSpace(token) == "" {
				return fmt.Errorf("config validation: server.remotes.%s.token is required for the default remote", defaultName)
			}
			entry.Token = token
		}
	}

	return ValidateRemoteGestaltd(cfg)
}

func defaultRemoteToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("GESTALT_API_KEY")); token != "" {
		return token, nil
	}
	path := gestaltConfigPath("credentials.json")
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("config validation: read Gestalt credentials at %s: %w", path, err)
	}
	var creds struct {
		APIToken string `json:"api_token"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("config validation: parse Gestalt credentials at %s: %w", path, err)
	}
	return strings.TrimSpace(creds.APIToken), nil
}

func gestaltConfigPath(filename string) string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "gestalt", filename)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "gestalt", filename)
	}
	return ""
}

func gestaltCLIConfigURL() (string, error) {
	path := gestaltConfigPath("config.json")
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("config validation: read Gestalt CLI config at %s: %w", path, err)
	}
	var cliConfig struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &cliConfig); err != nil {
		return "", fmt.Errorf("config validation: parse Gestalt CLI config at %s: %w", path, err)
	}
	url := strings.TrimRight(strings.TrimSpace(cliConfig.URL), "/")
	if url == "" {
		return "", nil
	}
	if err := validateHTTPOriginURL("gestalt CLI config url", url); err != nil {
		return "", err
	}
	return url, nil
}
