package declarative

import (
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

// ConnectionDef is the service-owned provider construction input for a
// resolved connection.
type ConnectionDef struct {
	Mode             providermanifestv1.ConnectionMode
	Auth             ConnectionAuthDef
	ConnectionParams map[string]ConnectionParamDef
	PostConnect      map[string]*core.PostConnectConfig
}

// ConnectionAuthDef is the auth material needed to construct a declarative
// provider or OAuth upstream.
type ConnectionAuthDef struct {
	Type                providermanifestv1.AuthType
	Token               string
	GrantType           string
	RefreshToken        string
	AuthorizationURL    string
	TokenURL            string
	ClientID            string
	ClientSecret        string
	RedirectURL         string
	ClientAuth          string
	TokenExchange       string
	TokenPrefix         string
	Scopes              []string
	ScopeParam          string
	ScopeSeparator      string
	PKCE                bool
	AuthorizationParams map[string]string
	TokenParams         map[string]string
	RefreshParams       map[string]string
	AcceptHeader        string
	AccessTokenPath     string
	TokenMetadata       []string
	Credentials         []CredentialFieldDef
	AuthMapping         *AuthMappingDef
}

func cloneAuthMapping(src *AuthMappingDef) *AuthMappingDef {
	if src == nil {
		return nil
	}
	dst := &AuthMappingDef{}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string]AuthValueDef, len(src.Headers))
		for name, value := range src.Headers {
			dst.Headers[name] = cloneAuthValue(value)
		}
	}
	if src.Basic != nil {
		dst.Basic = &BasicAuthMappingDef{
			Username: cloneAuthValue(src.Basic.Username),
			Password: cloneAuthValue(src.Basic.Password),
		}
	}
	return dst
}

func cloneAuthValue(src AuthValueDef) AuthValueDef {
	dst := src
	if src.ValueFrom != nil {
		dst.ValueFrom = &AuthValueFromDef{}
		if src.ValueFrom.CredentialFieldRef != nil {
			dst.ValueFrom.CredentialFieldRef = &CredentialFieldRefDef{
				Name: src.ValueFrom.CredentialFieldRef.Name,
			}
		}
	}
	return dst
}

func cloneAuthDef(src AuthDef) AuthDef {
	dst := src
	dst.Scopes = slices.Clone(src.Scopes)
	dst.AuthorizationParams = maps.Clone(src.AuthorizationParams)
	dst.TokenParams = maps.Clone(src.TokenParams)
	dst.RefreshParams = maps.Clone(src.RefreshParams)
	dst.TokenMetadata = slices.Clone(src.TokenMetadata)
	dst.ResponseCheck = cloneResponseCheckDef(src.ResponseCheck)
	return dst
}

func cloneResponseCheckDef(src *ResponseCheckDef) *ResponseCheckDef {
	if src == nil {
		return nil
	}
	return &ResponseCheckDef{
		SuccessBodyMatch: maps.Clone(src.SuccessBodyMatch),
		ErrorMessagePath: src.ErrorMessagePath,
	}
}
