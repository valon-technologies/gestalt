package proxy

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/internal/egress"
)

type proxyConfig struct {
	Path string `yaml:"path"`
}

type normalizedRequest struct {
	Note   string             `json:"note"`
	Policy egress.PolicyInput `json:"policy_input"`
	Target egress.Target      `json:"target"`
	Body   string             `json:"body,omitempty"`
}

func (c proxyConfig) validate(name string) error {
	if c.Path == "" || !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("proxy %q: path must be non-empty and start with /", name)
	}
	return nil
}

func (c proxyConfig) methods() []string {
	return []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodConnect,
	}
}

func (c proxyConfig) normalizedPath() string {
	path := strings.TrimSuffix(c.Path, "/")
	if path == "" {
		return "/"
	}
	return path
}

func (c proxyConfig) routePatterns() []string {
	path := c.normalizedPath()
	if path == "/" {
		return []string{"/", "/*"}
	}
	return []string{path, path + "/*"}
}
