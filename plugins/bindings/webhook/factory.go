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
	return New(name, cfg, broker), nil
}
