package none

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"gopkg.in/yaml.v3"
)

const providerName = "none"

type Provider struct{}

func (p *Provider) Name() string { return providerName }

func (p *Provider) LoginURL(_ string) (string, error) {
	return "", fmt.Errorf("none auth provider does not support login")
}

func (p *Provider) HandleCallback(_ context.Context, _ string) (*core.UserIdentity, error) {
	return nil, fmt.Errorf("none auth provider does not support callbacks")
}

func (p *Provider) ValidateToken(_ context.Context, _ string) (*core.UserIdentity, error) {
	return nil, fmt.Errorf("none auth provider does not validate tokens")
}

var Factory bootstrap.AuthFactory = func(_ yaml.Node, deps bootstrap.Deps) (core.AuthProvider, error) {
	if !deps.DevMode {
		return nil, fmt.Errorf("none auth provider requires server.dev_mode: true")
	}
	return &Provider{}, nil
}
