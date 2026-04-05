package provider

import (
	"fmt"
	"maps"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

// DefinitionOverlay captures runtime metadata and config that should be applied
// after loading a provider definition from a spec surface.
type DefinitionOverlay struct {
	DisplayName       string
	Description       string
	IconSVG           string
	BaseURL           string
	Headers           map[string]string
	ManagedParameters []pluginmanifestv1.ManagedParameter
	ResponseMapping   *pluginmanifestv1.ManifestResponseMapping
}

func NewDefinitionOverlay(manifestProvider *pluginmanifestv1.Provider, plugin *config.PluginDef, displayName, description, iconSVG string) DefinitionOverlay {
	return DefinitionOverlay{
		DisplayName:       displayName,
		Description:       description,
		IconSVG:           iconSVG,
		BaseURL:           config.MergedProviderBaseURL(manifestProvider, plugin),
		Headers:           config.MergedProviderHeaders(manifestProvider, plugin),
		ManagedParameters: config.MergedProviderManagedParameters(manifestProvider, plugin),
		ResponseMapping:   config.MergedProviderResponseMapping(manifestProvider, plugin),
	}
}

func ApplyDefinitionOverlay(def *Definition, overlay DefinitionOverlay) error {
	if def == nil {
		return nil
	}
	if overlay.DisplayName != "" {
		def.DisplayName = overlay.DisplayName
	}
	if overlay.Description != "" {
		def.Description = overlay.Description
	}
	if overlay.IconSVG != "" {
		def.IconSVG = overlay.IconSVG
	}
	if overlay.BaseURL != "" {
		def.BaseURL = overlay.BaseURL
	}
	if len(overlay.Headers) > 0 {
		def.Headers = maps.Clone(overlay.Headers)
	}
	if err := applyManagedParameters(def, overlay.ManagedParameters); err != nil {
		return err
	}
	if overlay.ResponseMapping != nil {
		def.ResponseMapping = responseMappingFromManifest(overlay.ResponseMapping)
	}
	return nil
}

func responseMappingFromManifest(mapping *pluginmanifestv1.ManifestResponseMapping) *ResponseMappingDef {
	if mapping == nil {
		return nil
	}
	out := &ResponseMappingDef{
		DataPath: mapping.DataPath,
	}
	if mapping.Pagination != nil {
		out.Pagination = &PaginationMappingDef{
			HasMorePath: mapping.Pagination.HasMorePath,
			CursorPath:  mapping.Pagination.CursorPath,
		}
	}
	return out
}

func applyManagedParameters(def *Definition, params []pluginmanifestv1.ManagedParameter) error {
	if def == nil || len(params) == 0 {
		return nil
	}

	if def.Headers == nil {
		def.Headers = make(map[string]string, len(params))
	}
	for _, param := range params {
		switch param.In {
		case config.ManagedParameterInHeader:
			if _, exists := def.Headers[param.Name]; exists {
				return fmt.Errorf("managed parameter %q conflicts with configured header", param.Name)
			}
			def.Headers[param.Name] = param.Value
		case config.ManagedParameterInPath:
		default:
			return fmt.Errorf("unsupported managed parameter location %q", param.In)
		}
	}

	for opName := range def.Operations {
		op := def.Operations[opName]
		for _, param := range params {
			if param.In != config.ManagedParameterInPath {
				continue
			}
			op.Path = strings.ReplaceAll(op.Path, "{"+param.Name+"}", param.Value)
		}
		filtered := op.Parameters[:0]
		for _, param := range op.Parameters {
			if isManagedOperationParameter(param, params) {
				continue
			}
			filtered = append(filtered, param)
		}
		op.Parameters = filtered
		def.Operations[opName] = op
	}

	return nil
}

func isManagedOperationParameter(param ParameterDef, managed []pluginmanifestv1.ManagedParameter) bool {
	location := strings.ToLower(param.Location)
	if location == "" {
		return false
	}

	wireName := param.WireName
	if wireName == "" {
		wireName = param.Name
	}
	target := config.NormalizeManagedParameter(pluginmanifestv1.ManagedParameter{In: location, Name: wireName})

	for _, managedParam := range managed {
		if managedParam.In == target.In && managedParam.Name == target.Name {
			return true
		}
	}
	return false
}
