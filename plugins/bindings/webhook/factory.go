package webhook

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
)

var Factory bootstrap.BindingFactory = func(_ context.Context, name string, def config.BindingDef, broker core.Broker) (core.Binding, error) {
	var cfg webhookConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Path == "" || !strings.HasPrefix(cfg.Path, "/") {
		return nil, fmt.Errorf("webhook %q: path must be non-empty and start with /", name)
	}
	if (cfg.Provider == "") != (cfg.Operation == "") {
		return nil, fmt.Errorf("webhook %q: provider and operation must both be set or both be empty", name)
	}

	switch cfg.AuthMode {
	case "", AuthModePublic:
		cfg.AuthMode = AuthModePublic
	case AuthModeSigned:
		if cfg.SigningSecret == "" {
			return nil, fmt.Errorf("webhook %q: signing_secret is required for auth_mode %q", name, AuthModeSigned)
		}
	case AuthModeTrustedUserHeader:
		if cfg.UserHeader == "" {
			return nil, fmt.Errorf("webhook %q: user_header is required for auth_mode %q", name, AuthModeTrustedUserHeader)
		}
	default:
		return nil, fmt.Errorf("webhook %q: unknown auth_mode %q", name, cfg.AuthMode)
	}

	if cfg.SignatureHeader == "" {
		cfg.SignatureHeader = DefaultSignatureHeader
	}

	if cfg.Provider != "" && len(def.Providers) > 0 {
		found := false
		for _, p := range def.Providers {
			if p == cfg.Provider {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("webhook %q: provider %q is not in the binding's allowed providers %v", name, cfg.Provider, def.Providers)
		}
	}

	return New(name, cfg, broker), nil
}
