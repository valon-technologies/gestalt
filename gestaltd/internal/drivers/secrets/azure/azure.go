package azure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"github.com/valon-technologies/gestalt/server/core"
)

const defaultTimeout = 10 * time.Second

type kvClient interface {
	GetSecret(ctx context.Context, name string, version string, options *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
}

type Provider struct {
	client  kvClient
	version string
}

func (p *Provider) GetSecret(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	resp, err := p.client.GetSecret(ctx, name, p.version, nil)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("%w: %q", core.ErrSecretNotFound, name)
		}
		return "", fmt.Errorf("accessing secret %q: %w", name, err)
	}
	if resp.Value == nil {
		return "", fmt.Errorf("secret %q has nil value", name)
	}
	return *resp.Value, nil
}
