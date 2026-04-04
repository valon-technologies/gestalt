package versioning

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func Resolve(ctx context.Context, provider, requested string, supported []string, detect func(context.Context) (string, string, error)) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested != "" && requested != "auto" && !slices.Contains(supported, requested) {
		return "", fmt.Errorf("%s: unsupported version %q (supported: %s)", provider, requested, strings.Join(supported, ", "))
	}

	resolved, raw, err := detect(ctx)
	if err != nil {
		return "", err
	}
	if !slices.Contains(supported, resolved) {
		return "", fmt.Errorf("%s: detected unsupported version %q from %q (supported: %s)", provider, resolved, raw, strings.Join(supported, ", "))
	}
	if requested != "" && requested != "auto" && requested != resolved {
		return "", fmt.Errorf("%s: configured version %q does not match detected version %q (%s)", provider, requested, resolved, raw)
	}

	return resolved, nil
}
