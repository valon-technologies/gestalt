package datadog

import (
	"fmt"

	"github.com/valon-technologies/gestalt/internal/apiexec"
	"github.com/valon-technologies/gestalt/internal/provider"
)

func init() {
	provider.RegisterTokenParser("datadog_keys", datadogTokenParser)
}

func datadogTokenParser(token string) (string, map[string]string, error) {
	var keys struct {
		APIKey string `json:"api_key"`
		AppKey string `json:"app_key"`
	}
	if err := apiexec.ParseJSONToken(token, &keys); err != nil {
		return "", nil, fmt.Errorf("datadog: expected JSON token with api_key and app_key: %w", err)
	}
	if keys.APIKey == "" || keys.AppKey == "" {
		return "", nil, fmt.Errorf("datadog: token missing api_key or app_key")
	}
	return "", map[string]string{
		"DD-API-KEY":         keys.APIKey,
		"DD-APPLICATION-KEY": keys.AppKey,
	}, nil
}
