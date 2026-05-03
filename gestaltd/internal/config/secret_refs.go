package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	secretRefTransportPrefix = "__GESTALT_SECRET_REF__:"
)

type SecretRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type ConfigStringTransformer func(string) (string, error)

func EncodeSecretRefTransport(ref SecretRef) string {
	payload, err := json.Marshal(ref)
	if err != nil {
		panic(fmt.Sprintf("marshal secret ref transport: %v", err))
	}
	return secretRefTransportPrefix + base64.RawURLEncoding.EncodeToString(payload)
}

func ParseSecretRefTransport(value string) (SecretRef, bool, error) {
	if !strings.HasPrefix(value, secretRefTransportPrefix) {
		return SecretRef{}, false, nil
	}
	payload := strings.TrimPrefix(value, secretRefTransportPrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return SecretRef{}, true, fmt.Errorf("decoding internal secret ref: %w", err)
	}
	var ref SecretRef
	if err := json.Unmarshal(decoded, &ref); err != nil {
		return SecretRef{}, true, fmt.Errorf("parsing internal secret ref: %w", err)
	}
	if strings.TrimSpace(ref.Provider) == "" || strings.TrimSpace(ref.Name) == "" {
		return SecretRef{}, true, fmt.Errorf("internal secret ref is incomplete")
	}
	return ref, true, nil
}

func NormalizeConfigSecretRefs(root *yaml.Node) error {
	return normalizeSecretRefsNode(documentValueNode(root), nil, true)
}

func NormalizeOpaqueSecretRefs(node *yaml.Node, allowSecretRefs bool) error {
	return normalizeSecretRefsNode(documentValueNode(node), nil, allowSecretRefs)
}

func normalizeSecretRefsNode(node *yaml.Node, path []string, allowSecretRefs bool) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			if err := normalizeSecretRefsNode(child, path, allowSecretRefs); err != nil {
				return err
			}
		}
		return nil
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			value := strings.TrimSpace(node.Value)
			if strings.HasPrefix(value, secretRefTransportPrefix) {
				return fmt.Errorf("parsing config YAML: %s uses reserved internal secret ref syntax", formatSecretRefPath(path))
			}
		}
		return nil
	case yaml.SequenceNode:
		for i, child := range node.Content {
			childPath := appendPath(path, fmt.Sprintf("[%d]", i))
			if err := normalizeSecretRefsNode(child, childPath, allowSecretRefs); err != nil {
				return err
			}
		}
		return nil
	case yaml.MappingNode:
		if candidate, ok := decodeStructuredSecretRef(node); ok {
			if !allowSecretRefs {
				return fmt.Errorf("parsing config YAML: %s does not allow secret refs", formatSecretRefPath(path))
			}
			ref, err := validateStructuredSecretRef(candidate, path)
			if err != nil {
				return err
			}
			*node = yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: EncodeSecretRefTransport(ref),
			}
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			if keyNode == nil {
				continue
			}
			key := keyNode.Value
			childPath := appendPath(path, key)
			childAllowRefs := allowSecretRefs
			if isSecretsProviderConfigPath(childPath) {
				childAllowRefs = false
			}
			if err := normalizeSecretRefsNode(valueNode, childPath, childAllowRefs); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func decodeStructuredSecretRef(node *yaml.Node) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return nil, false
	}
	key := node.Content[0]
	if key == nil || key.Value != "secret" {
		return nil, false
	}
	return node.Content[1], true
}

func validateStructuredSecretRef(secretNode *yaml.Node, path []string) (SecretRef, error) {
	if secretNode == nil || secretNode.Kind != yaml.MappingNode {
		return SecretRef{}, fmt.Errorf("parsing config YAML: %s.secret must be a mapping with provider and name", formatSecretRefPath(path))
	}
	raw := map[string]string{}
	for i := 0; i+1 < len(secretNode.Content); i += 2 {
		keyNode := secretNode.Content[i]
		valueNode := secretNode.Content[i+1]
		if keyNode == nil || valueNode == nil {
			continue
		}
		key := keyNode.Value
		switch key {
		case "provider", "name":
		default:
			return SecretRef{}, fmt.Errorf("parsing config YAML: %s.secret.%s is not supported", formatSecretRefPath(path), key)
		}
		if valueNode.Kind != yaml.ScalarNode || (valueNode.Tag != "" && valueNode.Tag != "!!str") {
			return SecretRef{}, fmt.Errorf("parsing config YAML: %s.secret.%s must be a string", formatSecretRefPath(path), key)
		}
		raw[key] = strings.TrimSpace(valueNode.Value)
	}
	if raw["provider"] == "" {
		return SecretRef{}, fmt.Errorf("parsing config YAML: %s.secret.provider is required", formatSecretRefPath(path))
	}
	if raw["name"] == "" {
		return SecretRef{}, fmt.Errorf("parsing config YAML: %s.secret.name is required", formatSecretRefPath(path))
	}
	return SecretRef{Provider: raw["provider"], Name: raw["name"]}, nil
}

