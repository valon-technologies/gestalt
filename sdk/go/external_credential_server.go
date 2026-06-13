package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// externalCredentialHandler bridges the ergonomic [ExternalCredentialProvider]
// facade onto the generated transport handler; wire conversion lives in the
// generated adapter. providerRPCError preserves root sentinel-error mapping.
type externalCredentialHandler struct {
	client.UnimplementedExternalCredentialsProvider
	provider ExternalCredentialProvider
}

func (h externalCredentialHandler) CreateCredential(ctx context.Context, req *client.CreateExternalCredentialRequest) (*client.ExternalCredential, error) {
	rootReq := &CreateExternalCredentialRequest{
		Credential: clientExternalCredentialToRoot(req.Credential),
	}
	credential, err := h.provider.CreateCredential(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("create external credential", err)
	}
	if credential == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil credential")
	}
	return rootExternalCredentialToClient(credential), nil
}

func (h externalCredentialHandler) UpsertCredential(ctx context.Context, req *client.UpsertExternalCredentialRequest) (*client.ExternalCredential, error) {
	rootReq := &UpsertExternalCredentialRequest{
		Credential: clientExternalCredentialToRoot(req.Credential),
	}
	credential, err := h.provider.UpsertCredential(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("upsert external credential", err)
	}
	if credential == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil credential")
	}
	return rootExternalCredentialToClient(credential), nil
}

func (h externalCredentialHandler) GetCredential(ctx context.Context, req *client.GetExternalCredentialRequest) (*client.ExternalCredential, error) {
	rootReq := &GetExternalCredentialRequest{
		Subject:   req.Subject,
		Audience:  req.Audience,
		Qualifier: req.Qualifier,
	}
	credential, err := h.provider.GetCredential(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("get external credential", err)
	}
	if credential == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil credential")
	}
	return rootExternalCredentialToClient(credential), nil
}

