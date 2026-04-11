package bootstrap

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"gopkg.in/yaml.v3"
)

const secretPrefix = "secret://"

var yamlNodeType = reflect.TypeOf(yaml.Node{})

// resolveSecretRefs walks the config struct and replaces any string value
// starting with "secret://" with the resolved secret value. The
// SecretsConfig.Config node is skipped to avoid self-referential resolution,
// but secrets.provider metadata is still resolved after the secret manager has
// been prepared so managed source auth can use secret-backed credentials.
func resolveSecretRefs(ctx context.Context, cfg *config.Config, sm core.SecretManager) error {
	resolve := func(val string) (string, error) {
		name, ok := strings.CutPrefix(val, secretPrefix)
		if !ok {
			return val, nil
		}
		resolved, err := sm.GetSecret(ctx, name)
		if err != nil {
			return "", &core.SecretResolutionError{Name: name, Err: err}
		}
		if resolved == "" {
			return "", &core.SecretResolutionError{
				Name: name,
				Err:  fmt.Errorf("resolved to empty value"),
			}
		}
		return resolved, nil
	}

	if err := resolveStringFields(&cfg.Server, resolve); err != nil {
		return err
	}
	for name := range cfg.Plugins {
		intg := cfg.Plugins[name]
		if err := resolveStringFields(&intg, resolve); err != nil {
			return err
		}
		if intg.Plugin != nil {
			if err := resolveStringFields(intg.Plugin, resolve); err != nil {
				return err
			}
			for _, conn := range intg.Plugin.Connections {
				if conn != nil {
					if err := resolveStringFields(conn, resolve); err != nil {
						return err
					}
				}
			}
		}
		cfg.Plugins[name] = intg
	}
	// resolveStringFields handles both Provider (*ProviderDef, via pointer
	// recursion) and Config (yaml.Node, via type detection) automatically.
	// Secrets is handled separately to skip its Config node and avoid
	// self-referential resolution.
	for _, comp := range []*config.ComponentConfig{&cfg.Auth, &cfg.UI, &cfg.Telemetry, &cfg.Audit} {
		if err := resolveStringFields(comp, resolve); err != nil {
			return err
		}
	}
	if cfg.Secrets.Provider != nil {
		if err := resolveStringFields(cfg.Secrets.Provider, resolve); err != nil {
			return err
		}
	}

	for name := range cfg.IndexedDBs {
		ds := cfg.IndexedDBs[name]
		if err := resolveStringFields(&ds, resolve); err != nil {
			return err
		}
		cfg.IndexedDBs[name] = ds
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
				if field.Type() == yamlNodeType {
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
			if field.Type().Key().Kind() == reflect.String && field.Type().Elem().Kind() == reflect.String {
				for _, k := range field.MapKeys() {
					resolved, err := resolve(field.MapIndex(k).String())
					if err != nil {
						return err
					}
					field.SetMapIndex(k, reflect.ValueOf(resolved))
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
