package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func mountedWebhooksFromEntries(entries map[string]*config.ProviderEntry) ([]MountedWebhook, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	mounted := make([]MountedWebhook, 0)
	for _, pluginName := range names {
		entry := entries[pluginName]
		if entry == nil {
			continue
		}
		schemes := entry.EffectiveWebhookSecuritySchemes()
		webhooks := entry.EffectiveWebhooks()
		if len(webhooks) == 0 {
			continue
		}

		webhookNames := make([]string, 0, len(webhooks))
		for name := range webhooks {
			webhookNames = append(webhookNames, name)
		}
		slices.Sort(webhookNames)

		for _, webhookName := range webhookNames {
			def := webhooks[webhookName]
			method, op, err := mountedWebhookOperation(def)
			if err != nil {
				return nil, fmt.Errorf("resolve webhook %s.%s: %w", pluginName, webhookName, err)
			}
			mounted = append(mounted, MountedWebhook{
				Name:            webhookName,
				PluginName:      pluginName,
				Path:            strings.TrimSpace(def.Path),
				Method:          method,
				Operation:       op,
				Target:          def.Target,
				Execution:       def.Execution,
				SecuritySchemes: schemes,
			})
		}
	}
	return mounted, nil
}

func mountedWebhookOperation(def *providermanifestv1.WebhookDef) (string, *providermanifestv1.WebhookOperation, error) {
	if def == nil {
		return "", nil, fmt.Errorf("webhook definition is required")
	}
	var (
		method string
		op     *providermanifestv1.WebhookOperation
		count  int
	)
	for _, candidate := range []struct {
		method string
		op     *providermanifestv1.WebhookOperation
	}{
		{method: http.MethodGet, op: def.Get},
		{method: http.MethodPost, op: def.Post},
		{method: http.MethodPut, op: def.Put},
		{method: http.MethodDelete, op: def.Delete},
	} {
		if candidate.op == nil {
			continue
		}
		method = candidate.method
		op = candidate.op
		count++
	}
	if count != 1 {
		return "", nil, fmt.Errorf("exactly one HTTP method must be configured")
	}
	return method, op, nil
}

func validateMountedWebhookRoutes(webhooks []MountedWebhook, mountedWebUIs []MountedWebUI) error {
	seen := make(map[string]string, len(webhooks))
	for _, webhook := range webhooks {
		path := strings.TrimSpace(webhook.Path)
		if path == "" {
			return fmt.Errorf("webhook %s.%s path is required", webhook.PluginName, webhook.Name)
		}
		if path == "/" {
			return fmt.Errorf("webhook %s.%s path \"/\" is not supported", webhook.PluginName, webhook.Name)
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("webhook %s.%s path must start with \"/\"", webhook.PluginName, webhook.Name)
		}
		if strings.Contains(path, "*") {
			return fmt.Errorf("webhook %s.%s path must not contain wildcards", webhook.PluginName, webhook.Name)
		}
		for _, prefix := range []string{"/api", "/mcp", "/metrics", "/health", "/ready"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return fmt.Errorf("webhook %s.%s path %q conflicts with core route namespace %q", webhook.PluginName, webhook.Name, path, prefix)
			}
		}
		if path == "/admin" || strings.HasPrefix(path, "/admin/") {
			return fmt.Errorf("webhook %s.%s path %q conflicts with admin route namespace", webhook.PluginName, webhook.Name, path)
		}
		for _, mounted := range mountedWebUIs {
			if mounted.Path == "" || mounted.Path == "/" {
				continue
			}
			if path == mounted.Path || strings.HasPrefix(path, mounted.Path+"/") {
				return fmt.Errorf("webhook %s.%s path %q conflicts with mounted UI %q", webhook.PluginName, webhook.Name, path, mounted.Path)
			}
		}
		key := webhook.Method + " " + path
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("webhook %s.%s duplicates webhook route %s", webhook.PluginName, webhook.Name, previous)
		}
		seen[key] = webhook.PluginName + "." + webhook.Name
	}
	return nil
}

func (s *Server) mountWebhookRoutes(r chi.Router) {
	for _, mounted := range s.mountedWebhooks {
		mounted := mounted
		r.MethodFunc(mounted.Method, mounted.Path, func(w http.ResponseWriter, r *http.Request) {
			s.handleWebhook(mounted, w, r)
		})
	}
}