func formatSecretRefPath(path []string) string {
	if len(path) == 0 {
		return "config"
	}
	var b strings.Builder
	for i, segment := range path {
		if strings.HasPrefix(segment, "[") {
			b.WriteString(segment)
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(segment)
	}
	return b.String()
}

func appendPath(path []string, segment string) []string {
	return append(slices.Clone(path), segment)
}

func isSecretsProviderConfigPath(path []string) bool {
	return len(path) >= 4 && path[0] == "providers" && path[1] == "secrets" && path[3] == "config"
}

func ReferencedConfigSecretProviders(cfg *Config) (map[string]struct{}, error) {
	providers := map[string]struct{}{}
	if cfg == nil {
		return providers, nil
	}
	if err := TransformConfigStringFields(cfg, func(value string) (string, error) {
		ref, ok, err := ParseSecretRefTransport(value)
		if err != nil {
			return "", err
		}
		if ok {
			providers[ref.Provider] = struct{}{}
		}
		return value, nil
	}); err != nil {
		return nil, err
	}
	return providers, nil
}

func TransformConfigStringFields(cfg *Config, transform ConfigStringTransformer) error {
	if cfg == nil {
		return nil
	}
	if err := transformConfigStringFieldsInStruct(&cfg.Server, transform); err != nil {
		return err
	}
	if err := transformConfigStringFieldsInStruct(&cfg.Authorization, transform); err != nil {
		return err
	}
	for _, conn := range cfg.Connections {
		if conn == nil {
			continue
		}
		if err := transformConfigStringFieldsInStruct(conn, transform); err != nil {
			return err
		}
	}
	for _, entries := range []map[string]*ProviderEntry{
		cfg.Providers.Authentication,
		cfg.Providers.Authorization,
		cfg.Providers.Telemetry,
		cfg.Providers.Audit,
		cfg.Providers.IndexedDB,
		cfg.Providers.Cache,
		cfg.Providers.S3,
		cfg.Providers.Workflow,
		cfg.Providers.Agent,
	} {
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			if err := transformConfigStringFieldsInStruct(entry, transform); err != nil {
				return err
			}
		}
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry == nil {
			continue
		}
		if err := transformConfigStringFieldsInStruct(entry, transform); err != nil {
			return err
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		if err := transformConfigStringFieldsInStruct(entry, transform); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.Secrets {
		if entry == nil {
			continue
		}
		savedConfig := entry.Config
		entry.Config = yaml.Node{}
		err := transformConfigStringFieldsInStruct(entry, transform)
		entry.Config = savedConfig
		cfg.Providers.Secrets[name] = entry
		if err != nil {
			return err
		}
	}
	for _, entry := range cfg.Plugins {
		if entry == nil {
			continue
		}
		if err := transformConfigStringFieldsInStruct(entry, transform); err != nil {
			return err
		}
		for _, conn := range entry.Connections {
			if conn != nil {
				if err := transformConfigStringFieldsInStruct(conn, transform); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var yamlNodeType = reflect.TypeOf(yaml.Node{})

func transformConfigStringFieldsInStruct(ptr any, transform ConfigStringTransformer) error {
	v := reflect.ValueOf(ptr)
	if !v.IsValid() || v.IsNil() {
		return nil
	}
	v = v.Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		switch field.Kind() {
		case reflect.String:
			if !field.CanSet() {
				continue
			}
			next, err := transform(field.String())
			if err != nil {
				return err
			}
			field.SetString(next)
		case reflect.Struct:
			if field.Type() == yamlNodeType {
				nodePtr := field.Addr().Interface().(*yaml.Node)
				if err := transformConfigStringFieldsInYAMLNode(nodePtr, transform); err != nil {
					return err
				}
			} else if field.CanAddr() {
				if err := transformConfigStringFieldsInStruct(field.Addr().Interface(), transform); err != nil {
					return err
				}
			}
		case reflect.Pointer:
			if !field.IsNil() && field.Elem().Kind() == reflect.Struct {
				if err := transformConfigStringFieldsInStruct(field.Interface(), transform); err != nil {
					return err
				}
			}
		case reflect.Map:
			if field.Type().Key().Kind() != reflect.String {
				continue
			}
			switch field.Type().Elem().Kind() {
			case reflect.String:
				for _, key := range field.MapKeys() {
					next, err := transform(field.MapIndex(key).String())
					if err != nil {
						return err
					}
					field.SetMapIndex(key, reflect.ValueOf(next))
				}
			case reflect.Struct:
				for _, key := range field.MapKeys() {
					current := field.MapIndex(key)
					next := reflect.New(field.Type().Elem())
					next.Elem().Set(current)
					if err := transformConfigStringFieldsInStruct(next.Interface(), transform); err != nil {
						return err
					}
					field.SetMapIndex(key, next.Elem())
				}
			case reflect.Pointer:
				if field.Type().Elem().Elem().Kind() != reflect.Struct {
					continue
				}
				for _, key := range field.MapKeys() {
					current := field.MapIndex(key)
					if current.IsNil() {
						continue
					}
					if err := transformConfigStringFieldsInStruct(current.Interface(), transform); err != nil {
						return err
					}
				}
			}
		case reflect.Slice:
			switch field.Type().Elem().Kind() {
			case reflect.String:
				for j := 0; j < field.Len(); j++ {
					next, err := transform(field.Index(j).String())
					if err != nil {
						return err
					}
					field.Index(j).SetString(next)
				}
			case reflect.Struct:
				for j := 0; j < field.Len(); j++ {
					if err := transformConfigStringFieldsInStruct(field.Index(j).Addr().Interface(), transform); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func transformConfigStringFieldsInYAMLNode(node *yaml.Node, transform ConfigStringTransformer) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			next, err := transform(node.Value)
			if err != nil {
				return err
			}
			node.Value = next
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			if err := transformConfigStringFieldsInYAMLNode(child, transform); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			if err := transformConfigStringFieldsInYAMLNode(node.Content[i], transform); err != nil {
				return err
			}
		}
	}
	return nil
}
