package gestalt

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

func externalCredentialFromProto(value *proto.ExternalCredential) (*ExternalCredential, error) {
	if value == nil {
		return nil, nil
	}
	createdAt, err := timePtrFromTimestamp(value.GetCreatedAt())
	if err != nil {
		return nil, err
	}
	updatedAt, err := timePtrFromTimestamp(value.GetUpdatedAt())
	if err != nil {
		return nil, err
	}
	out := &ExternalCredential{
		ID:           value.GetId(),
		Subject:      value.GetSubject(),
		Audience:     value.GetAudience(),
		Qualifier:    value.GetQualifier(),
		AccountKey:   value.GetAccountKey(),
		MetadataJSON: value.GetMetadataJson(),
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
	switch variant := value.GetCredential().(type) {
	case *proto.ExternalCredential_Grant:
		expiresAt, err := timePtrFromTimestamp(variant.Grant.GetExpiresAt())
		if err != nil {
			return nil, err
		}
		lastRefreshedAt, err := timePtrFromTimestamp(variant.Grant.GetLastRefreshedAt())
		if err != nil {
			return nil, err
		}
		out.Grant = &ExternalCredentialGrant{
			AccessToken:       variant.Grant.GetAccessToken(),
			RefreshToken:      variant.Grant.GetRefreshToken(),
			Scope:             variant.Grant.GetScope(),
			ExpiresAt:         expiresAt,
			LastRefreshedAt:   lastRefreshedAt,
			RefreshErrorCount: variant.Grant.GetRefreshErrorCount(),
		}
	case *proto.ExternalCredential_Client:
		clientSecretExpiresAt, err := timePtrFromTimestamp(variant.Client.GetClientSecretExpiresAt())
		if err != nil {
			return nil, err
		}
		out.Client = &ExternalCredentialClientInfo{
			ClientID:              variant.Client.GetClientId(),
			ClientSecret:          variant.Client.GetClientSecret(),
			ClientSecretExpiresAt: clientSecretExpiresAt,
		}
	case *proto.ExternalCredential_Opaque:
		out.Opaque = &ExternalCredentialOpaque{Fields: cloneMap(variant.Opaque.GetFields())}
	}
	return out, nil
}

func externalCredentialToProto(value *ExternalCredential) *proto.ExternalCredential {
	return convertProtoMessage(value, func(value *ExternalCredential) *proto.ExternalCredential {
		out := &proto.ExternalCredential{
			Id:           value.GetId(),
			Subject:      value.GetSubject(),
			Audience:     value.GetAudience(),
			Qualifier:    value.GetQualifier(),
			AccountKey:   value.GetAccountKey(),
			MetadataJson: value.GetMetadataJson(),
			CreatedAt:    timestampFromOptionalTime(value.GetCreatedAt()),
			UpdatedAt:    timestampFromOptionalTime(value.GetUpdatedAt()),
		}
		switch {
		case value.Grant != nil:
			out.Credential = &proto.ExternalCredential_Grant{Grant: &proto.ExternalCredentialGrant{
				AccessToken:       value.Grant.GetAccessToken(),
				RefreshToken:      value.Grant.GetRefreshToken(),
				Scope:             value.Grant.GetScope(),
				ExpiresAt:         timestampFromOptionalTime(value.Grant.GetExpiresAt()),
				LastRefreshedAt:   timestampFromOptionalTime(value.Grant.GetLastRefreshedAt()),
				RefreshErrorCount: value.Grant.GetRefreshErrorCount(),
			}}
		case value.Client != nil:
			out.Credential = &proto.ExternalCredential_Client{Client: &proto.ExternalCredentialClientInfo{
				ClientId:              value.Client.GetClientId(),
				ClientSecret:          value.Client.GetClientSecret(),
				ClientSecretExpiresAt: timestampFromOptionalTime(value.Client.GetClientSecretExpiresAt()),
			}}
		case value.Opaque != nil:
			out.Credential = &proto.ExternalCredential_Opaque{Opaque: &proto.ExternalCredentialOpaque{
				Fields: cloneMap(value.Opaque.GetFields()),
			}}
		}
		return out
	})
}

func createExternalCredentialRequestFromProto(value *proto.CreateExternalCredentialRequest) (*CreateExternalCredentialRequest, error) {
	if value == nil {
		return nil, nil
	}
	credential, err := externalCredentialFromProto(value.GetCredential())
	if err != nil {
		return nil, err
	}
	return &CreateExternalCredentialRequest{Credential: credential}, nil
}

func upsertExternalCredentialRequestFromProto(value *proto.UpsertExternalCredentialRequest) (*UpsertExternalCredentialRequest, error) {
	if value == nil {
		return nil, nil
	}
	credential, err := externalCredentialFromProto(value.GetCredential())
	if err != nil {
		return nil, err
	}
	return &UpsertExternalCredentialRequest{Credential: credential, ExpectedCredentialID: value.GetExpectedCredentialId()}, nil
}

func getExternalCredentialRequestFromProto(value *proto.GetExternalCredentialRequest) *GetExternalCredentialRequest {
	return convertProtoMessage(value, func(value *proto.GetExternalCredentialRequest) *GetExternalCredentialRequest {
		return &GetExternalCredentialRequest{
			Subject:   value.GetSubject(),
			Audience:  value.GetAudience(),
			Qualifier: value.GetQualifier(),
		}
	})
}

func listExternalCredentialsRequestFromProto(value *proto.ListExternalCredentialsRequest) *ListExternalCredentialsRequest {
	return convertProtoMessage(value, func(value *proto.ListExternalCredentialsRequest) *ListExternalCredentialsRequest {
		return &ListExternalCredentialsRequest{
			Subject:  value.GetSubject(),
			Audience: value.GetAudience(),
		}
	})
}

func listExternalCredentialsResponseToProto(value *ListExternalCredentialsResponse) *proto.ListExternalCredentialsResponse {
	return convertProtoMessage(value, func(value *ListExternalCredentialsResponse) *proto.ListExternalCredentialsResponse {
		credentials := convertAllocatedProtoSlice(value.GetCredentials(), externalCredentialToProto)
		return &proto.ListExternalCredentialsResponse{Credentials: credentials}
	})
}

func deleteExternalCredentialRequestFromProto(value *proto.DeleteExternalCredentialRequest) *DeleteExternalCredentialRequest {
	return convertProtoMessage(value, func(value *proto.DeleteExternalCredentialRequest) *DeleteExternalCredentialRequest {
		return &DeleteExternalCredentialRequest{ID: value.GetId()}
	})
}

func externalCredentialAuthConfigFromProto(value *proto.ExternalCredentialAuthConfig) *ExternalCredentialAuthConfig {
	return convertProtoMessage(value, func(value *proto.ExternalCredentialAuthConfig) *ExternalCredentialAuthConfig {
		drivers := convertAllocatedProtoSlice(value.GetTokenExchangeDrivers(), externalCredentialTokenExchangeDriverFromProto)
		return &ExternalCredentialAuthConfig{
			Type:                 value.GetType(),
			Token:                value.GetToken(),
			TokenPrefix:          value.GetTokenPrefix(),
			GrantType:            value.GetGrantType(),
			TokenURL:             value.GetTokenUrl(),
			ClientID:             value.GetClientId(),
			ClientSecret:         value.GetClientSecret(),
			ClientAuth:           value.GetClientAuth(),
			TokenExchange:        value.GetTokenExchange(),
			Scopes:               copyStringSlice(value.GetScopes()),
			ScopeParam:           value.GetScopeParam(),
			ScopeSeparator:       value.GetScopeSeparator(),
			TokenParams:          cloneMap(value.GetTokenParams()),
			RefreshParams:        cloneMap(value.GetRefreshParams()),
			AcceptHeader:         value.GetAcceptHeader(),
			AccessTokenPath:      value.GetAccessTokenPath(),
			TokenExchangeDrivers: drivers,
			RefreshToken:         value.GetRefreshToken(),
		}
	})
}

func externalCredentialTokenExchangeDriverFromProto(value *proto.ExternalCredentialTokenExchangeDriver) *ExternalCredentialTokenExchangeDriver {
	return convertProtoMessage(value, func(value *proto.ExternalCredentialTokenExchangeDriver) *ExternalCredentialTokenExchangeDriver {
		return &ExternalCredentialTokenExchangeDriver{
			Type:            value.GetType(),
			TargetPrincipal: value.GetTargetPrincipal(),
			Scopes:          copyStringSlice(value.GetScopes()),
			LifetimeSeconds: value.GetLifetimeSeconds(),
			Endpoint:        value.GetEndpoint(),
			Params:          cloneMap(value.GetParams()),
		}
	})
}

func validateExternalCredentialConfigRequestFromProto(value *proto.ValidateExternalCredentialConfigRequest) *ValidateExternalCredentialConfigRequest {
	return convertProtoMessage(value, func(value *proto.ValidateExternalCredentialConfigRequest) *ValidateExternalCredentialConfigRequest {
		return &ValidateExternalCredentialConfigRequest{
			Provider:         value.GetProvider(),
			Connection:       value.GetConnection(),
			ConnectionID:     value.GetConnectionId(),
			Mode:             value.GetMode(),
			Auth:             externalCredentialAuthConfigFromProto(value.GetAuth()),
			ConnectionParams: cloneMap(value.GetConnectionParams()),
		}
	})
}

func resolveExternalCredentialRequestFromProto(value *proto.ResolveExternalCredentialRequest) *ResolveExternalCredentialRequest {
	return convertProtoMessage(value, func(value *proto.ResolveExternalCredentialRequest) *ResolveExternalCredentialRequest {
		return &ResolveExternalCredentialRequest{
			Provider:            value.GetProvider(),
			Connection:          value.GetConnection(),
			ConnectionID:        value.GetConnectionId(),
			Mode:                value.GetMode(),
			CredentialSubjectID: value.GetCredentialSubjectId(),
			ActorSubjectID:      value.GetActorSubjectId(),
			Instance:            value.GetInstance(),
			Auth:                externalCredentialAuthConfigFromProto(value.GetAuth()),
			ConnectionParams:    cloneMap(value.GetConnectionParams()),
		}
	})
}

func resolveExternalCredentialResponseToProto(value *ResolveExternalCredentialResponse) *proto.ResolveExternalCredentialResponse {
	return convertProtoMessage(value, func(value *ResolveExternalCredentialResponse) *proto.ResolveExternalCredentialResponse {
		return &proto.ResolveExternalCredentialResponse{
			Token:        value.GetToken(),
			ExpiresAt:    timestampFromOptionalTime(value.GetExpiresAt()),
			MetadataJson: value.GetMetadataJson(),
			Params:       cloneMap(value.GetParams()),
			Credential:   externalCredentialToProto(value.GetCredential()),
		}
	})
}

func externalCredentialTokenResponseToProto(value *ExternalCredentialTokenResponse) *proto.ExternalCredentialTokenResponse {
	return convertProtoMessage(value, func(value *ExternalCredentialTokenResponse) *proto.ExternalCredentialTokenResponse {
		return &proto.ExternalCredentialTokenResponse{
			AccessToken:   value.GetAccessToken(),
			RefreshToken:  value.GetRefreshToken(),
			ExpiresIn:     value.GetExpiresIn(),
			TokenType:     value.GetTokenType(),
			ExtraJson:     value.GetExtraJson(),
			RefreshSource: value.GetRefreshSource(),
		}
	})
}

func exchangeExternalCredentialRequestFromProto(value *proto.ExchangeExternalCredentialRequest) *ExchangeExternalCredentialRequest {
	return convertProtoMessage(value, func(value *proto.ExchangeExternalCredentialRequest) *ExchangeExternalCredentialRequest {
		return &ExchangeExternalCredentialRequest{
			Provider:            value.GetProvider(),
			Connection:          value.GetConnection(),
			ConnectionID:        value.GetConnectionId(),
			CredentialSubjectID: value.GetCredentialSubjectId(),
			ActorSubjectID:      value.GetActorSubjectId(),
			Instance:            value.GetInstance(),
			Auth:                externalCredentialAuthConfigFromProto(value.GetAuth()),
			CredentialJSON:      value.GetCredentialJson(),
			ConnectionParams:    cloneMap(value.GetConnectionParams()),
		}
	})
}

func exchangeExternalCredentialResponseToProto(value *ExchangeExternalCredentialResponse) *proto.ExchangeExternalCredentialResponse {
	return convertProtoMessage(value, func(value *ExchangeExternalCredentialResponse) *proto.ExchangeExternalCredentialResponse {
		return &proto.ExchangeExternalCredentialResponse{
			TokenResponse: externalCredentialTokenResponseToProto(value.GetTokenResponse()),
		}
	})
}

func copyStringSlice(value []string) []string {
	if len(value) == 0 {
		return nil
	}
	out := make([]string, len(value))
	copy(out, value)
	return out
}
