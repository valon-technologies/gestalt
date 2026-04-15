package bootstrap

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"gopkg.in/yaml.v3"
)

// resolveSecretRefs walks the config struct and replaces any string value
// containing internal structured secret refs with resolved secret values. The
// providers.secrets.<name>.config subtree is skipped to avoid self-referential
// resolution, but the remaining secrets-provider metadata still resolves so
// managed source auth can use secret-backed credentials.
func resolveSecretRefs(cfg *config.Config, resolve func(config.SecretRef) (string, error)) error {
	resolveValue := func(val string) (string, error) {
		ref, ok, err := config.ParseSecretRefTransport(val)
		if err != nil {
			return "", err
		}
		if !ok {
			if config.IsLegacySecretRefString(val) {
				return "", fmt.Errorf("legacy secret:// syntax should have been rejected during config load")
			}
			return val, nil
		}
		resolved, err := resolve(ref)
		if err != nil {
			var secretErr *core.SecretResolutionError
			if errors.As(err, &secretErr) {
				return "", err
			}
			return "", &core.SecretResolutionError{
				Name: ref.Name,
				Err:  err,
			}
		}
		if resolved == "" {
			return "", &core.SecretResolutionError{Name: ref.Name, Err: fmt.Errorf("resolved to empty value")}
		}
		return resolved, nil
	}

	if err := resolveStringFields(&cfg.Server, resolveValue); err != nil {
		return err
	}
	if err := resolveStringFields(&cfg.Authorization, resolveValue); err != nil {
		return err
	}
	for name, entry := range cfg.Plugins {
		if entry == nil {
			continue
		}
		if err := resolveStringFields(entry, resolveValue); err != nil {
			return err
		}
		for _, conn := range entry.Connections {
			if conn != nil {
				if err := resolveStringFields(conn, resolveValue); err != nil {
					return err
				}
			}
		}
		cfg.Plugins[name] = entry
	}
	for _, entries := range []map[string]*config.ProviderEntry{
		cfg.Providers.Auth,
		cfg.Providers.Telemetry,
		cfg.Providers.Audit,
		cfg.Providers.Cache,
		cfg.Providers.S3,
	} {
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			if err := resolveStringFields(entry, resolveValue); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		if err := resolveStringFields(entry, resolveValue); err != nil {
			return err
		}
		cfg.Providers.UI[name] = entry
	}
	// Resolve secrets provider struct fields (Source, Env, AllowedHosts, etc.)
	// but skip their Config nodes to avoid self-referential resolution.
	for name, entry := range cfg.Providers.Secrets {
		if entry == nil {
			continue
		}
		savedConfig := entry.Config
		entry.Config = yaml.Node{}
		if err := resolveStringFields(entry, resolveValue); err != nil {
			entry.Config = savedConfig
			return err
		}
		entry.Config = savedConfig
		cfg.Providers.Secrets[name] = entry
	}

	for name, ds := range cfg.Providers.IndexedDB {
		if ds == nil {
			continue
		}
		if err := resolveStringFields(ds, resolveValue); err != nil {
			return err
		}
		cfg.Providers.IndexedDB[name] = ds
	}
	if err := config.NormalizeCompatibility(cfg); err != nil {
		return err
	}

	return nil
}

func resolveStringFields(ptr any, resolve func(string) (string, error)) error {
	v := reflect.ValueOf(ptr).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		switch field.Kind() {
		case reflect.String:
			if !field.CanSet() {
				continue
			}
			resolved, err := resolve(field.String())
			if err != nil {
				return err
			}
			field.SetString(resolved)
		case reflect.Struct:
			if field.CanSet() {
				if field.Type() == config.YAMLNodeType {
					nodePtr := field.Addr().Interface().(*yaml.Node)
					if err := resolveYAMLNode(nodePtr, resolve); err != nil {
						return err
					}
				} else {
					if err := resolveStringFields(field.Addr().Interface(), resolve); err != nil {
						return err
					}
				}
			}
		case reflect.Pointer:
			if !field.IsNil() && field.Elem().Kind() == reflect.Struct {
				if err := resolveStringFields(field.Interface(), resolve); err != nil {
					return err
				}
			}
		case reflect.Map:
			if field.Type().Key().Kind() != reflect.String {
				continue
			}
			switch field.Type().Elem().Kind() {
			case reflect.String:
				for _, k := range field.MapKeys() {
					resolved, err := resolve(field.MapIndex(k).String())
					if err != nil {
						return err
					}
					field.SetMapIndex(k, reflect.ValueOf(resolved))
				}
			case reflect.Struct:
				for _, k := range field.MapKeys() {
					current := field.MapIndex(k)
					next := reflect.New(field.Type().Elem())
					next.Elem().Set(current)
					if err := resolveStringFields(next.Interface(), resolve); err != nil {
						return err
					}
					field.SetMapIndex(k, next.Elem())
				}
			case reflect.Pointer:
				if field.Type().Elem().Elem().Kind() != reflect.Struct {
					continue
				}
				for _, k := range field.MapKeys() {
					current := field.MapIndex(k)
					if current.IsNil() {
						continue
					}
					if err := resolveStringFields(current.Interface(), resolve); err != nil {
						return err
					}
				}
			}
		case reflect.Slice:
			switch field.Type().Elem().Kind() {
			case reflect.String:
				for j := 0; j < field.Len(); j++ {
					elem := field.Index(j)
					resolved, err := resolve(elem.String())
					if err != nil {
						return err
					}
					elem.SetString(resolved)
				}
			case reflect.Struct:
				for j := 0; j < field.Len(); j++ {
					if err := resolveStringFields(field.Index(j).Addr().Interface(), resolve); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func resolveYAMLNode(node *yaml.Node, resolve func(string) (string, error)) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			resolved, err := resolve(node.Value)
			if err != nil {
				return err
			}
			node.Value = resolved
		}
	case yaml.MappingNode:
		// Content is [key, value, key, value, ...]; only resolve values.
		for i := 1; i < len(node.Content); i += 2 {
			if err := resolveYAMLNode(node.Content[i], resolve); err != nil {
				return err
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			if err := resolveYAMLNode(child, resolve); err != nil {
				return err
			}
		}
	}
	return nil
}
