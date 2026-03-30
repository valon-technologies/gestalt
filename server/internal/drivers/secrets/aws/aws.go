package aws

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/valon-technologies/gestalt/server/core"
)

const defaultTimeout = 10 * time.Second

type smClient interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type Provider struct {
	client       smClient
	versionStage string
}

func (p *Provider) GetSecret(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(name),
		VersionStage: aws.String(p.versionStage),
	}

	resp, err := p.client.GetSecretValue(ctx, input)
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return "", fmt.Errorf("%w: %q", core.ErrSecretNotFound, name)
		}
		return "", fmt.Errorf("accessing secret %q: %w", name, err)
	}
	if resp.SecretString == nil {
		return "", fmt.Errorf("secret %q has no string value (binary secrets are not supported)", name)
	}
	return *resp.SecretString, nil
}
