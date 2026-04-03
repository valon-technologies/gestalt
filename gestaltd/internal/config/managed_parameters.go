package config

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	ManagedParameterInHeader = "header"
	ManagedParameterInPath   = "path"
)

func NormalizeManagedParameter(param ManagedParameterDef) ManagedParameterDef {
	param.In = strings.ToLower(strings.TrimSpace(param.In))
	param.Name = canonicalManagedParameterName(param.In, strings.TrimSpace(param.Name))
	return param
}

func NormalizeManagedParameters(params []ManagedParameterDef) []ManagedParameterDef {
	if len(params) == 0 {
		return nil
	}

	out := make([]ManagedParameterDef, len(params))
	for i, param := range params {
		out[i] = NormalizeManagedParameter(param)
	}
	return out
}

func MergeManagedParameters(base, override []ManagedParameterDef) []ManagedParameterDef {
	out := NormalizeManagedParameters(base)
	if len(override) == 0 {
		return out
	}
	if len(out) == 0 {
		return NormalizeManagedParameters(override)
	}

	indexByKey := make(map[string]int, len(out))
	for i, param := range out {
		indexByKey[managedParameterKey(param.In, param.Name)] = i
	}

	for _, param := range NormalizeManagedParameters(override) {
		key := managedParameterKey(param.In, param.Name)
		if idx, ok := indexByKey[key]; ok {
			out[idx] = param
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, param)
	}

	return out
}

func ValidateManagedParameters(params []ManagedParameterDef) error {
	seen := make(map[string]struct{}, len(params))
	for i, param := range params {
		param = NormalizeManagedParameter(param)
		if param.In == "" {
			return fmt.Errorf("managed_parameters[%d].in is required", i)
		}
		if param.In != ManagedParameterInHeader && param.In != ManagedParameterInPath {
			return fmt.Errorf("managed_parameters[%d].in %q must be %q or %q", i, param.In, ManagedParameterInHeader, ManagedParameterInPath)
		}
		if param.Name == "" {
			return fmt.Errorf("managed_parameters[%d].name is required", i)
		}
		if param.Value == "" {
			return fmt.Errorf("managed_parameters[%d].value is required", i)
		}

		key := managedParameterKey(param.In, param.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate managed parameter %q in %q", param.Name, param.In)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func ValidateManagedParameterHeaderConflicts(headers map[string]string, params []ManagedParameterDef) error {
	if len(headers) == 0 || len(params) == 0 {
		return nil
	}

	normalizedHeaders := make(map[string]struct{}, len(headers))
	for name := range headers {
		normalizedHeaders[http.CanonicalHeaderKey(name)] = struct{}{}
	}
	for _, param := range NormalizeManagedParameters(params) {
		if param.In != ManagedParameterInHeader {
			continue
		}
		if _, exists := normalizedHeaders[http.CanonicalHeaderKey(param.Name)]; exists {
			return fmt.Errorf("managed parameter %q conflicts with configured header", param.Name)
		}
	}
	return nil
}

func canonicalManagedParameterName(location, name string) string {
	switch location {
	case ManagedParameterInHeader:
		return http.CanonicalHeaderKey(name)
	default:
		return name
	}
}

func managedParameterKey(location, name string) string {
	return location + "\x00" + canonicalManagedParameterName(location, name)
}