func (h externalCredentialHandler) ListCredentials(ctx context.Context, req *client.ListExternalCredentialsRequest) (*client.ListExternalCredentialsResponse, error) {
	rootReq := &ListExternalCredentialsRequest{
		Subject:  req.Subject,
		Audience: req.Audience,
	}
	resp, err := h.provider.ListCredentials(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("list external credentials", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil response")
	}
	out := &client.ListExternalCredentialsResponse{
		Credentials: make([]*client.ExternalCredential, 0, len(resp.Credentials)),
	}
	for _, c := range resp.Credentials {
		out.Credentials = append(out.Credentials, rootExternalCredentialToClient(c))
	}
	return out, nil
}

func (h externalCredentialHandler) DeleteCredential(ctx context.Context, req *client.DeleteExternalCredentialRequest) error {
	rootReq := &DeleteExternalCredentialRequest{ID: req.ID}
	if err := h.provider.DeleteCredential(ctx, rootReq); err != nil {
		return providerRPCError("delete external credential", err)
	}
	return nil
}

func (h externalCredentialHandler) ValidateCredentialConfig(ctx context.Context, req *client.ValidateExternalCredentialConfigRequest) error {
	rootReq := &ValidateExternalCredentialConfigRequest{
		Provider:         req.Provider,
		Connection:       req.Connection,
		ConnectionID:     req.ConnectionID,
		Mode:             req.Mode,
		Auth:             clientExternalCredentialAuthConfigToRoot(req.Auth),
		ConnectionParams: copyStringMap(req.ConnectionParams),
	}
	if err := h.provider.ValidateCredentialConfig(ctx, rootReq); err != nil {
		return providerRPCError("validate external credential config", err)
	}
	return nil
}

func (h externalCredentialHandler) ResolveCredential(ctx context.Context, req *client.ResolveExternalCredentialRequest) (*client.ResolveExternalCredentialResponse, error) {
	rootReq := &ResolveExternalCredentialRequest{
		Provider:            req.Provider,
		Connection:          req.Connection,
		ConnectionID:        req.ConnectionID,
		Mode:                req.Mode,
		CredentialSubjectID: req.CredentialSubjectID,
		ActorSubjectID:      req.ActorSubjectID,
		Instance:            req.Instance,
		Auth:                clientExternalCredentialAuthConfigToRoot(req.Auth),
		ConnectionParams:    copyStringMap(req.ConnectionParams),
	}
	resp, err := h.provider.ResolveCredential(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("resolve external credential", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil response")
	}
	return &client.ResolveExternalCredentialResponse{
		Token:        resp.Token,
		ExpiresAt:    resp.ExpiresAt,
		MetadataJSON: resp.MetadataJSON,
		Params:       copyStringMap(resp.Params),
		Credential:   rootExternalCredentialToClient(resp.Credential),
	}, nil
}

func (h externalCredentialHandler) ExchangeCredential(ctx context.Context, req *client.ExchangeExternalCredentialRequest) (*client.ExchangeExternalCredentialResponse, error) {
	rootReq := &ExchangeExternalCredentialRequest{
		Provider:            req.Provider,
		Connection:          req.Connection,
		ConnectionID:        req.ConnectionID,
		CredentialSubjectID: req.CredentialSubjectID,
		ActorSubjectID:      req.ActorSubjectID,
		Instance:            req.Instance,
		Auth:                clientExternalCredentialAuthConfigToRoot(req.Auth),
		CredentialJSON:      req.CredentialJSON,
		ConnectionParams:    copyStringMap(req.ConnectionParams),
	}
	resp, err := h.provider.ExchangeCredential(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("exchange external credential", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil response")
	}
	return &client.ExchangeExternalCredentialResponse{
		TokenResponse: rootExternalCredentialTokenResponseToClient(resp.TokenResponse),
	}, nil
}

// rootExternalCredentialToClient converts a root ExternalCredential (with
// Grant/Client/Opaque fields) to the client typed-oneof form.
func rootExternalCredentialToClient(src *ExternalCredential) *client.ExternalCredential {
	if src == nil {
		return nil
	}
	out := &client.ExternalCredential{
		ID:           src.ID,
		Subject:      src.Subject,
		Audience:     src.Audience,
		Qualifier:    src.Qualifier,
		MetadataJSON: src.MetadataJSON,
		CreatedAt:    src.CreatedAt,
		UpdatedAt:    src.UpdatedAt,
	}
	switch {
	case src.Grant != nil:
		out.Credential = &client.ExternalCredentialCredentialGrant{
			Value: &client.ExternalCredentialGrant{
				AccessToken:       src.Grant.AccessToken,
				RefreshToken:      src.Grant.RefreshToken,
				Scope:             src.Grant.Scope,
				ExpiresAt:         src.Grant.ExpiresAt,
				LastRefreshedAt:   src.Grant.LastRefreshedAt,
				RefreshErrorCount: src.Grant.RefreshErrorCount,
			},
		}
	case src.Client != nil:
		out.Credential = &client.ExternalCredentialCredentialClient{
			Value: &client.ExternalCredentialClientInfo{
				ClientID:              src.Client.ClientID,
				ClientSecret:          src.Client.ClientSecret,
				ClientSecretExpiresAt: src.Client.ClientSecretExpiresAt,
			},
		}
	case src.Opaque != nil:
		out.Credential = &client.ExternalCredentialCredentialOpaque{
			Value: &client.ExternalCredentialOpaque{Fields: copyStringMap(src.Opaque.Fields)},
		}
	}
	return out
}

// clientExternalCredentialToRoot converts the client typed-oneof form back to
// the root ExternalCredential with Grant/Client/Opaque fields.
func clientExternalCredentialToRoot(src *client.ExternalCredential) *ExternalCredential {
	if src == nil {
		return nil
	}
	out := &ExternalCredential{
		ID:           src.ID,
		Subject:      src.Subject,
		Audience:     src.Audience,
		Qualifier:    src.Qualifier,
		MetadataJSON: src.MetadataJSON,
		CreatedAt:    src.CreatedAt,
		UpdatedAt:    src.UpdatedAt,
	}
	switch variant := src.Credential.(type) {
	case *client.ExternalCredentialCredentialGrant:
		if g := variant.Value; g != nil {
			out.Grant = &ExternalCredentialGrant{
				AccessToken:       g.AccessToken,
				RefreshToken:      g.RefreshToken,
				Scope:             g.Scope,
				ExpiresAt:         g.ExpiresAt,
				LastRefreshedAt:   g.LastRefreshedAt,
				RefreshErrorCount: g.RefreshErrorCount,
			}
		}
	case *client.ExternalCredentialCredentialClient:
		if c := variant.Value; c != nil {
			out.Client = &ExternalCredentialClientInfo{
				ClientID:              c.ClientID,
				ClientSecret:          c.ClientSecret,
				ClientSecretExpiresAt: c.ClientSecretExpiresAt,
			}
		}
	case *client.ExternalCredentialCredentialOpaque:
		if o := variant.Value; o != nil {
			out.Opaque = &ExternalCredentialOpaque{Fields: copyStringMap(o.Fields)}
		}
	}
	return out
}

func clientExternalCredentialAuthConfigToRoot(src *client.ExternalCredentialAuthConfig) *ExternalCredentialAuthConfig {
	if src == nil {
		return nil
	}
	drivers := make([]*ExternalCredentialTokenExchangeDriver, 0, len(src.TokenExchangeDrivers))
	for _, d := range src.TokenExchangeDrivers {
		if d == nil {
			continue
		}
		drivers = append(drivers, &ExternalCredentialTokenExchangeDriver{
			Type:            d.Type,
			TargetPrincipal: d.TargetPrincipal,
			Scopes:          copyStringSlice(d.Scopes),
			LifetimeSeconds: d.LifetimeSeconds,
			Endpoint:        d.Endpoint,
			Params:          copyStringMap(d.Params),
		})
	}
	return &ExternalCredentialAuthConfig{
		Type:                 src.Type,
		Token:                src.Token,
		TokenPrefix:          src.TokenPrefix,
		GrantType:            src.GrantType,
		TokenURL:             src.TokenURL,
		ClientID:             src.ClientID,
		ClientSecret:         src.ClientSecret,
		ClientAuth:           src.ClientAuth,
		TokenExchange:        src.TokenExchange,
		Scopes:               copyStringSlice(src.Scopes),
		ScopeParam:           src.ScopeParam,
		ScopeSeparator:       src.ScopeSeparator,
		TokenParams:          copyStringMap(src.TokenParams),
		RefreshParams:        copyStringMap(src.RefreshParams),
		AcceptHeader:         src.AcceptHeader,
		AccessTokenPath:      src.AccessTokenPath,
		TokenExchangeDrivers: drivers,
		RefreshToken:         src.RefreshToken,
	}
}

func rootExternalCredentialTokenResponseToClient(src *ExternalCredentialTokenResponse) *client.ExternalCredentialTokenResponse {
	if src == nil {
		return nil
	}
	return &client.ExternalCredentialTokenResponse{
		AccessToken:   src.AccessToken,
		RefreshToken:  src.RefreshToken,
		ExpiresIn:     src.ExpiresIn,
		TokenType:     src.TokenType,
		ExtraJSON:     src.ExtraJSON,
		RefreshSource: src.RefreshSource,
	}
}
