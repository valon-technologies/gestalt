package managedparams

import (
	"fmt"
	"net/http"
	"strings"
)

const InHeader = "header"

type Parameter struct {
	In    string
	Name  string
	Value string
}

func Normalize(params []Parameter) []Parameter {
	if len(params) == 0 {
		return nil
	}

	out := make([]Parameter, len(params))
	for i, param := range params {
		param.In = strings.ToLower(strings.TrimSpace(param.In))
		param.Name = canonicalName(param.In, strings.TrimSpace(param.Name))
		out[i] = param
	}
	return out
}

func Merge(base, override []Parameter) []Parameter {
	out := Normalize(base)
	if len(override) == 0 {
		return out
	}
	if len(out) == 0 {
		return Normalize(override)
	}

	indexByKey := make(map[string]int, len(out))
	for i, param := range out {
		indexByKey[key(param.In, param.Name)] = i
	}

	for _, param := range Normalize(override) {
		k := key(param.In, param.Name)
		if idx, ok := indexByKey[k]; ok {
			out[idx] = param
			continue
		}
		indexByKey[k] = len(out)
		out = append(out, param)
	}

	return out
}

func Validate(params []Parameter) error {
	seen := make(map[string]struct{}, len(params))
	for i, param := range params {
		in := strings.ToLower(strings.TrimSpace(param.In))
		if in == "" {
			return fmt.Errorf("managed_parameters[%d].in is required", i)
		}
		if in != InHeader {
			return fmt.Errorf("managed_parameters[%d].in %q must be %q", i, param.In, InHeader)
		}

		name := canonicalName(in, strings.TrimSpace(param.Name))
		if name == "" {
			return fmt.Errorf("managed_parameters[%d].name is required", i)
		}
		if param.Value == "" {
			return fmt.Errorf("managed_parameters[%d].value is required", i)
		}

		k := key(in, name)
		if _, exists := seen[k]; exists {
			return fmt.Errorf("duplicate managed parameter %q in %q", name, in)
		}
		seen[k] = struct{}{}
	}
	return nil
}

func ValidateHeaderConflicts(headers map[string]string, params []Parameter) error {
	if len(headers) == 0 || len(params) == 0 {
		return nil
	}
	normalizedHeaders := make(map[string]struct{}, len(headers))
	for name := range headers {
		normalizedHeaders[http.CanonicalHeaderKey(name)] = struct{}{}
	}
	for _, param := range Normalize(params) {
		if param.In != InHeader {
			continue
		}
		if _, exists := normalizedHeaders[http.CanonicalHeaderKey(param.Name)]; exists {
			return fmt.Errorf("managed parameter %q conflicts with configured header", param.Name)
		}
	}
	return nil
}

func canonicalName(in, name string) string {
	switch in {
	case InHeader:
		return http.CanonicalHeaderKey(name)
	default:
		return name
	}
}

func key(in, name string) string {
	return in + "\x00" + canonicalName(in, name)
}
