package vault

import (
	"context"
	"fmt"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/valon-technologies/gestalt/server/core"
)

const (
	defaultTimeout = 10 * time.Second
	kvDataKey      = "value"
)

type Provider struct {
	client    *vaultapi.Client
	mountPath string
}

func (p *Provider) GetSecret(ctx context.Context, name string) (string, error) {
	if strings.Contains(name, "/") {
		return "", fmt.Errorf("invalid secret name %q: must not contain '/'", name)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	path := fmt.Sprintf("%s/data/%s", p.mountPath, name)
	secret, err := p.client.Logical().ReadWithContext(ctx, path)
	if err != nil {
		return "", fmt.Errorf("accessing secret %q: %w", name, err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("%w: %q", core.ErrSecretNotFound, name)
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("%w: %q (no data in KV v2 response)", core.ErrSecretNotFound, name)
	}

	value, ok := data[kvDataKey].(string)
	if !ok {
		return "", fmt.Errorf("secret %q: missing or non-string %q key in KV data", name, kvDataKey)
	}
	return value, nil
}
