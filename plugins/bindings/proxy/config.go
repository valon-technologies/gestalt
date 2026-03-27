package proxy

import (
	"fmt"
	"net/http"
	"strings"
)

type proxyConfig struct {
	Path string `yaml:"path"`
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
