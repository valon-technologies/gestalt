package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type gestaltCredentials struct {
	APIToken   string `json:"api_token"`
	APITokenID string `json:"api_token_id"`
}

type gestaltCredentialsFile struct {
	Servers map[string]gestaltCredentials `json:"servers"`
}

func defaultRemoteTokenForOrigin(origin string) (string, error) {
	if token := strings.TrimSpace(os.Getenv("GESTALT_API_KEY")); token != "" {
		return token, nil
	}
	normalizedOrigin := normalizeGestaltOrigin(origin)
	if normalizedOrigin == "" {
		return "", nil
	}
	path := gestaltCredentialsPath()
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("config validation: read Gestalt credentials: %w", err)
	}

	var file gestaltCredentialsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return "", fmt.Errorf("config validation: parse Gestalt credentials: %w", err)
	}
	if creds, ok := file.Servers[normalizedOrigin]; ok {
		return strings.TrimSpace(creds.APIToken), nil
	}
	return "", nil
}

func normalizeGestaltOrigin(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return trimmed
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port := parsed.Port(); port != "" && !isDefaultGestaltOriginPort(scheme, port) {
		host = host + ":" + port
	}
	return scheme + "://" + host
}

func isDefaultGestaltOriginPort(scheme, port string) bool {
	return (scheme == "https" && port == "443") || (scheme == "http" && port == "80")
}

func gestaltCredentialsPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "gestalt", "credentials.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "gestalt", "credentials.json")
	}
	return ""
}
