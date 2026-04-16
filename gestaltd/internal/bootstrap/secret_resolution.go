package bootstrap

import (
	"errors"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
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

	if err := config.TransformConfigStringFields(cfg, resolveValue); err != nil {
		return err
	}
	if err := config.CanonicalizeStructure(cfg); err != nil {
		return err
	}

	return nil
}
