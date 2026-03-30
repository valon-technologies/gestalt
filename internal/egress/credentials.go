package egress

import (
	"encoding/base64"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
)

// AuthStyle determines how a stored credential should be materialized.
type AuthStyle string

const (
	AuthStyleBearer AuthStyle = "bearer"
	AuthStyleRaw    AuthStyle = "raw"
	AuthStyleNone   AuthStyle = "none"
	AuthStyleBasic  AuthStyle = "basic"
)

// TokenParser transforms a stored token into an auth header plus extra headers.
type TokenParser func(token string) (authHeader string, extraHeaders map[string]string, err error)

// CredentialMaterialization is a transport-neutral representation of how a
// credential should be injected into an outbound request.
type CredentialMaterialization struct {
	Authorization string
	Headers       []HeaderMutation
}

// MaterializeCredential converts a stored token into a concrete injection shape.
func MaterializeCredential(token string, style AuthStyle, parser TokenParser) (CredentialMaterialization, error) {
	if parser != nil {
		authHeader, extraHeaders, err := parser(token)
		if err != nil {
			return CredentialMaterialization{}, err
		}
		return CredentialMaterialization{
			Authorization: authHeader,
			Headers:       headerMutationsFromMap(extraHeaders),
		}, nil
	}

	switch style {
	case "", AuthStyleBearer:
		if token == "" {
			return CredentialMaterialization{}, nil
		}
		return CredentialMaterialization{Authorization: core.BearerScheme + token}, nil
	case AuthStyleRaw:
		return CredentialMaterialization{Authorization: token}, nil
	case AuthStyleBasic:
		if token == "" {
			return CredentialMaterialization{}, nil
		}
		return CredentialMaterialization{
			Authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte(token)),
		}, nil
	case AuthStyleNone:
		return CredentialMaterialization{}, nil
	default:
		return CredentialMaterialization{}, fmt.Errorf("unknown auth style %q", style)
	}
}

func headerMutationsFromMap(headers map[string]string) []HeaderMutation {
	if len(headers) == 0 {
		return nil
	}
	out := make([]HeaderMutation, 0, len(headers))
	for name, value := range headers {
		out = append(out, HeaderMutation{
			Action: HeaderActionSet,
			Name:   name,
			Value:  value,
		})
	}
	return out
}
