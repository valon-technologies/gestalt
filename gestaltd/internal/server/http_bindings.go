package server

import (
	"fmt"
	"net/http"
	stdpath "path"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/httpbinding"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

var validMountedHTTPBindingMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

func mountedHTTPBindingsFromEntries(entries map[string]*config.ProviderEntry, providers *registry.ProviderMap[core.Provider], mountedUIs []MountedUI) ([]MountedHTTPBinding, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	mounted := make([]MountedHTTPBinding, 0)
	for _, pluginName := range names {
		entry := entries[pluginName]
		if entry == nil {
			continue
		}
		schemes := entry.EffectiveHTTPSecuritySchemes()
		bindings := entry.EffectiveHTTPBindings()
		if len(bindings) == 0 {
			continue
		}
		for schemeName, scheme := range schemes {
			if err := validateMountedHTTPSecurityScheme(pluginName, schemeName, scheme); err != nil {
				return nil, err
			}
		}

		operationIDs, err := providerOperationIDs(providers, pluginName)
		if err != nil {
			return nil, fmt.Errorf("resolve http bindings for %s: %w", pluginName, err)
		}

		bindingNames := make([]string, 0, len(bindings))
		for name := range bindings {
			bindingNames = append(bindingNames, name)
		}
		slices.Sort(bindingNames)

		for _, bindingName := range bindingNames {
			binding := bindings[bindingName]
			if binding == nil {
				return nil, fmt.Errorf("http binding %s.%s is required", pluginName, bindingName)
			}
			if err := validateMountedHTTPBinding(pluginName, bindingName, binding, schemes); err != nil {
				return nil, err
			}
			relativePath, err := normalizeHTTPBindingMountedPath(binding.Path)
			if err != nil {
				return nil, fmt.Errorf("http binding %s.%s: %w", pluginName, bindingName, err)
			}
			method := strings.ToUpper(strings.TrimSpace(binding.Method))
			target := strings.TrimSpace(binding.Target)
			if len(operationIDs) > 0 {
				if _, ok := operationIDs[target]; !ok {
					return nil, fmt.Errorf("http binding %s.%s target %q is not in provider catalog", pluginName, bindingName, target)
				}
				if relativePathConflictsWithGenericOperation(relativePath, operationIDs) {
					return nil, fmt.Errorf("http binding %s.%s path %q conflicts with the generic operation route", pluginName, bindingName, binding.Path)
				}
			}
			securityName := strings.TrimSpace(binding.Security)
			scheme := schemes[securityName]
			if scheme == nil {
				return nil, fmt.Errorf("http binding %s.%s references undefined security scheme %q", pluginName, bindingName, securityName)
			}
			mounted = append(mounted, MountedHTTPBinding{
				Name:         bindingName,
				PluginName:   pluginName,
				Path:         mountedHTTPBindingPath(pluginName, relativePath),
				Method:       method,
				Target:       target,
				RequestBody:  binding.RequestBody,
				Ack:          binding.Ack,
				SecurityName: securityName,
				Security:     scheme,
			})
		}
	}
	if err := validateMountedHTTPBindingRoutes(mounted, mountedUIs); err != nil {
		return nil, err
	}
	return mounted, nil
}

func normalizeHTTPBindingMountedPath(pathValue string) (string, error) {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.Contains(pathValue, "*") {
		return "", fmt.Errorf("path must not contain wildcards")
	}
	cleaned := stdpathClean(pathValue)
	if cleaned == "" {
		return "", fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned, nil
}

func mountedHTTPBindingPath(pluginName, relativePath string) string {
	base := "/api/v1/" + strings.TrimSpace(pluginName)
	if relativePath == "" || relativePath == "/" {
		return base
	}
	return base + relativePath
}

func relativePathConflictsWithGenericOperation(relativePath string, operationIDs map[string]struct{}) bool {
	trimmed := strings.Trim(strings.TrimSpace(relativePath), "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return false
	}
	_, ok := operationIDs[trimmed]
	return ok
}

func providerOperationIDs(providers *registry.ProviderMap[core.Provider], pluginName string) (map[string]struct{}, error) {
	if providers == nil {
		return nil, nil
	}
	provider, err := providers.Get(pluginName)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, nil
	}
	cat := provider.Catalog()
	if cat == nil {
		return nil, nil
	}
	ids := make(map[string]struct{}, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if op.Transport == catalog.TransportMCPPassthrough {
			continue
		}
		ids[strings.TrimSpace(op.ID)] = struct{}{}
	}
	return ids, nil
}

func validateMountedHTTPSecurityScheme(pluginName, schemeName string, scheme *config.HTTPSecurityScheme) error {
	return httpbinding.ValidateHTTPSecurityScheme(fmt.Sprintf("http security scheme %s.%s", pluginName, schemeName), scheme)
}

func validateMountedHTTPBinding(pluginName, bindingName string, binding *config.HTTPBinding, schemes map[string]*config.HTTPSecurityScheme) error {
	path := fmt.Sprintf("http binding %s.%s", pluginName, bindingName)
	binding.Path = strings.TrimSpace(binding.Path)
	if binding.Path == "" {
		return fmt.Errorf("%s.path is required", path)
	}
	method := strings.ToUpper(strings.TrimSpace(binding.Method))
	if !validMountedHTTPBindingMethods[method] {
		return fmt.Errorf("%s.method %q is not a valid HTTP method", path, binding.Method)
	}
	binding.Method = method
	binding.Target = strings.TrimSpace(binding.Target)
	if binding.Target == "" {
		return fmt.Errorf("%s.target is required", path)
	}
	binding.Security = strings.TrimSpace(binding.Security)
	if binding.Security == "" {
		return fmt.Errorf("%s.security is required", path)
	}
	if schemes == nil || schemes[binding.Security] == nil {
		return fmt.Errorf("%s.security %q references an undefined security scheme", path, binding.Security)
	}
	if binding.RequestBody != nil {
		normalizedContent := make(map[string]*providermanifestv1.HTTPMediaType, len(binding.RequestBody.Content))
		for mediaType := range binding.RequestBody.Content {
			normalizedMediaType, err := requestMediaType(mediaType)
			if err != nil || normalizedMediaType == "" {
				return fmt.Errorf("%s.requestBody.content %q must be a valid media type", path, mediaType)
			}
			if _, exists := normalizedContent[normalizedMediaType]; exists {
				return fmt.Errorf("%s.requestBody.content %q is duplicated after normalization", path, normalizedMediaType)
			}
			normalizedContent[normalizedMediaType] = binding.RequestBody.Content[mediaType]
		}
		binding.RequestBody.Content = normalizedContent
	}
	if binding.Ack != nil {
		if binding.Ack.Status == 0 {
			binding.Ack.Status = http.StatusOK
		}
		if binding.Ack.Status < 200 || binding.Ack.Status > 299 {
			return fmt.Errorf("%s.ack.status must be a 2xx status", path)
		}
	}
	return nil
}

func validateMountedHTTPBindingRoutes(bindings []MountedHTTPBinding, mountedUIs []MountedUI) error {
	seen := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		if binding.Path == "" {
			return fmt.Errorf("http binding %s.%s path is required", binding.PluginName, binding.Name)
		}
		for _, prefix := range []string{"/api/v1/auth", "/api/v1/tokens", "/api/v1/identities", "/api/v1/workflow", "/api/v1/integrations"} {
			if binding.Path == prefix || strings.HasPrefix(binding.Path, prefix+"/") {
				return fmt.Errorf("http binding %s.%s path %q conflicts with core route namespace %q", binding.PluginName, binding.Name, binding.Path, prefix)
			}
		}
		for _, mounted := range mountedUIs {
			if mounted.Path == "" || mounted.Path == "/" {
				continue
			}
			if binding.Path == mounted.Path || strings.HasPrefix(binding.Path, mounted.Path+"/") {
				return fmt.Errorf("http binding %s.%s path %q conflicts with mounted UI %q", binding.PluginName, binding.Name, binding.Path, mounted.Path)
			}
		}
		key := binding.Method + " " + binding.Path
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("http binding %s.%s duplicates http route %s", binding.PluginName, binding.Name, previous)
		}
		seen[key] = binding.PluginName + "." + binding.Name
	}
	return nil
}

func (s *Server) mountHTTPBindingRoutes(r chi.Router) {
	for _, binding := range s.mountedHTTPBindings {
		binding := binding
		r.MethodFunc(binding.Method, binding.Path, func(w http.ResponseWriter, r *http.Request) {
			s.handleHTTPBinding(binding, w, r)
		})
	}
}

func stdpathClean(pathValue string) string {
	cleaned := strings.ReplaceAll(strings.TrimSpace(pathValue), "\\", "/")
	if cleaned == "" {
		return ""
	}
	cleaned = stdpath.Clean(cleaned)
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}
