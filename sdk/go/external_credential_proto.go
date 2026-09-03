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
		out.Opaque = &ExternalCredentialOpaque{Fields: copyStringMap(variant.Opaque.GetFields())}
	}
	return out, nil
}

func externalCredentialToProto(value *ExternalCredential) *proto.ExternalCredential {
	if value == nil {
		return nil
	}
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
			Fields: copyStringMap(value.Opaque.GetFields()),
		}}
	}
	return out
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
	return &UpsertExternalCredentialRequest{Credential: credential}, nil
}

func getExternalCredentialRequestFromProto(value *proto.GetExternalCredentialRequest) *GetExternalCredentialRequest {
	if value == nil {
		return nil
	}
	return &GetExternalCredentialRequest{
		Subject:   value.GetSubject(),
		Audience:  value.GetAudience(),
		Qualifier: value.GetQualifier(),
	}
}

func listExternalCredentialsRequestFromProto(value *proto.ListExternalCredentialsRequest) *ListExternalCredentialsRequest {
	if value == nil {
		return nil
	}
	return &ListExternalCredentialsRequest{
		Subject:  value.GetSubject(),
		Audience: value.GetAudience(),
	}
}

func listExternalCredentialsResponseToProto(value *ListExternalCredentialsResponse) *proto.ListExternalCredentialsResponse {
	if value == nil {
		return nil
	}
	credentials := make([]*proto.ExternalCredential, 0, len(value.GetCredentials()))
	for _, credential := range value.GetCredentials() {
		credentials = append(credentials, externalCredentialToProto(credential))
	}
	return &proto.ListExternalCredentialsResponse{Credentials: credentials}
}

func deleteExternalCredentialRequestFromProto(value *proto.DeleteExternalCredentialRequest) *DeleteExternalCredentialRequest {
	if value == nil {
		return nil
	}
	return &DeleteExternalCredentialRequest{ID: value.GetId()}
}

func externalCredentialAuthConfigFromProto(value *proto.ExternalCredentialAuthConfig) *ExternalCredentialAuthConfig {
	if value == nil {
		return nil
	}
	drivers := make([]*ExternalCredentialTokenExchangeDriver, 0, len(value.GetTokenExchangeDrivers()))
	for _, driver := range value.GetTokenExchangeDrivers() {
		drivers = append(drivers, externalCredentialTokenExchangeDriverFromProto(driver))
	}
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
		TokenParams:          copyStringMap(value.GetTokenParams()),
		RefreshParams:        copyStringMap(value.GetRefreshParams()),
		AcceptHeader:         value.GetAcceptHeader(),
		AccessTokenPath:      value.GetAccessTokenPath(),
		TokenExchangeDrivers: drivers,
		RefreshToken:         value.GetRefreshToken(),
	}
}

func externalCredentialTokenExchangeDriverFromProto(value *proto.ExternalCredentialTokenExchangeDriver) *ExternalCredentialTokenExchangeDriver {
	if value == nil {
		return nil
	}
	return &ExternalCredentialTokenExchangeDriver{
		Type:            value.GetType(),
		TargetPrincipal: value.GetTargetPrincipal(),
		Scopes:          copyStringSlice(value.GetScopes()),
		LifetimeSeconds: value.GetLifetimeSeconds(),
		Endpoint:        value.GetEndpoint(),
		Params:          copyStringMap(value.GetParams()),
	}
}

func validateExternalCredentialConfigRequestFromProto(value *proto.ValidateExternalCredentialConfigRequest) *ValidateExternalCredentialConfigRequest {
	if value == nil {
		return nil
	}
	return &ValidateExternalCredentialConfigRequest{
		Provider:         value.GetProvider(),
		Connection:       value.GetConnection(),
		ConnectionID:     value.GetConnectionId(),
		Mode:             value.GetMode(),
		Auth:             externalCredentialAuthConfigFromProto(value.GetAuth()),
		ConnectionParams: copyStringMap(value.GetConnectionParams()),
	}
}

func resolveExternalCredentialRequestFromProto(value *proto.ResolveExternalCredentialRequest) *ResolveExternalCredentialRequest {
	if value == nil {
		return nil
	}
	return &ResolveExternalCredentialRequest{
		Provider:            value.GetProvider(),
		Connection:          value.GetConnection(),
		ConnectionID:        value.GetConnectionId(),
		Mode:                value.GetMode(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		ActorSubjectID:      value.GetActorSubjectId(),
		Instance:            value.GetInstance(),
		Auth:                externalCredentialAuthConfigFromProto(value.GetAuth()),
		ConnectionParams:    copyStringMap(value.GetConnectionParams()),
	}
}

func resolveExternalCredentialResponseToProto(value *ResolveExternalCredentialResponse) *proto.ResolveExternalCredentialResponse {
	if value == nil {
		return nil
	}
	return &proto.ResolveExternalCredentialResponse{
		Token:        value.GetToken(),
		ExpiresAt:    timestampFromOptionalTime(value.GetExpiresAt()),
		MetadataJson: value.GetMetadataJson(),
		Params:       copyStringMap(value.GetParams()),
		Credential:   externalCredentialToProto(value.GetCredential()),
	}
}

func externalCredentialTokenResponseToProto(value *ExternalCredentialTokenResponse) *proto.ExternalCredentialTokenResponse {
	if value == nil {
		return nil
	}
	return &proto.ExternalCredentialTokenResponse{
		AccessToken:   value.GetAccessToken(),
		RefreshToken:  value.GetRefreshToken(),
		ExpiresIn:     value.GetExpiresIn(),
		TokenType:     value.GetTokenType(),
		ExtraJson:     value.GetExtraJson(),
		RefreshSource: value.GetRefreshSource(),
	}
}

func exchangeExternalCredentialRequestFromProto(value *proto.ExchangeExternalCredentialRequest) *ExchangeExternalCredentialRequest {
	if value == nil {
		return nil
	}
	return &ExchangeExternalCredentialRequest{
		Provider:            value.GetProvider(),
		Connection:          value.GetConnection(),
		ConnectionID:        value.GetConnectionId(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		ActorSubjectID:      value.GetActorSubjectId(),
		Instance:            value.GetInstance(),
		Auth:                externalCredentialAuthConfigFromProto(value.GetAuth()),
		CredentialJSON:      value.GetCredentialJson(),
		ConnectionParams:    copyStringMap(value.GetConnectionParams()),
	}
}

func exchangeExternalCredentialResponseToProto(value *ExchangeExternalCredentialResponse) *proto.ExchangeExternalCredentialResponse {
	if value == nil {
		return nil
	}
	return &proto.ExchangeExternalCredentialResponse{
		TokenResponse: externalCredentialTokenResponseToProto(value.GetTokenResponse()),
	}
}

func copyStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, entry := range value {
		out[key] = entry
	}
	return out
}

func copyStringSlice(value []string) []string {
	if len(value) == 0 {
		return nil
	}
	out := make([]string, len(value))
	copy(out, value)
	return out
}
