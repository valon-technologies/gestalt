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
		if strings.TrimSpace(scheme.TimestampHeader) == "" {
			return fmt.Errorf("%s.timestampHeader is required for replay protection", path)
		}
		if scheme.MaxAgeSeconds <= 0 {
			return fmt.Errorf("%s.maxAgeSeconds is required for replay protection", path)
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

func ValidateHTTPBindingHMACCoverage(path string, binding *providermanifestv1.HTTPBinding, scheme *providermanifestv1.HTTPSecurityScheme) error {
	if binding == nil || scheme == nil || scheme.Type != providermanifestv1.HTTPSecuritySchemeTypeHMAC {
		return nil
	}
	if httpBindingContentTypeAffectsParsing(binding.RequestBody) && !PayloadTemplateReferencesHeader(scheme.PayloadTemplate, "Content-Type") {
		return fmt.Errorf("%s.security %q uses hmac with content-type-dependent body parsing; payloadTemplate must include {header:Content-Type}", path, binding.Security)
	}
	return nil
}

func httpBindingContentTypeAffectsParsing(requestBody *providermanifestv1.HTTPRequestBody) bool {
	if requestBody == nil || len(requestBody.Content) == 0 {
		return true
	}
	if len(requestBody.Content) != 1 {
		return true
	}
	_, ok := requestBody.Content["*/*"]
	return ok
}
