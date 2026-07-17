package publicclient

import (
	"fmt"
	"net/url"
	"strings"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
)

func buildRestPath(http generated.Method, fields map[string]any) (string, error) {
	path := http.HTTPPath
	for _, field := range http.HTTPPathFields {
		value, ok := fieldValue(fields, field.Name, field.JSONName)
		if !ok || value == nil {
			return "", &generated.GestaltError{
				Code:    gestaltclient.GestaltErrorCodeInvalidArgument,
				Message: fmt.Sprintf("missing path parameter %s", field.Name),
			}
		}
		if _, isObject := value.(map[string]any); isObject {
			return "", &generated.GestaltError{
				Code:    gestaltclient.GestaltErrorCodeInvalidArgument,
				Message: fmt.Sprintf("path parameter %s must be scalar", field.Name),
			}
		}
		placeholder := "{" + field.Name + "}"
		path = strings.ReplaceAll(path, placeholder, url.PathEscape(fmt.Sprint(value)))
	}
	if strings.Contains(path, "{") {
		return "", &generated.GestaltError{
			Code:    gestaltclient.GestaltErrorCodeInvalidArgument,
			Message: fmt.Sprintf("missing path parameter in %q", path),
		}
	}
	return path, nil
}

func buildRestBody(http generated.Method, fields map[string]any) map[string]any {
	switch strings.ToUpper(http.HTTPVerb) {
	case "GET", "DELETE":
		return nil
	}
	excluded := bodyExcludedFieldKeys(http)
	body := make(map[string]any, len(fields))
	for key, value := range fields {
		if value == nil || excluded[key] {
			continue
		}
		body[key] = value
	}
	return body
}

func buildRestQuery(http generated.Method, fields map[string]any) url.Values {
	values := url.Values{}
	for _, field := range http.HTTPQueryFields {
		value, ok := fieldValue(fields, field.Name, field.JSONName)
		if !ok || value == nil {
			continue
		}
		encodeQueryValue(values, field.JSONName, value)
	}
	return values
}

func bodyExcludedFieldKeys(method generated.Method) map[string]bool {
	keys := make(map[string]bool)
	add := func(name string) {
		if name == "" {
			return
		}
		keys[name] = true
		if camel := snakeToCamel(name); camel != name {
			keys[camel] = true
		}
	}
	for _, field := range append(method.HTTPPathFields, method.HTTPQueryFields...) {
		add(field.Name)
		if field.JSONName != "" {
			keys[field.JSONName] = true
		}
	}
	for _, name := range append(method.Fill, method.Reject...) {
		add(name)
	}
	return keys
}

func snakeToCamel(name string) string {
	parts := strings.Split(name, "_")
	if len(parts) <= 1 {
		return name
	}
	out := parts[0]
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		out += strings.ToUpper(part[:1]) + part[1:]
	}
	return out
}

func fieldValue(fields map[string]any, name, jsonName string) (any, bool) {
	if jsonName != "" {
		if value, ok := fields[jsonName]; ok {
			return value, true
		}
	}
	if value, ok := fields[name]; ok {
		return value, true
	}
	return nil, false
}

func encodeQueryValue(values url.Values, key string, value any) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			encodeQueryValue(values, key, item)
		}
	case []string:
		for _, item := range typed {
			values.Add(key, item)
		}
	case map[string]any:
		for nestedKey, nestedValue := range typed {
			encodeQueryValue(values, key+"."+nestedKey, nestedValue)
		}
	default:
		values.Add(key, fmt.Sprint(value))
	}
}

func joinURL(baseURL, path string) (string, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("publicclient: base URL is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base.Scheme + "://" + base.Host + strings.TrimSuffix(base.Path, "/") + path, nil
}
