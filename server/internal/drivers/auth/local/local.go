package local

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	defaultEmail      = "local@gestalt.local"
	defaultSessionTTL = 24 * time.Hour
	localCode         = "local"
)

type Provider struct {
	secret []byte
	ttl    time.Duration
	email  string
}

func (p *Provider) Name() string        { return "local" }
func (p *Provider) DisplayName() string { return "Local" }

func (p *Provider) SessionTokenTTL() time.Duration { return p.ttl }

func (p *Provider) LoginURL(state string) (string, error) {
	return config.AuthCallbackPath + "?code=" + localCode + "&state=" + url.QueryEscape(state), nil
}

func (p *Provider) HandleCallback(_ context.Context, code string) (*core.UserIdentity, error) {
	if code != localCode {
		return nil, fmt.Errorf("invalid local auth code")
	}
	return &core.UserIdentity{
		Email:       p.email,
		DisplayName: "Local User",
	}, nil
}

func (p *Provider) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return session.IssueToken(identity, p.secret, p.ttl)
}

func (p *Provider) ValidateToken(_ context.Context, token string) (*core.UserIdentity, error) {
	return session.ValidateToken(token, p.secret)
}

type yamlConfig struct {
	Email string `yaml:"email"`
}

var Factory bootstrap.AuthFactory = func(node yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
	var cfg yamlConfig
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("local auth: parsing config: %w", err)
	}
	email := cfg.Email
	if email == "" {
		email = defaultEmail
	}
	if len(deps.EncryptionKey) == 0 {
		return nil, fmt.Errorf("local auth: encryption key is required")
	}
	return &Provider{
		secret: deps.EncryptionKey,
		ttl:    defaultSessionTTL,
		email:  email,
	}, nil
}
