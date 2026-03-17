package google

import (
	"fmt"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	ClientID       string   `yaml:"client_id"`
	ClientSecret   string   `yaml:"client_secret"`
	RedirectURL    string   `yaml:"redirect_url"`
	AllowedDomains []string `yaml:"allowed_domains"`
}

var Factory bootstrap.AuthFactory = func(node yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("google auth: parsing config: %w", err)
	}
	return New(Config{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		RedirectURL:    cfg.RedirectURL,
		AllowedDomains: cfg.AllowedDomains,
		SessionSecret:  deps.EncryptionKey,
	})
}
