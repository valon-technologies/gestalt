package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/valon-technologies/gestalt/core"
)

const defaultTimeout = 10 * time.Second

type Provider struct {
	client  *secretmanager.Client
	project string
	version string
}

func (p *Provider) GetSecret(ctx context.Context, name string) (string, error) {
	if strings.Contains(name, "/") {
		return "", fmt.Errorf("invalid secret name %q: must not contain '/'", name)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	resourceName := fmt.Sprintf("projects/%s/secrets/%s/versions/%s", p.project, name, p.version)
	resp, err := p.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: resourceName,
	})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			return "", fmt.Errorf("%w: %q in project %q", core.ErrSecretNotFound, name, p.project)
		}
		return "", fmt.Errorf("accessing secret %q: %w", name, err)
	}
	if resp.Payload == nil {
		return "", fmt.Errorf("accessing secret %q: response payload is nil", name)
	}
	return string(resp.Payload.Data), nil
}

func (p *Provider) Close() error {
	return p.client.Close()
}
