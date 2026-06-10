package gestalt

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

func externalCredentialFromProto(value *proto.ExternalCredential) (*ExternalCredential, error) {
	if value == nil {
		return nil, nil
	}
	expiresAt, err := timePtrFromTimestamp(value.GetExpiresAt())
	if err != nil {
		return nil, err
	}
	lastRefreshedAt, err := timePtrFromTimestamp(value.GetLastRefreshedAt())
	if err != nil {
		return nil, err
	}
	createdAt, err := timePtrFromTimestamp(value.GetCreatedAt())
	if err != nil {
		return nil, err
	}
	updatedAt, err := timePtrFromTimestamp(value.GetUpdatedAt())
	if err != nil {
		return nil, err
	}
	return &ExternalCredential{
		ID:                value.GetId(),
		SubjectID:         value.GetSubjectId(),
		Instance:          value.GetInstance(),
		AccessToken:       value.GetAccessToken(),
		RefreshToken:      value.GetRefreshToken(),
		Scopes:            value.GetScopes(),
		ExpiresAt:         expiresAt,
		LastRefreshedAt:   lastRefreshedAt,
		RefreshErrorCount: value.GetRefreshErrorCount(),
		MetadataJSON:      value.GetMetadataJson(),
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ConnectionID:      value.GetConnectionId(),
	}, nil
}

func externalCredentialToProto(value *ExternalCredential) *proto.ExternalCredential {
	if value == nil {
		return nil
	}
	return &proto.ExternalCredential{
		Id:                value.GetId(),
		SubjectId:         value.GetSubjectId(),
		Instance:          value.GetInstance(),
		AccessToken:       value.GetAccessToken(),
		RefreshToken:      value.GetRefreshToken(),
		Scopes:            value.GetScopes(),
		ExpiresAt:         timestampFromOptionalTime(value.GetExpiresAt()),
		LastRefreshedAt:   timestampFromOptionalTime(value.GetLastRefreshedAt()),
		RefreshErrorCount: value.GetRefreshErrorCount(),
		MetadataJson:      value.GetMetadataJson(),
		CreatedAt:         timestampFromOptionalTime(value.GetCreatedAt()),
		UpdatedAt:         timestampFromOptionalTime(value.GetUpdatedAt()),
		ConnectionId:      value.GetConnectionId(),
	}
}

func externalCredentialLookupFromProto(value *proto.ExternalCredentialLookup) *ExternalCredentialLookup {
	if value == nil {
		return nil
	}
	return &ExternalCredentialLookup{
		SubjectID:    value.GetSubjectId(),
		Instance:     value.GetInstance(),
		ConnectionID: value.GetConnectionId(),
	}
}

func upsertExternalCredentialRequestFromProto(value *proto.UpsertExternalCredentialRequest) (*UpsertExternalCredentialRequest, error) {
	if value == nil {
		return nil, nil
	}
	credential, err := externalCredentialFromProto(value.GetCredential())
	if err != nil {
		return nil, err
	}
	return &UpsertExternalCredentialRequest{
		Credential:         credential,
		PreserveTimestamps: value.GetPreserveTimestamps(),
	}, nil
}

func getExternalCredentialRequestFromProto(value *proto.GetExternalCredentialRequest) *GetExternalCredentialRequest {
	if value == nil {
		return nil
	}
	return &GetExternalCredentialRequest{Lookup: externalCredentialLookupFromProto(value.GetLookup())}
}

func listExternalCredentialsRequestFromProto(value *proto.ListExternalCredentialsRequest) *ListExternalCredentialsRequest {
	if value == nil {
		return nil
	}
	return &ListExternalCredentialsRequest{
		SubjectID:    value.GetSubjectId(),
		Instance:     value.GetInstance(),
		ConnectionID: value.GetConnectionId(),
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
