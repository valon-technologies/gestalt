package declarative

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type AuthMappingDef = providermanifestv1.AuthMapping
type AuthValueDef = providermanifestv1.AuthValue

// MappedCredentialParser maps a structured credential JSON token into request
// auth material according to an authMapping definition.
func MappedCredentialParser(mapping *AuthMappingDef) func(string) (string, map[string]string, error) {
	return func(token string) (string, map[string]string, error) {
		var (
			tokenData map[string]any
			headers   map[string]string
		)
		if len(mapping.Headers) > 0 {
			headers = make(map[string]string, len(mapping.Headers))
			for headerName, value := range mapping.Headers {
				resolved, err := resolveAuthValue(value, token, &tokenData)
				if err != nil {
					return "", nil, fmt.Errorf("authMapping.headers[%q]: %w", headerName, err)
				}
				headers[headerName] = resolved
			}
		}
		authToken := ""
		if mapping.Basic != nil {
			username, err := resolveAuthValue(mapping.Basic.Username, token, &tokenData)
			if err != nil {
				return "", nil, fmt.Errorf("authMapping.basic.username: %w", err)
			}
			password, err := resolveAuthValue(mapping.Basic.Password, token, &tokenData)
			if err != nil {
				return "", nil, fmt.Errorf("authMapping.basic.password: %w", err)
			}
			credential := fmt.Sprintf("%s:%s", username, password)
			authToken = "Basic " + base64.StdEncoding.EncodeToString([]byte(credential))
		}
		if len(headers) == 0 {
			headers = nil
		}
		return authToken, headers, nil
	}
}

func resolveAuthValue(value AuthValueDef, token string, tokenData *map[string]any) (string, error) {
	hasValue := value.Value != ""
	hasValueFrom := value.ValueFrom != nil
	if hasValue == hasValueFrom {
		return "", fmt.Errorf("must set exactly one of value or valueFrom.credentialFieldRef")
	}
	if hasValue {
		return value.Value, nil
	}
	if value.ValueFrom == nil || value.ValueFrom.CredentialFieldRef == nil {
		return "", fmt.Errorf("must set exactly one of value or valueFrom.credentialFieldRef")
	}
	if *tokenData == nil {
		parsed, err := parseMappedToken(token)
		if err != nil {
			return "", err
		}
		*tokenData = parsed
	}
	fieldName := value.ValueFrom.CredentialFieldRef.Name
	val, ok := (*tokenData)[fieldName]
	if !ok || val == nil {
		return "", fmt.Errorf("token field %q is missing or null", fieldName)
	}
	return fmt.Sprintf("%v", val), nil
}

func parseMappedToken(token string) (map[string]any, error) {
	var tokenData map[string]any
	if err := json.Unmarshal([]byte(token), &tokenData); err != nil {
		return nil, fmt.Errorf("parsing token as JSON for authMapping: %w", err)
	}
	return tokenData, nil
}
