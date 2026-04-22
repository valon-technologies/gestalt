package httpbinding

import (
	"fmt"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func ValidateHTTPSecurityScheme(path string, scheme *providermanifestv1.HTTPSecurityScheme) error {
	if scheme == nil {
		return fmt.Errorf("%s is required", path)
	}
	switch scheme.Type {
	case providermanifestv1.HTTPSecuritySchemeTypeHMAC:
		if strings.TrimSpace(scheme.SignatureHeader) == "" {
			return fmt.Errorf("%s.signatureHeader is required", path)
		}
		if strings.TrimSpace(scheme.PayloadTemplate) == "" {
			return fmt.Errorf("%s.payloadTemplate is required", path)
		}
		if scheme.Secret == nil {
			return fmt.Errorf("%s.secret is required", path)
		}
		if strings.TrimSpace(scheme.TimestampHeader) == "" && scheme.MaxAgeSeconds != 0 {
			return fmt.Errorf("%s.maxAgeSeconds requires %s.timestampHeader to be set", path, path)
		}
		if strings.TrimSpace(scheme.TimestampHeader) != "" && scheme.MaxAgeSeconds <= 0 {
			return fmt.Errorf("%s.timestampHeader requires %s.maxAgeSeconds to be greater than zero", path, path)
		}
		if err := ValidatePayloadTemplate(scheme.PayloadTemplate, scheme.TimestampHeader); err != nil {
			return fmt.Errorf("%s.payloadTemplate %s", path, err)
		}
	case providermanifestv1.HTTPSecuritySchemeTypeAPIKey:
		if strings.TrimSpace(scheme.Name) == "" {
			return fmt.Errorf("%s.name is required", path)
		}
		switch scheme.In {
		case providermanifestv1.HTTPInHeader, providermanifestv1.HTTPInQuery:
		default:
			return fmt.Errorf("%s.in %q is not supported", path, scheme.In)
		}
		if scheme.Secret == nil {
			return fmt.Errorf("%s.secret is required", path)
		}
	case providermanifestv1.HTTPSecuritySchemeTypeHTTP:
		switch scheme.Scheme {
		case providermanifestv1.HTTPAuthSchemeBasic, providermanifestv1.HTTPAuthSchemeBearer:
		default:
			return fmt.Errorf("%s.scheme %q is not supported", path, scheme.Scheme)
		}
		if scheme.Secret == nil {
			return fmt.Errorf("%s.secret is required", path)
		}
	case providermanifestv1.HTTPSecuritySchemeTypeNone:
	default:
		return fmt.Errorf("%s.type %q is not supported", path, scheme.Type)
	}
	if scheme.Secret != nil && strings.TrimSpace(scheme.Secret.Env) == "" && strings.TrimSpace(scheme.Secret.Secret) == "" {
		return fmt.Errorf("%s.secret must set env or secret", path)
	}
	return nil
}
