package observability

import (
	"context"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"go.opentelemetry.io/otel/attribute"
)

type observedExternalCredentialProvider struct {
	name     string
	delegate core.ExternalCredentialProvider
}

func InstrumentExternalCredentialProvider(name string, provider core.ExternalCredentialProvider) core.ExternalCredentialProvider {
	if provider == nil {
		return nil
	}
	if _, ok := provider.(*observedExternalCredentialProvider); ok {
		return provider
	}
	return &observedExternalCredentialProvider{
		name:     strings.TrimSpace(name),
		delegate: provider,
	}
}

func (p *observedExternalCredentialProvider) PutCredential(ctx context.Context, credential *core.ExternalCredential) (err error) {
	ctx, end := p.start(ctx, "put_credential", credentialIntegration(credential))
	defer func() { end(err) }()
	return p.delegate.PutCredential(ctx, credential)
}

func (p *observedExternalCredentialProvider) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) (err error) {
	ctx, end := p.start(ctx, "restore_credential", credentialIntegration(credential))
	defer func() { end(err) }()
	return p.delegate.RestoreCredential(ctx, credential)
}

func (p *observedExternalCredentialProvider) GetCredential(ctx context.Context, subjectID, connectionID, instance string) (credential *core.ExternalCredential, err error) {
	ctx, end := p.start(ctx, "get_credential", connectionID)
	defer func() { end(err) }()
	return p.delegate.GetCredential(ctx, subjectID, connectionID, instance)
}

func (p *observedExternalCredentialProvider) ListCredentials(ctx context.Context, subjectID string) (credentials []*core.ExternalCredential, err error) {
	ctx, end := p.start(ctx, "list_credentials", "")
	defer func() { end(err) }()
	return p.delegate.ListCredentials(ctx, subjectID)
}

func (p *observedExternalCredentialProvider) ListCredentialsForConnection(ctx context.Context, subjectID, connectionID string) (credentials []*core.ExternalCredential, err error) {
	ctx, end := p.start(ctx, "list_credentials_for_connection", connectionID)
	defer func() { end(err) }()
	return p.delegate.ListCredentialsForConnection(ctx, subjectID, connectionID)
}

func (p *observedExternalCredentialProvider) DeleteCredential(ctx context.Context, id string) (err error) {
	ctx, end := p.start(ctx, "delete_credential", "")
	defer func() { end(err) }()
	return p.delegate.DeleteCredential(ctx, id)
}

func (p *observedExternalCredentialProvider) ValidateCredentialConfig(ctx context.Context, req *core.ValidateExternalCredentialConfigRequest) (err error) {
	ctx, end := p.start(ctx, "validate_credential_config", requestConnectionID(req))
	defer func() { end(err) }()
	return p.delegate.ValidateCredentialConfig(ctx, req)
}

func (p *observedExternalCredentialProvider) ResolveCredential(ctx context.Context, req *core.ResolveExternalCredentialRequest) (resp *core.ResolveExternalCredentialResponse, err error) {
	ctx, end := p.start(ctx, "resolve_credential", resolveRequestConnectionID(req))
	defer func() { end(err) }()
	return p.delegate.ResolveCredential(ctx, req)
}

func (p *observedExternalCredentialProvider) ExchangeCredential(ctx context.Context, req *core.ExchangeExternalCredentialRequest) (resp *core.ExchangeExternalCredentialResponse, err error) {
	ctx, end := p.start(ctx, "exchange_credential", exchangeRequestConnectionID(req))
	defer func() { end(err) }()
	return p.delegate.ExchangeCredential(ctx, req)
}

func (p *observedExternalCredentialProvider) Close() error {
	closer, ok := p.delegate.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func (p *observedExternalCredentialProvider) start(ctx context.Context, operation string, integration string) (context.Context, func(error)) {
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		AttrCredentialProvider.String(p.name),
		AttrCredentialOperation.String(operation),
	}
	if strings.TrimSpace(integration) != "" {
		attrs = append(attrs, attribute.String("gestalt.provider", strings.TrimSpace(integration)))
	}
	ctx, span := StartSpan(ctx, "credential.provider.operation", attrs...)
	return ctx, func(err error) {
		EndSpan(span, err)
		RecordCredentialProviderOperation(ctx, startedAt, err != nil, attrs...)
	}
}

func credentialIntegration(credential *core.ExternalCredential) string {
	if credential == nil {
		return ""
	}
	if credential.ConnectionID != "" {
		return credential.ConnectionID
	}
	return credential.Integration
}

func requestConnectionID(req *core.ValidateExternalCredentialConfigRequest) string {
	if req == nil {
		return ""
	}
	return req.ConnectionID
}

func resolveRequestConnectionID(req *core.ResolveExternalCredentialRequest) string {
	if req == nil {
		return ""
	}
	return req.ConnectionID
}

func exchangeRequestConnectionID(req *core.ExchangeExternalCredentialRequest) string {
	if req == nil {
		return ""
	}
	return req.ConnectionID
}
