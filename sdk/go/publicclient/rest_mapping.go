package publicclient

import (
	"fmt"
	"net/url"
	"strings"
)

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
